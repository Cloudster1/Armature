//go:build integration

package test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/perm"
)

// perms builds the permission store over the harness's cluster.
func (h *harness) perms() *perm.Store { return perm.NewStore(h.cluster) }

// resolve reads what somebody may do, the way a request does.
func (h *harness) resolve(t *testing.T, ws *workspace, userID uuid.UUID, owner bool) perm.Set {
	t.Helper()
	set, err := h.perms().ResolveFor(ws.ctx, ws.orgID, userID, owner)
	if err != nil {
		t.Fatalf("resolve permissions: %v", err)
	}
	return set
}

// joinAs invites somebody into the workspace's organization at a standing and
// returns their user id.
func (h *harness) joinAs(t *testing.T, ws *workspace, name, role string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	email := h.email(t, name)
	if err := h.super.QueryRow(context.Background(), `
		INSERT INTO app_user (email, name) VALUES ($1, $2) RETURNING id`,
		email, name).Scan(&id); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	if _, err := h.super.Exec(context.Background(), `
		INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, $3)`,
		ws.orgID, id, role); err != nil {
		t.Fatalf("add %s to the organization: %v", name, err)
	}
	return id
}

func TestARoleGrantedOverOneProjectStaysThere(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "scoped")
	other := h.newWorkspace(t, "other")
	perms := h.perms()

	member := h.joinAs(t, ws, "scrummy", "member")
	if _, _, err := perms.Grant(ws.ctx, perm.GrantInput{
		Role: perm.ScrumMaster, ProjectKey: ws.project.Key, UserID: &member,
	}, ws.actor.UserID); err != nil {
		t.Fatalf("grant the role: %v", err)
	}

	set := h.resolve(t, ws, member, false)
	if !set.Can(perm.SprintManage, ws.project.Key) {
		t.Error("the role does not apply where it was granted")
	}
	if set.Can(perm.SprintManage, other.project.Key) {
		t.Error("a role granted over one project answered for another")
	}
	if set.CanInOrg(perm.OrgAdminister) {
		t.Error("a scrum master can administer the organization")
	}
}

// The whole point of groups: a role granted to one reaches everybody in it, and
// stops reaching somebody the moment they leave.
func TestARoleGrantedToAGroupReachesItsMembers(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "grouped")
	perms := h.perms()

	member := h.joinAs(t, ws, "grouped", "member")
	group, _, err := perms.CreateGroup(ws.ctx, perm.GroupInput{Name: "Release managers"}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create the group: %v", err)
	}
	if _, _, err := perms.Grant(ws.ctx, perm.GrantInput{
		Role: perm.ProjectAdministrator, ProjectKey: ws.project.Key, GroupID: &group.ID,
	}, ws.actor.UserID); err != nil {
		t.Fatalf("grant to the group: %v", err)
	}

	if h.resolve(t, ws, member, false).Can(perm.ProjectAdminister, ws.project.Key) {
		t.Fatal("somebody who is not in the group holds what it was granted")
	}

	if _, _, err := perms.AddToGroup(ws.ctx, group.ID, member, ws.actor.UserID); err != nil {
		t.Fatalf("add to the group: %v", err)
	}
	if !h.resolve(t, ws, member, false).Can(perm.ProjectAdminister, ws.project.Key) {
		t.Error("joining the group did not bring the role with it")
	}

	if _, _, err := perms.RemoveFromGroup(ws.ctx, group.ID, member, ws.actor.UserID); err != nil {
		t.Fatalf("remove from the group: %v", err)
	}
	if h.resolve(t, ws, member, false).Can(perm.ProjectAdminister, ws.project.Key) {
		t.Error("leaving the group left the role behind")
	}
}

func TestAccessIsNeverGrantedToAStranger(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "strangers")
	elsewhere := h.newWorkspace(t, "elsewhere")
	perms := h.perms()

	t.Run("a role cannot be granted to somebody outside the organization", func(t *testing.T) {
		_, _, err := perms.Grant(ws.ctx, perm.GrantInput{
			Role: perm.User, UserID: &elsewhere.actor.UserID,
		}, ws.actor.UserID)
		if !errors.Is(err, perm.ErrNotAMember) {
			t.Errorf("error = %v, want it refused", err)
		}
	})

	t.Run("and SQL refuses it too", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(), `
			INSERT INTO role_assignment (org_id, role, user_id) VALUES ($1, 'user', $2)`,
			ws.orgID, elsewhere.actor.UserID)
		if err == nil {
			t.Fatal("SQL granted a role to somebody outside the organization")
		}
		if !strings.Contains(err.Error(), "member of the organization") {
			t.Errorf("error = %v, want the guard's own message", err)
		}
	})

	t.Run("nor can a role be granted over another organization's project", func(t *testing.T) {
		var projectID uuid.UUID
		if err := h.super.QueryRow(context.Background(),
			`SELECT id FROM project WHERE key = $1`, elsewhere.project.Key).Scan(&projectID); err != nil {
			t.Fatal(err)
		}
		_, err := h.super.Exec(context.Background(), `
			INSERT INTO role_assignment (org_id, role, project_id, user_id)
			VALUES ($1, 'reader', $2, $3)`, ws.orgID, projectID, ws.actor.UserID)
		if err == nil {
			t.Fatal("SQL granted a role over another organization's project")
		}
		if !strings.Contains(err.Error(), "another organization") {
			t.Errorf("error = %v, want the guard's own message", err)
		}
	})
}

