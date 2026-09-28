//go:build integration

package test

import (
	"net/http"
	"testing"

	"github.com/armature/armature/backend/internal/perm"
)

// Deleting an issue, and removing what somebody else wrote or logged on one,
// follow the project's administration in the matrix, not the standing on the
// Users page. A person's own comment and time stay their own to remove.
func TestAdministeringTheProjectDecidesWhatMayBeRemoved(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "administering")
	want(t, owner.post("/api/v1/projects", map[string]string{"name": "Administering", "key": "ADM"}), http.StatusCreated, "project")

	file := func() (issueKey, commentID, worklogID string) {
		t.Helper()
		filed := want(t, owner.post("/api/v1/issues", map[string]any{"projectKey": "ADM", "summary": "owned by the owner"}), http.StatusCreated, "issue")
		issueKey = filed.Body["issue"].(map[string]any)["key"].(string)
		commentID = idOf(t, want(t, owner.post("/api/v1/issues/"+issueKey+"/comments", map[string]any{"text": "the owner's remark"}), http.StatusCreated, "comment"), "comment")
		worklogID = idOf(t, want(t, owner.post("/api/v1/issues/"+issueKey+"/worklogs", map[string]any{"minutes": 30}), http.StatusCreated, "worklog"), "worklog")
		return issueKey, commentID, worklogID
	}

	issueKey, commentID, worklogID := file()
	user := api.asRole(t, h, owner, "ADM", perm.User)
	want(t, user.delete("/api/v1/issues/"+issueKey+"/comments/"+commentID), http.StatusForbidden, "a user removing the owner's comment")
	want(t, user.delete("/api/v1/worklogs/"+worklogID), http.StatusForbidden, "a user removing the owner's time")
	want(t, user.patch("/api/v1/worklogs/"+worklogID, map[string]any{"minutes": 5}), http.StatusForbidden, "a user correcting the owner's time")
	want(t, user.delete("/api/v1/issues/"+issueKey), http.StatusForbidden, "a user deleting an issue")
	ownComment := idOf(t, want(t, user.post("/api/v1/issues/"+issueKey+"/comments", map[string]any{"text": "my own remark"}), http.StatusCreated, "own comment"), "comment")
	want(t, user.delete("/api/v1/issues/"+issueKey+"/comments/"+ownComment), http.StatusNoContent, "a user removing their own comment")

	admin := api.asRole(t, h, owner, "ADM", perm.ProjectAdministrator)
	want(t, admin.patch("/api/v1/worklogs/"+worklogID, map[string]any{"minutes": 5}), http.StatusOK, "a project administrator correcting the owner's time")
	want(t, admin.delete("/api/v1/worklogs/"+worklogID), http.StatusNoContent, "a project administrator removing the owner's time")
	want(t, admin.delete("/api/v1/issues/"+issueKey+"/comments/"+commentID), http.StatusNoContent, "a project administrator removing the owner's comment")
	want(t, admin.delete("/api/v1/issues/"+issueKey), http.StatusNoContent, "a project administrator deleting an issue")

	// The same role over another project reaches nothing here.
	want(t, owner.post("/api/v1/projects", map[string]string{"name": "Elsewhere", "key": "ELS"}), http.StatusCreated, "another project")
	issueKey, commentID, worklogID = file()
	elsewhere := api.asRole(t, h, owner, "ELS", perm.ProjectAdministrator)
	// As a plain user here they see the issue; what they administer is elsewhere.
	for _, row := range want(t, owner.get("/api/v1/role-assignments"), http.StatusOK, "assignments").Body["assignments"].([]any) {
		a := row.(map[string]any)
		if a["projectKey"] == "ELS" && a["role"] == string(perm.ProjectAdministrator) {
			want(t, owner.post("/api/v1/role-assignments", map[string]any{"role": string(perm.User), "projectKey": "ADM", "userId": a["userId"]}), http.StatusCreated, "user here")
		}
	}
	h.waitForPrimary(t)
	want(t, elsewhere.delete("/api/v1/issues/"+issueKey+"/comments/"+commentID), http.StatusForbidden, "another project's administrator removing a comment")
	want(t, elsewhere.delete("/api/v1/worklogs/"+worklogID), http.StatusForbidden, "another project's administrator removing time")
	want(t, elsewhere.delete("/api/v1/issues/"+issueKey), http.StatusForbidden, "another project's administrator deleting an issue")
}
