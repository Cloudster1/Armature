//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/perm"
)

// asRole makes somebody who holds nothing in the installation, grants them one
// role over the project (or everywhere, for ""), and returns a client signed in as them.
func (s *apiServer) asRole(t *testing.T, h *harness, owner *client, projectKey string, role perm.Role) *client {
	t.Helper()
	email := h.outsider(t, string(role))
	var userID string
	if err := h.super.QueryRow(context.Background(), `SELECT id::text FROM app_user WHERE email = $1`, email).Scan(&userID); err != nil {
		t.Fatalf("find %s: %v", email, err)
	}
	granted := owner.post("/api/v1/role-assignments", map[string]any{
		"role": string(role), "projectKey": projectKey, "userId": userID,
	})
	if granted.Status != http.StatusCreated {
		t.Fatalf("granting %s returned %d: %s", role, granted.Status, granted.Raw)
	}
	// The owner's writes pin the owner to the primary, not this client, whose
	// first requests read the replica; until the grant is there it holds nothing.
	h.waitForPrimary(t)
	joined := s.client(t)
	joined.signIn(t, email)
	return joined
}

// TestWhatEachRoleMayDo walks the five roles across the same set of actions.
// One table, so that adding a permission means deciding what every role does
// with it rather than forgetting four of them.
func TestWhatEachRoleMayDo(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	guardKey := h.projectKey(t, "GUARD")
	api := newAPIServer(t, h)

	owner := api.client(t)
	owner.signIn(t, h.person(t, "roles", auth.RoleOwner))
	created := owner.post("/api/v1/projects", map[string]string{"name": "Guarded", "key": guardKey})
	if created.Status != http.StatusCreated {
		t.Fatalf("create project returned %d: %s", created.Status, created.Raw)
	}
	issue := owner.post("/api/v1/projects/"+guardKey+"/issues", map[string]any{"summary": "something to touch"})
	guardedSprint := idOf(t, want(t, owner.post("/api/v1/projects/"+guardKey+"/sprints", map[string]any{"name": "Guarded sprint"}), http.StatusCreated, "guarded sprint"), "sprint")
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
			do:   func(c *client) response { return c.get("/api/v1/projects/" + guardKey) },
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
				return c.post("/api/v1/projects/"+guardKey+"/issues", map[string]any{"summary": "filed"})
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
				return c.post("/api/v1/projects/"+guardKey+"/issues", map[string]any{"summary": "committed on arrival", "sprintId": guardedSprint})
			},
			allowed: map[perm.Role]bool{
				perm.ScrumMaster: true, perm.ProjectAdministrator: true, perm.GlobalAdministrator: true,
			},
		},
		{
			what: "plan a sprint",
			do: func(c *client) response {
				return c.post("/api/v1/projects/"+guardKey+"/sprints", map[string]any{"name": unique("sprint")})
			},
			allowed: map[perm.Role]bool{
				perm.ScrumMaster: true, perm.ProjectAdministrator: true, perm.GlobalAdministrator: true,
			},
		},
		{
			what: "form a team",
			do: func(c *client) response {
				return c.post("/api/v1/projects/"+guardKey+"/teams", map[string]any{"name": unique("team")})
			},
			allowed: map[perm.Role]bool{
				perm.ScrumMaster: true, perm.ProjectAdministrator: true, perm.GlobalAdministrator: true,
			},
		},
		{
			what: "configure the project",
			do: func(c *client) response {
				return c.do(http.MethodPatch, "/api/v1/projects/"+guardKey, map[string]any{"description": "changed"})
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
				return c.post("/api/v1/statuses", map[string]string{"name": h.name(t, "Coined"), "category": "todo"})
			},
			allowed: map[perm.Role]bool{
				perm.GlobalAdministrator: true,
			},
		},
		{
			what: "make a project",
			do: func(c *client) response {
				return c.post("/api/v1/projects", map[string]string{"name": "Mine", "key": h.projectKey(t, "K")})
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
			scope := guardKey
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

func allowedWord(allowed bool) string {
	if allowed {
		return "allowed"
	}
	return "refused"
}
