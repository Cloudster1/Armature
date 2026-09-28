//go:build integration

package test

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/armature/armature/backend/internal/oidc"
)

// Somebody the provider vouches for is still refused, but the refusal is kept
// where an administrator can turn it into a membership with one click.
func TestAStrangerAtTheProviderCanBeLetInFromTheUsersPage(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	ws := h.newWorkspace(t, "letin")
	provider := newIDP(t)
	svc, clientID := configured(t, h, ws, provider, false)

	owner := api.client(t)
	base, _ := url.Parse(api.URL)
	owner.http.Jar.SetCookies(base, []*http.Cookie{{Name: testCookieName, Value: ws.owner.SessionSecret}})

	email := h.email(t, "stranger")
	claims := map[string]any{"email": email, "name": "A Stranger"}
	if _, err := signInWith(t, h, svc, ws, provider, clientID, claims); !errors.Is(err, oidc.ErrNotAMember) {
		t.Fatalf("error = %v, want them refused", err)
	}
	// Asking twice is one request.
	if _, err := signInWith(t, h, svc, ws, provider, clientID, claims); !errors.Is(err, oidc.ErrNotAMember) {
		t.Fatalf("second error = %v, want them refused", err)
	}

	h.waitForPrimary(t)
	listed := want(t, owner.get("/api/v1/users/requests"), http.StatusOK, "list requests")
	requests := list(t, listed, "requests")
	if len(requests) != 1 {
		t.Fatalf("requests = %s, want the one stranger", listed.Raw)
	}
	request := requests[0].(map[string]any)
	if request["email"] != email || request["name"] != "A Stranger" {
		t.Fatalf("request = %v", request)
	}
	userID := request["userId"].(string)

	// A member may not see or decide this.
	member := api.client(t)
	memberEmail := h.email(t, "plain")
	want(t, owner.post("/api/v1/users", map[string]string{"email": memberEmail, "name": "Plain Member", "role": "member", "password": testPassword}), http.StatusCreated, "make a member")
	want(t, member.post("/api/v1/auth/login", map[string]string{"email": memberEmail, "password": testPassword}), http.StatusOK, "member signs in")
	want(t, member.get("/api/v1/users/requests"), http.StatusForbidden, "member lists requests")
	want(t, member.post("/api/v1/users/requests/"+userID+"/admit", map[string]string{"role": "member"}), http.StatusForbidden, "member admits")
	want(t, member.delete("/api/v1/users/requests/"+userID), http.StatusForbidden, "member declines")

	t.Run("turned away, they may ask again", func(t *testing.T) {
		want(t, owner.delete("/api/v1/users/requests/"+userID), http.StatusNoContent, "decline")
		want(t, owner.delete("/api/v1/users/requests/"+userID), http.StatusNotFound, "decline twice")
		h.waitForPrimary(t)
		if got := list(t, want(t, owner.get("/api/v1/users/requests"), http.StatusOK, "list"), "requests"); len(got) != 0 {
			t.Fatalf("a declined request is still listed: %v", got)
		}
		if _, err := signInWith(t, h, svc, ws, provider, clientID, claims); !errors.Is(err, oidc.ErrNotAMember) {
			t.Fatalf("error = %v, want them refused", err)
		}
	})

	t.Run("let in, their next sign-in succeeds", func(t *testing.T) {
		h.waitForPrimary(t)
		if got := owner.post("/api/v1/users/requests/"+userID+"/admit", map[string]string{"role": "owner"}); got.Status != http.StatusUnprocessableEntity {
			t.Fatalf("admitted as owner: %d %s", got.Status, got.Raw)
		}
		admitted := want(t, owner.post("/api/v1/users/requests/"+userID+"/admit", map[string]string{"role": "member"}), http.StatusOK, "admit")
		user := obj(t, admitted, "user")
		if user["role"] != "member" || user["signsInWith"] != "provider" {
			t.Fatalf("admitted user = %s", admitted.Raw)
		}
		want(t, owner.post("/api/v1/users/requests/"+userID+"/admit", map[string]string{"role": "member"}), http.StatusNotFound, "admit twice")

		session, err := signInWith(t, h, svc, ws, provider, clientID, claims)
		if err != nil {
			t.Fatalf("sign in after being let in: %v", err)
		}
		if session.OrgID != ws.orgID {
			t.Fatalf("session in %s, want %s", session.OrgID, ws.orgID)
		}
	})
}
