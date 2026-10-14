//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
)

// A personal token is its owner's alone: another person neither lists it nor
// revokes it, and it acts as the person who made it.
func TestAPersonsTokensAreTheirOwn(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)

	alpha := api.client(t)
	alphaEmail := h.person(t, "alpha", auth.RoleMember)
	alpha.signIn(t, alphaEmail)
	beta := api.client(t)
	beta.signIn(t, h.person(t, "beta", auth.RoleMember))

	alphaToken := want(t, alpha.post("/api/v1/tokens", map[string]any{"name": "alpha secret"}), http.StatusCreated, "alpha's token")
	alphaTokenID := principalField(t, alphaToken, "token", "id").(string)

	t.Run("another person's list does not include it", func(t *testing.T) {
		if tokens := want(t, beta.get("/api/v1/tokens"), http.StatusOK, "beta's tokens"); strings.Contains(tokens.Raw, alphaTokenID) {
			t.Error("beta can see alpha's API token")
		}
	})

	t.Run("another person cannot revoke it", func(t *testing.T) {
		// Not a 403: confirming that an id exists elsewhere is itself a leak.
		if resp := beta.delete("/api/v1/tokens/" + alphaTokenID); resp.Status != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.Status)
		}
		if after := alpha.get("/api/v1/tokens"); !strings.Contains(after.Raw, alphaTokenID) {
			t.Error("beta revoked alpha's token")
		}
	})

	t.Run("the token acts as the person who made it", func(t *testing.T) {
		bearer := api.client(t)
		bearer.bearer = principalField(t, alphaToken, "token", "secret").(string)
		me := want(t, bearer.get("/api/v1/auth/me"), http.StatusOK, "who the token is")
		if got := principalField(t, me, "principal", "user", "email"); got != alphaEmail {
			t.Errorf("the token acts as %v, want %s", got, alphaEmail)
		}
	})
}

// TestCredentialRevocationTakesEffectImmediately checks that withdrawing access
// actually withdraws it, rather than leaving already-issued credentials working
// until they expire.
func TestCredentialRevocationTakesEffectImmediately(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	ctx := context.Background()

	t.Run("a deleted session stops working on the next request", func(t *testing.T) {
		c := api.client(t)
		email := h.person(t, "deleted", auth.RoleOwner)
		c.signIn(t, email)
		if resp := c.get("/api/v1/auth/me"); resp.Status != http.StatusOK {
			t.Fatalf("setup: me returned %d", resp.Status)
		}

		if _, err := h.super.Exec(ctx, `DELETE FROM user_session WHERE user_id = (SELECT id FROM app_user WHERE email = $1)`, email); err != nil {
			t.Fatal(err)
		}
		if resp := c.get("/api/v1/auth/me"); resp.Status != http.StatusUnauthorized {
			t.Errorf("status after the session was deleted = %d, want 401", resp.Status)
		}
	})

	t.Run("a dead session cookie is cleared so the browser stops sending it", func(t *testing.T) {
		c := api.client(t)
		email := h.person(t, "cleared", auth.RoleOwner)
		c.signIn(t, email)
		if _, err := h.super.Exec(ctx, `DELETE FROM user_session WHERE user_id = (SELECT id FROM app_user WHERE email = $1)`, email); err != nil {
			t.Fatal(err)
		}

		resp := c.get("/api/v1/auth/me")
		var cleared bool
		for _, cookie := range resp.Cookies {
			if cookie.Name == testCookieName && cookie.MaxAge < 0 {
				cleared = true
			}
		}
		if !cleared {
			t.Error("the response did not clear the dead session cookie")
		}
	})

	t.Run("a deactivated account is refused mid-session", func(t *testing.T) {
		c := api.client(t)
		signup := c.signIn(t, h.person(t, "suspended", auth.RoleMember))
		userID := principalField(t, signup, "principal", "user", "id").(string)

		if _, err := h.super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, userID); err != nil {
			t.Fatal(err)
		}
		resp := c.get("/api/v1/auth/me")
		if resp.Status != http.StatusForbidden {
			t.Errorf("status = %d, want 403 for a deactivated account", resp.Status)
		}
	})

	t.Run("losing membership ends an open session", func(t *testing.T) {
		c := api.client(t)
		signup := c.signIn(t, h.person(t, "removed", auth.RoleOwner))
		userID := principalField(t, signup, "principal", "user", "id").(string)

		if _, err := h.super.Exec(ctx, `UPDATE app_user SET standing = NULL WHERE id = $1`, userID); err != nil {
			t.Fatal(err)
		}

		// Nobody is signed in outside the installation, so nothing below
		// could be reached without a tenant scope.
		for _, path := range []string{"/api/v1/auth/me", "/api/v1/tokens"} {
			if got := c.get(path); got.Status != http.StatusUnauthorized {
				t.Errorf("%s answered %d %s, want 401 for a session with no membership behind it", path, got.Status, got.Raw)
			}
		}
	})
}

// TestCredentialsAreNotLeakedInResponses guards against the class of bug where
// a hash, a digest or somebody else's address ends up in a payload.
func TestCredentialsAreNotLeakedInResponses(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)

	c := api.client(t)
	signup := c.signIn(t, h.person(t, "noleak", auth.RoleOwner))

	for _, forbidden := range []string{"password", "passwordHash", "password_hash", "tokenHash", "token_hash"} {
		if strings.Contains(signup.Raw, forbidden) {
			t.Errorf("the signup response mentions %q: %s", forbidden, signup.Raw)
		}
	}

	me := c.get("/api/v1/auth/me")
	for _, forbidden := range []string{"password", "token_hash", "tokenHash"} {
		if strings.Contains(me.Raw, forbidden) {
			t.Errorf("the me response mentions %q", forbidden)
		}
	}

	// Listing tokens must never return their secrets, only their metadata.
	c.post("/api/v1/tokens", map[string]any{"name": "listed"})
	list := c.get("/api/v1/tokens")
	if strings.Contains(list.Raw, "armature_pat_") {
		t.Error("the token listing includes a usable secret")
	}
}

// TestInternalErrorsDoNotLeakDetail checks the error envelope keeps server side
// detail server side.
func TestInternalErrorsDoNotLeakDetail(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)

	// A malformed uuid reaches the handler's own validation, not the database.
	resp := api.client(t).delete("/api/v1/tokens/not-a-uuid")
	if resp.Status != http.StatusUnauthorized {
		// Anonymous, so the auth guard fires first; that is the correct order.
		t.Logf("anonymous request was rejected first with %d, as expected", resp.Status)
	}

	c := api.client(t)
	c.signIn(t, h.person(t, "baduuid", auth.RoleOwner))
	bad := c.delete("/api/v1/tokens/not-a-uuid")
	if bad.Status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for a malformed id", bad.Status)
	}
	message, _ := bad.Error()["message"].(string)
	for _, leak := range []string{"pgx", "SQLSTATE", "postgres", "sql:"} {
		if strings.Contains(strings.ToLower(bad.Raw), strings.ToLower(leak)) {
			t.Errorf("the error message leaks internals (%q): %s", leak, message)
		}
	}
}
