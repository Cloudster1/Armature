//go:build integration

package test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/perm"
)

// perms builds the permission store over the harness's cluster.
func (h *harness) perms() *perm.Store { return perm.NewStore(h.cluster) }

// resolve reads what somebody may do, the way a request does.
func (h *harness) resolve(t *testing.T, ws *workspace, userID uuid.UUID, owner bool) perm.Set {
	t.Helper()
	set, err := h.perms().ResolveFor(ws.ctx, userID, owner)
	if err != nil {
		t.Fatalf("resolve permissions: %v", err)
	}
	return set
}

// joinAs puts somebody in the installation at a standing, with no role yet,
// and returns their user id.
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
		UPDATE app_user SET standing = $2 WHERE id = $1`, id, role); err != nil {
		t.Fatalf("add %s to the organization: %v", name, err)
	}
	return id
}

func TestARoleGrantedOverOneProjectStaysThere(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "scoped")
	other := h.newProject(t, "other")
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
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "grouped")
	perms := h.perms()

	member := h.joinAs(t, ws, "grouped", "member")
	group, _, err := perms.CreateGroup(ws.ctx, perm.GroupInput{Name: h.name(t, "Release managers")}, ws.actor.UserID)
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

// everyone is the built-in group that holds every member.
func (h *harness) everyone(t *testing.T, ws *workspace) perm.Group {
	t.Helper()
	groups, err := h.perms().Groups(ws.ctx)
	if err != nil {
		t.Fatalf("list the groups: %v", err)
	}
	if len(groups) == 0 || groups[0].Builtin != perm.Everyone {
		t.Fatalf("the groups do not start with Everyone: %+v", groups)
	}
	return groups[0]
}

// A grant to Everyone reaches each member, the moment they join and no longer
// once they leave, and never a portal customer.
func TestAGrantToEveryoneReachesEveryMemberAndNoCustomer(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "everyone")
	perms := h.perms()
	everyone := h.everyone(t, ws)

	if _, _, err := perms.Grant(ws.ctx, perm.GrantInput{
		Role: perm.Reader, ProjectKey: ws.project.Key, GroupID: &everyone.ID,
	}, ws.actor.UserID); err != nil {
		t.Fatalf("grant to Everyone: %v", err)
	}

	member := h.joinAs(t, ws, "everyone member", "member")
	customer := h.joinAs(t, ws, "everyone customer", "customer")
	if !h.resolve(t, ws, member, false).Can(perm.Read, ws.project.Key) {
		t.Error("a member who just joined does not hold what Everyone was granted")
	}
	if h.resolve(t, ws, customer, false).Can(perm.Read, ws.project.Key) {
		t.Error("a portal customer holds what Everyone was granted")
	}

	if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET standing = 'customer' WHERE id = $1`, member); err != nil {
		t.Fatalf("make the member a customer: %v", err)
	}
	if h.resolve(t, ws, member, false).Can(perm.Read, ws.project.Key) {
		t.Error("somebody who is no longer a member keeps what Everyone was granted")
	}
}

// Everyone's members follow the installation's, so the service refuses every
// edit by hand, and administration given to all of them at once.
func TestEveryoneIsNotEditedByHandNorMadeAnAdministrator(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "everyone fixed")
	perms := h.perms()
	everyone := h.everyone(t, ws)
	member := h.joinAs(t, ws, "everyone fixed", "member")

	if _, _, err := perms.RemoveFromGroup(ws.ctx, everyone.ID, member, ws.actor.UserID); !errors.Is(err, perm.ErrBuiltinGroup) {
		t.Errorf("taking a member out of Everyone answered %v, want ErrBuiltinGroup", err)
	}
	if _, _, err := perms.AddToGroup(ws.ctx, everyone.ID, member, ws.actor.UserID); !errors.Is(err, perm.ErrBuiltinGroup) {
		t.Errorf("adding to Everyone answered %v, want ErrBuiltinGroup", err)
	}
	if _, err := perms.DeleteGroup(ws.ctx, everyone.ID, ws.actor.UserID); !errors.Is(err, perm.ErrBuiltinGroup) {
		t.Errorf("deleting Everyone answered %v, want ErrBuiltinGroup", err)
	}
	if _, _, err := perms.Grant(ws.ctx, perm.GrantInput{Role: perm.GlobalAdministrator, GroupID: &everyone.ID}, ws.actor.UserID); !errors.Is(err, perm.ErrEveryoneAdministers) {
		t.Errorf("making Everyone global administrators answered %v, want ErrEveryoneAdministers", err)
	}
	if _, err := h.super.Exec(context.Background(), `
		INSERT INTO role_assignment (role, group_id) VALUES ('global_administrator', $1)`, everyone.ID); err == nil || !strings.Contains(err.Error(), "cannot administer") {
		t.Errorf("the database let Everyone administer the installation: %v", err)
	}
	if _, err := h.super.Exec(context.Background(), `
		DELETE FROM group_member WHERE group_id = $1 AND user_id = $2`, everyone.ID, member); err == nil {
		t.Error("the database let a member be taken out of Everyone")
	}
}