// Global administration is administration of the tenant. Scoping it to one
// project would read as a smaller thing than it is.
func TestGlobalAdministrationCannotBeScopedToAProject(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "globalscope")

	_, _, err := h.perms().Grant(ws.ctx, perm.GrantInput{
		Role: perm.GlobalAdministrator, ProjectKey: ws.project.Key, UserID: &ws.actor.UserID,
	}, ws.actor.UserID)
	if !errors.Is(err, perm.ErrScope) {
		t.Errorf("error = %v, want it refused", err)
	}
}

// An organization nobody can administer is one where the fix is a database
// console.
func TestTheLastGlobalAdministratorCannotBeRevoked(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "lastadmin")
	perms := h.perms()

	held, err := perms.Assignments(ws.ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	var administrators []uuid.UUID
	for _, a := range held {
		if a.Role == perm.GlobalAdministrator {
			administrators = append(administrators, a.ID)
		}
	}
	if len(administrators) != 1 {
		t.Fatalf("administrators = %d, want the owner's own", len(administrators))
	}

	if _, err := perms.Revoke(ws.ctx, administrators[0], ws.actor.UserID); err == nil {
		t.Fatal("the last global administrator was revoked")
	} else if !strings.Contains(err.Error(), "last global administrator") {
		t.Errorf("error = %q, want it to say why", err)
	}

	// With a second one in place it is allowed, which is what makes handing the
	// role over possible.
	another := h.joinAs(t, ws, "successor", "member")
	if _, _, err := perms.Grant(ws.ctx, perm.GrantInput{
		Role: perm.GlobalAdministrator, UserID: &another,
	}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := perms.Revoke(ws.ctx, administrators[0], ws.actor.UserID); err != nil {
		t.Errorf("revoking one of two administrators: %v", err)
	}
}

// An owner who edits their own roles must not be able to lock themselves out of
// the organization they own.
func TestTheOwnerAlwaysAdministersTheirOwnOrganization(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "ownerlock")

	if _, err := h.super.Exec(context.Background(),
		`DELETE FROM role_assignment WHERE org_id = $1`, ws.orgID); err != nil {
		t.Fatal(err)
	}

	set := h.resolve(t, ws, ws.actor.UserID, true)
	if !set.CanInOrg(perm.OrgAdminister) {
		t.Error("an owner with no assignments cannot administer their own organization")
	}
	// And somebody who is not the owner holds nothing at all.
	if h.resolve(t, ws, ws.actor.UserID, false).CanInOrg(perm.OrgAdminister) {
		t.Error("the standing role was granted to somebody who is not the owner")
	}
}

func TestGrantingTheSameRoleTwiceIsTheSameGrant(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "twice")
	perms := h.perms()

	member := h.joinAs(t, ws, "twice", "member")
	first, _, err := perms.Grant(ws.ctx, perm.GrantInput{
		Role: perm.Reader, ProjectKey: ws.project.Key, UserID: &member,
	}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	again, _, err := perms.Grant(ws.ctx, perm.GrantInput{
		Role: perm.Reader, ProjectKey: ws.project.Key, UserID: &member,
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("granting the same role twice: %v", err)
	}
	if first.ID != again.ID {
		t.Error("granting the same role twice made two grants")
	}
}

func TestJoiningAnOrganizationGrantsARole(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	owner.signup(t, h, "joining")

	invite := owner.post("/api/v1/invites", map[string]string{
		"email": h.email(t, "newmember"), "role": "member",
	})
	member := api.client(t)
	member.post("/api/v1/auth/invites/accept", map[string]string{
		"token": invite.Body["token"].(string), "name": "New Member", "password": testPassword,
	})

	access := member.get("/api/v1/access/me")
	if access.Status != 200 {
		t.Fatalf("access returned %d: %s", access.Status, access.Raw)
	}
	grants, _ := access.Body["grants"].([]any)
	if len(grants) != 1 {
		t.Fatalf("grants = %v, want the one that comes with joining", grants)
	}
	if got := grants[0].(map[string]any)["role"]; got != "user" {
		t.Errorf("role = %v, want user", got)
	}
}
