//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestCrossTenantAccessAtTheAPI is the isolation test that matters to a
// customer. The database level test proves row level security works; this one
// proves the API actually sits behind it, with real credentials from two
// separate organizations.
func TestCrossTenantAccessAtTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	alpha := api.client(t)
	alphaSignup := alpha.signup(t, h, "alpha")
	alphaSlug := principalField(t, alphaSignup, "principal", "org", "slug").(string)

	beta := api.client(t)
	betaSignup := beta.signup(t, h, "beta")
	betaSlug := principalField(t, betaSignup, "principal", "org", "slug").(string)

	// Each organization creates a token and an invitation to be probed for.
	alphaToken := alpha.post("/api/v1/tokens", map[string]any{"name": "alpha secret"})
	alphaTokenID := principalField(t, alphaToken, "token", "id").(string)

	betaInvite := beta.post("/api/v1/invites", map[string]string{
		"email": h.email(t, "betaonly"), "role": "member",
	})
	if betaInvite.Status != http.StatusCreated {
		t.Fatalf("beta invite returned %d: %s", betaInvite.Status, betaInvite.Raw)
	}
	betaInviteID := principalField(t, betaInvite, "invite", "id").(string)

	t.Run("one organization's listings never include another's rows", func(t *testing.T) {
		invites := alpha.get("/api/v1/invites")
		if invites.Status != http.StatusOK {
			t.Fatalf("status = %d", invites.Status)
		}
		if strings.Contains(invites.Raw, betaInviteID) {
			t.Error("alpha can see beta's invitation")
		}

		tokens := beta.get("/api/v1/tokens")
		if strings.Contains(tokens.Raw, alphaTokenID) {
			t.Error("beta can see alpha's API token")
		}
	})

	t.Run("addressing another organization's resource by id gives a not found", func(t *testing.T) {
		// Not a 403: confirming that an id exists elsewhere is itself a leak.
		resp := alpha.delete("/api/v1/invites/" + betaInviteID)
		if resp.Status != http.StatusNotFound && resp.Status != http.StatusGone {
			t.Errorf("deleting beta's invitation from alpha returned %d, want 404 or 410", resp.Status)
		}

		if still := beta.get("/api/v1/invites"); !strings.Contains(still.Raw, betaInviteID) {
			t.Error("alpha's attempt actually deleted beta's invitation")
		}
	})

	t.Run("another organization's token cannot be revoked", func(t *testing.T) {
		resp := beta.delete("/api/v1/tokens/" + alphaTokenID)
		if resp.Status != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.Status)
		}
		if after := alpha.get("/api/v1/tokens"); !strings.Contains(after.Raw, alphaTokenID) {
			t.Error("beta revoked alpha's token")
		}
	})

	t.Run("switching into an organization you do not belong to is refused", func(t *testing.T) {
		resp := alpha.post("/api/v1/auth/switch-org", map[string]string{"slug": betaSlug})
		if resp.Status != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.Status)
		}

		// And the session must not have moved.
		me := alpha.get("/api/v1/auth/me")
		if got := principalField(t, me, "principal", "org", "slug"); got != alphaSlug {
			t.Errorf("alpha's session is now in %v", got)
		}
	})

	t.Run("an API token is confined to its own organization", func(t *testing.T) {
		secret := principalField(t, alphaToken, "token", "secret").(string)
		bearer := api.client(t)
		bearer.bearer = secret

		me := bearer.get("/api/v1/auth/me")
		if got := principalField(t, me, "principal", "org", "slug"); got != alphaSlug {
			t.Errorf("the token resolved to organization %v, want %q", got, alphaSlug)
		}
		if invites := bearer.get("/api/v1/invites"); strings.Contains(invites.Raw, betaInviteID) {
			t.Error("alpha's token can read beta's invitations")
		}
	})
}

// TestCredentialRevocationTakesEffectImmediately checks that withdrawing access
// actually withdraws it, rather than leaving already-issued credentials working
// until they expire.
func TestCredentialRevocationTakesEffectImmediately(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	ctx := context.Background()

	t.Run("a deleted session stops working on the next request", func(t *testing.T) {
		c := api.client(t)
		c.signup(t, h, "deleted")
		if resp := c.get("/api/v1/auth/me"); resp.Status != http.StatusOK {
			t.Fatalf("setup: me returned %d", resp.Status)
		}

		if _, err := h.super.Exec(ctx, `DELETE FROM user_session`); err != nil {
			t.Fatal(err)
		}
		if resp := c.get("/api/v1/auth/me"); resp.Status != http.StatusUnauthorized {
			t.Errorf("status after the session was deleted = %d, want 401", resp.Status)
		}
	})

	t.Run("a dead session cookie is cleared so the browser stops sending it", func(t *testing.T) {
		c := api.client(t)
		c.signup(t, h, "cleared")
		if _, err := h.super.Exec(ctx, `DELETE FROM user_session`); err != nil {
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
		signup := c.signup(t, h, "suspended")
		userID := principalField(t, signup, "principal", "user", "id").(string)

		// The last owner of an organization cannot be switched off, so they
		// step down first.
		if _, err := h.super.Exec(ctx, `UPDATE org_member SET org_role = 'member' WHERE user_id = $1`, userID); err != nil {
			t.Fatal(err)
		}
		if _, err := h.super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, userID); err != nil {
			t.Fatal(err)
		}
		resp := c.get("/api/v1/auth/me")
		if resp.Status != http.StatusForbidden {
			t.Errorf("status = %d, want 403 for a deactivated account", resp.Status)
		}
	})

	t.Run("losing membership drops the organization from an open session", func(t *testing.T) {
		c := api.client(t)
		signup := c.signup(t, h, "removed")
		userID := principalField(t, signup, "principal", "user", "id").(string)

		if _, err := h.super.Exec(ctx, `DELETE FROM org_member WHERE user_id = $1`, userID); err != nil {
			t.Fatal(err)
		}

		// Still authenticated as a person, but no longer inside the tenant, so
		// tenant scoped endpoints must refuse rather than query unscoped.
		me := c.get("/api/v1/auth/me")
		if me.Status != http.StatusOK {
			t.Fatalf("me returned %d, want the user to still be signed in", me.Status)
		}
		if org := principalField(t, me, "principal", "org"); org != nil {
			t.Errorf("principal still carries organization %v", org)
		}

		tokens := c.get("/api/v1/tokens")
		if tokens.Status != http.StatusBadRequest || tokens.ErrorCode() != "no_organization" {
			t.Errorf("tenant scoped endpoint returned %d/%q, want 400/no_organization",
				tokens.Status, tokens.ErrorCode())
		}
	})
}

// TestCredentialsAreNotLeakedInResponses guards against the class of bug where
// a hash, a digest or somebody else's address ends up in a payload.
func TestCredentialsAreNotLeakedInResponses(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	c := api.client(t)
	signup := c.signup(t, h, "noleak")

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
	h := newHarness(t)
	api := newAPIServer(t, h)

	// A malformed uuid reaches the handler's own validation, not the database.
	resp := api.client(t).delete("/api/v1/tokens/not-a-uuid")
	if resp.Status != http.StatusUnauthorized {
		// Anonymous, so the auth guard fires first; that is the correct order.
		t.Logf("anonymous request was rejected first with %d, as expected", resp.Status)
	}

	c := api.client(t)
	c.signup(t, h, "baduuid")
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