// Somebody with an account but no membership is nobody here: no role reaches
// them, through the service or straight through SQL.
func TestAccessIsNeverGrantedToAStranger(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "strangers")
	perms := h.perms()
	var stranger uuid.UUID
	if err := h.super.QueryRow(context.Background(), `
		INSERT INTO app_user (email, name) VALUES ($1, 'Stranger') RETURNING id`, h.email(t, "stranger")).Scan(&stranger); err != nil {
		t.Fatal(err)
	}

	t.Run("a role cannot be granted to somebody who is not a member", func(t *testing.T) {
		_, _, err := perms.Grant(ws.ctx, perm.GrantInput{
			Role: perm.User, UserID: &stranger,
		}, ws.actor.UserID)
		if !errors.Is(err, perm.ErrNotAMember) {
			t.Errorf("error = %v, want it refused", err)
		}
	})

	t.Run("and SQL refuses it too", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(), `
			INSERT INTO role_assignment (role, user_id) VALUES ('user', $1)`, stranger)
		if err == nil {
			t.Fatal("SQL granted a role to somebody who is not a member")
		}
		if !strings.Contains(err.Error(), "member here") {
			t.Errorf("error = %v, want the guard's own message", err)
		}
	})
}

// Global administration is administration of the installation. Scoping it to one
// project would read as a smaller thing than it is.
func TestGlobalAdministrationCannotBeScopedToAProject(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "globalscope")

	_, _, err := h.perms().Grant(ws.ctx, perm.GrantInput{
		Role: perm.GlobalAdministrator, ProjectKey: ws.project.Key, UserID: &ws.actor.UserID,
	}, ws.actor.UserID)
	if !errors.Is(err, perm.ErrScope) {
		t.Errorf("error = %v, want it refused", err)
	}
}

// An installation nobody can administer is one where the fix is a database
// console. It runs in one of its own: the shared one always has several.
func TestTheLastGlobalAdministratorCannotBeRevoked(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	_, own := emptyInstallation(t, h, "armature_last_administrator_revoke_test")
	ctx := context.Background()
	var orgID, ownerID uuid.UUID
	if _, err := own.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `INSERT INTO installation (slug, name) VALUES ('lastadmin', 'Last Admin') RETURNING id`).Scan(&orgID); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ('owner@lastadmin.test', 'Owner') RETURNING id`).Scan(&ownerID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE app_user SET standing = 'owner' WHERE id = $1`, ownerID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO role_assignment (role, user_id) VALUES ('global_administrator', $1)`, ownerID)
		return err
	}); err != nil {
		t.Fatalf("set the installation up: %v", err)
	}
	ctx = db.PinPrimary(ctx)
	perms := perm.NewStore(own.cluster)

	held, err := perms.Assignments(ctx, "")
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

	if _, err := perms.Revoke(ctx, administrators[0], ownerID); err == nil {
		t.Fatal("the last global administrator was revoked")
	} else if !strings.Contains(err.Error(), "last global administrator") {
		t.Errorf("error = %q, want it to say why", err)
	}

	// With a second one in place it is allowed, which is what makes handing the
	// role over possible.
	var another uuid.UUID
	if _, err := own.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ('successor@lastadmin.test', 'Successor') RETURNING id`).Scan(&another); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE app_user SET standing = 'member' WHERE id = $1`, another)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := perms.Grant(ctx, perm.GrantInput{
		Role: perm.GlobalAdministrator, UserID: &another,
	}, ownerID); err != nil {
		t.Fatal(err)
	}
	if _, err := perms.Revoke(ctx, administrators[0], ownerID); err != nil {
		t.Errorf("revoking one of two administrators: %v", err)
	}
}

// An owner who edits their own roles must not be able to lock themselves out of
// the installation.
func TestTheOwnerAlwaysAdministersTheInstallation(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "ownerlock")

	if _, err := h.super.Exec(context.Background(),
		`DELETE FROM role_assignment WHERE user_id = $1`, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}

	set := h.resolve(t, ws, ws.actor.UserID, true)
	if !set.CanInOrg(perm.OrgAdminister) {
		t.Error("an owner with no assignments cannot administer the installation")
	}
	// And somebody who is not the owner holds nothing at all.
	if h.resolve(t, ws, ws.actor.UserID, false).CanInOrg(perm.OrgAdminister) {
		t.Error("the standing role was granted to somebody who is not the owner")
	}
}

func TestGrantingTheSameRoleTwiceIsTheSameGrant(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "twice")
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

func TestJoiningTheInstallationGrantsARole(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	owner.signIn(t, h.person(t, "joining", auth.RoleOwner))

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
