//go:build integration

package test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
)

// A comment is changed through the issue it was written on. One that is not
// there, or that hangs off another issue, is not found rather than refused.
func TestACommentIsChangedOnlyThroughItsOwnIssue(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	cedKey := h.projectKey(t, "CED")
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "commentedits", auth.RoleOwner))
	want(t, owner.post("/api/v1/projects", map[string]string{"name": "Comment Edits", "key": cedKey}), http.StatusCreated, "project")
	file := func(summary string) string {
		t.Helper()
		return obj(t, want(t, owner.post("/api/v1/issues", map[string]any{"projectKey": cedKey, "summary": summary}), http.StatusCreated, summary), "issue")["key"].(string)
	}
	onKey, otherKey := file("commented on"), file("not commented on")
	commentID := idOf(t, want(t, owner.post("/api/v1/issues/"+onKey+"/comments", map[string]any{"text": "a remark"}), http.StatusCreated, "comment"), "comment")

	missing := uuid.NewString()
	for _, attempt := range []struct {
		what string
		got  response
	}{
		{"editing a comment that is not there", owner.patch("/api/v1/issues/"+onKey+"/comments/"+missing, map[string]any{"text": "edited"})},
		{"deleting a comment that is not there", owner.delete("/api/v1/issues/" + onKey + "/comments/" + missing)},
		{"editing a comment through another issue", owner.patch("/api/v1/issues/"+otherKey+"/comments/"+commentID, map[string]any{"text": "edited elsewhere"})},
		{"deleting a comment through another issue", owner.delete("/api/v1/issues/" + otherKey + "/comments/" + commentID)},
	} {
		if attempt.got.Status != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404: %s", attempt.what, attempt.got.Status, attempt.got.Raw)
		}
	}

	comments := list(t, want(t, owner.get("/api/v1/issues/"+onKey+"/comments"), http.StatusOK, "comments"), "comments")
	if len(comments) != 1 || comments[0].(map[string]any)["editedAt"] != nil {
		t.Fatalf("the comment was changed through another issue: %v", comments)
	}
	want(t, owner.patch("/api/v1/issues/"+onKey+"/comments/"+commentID, map[string]any{"text": "edited here"}), http.StatusOK, "editing it on its own issue")
	want(t, owner.delete("/api/v1/issues/"+onKey+"/comments/"+commentID), http.StatusNoContent, "deleting it on its own issue")
}
