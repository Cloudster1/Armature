//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
)

// Disconnecting a repository takes its commits, branches and pull requests
// along, so the log keeps who did it, which repository and how much went.
func TestDisconnectingARepositoryIsAudited(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	key := h.projectKey(t, "DSC")
	api := newAPIServer(t, h)
	owner := api.client(t)
	signedIn := owner.signIn(t, h.person(t, "repodisconnect", auth.RoleOwner))
	ownerID := principalField(t, signedIn, "principal", "user", "id").(string)
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Disconnected", "key": key}), http.StatusCreated, "the project")
	repoID := idOf(t, want(t, owner.post("/api/v1/projects/"+key+"/repositories", map[string]any{"host": "github", "name": "acme/leaving"}), http.StatusCreated, "connect"), "repository")

	ctx := context.Background()
	if _, err := h.super.Exec(ctx, `INSERT INTO git_commit (repository_id, sha, message) VALUES ($1, 'abc1234', 'First'), ($1, 'def5678', 'Second')`, repoID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO git_branch (repository_id, name) VALUES ($1, 'feature')`, repoID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO pull_request (repository_id, number, title) VALUES ($1, 7, 'Leave')`, repoID); err != nil {
		t.Fatal(err)
	}

	want(t, owner.delete("/api/v1/repositories/"+repoID), http.StatusNoContent, "disconnecting")

	var (
		actor, targetType string
		raw               []byte
	)
	if err := h.super.QueryRow(ctx, `
		SELECT actor_user_id::text, target_type, data FROM audit_log
		WHERE action = 'vcs.repository.disconnected' AND target_id = $1`, repoID).Scan(&actor, &targetType, &raw); err != nil {
		t.Fatalf("no audit row for the disconnect: %v", err)
	}
	if actor != ownerID {
		t.Errorf("the disconnect names actor %s, want the owner %s", actor, ownerID)
	}
	if targetType != "repository" {
		t.Errorf("the disconnect targets a %s, want a repository", targetType)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	for field, value := range map[string]any{
		"name": "acme/leaving", "project": key, "host": "github",
		"commits": float64(2), "branches": float64(1), "pullRequests": float64(1),
	} {
		if data[field] != value {
			t.Errorf("the entry's %s = %v, want %v: %s", field, data[field], value, raw)
		}
	}

	// The audit page finds it under the actor, as it finds every other act.
	page := want(t, owner.get("/api/v1/audit?action=vcs.repository.disconnected&actor="+ownerID), http.StatusOK, "the log")
	if entries := list(t, page, "entries"); len(entries) != 1 || entries[0].(map[string]any)["targetId"] != repoID {
		t.Errorf("the log shows %v, want the one disconnect of %s", entries, repoID)
	}

	// A second disconnect finds nothing to remove and records nothing.
	want(t, owner.delete("/api/v1/repositories/"+repoID), http.StatusNotFound, "disconnecting again")
	var entries int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'vcs.repository.disconnected' AND target_id = $1`, repoID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 1 {
		t.Errorf("%d audit rows after a refused second disconnect, want one", entries)
	}
}
