//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
)

const staleEditSentence = "Somebody changed this issue since you opened it. Reload it and apply your edit again."

func paragraphDoc(text string) map[string]any {
	return map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}},
	}}
}

// Two tabs open the same description. The one that saves second started from
// an older copy, so its save is refused instead of erasing the first one's words.
func TestAStaleEditOfAnIssueIsRefused(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	tabKey := h.projectKey(t, "TAB")
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "two-tabs", auth.RoleOwner))
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Two tabs", "key": tabKey}), http.StatusCreated, "create project")
	issued := want(t, owner.post("/api/v1/projects/"+tabKey+"/issues", map[string]any{"summary": "shared", "description": paragraphDoc("as it was")}), http.StatusCreated, "create issue")
	key := issued.Body["issue"].(map[string]any)["key"].(string)
	opened := issued.Body["issue"].(map[string]any)["updatedAt"].(string)

	first := want(t, owner.do(http.MethodPatch, "/api/v1/issues/"+key, map[string]any{
		"description": paragraphDoc("tab A's words"), "expectedUpdatedAt": opened,
	}), http.StatusOK, "the first save")
	afterFirst := first.Body["issue"].(map[string]any)["updatedAt"].(string)
	if afterFirst == opened {
		t.Fatalf("a save that changed the description left updatedAt at %s", opened)
	}

	stale := want(t, owner.do(http.MethodPatch, "/api/v1/issues/"+key, map[string]any{
		"description": paragraphDoc("tab B's words"), "expectedUpdatedAt": opened,
	}), http.StatusConflict, "the stale save")
	if got := stale.Body["error"].(map[string]any)["message"]; got != staleEditSentence {
		t.Fatalf("the refusal says %q, want %q", got, staleEditSentence)
	}
	staleSummary := want(t, owner.do(http.MethodPatch, "/api/v1/issues/"+key, map[string]any{
		"summary": "tab B's summary", "expectedUpdatedAt": opened,
	}), http.StatusConflict, "a stale summary")
	if got := staleSummary.Body["error"].(map[string]any)["message"]; got != staleEditSentence {
		t.Fatalf("the summary refusal says %q, want %q", got, staleEditSentence)
	}

	kept := want(t, owner.get("/api/v1/issues/"+key), http.StatusOK, "read back").Raw
	if !strings.Contains(kept, "tab A's words") || strings.Contains(kept, "tab B's words") || strings.Contains(kept, "tab B's summary") {
		t.Fatalf("the refused saves changed the issue: %s", kept)
	}

	// A save that started from what is there now goes through.
	fresh := want(t, owner.do(http.MethodPatch, "/api/v1/issues/"+key, map[string]any{
		"description": paragraphDoc("tab B, having read A"), "summary": "merged", "expectedUpdatedAt": afterFirst,
	}), http.StatusOK, "the fresh save")
	if !strings.Contains(fresh.Raw, "tab B, having read A") || fresh.Body["issue"].(map[string]any)["summary"] != "merged" {
		t.Fatalf("the fresh save did not land: %s", fresh.Raw)
	}

	// A client that does not say where it started is not refused, as before.
	want(t, owner.do(http.MethodPatch, "/api/v1/issues/"+key, map[string]any{"priority": "high"}), http.StatusOK, "an edit without expectedUpdatedAt")
}

// Saving the description the issue already has, or the due date it already
// has, is not a change: no changelog entry, no event and no new updatedAt.
func TestASaveThatChangesNothingRecordsNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	nopKey := h.projectKey(t, "NOP")
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "no-op-saver", auth.RoleOwner))
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Unchanged", "key": nopKey}), http.StatusCreated, "create project")
	issued := want(t, owner.post("/api/v1/projects/"+nopKey+"/issues", map[string]any{"summary": "steady"}), http.StatusCreated, "create issue")
	key := issued.Body["issue"].(map[string]any)["key"].(string)
	issueID := issued.Body["issue"].(map[string]any)["id"].(string)

	want(t, owner.do(http.MethodPatch, "/api/v1/issues/"+key, map[string]any{"description": paragraphDoc("settled"), "dueDate": "2026-12-01"}), http.StatusOK, "the real edit")
	before := want(t, owner.get("/api/v1/issues/"+key), http.StatusOK, "read").Body["issue"].(map[string]any)["updatedAt"].(string)
	entries := func() int {
		var n int
		if err := h.super.QueryRow(context.Background(),
			`SELECT count(*) FROM issue_history WHERE issue_id = $1`, issueID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	recorded := entries()

	// The same document with its keys in another order is still the same document.
	reordered := map[string]any{"content": []any{
		map[string]any{"content": []any{map[string]any{"text": "settled", "type": "text"}}, "type": "paragraph"},
	}, "type": "doc"}
	again := want(t, owner.do(http.MethodPatch, "/api/v1/issues/"+key, map[string]any{
		"description": reordered, "dueDate": "2026-12-01", "expectedUpdatedAt": before,
	}), http.StatusOK, "the save that changes nothing")
	if got := again.Body["issue"].(map[string]any)["updatedAt"]; got != before {
		t.Fatalf("a save that changed nothing moved updatedAt from %s to %v", before, got)
	}
	if got := entries(); got != recorded {
		t.Fatalf("a save that changed nothing wrote %d changelog entries", got-recorded)
	}
	history := want(t, owner.get("/api/v1/issues/"+key+"/history"), http.StatusOK, "history").Raw
	if strings.Count(history, `"field":"description"`) != 1 || strings.Count(history, `"field":"dueDate"`) != 1 {
		t.Fatalf("the changelog should hold one description and one due date change: %s", history)
	}
}
