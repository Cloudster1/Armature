//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// A sign-in lands in the installation: the session acts in the person's
// organization without naming one, and there is nothing to switch to.
func TestASignInLandsInTheInstallation(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	ctx := context.Background()

	owner := api.client(t)
	signedUp := owner.signup(t, h, "lands")
	orgID := principalField(t, signedUp, "principal", "org", "id").(string)
	email := principalField(t, signedUp, "principal", "user", "email").(string)
	userID := principalField(t, signedUp, "principal", "user", "id").(string)
	in := func(t *testing.T, c *client) string {
		t.Helper()
		return principalField(t, want(t, c.get("/api/v1/auth/me"), http.StatusOK, "who is signed in"), "principal", "org", "id").(string)
	}

	t.Run("the session acts in the person's organization, and nothing lists others", func(t *testing.T) {
		me := want(t, owner.get("/api/v1/auth/me"), http.StatusOK, "who is signed in")
		if got := principalField(t, me, "principal", "org", "id"); got != orgID {
			t.Errorf("the session acts in %v, want %v", got, orgID)
		}
		if listed, ok := me.Body["organizations"]; ok {
			t.Errorf("/auth/me still lists organizations to switch to: %v", listed)
		}
	})

	t.Run("a session names no organization, and the database keeps it so", func(t *testing.T) {
		var named int
		if err := h.super.QueryRow(ctx, `
			SELECT count(*) FROM user_session
			WHERE user_id = $1 AND (current_org_id IS NOT NULL OR proof_org_id IS NOT NULL)`, userID).Scan(&named); err != nil {
			t.Fatal(err)
		}
		if named != 0 {
			t.Errorf("%d sessions name an organization", named)
		}
		_, err := h.super.Exec(ctx, `UPDATE user_session SET current_org_id = $2 WHERE user_id = $1`, userID, orgID)
		refusedByCheck(t, err, "a session pointed at an organization")
		_, err = h.super.Exec(ctx, `UPDATE user_session SET proof_org_id = $2 WHERE user_id = $1`, userID, orgID)
		refusedByCheck(t, err, "a session's proof tied to an organization")
	})

	t.Run("a membership made later moves no session, and there is no switching to it", func(t *testing.T) {
		var elsewhere string
		if err := h.super.QueryRow(ctx, `
			INSERT INTO org_member (org_id, user_id, org_role)
			SELECT m.org_id, $2, 'member' FROM org_member m JOIN app_user u ON u.id = m.user_id WHERE u.email = $1
			RETURNING (SELECT slug FROM org WHERE id = org_id)`,
			h.foundOrganization(t, "landsaway", "Elsewhere"), userID).Scan(&elsewhere); err != nil {
			t.Fatal(err)
		}
		if got := owner.post("/api/v1/auth/switch-org", map[string]string{"slug": elsewhere}); got.Status != http.StatusNotFound {
			t.Errorf("switching to the other organization answered %d %s, want 404: there is no switching", got.Status, got.Raw)
		}
		if got := in(t, owner); got != orgID {
			t.Errorf("the open session moved to %v, want %v", got, orgID)
		}
		again := api.client(t)
		want(t, again.post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusOK, "sign in again")
		if got := in(t, again); got != orgID {
			t.Errorf("a new sign-in landed in %v, want the organization joined first, %v", got, orgID)
		}
	})
}

// Single sign-on starts at one address, the installation's provider's, and
// the address that named an organization sends the browser there for a release.
func TestSingleSignOnStartsAtOneAddress(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api, _ := emptyInstallation(t, h, "armature_one_provider_test")
	browser := api.client(t).noRedirects()
	get := func(path string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, api.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, _ := browser.raw(req)
		return resp
	}

	if resp := get("/api/v1/auth/oidc/start"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("a sign-in with no provider answered %d, want 404", resp.StatusCode)
	}

	owner := api.client(t)
	email := h.email(t, "provider-owner")
	want(t, owner.post("/api/v1/auth/signup", map[string]string{"email": email, "password": testPassword, "name": "Provider Owner"}), http.StatusCreated, "set the installation up")
	installation := principalField(t, owner.get("/api/v1/auth/me"), "principal", "org", "id")
	provider := newIDP(t)
	want(t, owner.put("/api/v1/oidc-provider", map[string]any{
		"issuer": provider.URL, "clientId": "armature-test", "clientSecret": "shh", "groupsClaim": "groups",
		"scopes": "openid profile email", "createGroups": false, "enabled": true,
	}), http.StatusOK, "configure the provider")

	t.Run("the old address names an organization and is sent to the new one", func(t *testing.T) {
		resp := get("/api/v1/auth/oidc/some-company/start?next=" + url.QueryEscape("/projects"))
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("the old address answered %d, want 302", resp.StatusCode)
		}
		target, err := url.Parse(resp.Header.Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		if target.Path != "/api/v1/auth/oidc/start" || target.Query().Get("next") != "/projects" {
			t.Errorf("the old address sends the browser to %q, want the new one carrying where it was going", resp.Header.Get("Location"))
		}
	})

	t.Run("a sign-in starts at the new address and lands in the installation", func(t *testing.T) {
		resp := get("/api/v1/auth/oidc/start?next=" + url.QueryEscape("/projects"))
		if resp.StatusCode != http.StatusFound || !strings.HasPrefix(resp.Header.Get("Location"), provider.URL) {
			t.Fatalf("the start answered %d to %q, want the provider", resp.StatusCode, resp.Header.Get("Location"))
		}
		state, nonce := stateFrom(t, resp.Header.Get("Location"))
		provider.says("armature-test", map[string]any{"nonce": nonce, "email": email, "sub": "subject-" + uuid.NewString()})
		back := get("/api/v1/auth/oidc/callback?" + url.Values{"state": {state}, "code": {"any-code"}}.Encode())
		if back.StatusCode != http.StatusFound || !strings.HasSuffix(back.Header.Get("Location"), "/projects") {
			t.Fatalf("the callback answered %d to %q, want the projects page", back.StatusCode, back.Header.Get("Location"))
		}
		me := want(t, browser.get("/api/v1/auth/me"), http.StatusOK, "signed in through the provider")
		if got := principalField(t, me, "principal", "org", "id"); got != installation {
			t.Errorf("the provider's sign-in acts in %v, want the installation %v", got, installation)
		}
	})
}
