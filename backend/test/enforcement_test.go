//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/perm"
)

// asRole invites somebody into the owner's organization, grants them one role
// over the project, and returns a client signed in as them.
func (s *apiServer) asRole(t *testing.T, h *harness, owner *client, projectKey string, role perm.Role) *client {
	t.Helper()

	invite := owner.post("/api/v1/invites", map[string]string{
		"email": h.email(t, "roled"), "role": "member",
	})
	if invite.Status != http.StatusCreated {
		t.Fatalf("invite returned %d: %s", invite.Status, invite.Raw)
	}

	joined := s.client(t)
	accepted := joined.post("/api/v1/auth/invites/accept", map[string]string{
		"token": invite.Body["token"].(string), "name": string(role), "password": testPassword,
	})
	if accepted.Status != http.StatusOK {
		t.Fatalf("accept returned %d: %s", accepted.Status, accepted.Raw)
	}
	userID := principalField(t, accepted, "principal", "user", "id")

	// Joining grants an organization-wide user role. A test about one role has
	// to start from nothing, so that role is taken away first. The grant was
	// the joiner's write, so the owner's read of it waits for the replica.
	h.waitForPrimary(t)
	held := owner.get("/api/v1/role-assignments")
	for _, row := range held.Body["assignments"].([]any) {
		a := row.(map[string]any)
		if a["userId"] == userID {
			owner.delete("/api/v1/role-assignments/" + a["id"].(string))
		}
	}

	granted := owner.post("/api/v1/role-assignments", map[string]any{
		"role": string(role), "projectKey": projectKey, "userId": userID,
	})
	if granted.Status != http.StatusCreated {
		t.Fatalf("granting %s returned %d: %s", role, granted.Status, granted.Raw)
	}
	// The owner's writes pin the owner to the primary, not this client, whose
	// first requests read the replica. Until the grant has arrived there, the
	// role it was given on joining is what those requests would see.
	h.waitForPrimary(t)
	return joined
}

