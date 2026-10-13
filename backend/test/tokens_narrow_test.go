//go:build integration

package test

import (
	"net/http"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
)

// A key holds what the person who made it holds, and never more: not the
// administration, and not a project it was not given.
func TestAKeyIsNarrowerThanTheOwnerWhoMadeIt(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	lateKey := h.projectKey(t, "LATE")
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "narrow", auth.RoleOwner))
	near, far := h.projectKey(t, "NEAR"), h.projectKey(t, "FAR")

	for _, key := range []string{near, far} {
		if made := owner.post("/api/v1/projects", map[string]any{"name": "Project " + key, "key": key}); made.Status != http.StatusCreated {
			t.Fatalf("create project %s: %d %s", key, made.Status, made.Raw)
		}
		owner.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "already here"})
	}

	created := owner.post("/api/v1/tokens", map[string]any{"name": "pipeline", "projects": []string{near}})
	if created.Status != http.StatusCreated {
		t.Fatalf("create token: %d %s", created.Status, created.Raw)
	}
	confined := api.client(t)
	confined.bearer = principalField(t, created, "token", "secret").(string)
	// A new key is not pinned to its owner's writes; let the replica catch up.
	h.waitForPrimary(t)

	t.Run("it works in the project it names", func(t *testing.T) {
		if resp := confined.get("/api/v1/projects/" + near + "/issues"); resp.Status != http.StatusOK {
			t.Fatalf("read the named project: %d %s", resp.Status, resp.Raw)
		}
		if resp := confined.post("/api/v1/projects/"+near+"/issues", map[string]any{"summary": "from the key"}); resp.Status != http.StatusCreated {
			t.Fatalf("write in the named project: %d %s", resp.Status, resp.Raw)
		}
	})

	t.Run("the other project is not found, while its owner still sees it", func(t *testing.T) {
		if resp := confined.get("/api/v1/projects/" + far + "/issues"); resp.Status != http.StatusNotFound {
			t.Fatalf("read an unnamed project: %d %s", resp.Status, resp.Raw)
		}
		if resp := confined.post("/api/v1/projects/"+far+"/issues", map[string]any{"summary": "should not land"}); resp.Status < 400 {
			t.Fatalf("wrote into an unnamed project: %d %s", resp.Status, resp.Raw)
		}
		// The refusal has to be the key's, not the person's.
		if resp := owner.get("/api/v1/projects/" + far + "/issues"); resp.Status != http.StatusOK || resp.Body["total"].(float64) != 1 {
			t.Fatalf("the owner lost the project too, so this proves nothing: %d %s", resp.Status, resp.Raw)
		}
	})

	t.Run("a listing shows only what the key names", func(t *testing.T) {
		listed := confined.get("/api/v1/projects")
		if listed.Status != http.StatusOK {
			t.Fatalf("list projects: %d %s", listed.Status, listed.Raw)
		}
		projects, _ := listed.Body["projects"].([]any)
		if len(projects) != 1 {
			t.Fatalf("a confined key lists %d projects, want 1: %s", len(projects), listed.Raw)
		}
		if key := projects[0].(map[string]any)["key"]; key != near {
			t.Errorf("listed %v, want %s", key, near)
		}
	})

	t.Run("a project made afterwards is still out of reach", func(t *testing.T) {
		if made := owner.post("/api/v1/projects", map[string]any{"name": "Later", "key": lateKey}); made.Status != http.StatusCreated {
			t.Fatalf("create the later project: %d %s", made.Status, made.Raw)
		}
		if resp := confined.get("/api/v1/projects/" + lateKey + "/issues"); resp.Status != http.StatusNotFound {
			t.Fatalf("a key reached a project made after it: %d %s", resp.Status, resp.Raw)
		}
	})

	t.Run("naming a project the owner cannot reach is refused", func(t *testing.T) {
		resp := owner.post("/api/v1/tokens", map[string]any{"name": "hopeful", "projects": []string{uniqueKey("NOPE")}})
		if resp.Status != http.StatusUnprocessableEntity {
			t.Fatalf("named an unknown project: %d %s", resp.Status, resp.Raw)
		}
	})
}

// The owner here is an owner of the installation, which is as much standing as
// anybody has. Their key still does not administer.
func TestAKeyNeverAdministersHoweverMuchItsOwnerCan(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	wrkKey := h.projectKey(t, "WRK")
	snkKey := h.projectKey(t, "SNK")
	api := newAPIServer(t, h)
	owner := api.client(t)
	joined := owner.signIn(t, h.person(t, "noadmin", auth.RoleOwner))
	ownerID := principalField(t, joined, "principal", "user", "id").(string)
	if made := owner.post("/api/v1/projects", map[string]any{"name": "Work", "key": wrkKey}); made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}

	created := owner.post("/api/v1/tokens", map[string]any{"name": "everything"})
	key := api.client(t)
	key.bearer = principalField(t, created, "token", "secret").(string)
	// A new key is not pinned to its owner's writes; let the replica catch up.
	h.waitForPrimary(t)

	t.Run("it cannot grant a role, though its owner can", func(t *testing.T) {
		body := map[string]any{"role": "project_administrator", "projectKey": wrkKey, "userId": ownerID}
		refused := key.post("/api/v1/role-assignments", body)
		if refused.Status != http.StatusForbidden {
			t.Fatalf("a key granted a role: %d %s", refused.Status, refused.Raw)
		}
		if granted := owner.post("/api/v1/role-assignments", body); granted.Status != http.StatusCreated {
			t.Fatalf("the owner cannot grant it either, so this proves nothing: %d %s", granted.Status, granted.Raw)
		}
	})

	t.Run("it cannot make a project", func(t *testing.T) {
		if refused := key.post("/api/v1/projects", map[string]any{"name": "Sneaky", "key": snkKey}); refused.Status != http.StatusForbidden {
			t.Fatalf("a key made a project: %d %s", refused.Status, refused.Raw)
		}
	})

	t.Run("it cannot archive one, which its owner can", func(t *testing.T) {
		refused := key.delete("/api/v1/projects/" + wrkKey)
		if refused.Status != http.StatusForbidden || refused.ErrorCode() != "session_only" {
			t.Fatalf("a key archived a project: %d %s", refused.Status, refused.Raw)
		}
	})

	t.Run("it cannot mint another key", func(t *testing.T) {
		refused := key.post("/api/v1/tokens", map[string]any{"name": "child"})
		if refused.Status != http.StatusForbidden || refused.ErrorCode() != "session_only" {
			t.Fatalf("a key minted a key, so revoking the first would not end the access: %d %s", refused.Status, refused.Raw)
		}
	})

	t.Run("it still does the ordinary work", func(t *testing.T) {
		if resp := key.post("/api/v1/projects/"+wrkKey+"/issues", map[string]any{"summary": "from the key"}); resp.Status != http.StatusCreated {
			t.Fatalf("a key lost the work along with the administration: %d %s", resp.Status, resp.Raw)
		}
	})
}
