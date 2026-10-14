//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/perm"
)

// Archiving a project and bringing it back are both in the audit log, naming
// who did it, and a reader of the project may do neither.
func TestRestoringAProjectIsAuditedAndKeptToItsAdministrators(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	key := h.projectKey(t, "RST")
	api := newAPIServer(t, h)
	owner := api.client(t)
	ownerID := principalField(t, owner.signIn(t, h.person(t, "restorer", auth.RoleOwner)), "principal", "user", "id").(string)
	projectID := idOf(t, want(t, owner.post("/api/v1/projects", map[string]any{"name": "Brought back", "key": key}), http.StatusCreated, "the project"), "project")
	reader := api.asRole(t, h, owner, key, perm.Reader)

	want(t, reader.delete("/api/v1/projects/"+key), http.StatusForbidden, "a reader archiving the project")
	want(t, owner.delete("/api/v1/projects/"+key), http.StatusNoContent, "archiving the project")
	want(t, reader.post("/api/v1/projects/"+key+"/restore", nil), http.StatusForbidden, "a reader restoring the project")
	want(t, owner.post("/api/v1/projects/"+key+"/restore", nil), http.StatusNoContent, "restoring the project")
	if got := obj(t, want(t, owner.get("/api/v1/projects/"+key), http.StatusOK, "the restored project"), "project"); got["archivedAt"] != nil {
		t.Errorf("the restored project is still archived at %v", got["archivedAt"])
	}

	h.copyToAudit(t, ownerID)
	for _, action := range []string{"project.archived", "project.restored"} {
		var n int
		if err := h.super.QueryRow(context.Background(), `
			SELECT count(*) FROM audit_log
			WHERE action = $1 AND target_type = 'project' AND target_id = $2 AND actor_user_id = $3`,
			action, projectID, ownerID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s by the owner about the project: %d entries, want one", action, n)
		}
	}
}