// TestWhatEachRoleMayDo walks the five roles across the same set of actions.
// One table, so that adding a permission means deciding what every role does
// with it rather than forgetting four of them.
func TestWhatEachRoleMayDo(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	owner.signup(t, h, "roles")
	created := owner.post("/api/v1/projects", map[string]string{"name": "Guarded", "key": "GUARD"})
	if created.Status != http.StatusCreated {
		t.Fatalf("create project returned %d: %s", created.Status, created.Raw)
	}
	issue := owner.post("/api/v1/projects/GUARD/issues", map[string]any{"summary": "something to touch"})
	guardedSprint := idOf(t, want(t, owner.post("/api/v1/projects/GUARD/sprints", map[string]any{"name": "Guarded sprint"}), http.StatusCreated, "guarded sprint"), "sprint")
	issueKey := issue.Body["issue"].(map[string]any)["key"].(string)

	// what one role is expected to be able to do. Reading is always allowed.
	type attempt struct {
		what    string
		do      func(c *client) response
		allowed map[perm.Role]bool
	}

	attempts := []attempt{
		{
			what: "read the project",
			do:   func(c *client) response { return c.get("/api/v1/projects/GUARD") },
			allowed: map[perm.Role]bool{
				perm.Reader: true, perm.User: true, perm.ScrumMaster: true,
				perm.ProjectAdministrator: true, perm.GlobalAdministrator: true,
			},
		},
		{
			what: "search with a query",
			do:   func(c *client) response { return c.get("/api/v1/issues?q=assignee%20%3D%20currentUser()") },
			allowed: map[perm.Role]bool{
				perm.Reader: true, perm.User: true, perm.ScrumMaster: true,
				perm.ProjectAdministrator: true, perm.GlobalAdministrator: true,
			},
		},
		{
			what: "file an issue",
			do: func(c *client) response {
				return c.post("/api/v1/projects/GUARD/issues", map[string]any{"summary": "filed"})
			},
			allowed: map[perm.Role]bool{
				perm.User: true, perm.ScrumMaster: true,
				perm.ProjectAdministrator: true, perm.GlobalAdministrator: true,
			},
		},
		{
			what: "comment on one",
			do: func(c *client) response {
				return c.post("/api/v1/issues/"+issueKey+"/comments", map[string]any{"text": "a note"})
			},
			allowed: map[perm.Role]bool{
				perm.User: true, perm.ScrumMaster: true,
				perm.ProjectAdministrator: true, perm.GlobalAdministrator: true,
			},
		},
		{
			// Filing straight into a sprint is a commitment, and takes the
			// sprint permission that moving into one does.
			what: "file an issue straight into a sprint",
			do: func(c *client) response {
				return c.post("/api/v1/projects/GUARD/issues", map[string]any{"summary": "committed on arrival", "sprintId": guardedSprint})
			},
			allowed: map[perm.Role]bool{
				perm.ScrumMaster: true, perm.ProjectAdministrator: true, perm.GlobalAdministrator: true,
			},
		},
		{
			what: "plan a sprint",
			do: func(c *client) response {
				return c.post("/api/v1/projects/GUARD/sprints", map[string]any{"name": unique("sprint")})
			},
			allowed: map[perm.Role]bool{
				perm.ScrumMaster: true, perm.ProjectAdministrator: true, perm.GlobalAdministrator: true,
			},
		},
		{
			what: "form a team",
			do: func(c *client) response {
				return c.post("/api/v1/projects/GUARD/teams", map[string]any{"name": unique("team")})
			},
			allowed: map[perm.Role]bool{
				perm.ScrumMaster: true, perm.ProjectAdministrator: true, perm.GlobalAdministrator: true,
			},
		},
		{
			what: "configure the project",
			do: func(c *client) response {
				return c.do(http.MethodPatch, "/api/v1/projects/GUARD", map[string]any{"description": "changed"})
			},
			allowed: map[perm.Role]bool{
				perm.ProjectAdministrator: true, perm.GlobalAdministrator: true,
			},
		},
		{
			what: "administer the organization",
			do:   func(c *client) response { return c.get("/api/v1/invites") },
			allowed: map[perm.Role]bool{
				perm.GlobalAdministrator: true,
			},
		},
		{
			what: "coin a status",
			do: func(c *client) response {
				return c.post("/api/v1/statuses", map[string]string{"name": "Coined " + freshProjectKey(), "category": "todo"})
			},
			allowed: map[perm.Role]bool{
				perm.GlobalAdministrator: true,
			},
		},
		{
			what: "make a project",
			do: func(c *client) response {
				return c.post("/api/v1/projects", map[string]string{"name": "Mine", "key": freshProjectKey()})
			},
			allowed: map[perm.Role]bool{
				perm.GlobalAdministrator: true,
			},
		},
		{
			what: "see the organization's accounts",
			do:   func(c *client) response { return c.get("/api/v1/users") },
			allowed: map[perm.Role]bool{
				perm.GlobalAdministrator: true,
			},
		},
		{
			what: "make an account",
			do: func(c *client) response {
				return c.post("/api/v1/users", map[string]string{
					"email": h.email(t, "made"), "name": "Made", "role": "member", "password": testPassword})
			},
			allowed: map[perm.Role]bool{
				perm.GlobalAdministrator: true,
			},
		},
	}

	for _, role := range perm.Roles {
		t.Run(string(role), func(t *testing.T) {
			// A role that only makes sense over the whole tenant is granted
			// that way; the rest are scoped to the one project.
			scope := "GUARD"
			if role.OrgWideOnly() {
				scope = ""
			}
			actor := api.asRole(t, h, owner, scope, role)

			for _, each := range attempts {
				got := each.do(actor)
				want := each.allowed[role]
				succeeded := got.Status >= 200 && got.Status < 300

				if succeeded != want {
					t.Errorf("%s: %s got %d, want it %s",
						role, each.what, got.Status, allowedWord(want))
				}
				if !want && got.Status != http.StatusForbidden {
					t.Errorf("%s: %s was refused with %d, want 403 naming the permission",
						role, each.what, got.Status)
				}
			}
		})
	}
}

// freshProjectKey is a key nothing else has taken, so that the one role allowed
// to make a project does not collide with an earlier attempt or an earlier run.
func freshProjectKey() string {
	return "K" + strings.ToUpper(strings.ReplaceAll(uuid.New().String()[:6], "-", ""))
}

func allowedWord(allowed bool) string {
	if allowed {
		return "allowed"
	}
	return "refused"
}
