//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/perm"
)

// The installation's roles are its own: the five built in can be changed, its
// own added and removed, and every guard the service applies holds in SQL too.
func TestTheInstallationEditsItsOwnRoles(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	mtxKey := h.projectKey(t, "MTX")
	api := newAPIServer(t, h)
	owner := api.client(t)
	signedUp := owner.signIn(t, h.person(t, "rolematrix", auth.RoleOwner))
	want(t, owner.post("/api/v1/projects", map[string]string{"name": "Matrix", "key": mtxKey}), http.StatusCreated, "project")

	// Other tests add roles of their own beside these, so the five are looked for.
	listed := want(t, owner.get("/api/v1/roles"), http.StatusOK, "roles")
	for _, builtin := range []string{"global_administrator", "project_administrator", "scrum_master", "user", "reader"} {
		if got := find(t, listed.Body["roles"], "role", builtin); got["builtin"] != true {
			t.Fatalf("%s is not built in: %v", builtin, got)
		}
	}
	perms := list(t, want(t, owner.get("/api/v1/permissions"), http.StatusOK, "permissions"), "permissions")
	if len(perms) != 10 || perms[0].(map[string]any)["words"] == "" {
		t.Fatalf("permissions = %v", perms)
	}

	plannerName := h.name(t, "Sprint planner")
	made := want(t, owner.post("/api/v1/roles", map[string]any{"name": plannerName, "description": "Plans, nothing else.", "permissions": []string{"read", "sprint.manage"}}), http.StatusCreated, "add a role")
	role := obj(t, made, "role")
	plannerKey, _ := role["role"].(string)
	if !strings.HasPrefix(plannerKey, "sprint_planner") || role["builtin"] != false || len(role["permissions"].([]any)) != 2 {
		t.Fatalf("made = %s", made.Raw)
	}

	// Made here, not inside a subtest: the helper registers the account's
	// cleanup on the t it is given, and a subtest's cleanup would take the
	// account and its grant away before the later subtests look.
	planner := api.asRole(t, h, owner, mtxKey, perm.Role(plannerKey))

	t.Run("what is refused", func(t *testing.T) {
		refuse := func(what string, got response, status int) {
			t.Helper()
			if got.Status != status {
				t.Errorf("%s: got %d, want %d: %s", what, got.Status, status, got.Raw)
			}
		}
		refuse("the same name twice", owner.post("/api/v1/roles", map[string]any{"name": strings.ToUpper(plannerName), "permissions": []string{"read"}}), http.StatusConflict)
		refuse("no name", owner.post("/api/v1/roles", map[string]any{"permissions": []string{"read"}}), http.StatusUnprocessableEntity)
		refuse("a permission that is a word", owner.post("/api/v1/roles", map[string]any{"name": "Wizard", "permissions": []string{"fly"}}), http.StatusUnprocessableEntity)
		refuse("a key with spaces", owner.post("/api/v1/roles", map[string]any{"key": "no way", "name": "Spaced", "permissions": []string{"read"}}), http.StatusUnprocessableEntity)
		refuse("deleting a built-in role", owner.delete("/api/v1/roles/reader"), http.StatusConflict)
		refuse("a role that is not there", owner.patch("/api/v1/roles/nobody", map[string]any{"name": "X"}), http.StatusNotFound)
		refuse("the global administrator without administering", owner.patch("/api/v1/roles/global_administrator", map[string]any{"permissions": []string{"read"}}), http.StatusConflict)
		refuse("granting a role that is not there", owner.post("/api/v1/role-assignments", map[string]any{"role": "wizard", "userId": principalField(t, signedUp, "principal", "user", "id")}), http.StatusNotFound)
	})

	t.Run("SQL refuses the same", func(t *testing.T) {
		ctx := context.Background()
		if _, err := h.super.Exec(ctx, `DELETE FROM org_role WHERE key = 'reader'`); err == nil {
			t.Error("SQL deleted a built-in role")
		}
		if _, err := h.super.Exec(ctx, `UPDATE org_role SET permissions = '{read}' WHERE key = 'global_administrator'`); err == nil {
			t.Error("SQL let the global administrator stop administering")
		}
		if _, err := h.super.Exec(ctx, `UPDATE org_role SET permissions = '{read,fly}' WHERE key = $1`, plannerKey); err == nil {
			t.Error("SQL took a permission that is a word")
		}
		if _, err := h.super.Exec(ctx, `UPDATE org_role SET key = 'planner' WHERE key = $1`, plannerKey); err == nil {
			t.Error("SQL let a role's key change")
		}
	})

	t.Run("a role of the organization's own works end to end, and an edit follows at once", func(t *testing.T) {
		want(t, planner.post("/api/v1/projects/"+mtxKey+"/sprints", map[string]any{"name": "Sprint 1"}), http.StatusCreated, "a planner plans")
		if got := planner.post("/api/v1/projects/"+mtxKey+"/issues", map[string]any{"summary": "not theirs to file"}); got.Status != http.StatusForbidden {
			t.Fatalf("a planner filed an issue: %d %s", got.Status, got.Raw)
		}
		want(t, owner.patch("/api/v1/roles/"+plannerKey, map[string]any{"permissions": []string{"read", "sprint.manage", "issue.write"}}), http.StatusOK, "let planners file")
		h.waitForPrimary(t)
		want(t, planner.post("/api/v1/projects/"+mtxKey+"/issues", map[string]any{"summary": "now theirs to file"}), http.StatusCreated, "the edit reached them")
		access := want(t, planner.get("/api/v1/access/me"), http.StatusOK, "what a planner holds")
		held := access.Body["permissions"].(map[string]any)["projects"].(map[string]any)[mtxKey].([]any)
		if len(held) != 3 {
			t.Fatalf("a planner holds %v in "+mtxKey, held)
		}
	})

	t.Run("an org-wide-only role of the organization's own cannot be scoped, over the API and in SQL", func(t *testing.T) {
		made := want(t, owner.post("/api/v1/roles", map[string]any{"name": h.name(t, "Auditor"), "orgWideOnly": true, "permissions": []string{"read"}}), http.StatusCreated, "add an org-wide role")
		key := obj(t, made, "role")["role"].(string)
		userID := principalField(t, signedUp, "principal", "user", "id").(string)
		if got := owner.post("/api/v1/role-assignments", map[string]any{"role": key, "projectKey": mtxKey, "userId": userID}); got.Status != http.StatusBadRequest {
			t.Fatalf("an org-wide role was scoped: %d %s", got.Status, got.Raw)
		}
		if _, err := h.super.Exec(context.Background(), `
			INSERT INTO role_assignment (role, project_id, user_id)
			SELECT $1, p.id, $2 FROM project p WHERE p.key = $3`, key, userID, mtxKey); err == nil {
			t.Fatal("SQL let an org-wide role be granted over one project")
		}
	})

	t.Run("deleting a role takes its grants with it", func(t *testing.T) {
		h.waitForPrimary(t)
		before := want(t, owner.get("/api/v1/roles"), http.StatusOK, "roles")
		planner := find(t, before.Body["roles"], "role", plannerKey)
		if planner["inUse"].(float64) < 1 {
			t.Fatalf("the planner role shows no grants: %v", planner)
		}
		want(t, owner.delete("/api/v1/roles/"+plannerKey), http.StatusNoContent, "delete")
		h.waitForPrimary(t)
		for _, row := range list(t, want(t, owner.get("/api/v1/role-assignments"), http.StatusOK, "assignments"), "assignments") {
			if row.(map[string]any)["role"] == plannerKey {
				t.Fatalf("a grant of a deleted role survived: %v", row)
			}
		}
	})
}

// The built-in roles read as the code describes them, in the words the rest of
// the product uses: an installation, never a tenant (#218).
func TestTheBuiltInRolesReadAsTheCodeDescribesThem(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	for _, want := range perm.Builtins() {
		var name, description string
		if err := h.super.QueryRow(context.Background(), `SELECT name, description FROM org_role WHERE key = $1 AND builtin`, string(want.Key)).Scan(&name, &description); err != nil {
			t.Fatalf("read %s: %v", want.Key, err)
		}
		if name != want.Name || description != want.Description {
			t.Errorf("%s reads %q, %q; want %q, %q", want.Key, name, description, want.Name, want.Description)
		}
	}
}
