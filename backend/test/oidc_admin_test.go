//go:build integration

package test

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/armature/armature/backend/internal/oidc"
)

// Somebody who came in through the identity provider, was let in, and was
// then made a global administrator with the joining role taken away, can
// author a workflow. Reported as failing on a deployment; tried here in
// exactly that order.
func TestAProviderAccountMadeGlobalAdministratorCanAuthorWorkflows(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	ws := h.newWorkspace(t, "oidcadmin")
	provider := newIDP(t)
	svc, clientID := configured(t, h, ws, provider, false)

	owner := api.client(t)
	base, _ := url.Parse(api.URL)
	owner.http.Jar.SetCookies(base, []*http.Cookie{{Name: testCookieName, Value: ws.owner.SessionSecret}})

	email := h.email(t, "colleague")
	claims := map[string]any{"email": email, "name": "A Colleague"}
	if _, err := signInWith(t, h, svc, ws, provider, clientID, claims); !errors.Is(err, oidc.ErrNotAMember) {
		t.Fatalf("error = %v, want them refused until let in", err)
	}
	h.waitForPrimary(t)
	requests := list(t, want(t, owner.get("/api/v1/users/requests"), http.StatusOK, "requests"), "requests")
	colleagueID := requests[0].(map[string]any)["userId"].(string)
	want(t, owner.post("/api/v1/users/requests/"+colleagueID+"/admit", map[string]string{"role": "member"}), http.StatusOK, "admit")

	// The Access page: global administrator granted, the joining role revoked.
	want(t, owner.post("/api/v1/role-assignments", map[string]any{"role": "global_administrator", "userId": colleagueID}), http.StatusCreated, "grant")
	h.waitForPrimary(t)
	for _, row := range list(t, want(t, owner.get("/api/v1/role-assignments"), http.StatusOK, "assignments"), "assignments") {
		a := row.(map[string]any)
		if a["userId"] == colleagueID && a["role"] == "user" {
			want(t, owner.delete("/api/v1/role-assignments/"+a["id"].(string)), http.StatusNoContent, "revoke user")
		}
	}

	session, err := signInWith(t, h, svc, ws, provider, clientID, claims)
	if err != nil {
		t.Fatalf("sign in through the provider: %v", err)
	}
	colleague := api.client(t)
	colleague.http.Jar.SetCookies(base, []*http.Cookie{{Name: testCookieName, Value: session.Secret}})
	h.waitForPrimary(t)

	access := want(t, colleague.get("/api/v1/access/me"), http.StatusOK, "access")
	if access.Body["canAdministerOrg"] != true {
		t.Fatalf("the colleague is not offered administration: %s", access.Raw)
	}
	statuses := colleague.get("/api/v1/statuses")
	review := find(t, statuses.Body["statuses"], "name", "In Review")["id"].(string)
	done := find(t, statuses.Body["statuses"], "name", "Done")["id"].(string)
	made := colleague.post("/api/v1/workflows", map[string]any{
		"name":        "Colleague's flow",
		"steps":       []map[string]any{{"statusId": review, "isInitial": true}, {"statusId": done}},
		"transitions": []map[string]any{{"name": "Resolve", "fromStatusId": review, "toStatusId": done}},
	})
	if made.Status != http.StatusCreated {
		t.Fatalf("the colleague could not author a workflow: %d %s", made.Status, made.Raw)
	}
	workflowID := obj(t, made, "workflow")["id"].(string)
	saved := colleague.put("/api/v1/workflows/"+workflowID, map[string]any{
		"name":        "Colleague's flow, renamed",
		"steps":       []map[string]any{{"statusId": review, "isInitial": true}, {"statusId": done}},
		"transitions": []map[string]any{{"name": "Resolve", "fromStatusId": review, "toStatusId": done}},
	})
	if saved.Status != http.StatusOK {
		t.Fatalf("the colleague could not edit a workflow: %d %s", saved.Status, saved.Raw)
	}
}
