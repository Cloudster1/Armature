//go:build integration

package test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
)

func TestLogin(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	svc := h.authService()
	ctx := context.Background()

	creds := h.signup(t, svc, "login")
	email := creds.Principal.User.Email

	t.Run("succeeds with the right password", func(t *testing.T) {
		got, err := svc.Login(ctx, email, testPassword, "test-agent", "203.0.113.5")
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		if got.Principal.User.ID != creds.Principal.User.ID {
			t.Error("login resolved to a different user")
		}
		if got.Principal.Org == nil {
			t.Error("login did not select the user's only organization")
		}
		if got.SessionSecret == creds.SessionSecret {
			t.Error("login reused the signup session secret instead of issuing a new one")
		}
	})

	t.Run("is case insensitive about the address", func(t *testing.T) {
		if _, err := svc.Login(ctx, upper(email), testPassword, "", ""); err != nil {
			t.Errorf("login with a differently cased address failed: %v", err)
		}
	})

	t.Run("rejects a wrong password", func(t *testing.T) {
		_, err := svc.Login(ctx, email, "definitely not the password", "", "")
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("error = %v, want ErrInvalidCredentials", err)
		}
	})

	// The same error for both cases is what stops the endpoint being used to
	// discover which addresses have accounts.
	t.Run("gives the same answer for an unknown address", func(t *testing.T) {
		_, err := svc.Login(ctx, "nobody-"+unique("x")+"@armature.test", testPassword, "", "")
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("error = %v, want the same ErrInvalidCredentials as a wrong password", err)
		}
	})

	t.Run("refuses a deactivated account", func(t *testing.T) {
		deactivated := h.signup(t, svc, "deactivated")
		// The last owner of an organization cannot be switched off, so they
		// step down first.
		if _, err := h.super.Exec(ctx, `UPDATE org_member SET org_role = 'member' WHERE user_id = $1`, deactivated.Principal.User.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := h.super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, deactivated.Principal.User.ID); err != nil {
			t.Fatal(err)
		}
		_, err := svc.Login(ctx, deactivated.Principal.User.Email, testPassword, "", "")
		if !errors.Is(err, auth.ErrUserInactive) {
			t.Fatalf("error = %v, want ErrUserInactive", err)
		}
	})
}

func TestSessionLifecycle(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	svc := h.authService()
	ctx := context.Background()

	t.Run("logout invalidates the session", func(t *testing.T) {
		creds := h.signup(t, svc, "logout")
		if err := svc.Logout(ctx, *creds.Principal.SessionID); err != nil {
			t.Fatalf("logout: %v", err)
		}
		if _, err := svc.Authenticate(ctx, creds.SessionSecret); !errors.Is(err, auth.ErrInvalidToken) {
			t.Fatalf("error after logout = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("logout is idempotent", func(t *testing.T) {
		creds := h.signup(t, svc, "twice")
		if err := svc.Logout(ctx, *creds.Principal.SessionID); err != nil {
			t.Fatal(err)
		}
		if err := svc.Logout(ctx, *creds.Principal.SessionID); err != nil {
			t.Errorf("second logout returned %v, want nil", err)
		}
	})

	t.Run("an expired session does not authenticate", func(t *testing.T) {
		creds := h.signup(t, svc, "expired")
		if _, err := h.super.Exec(ctx,
			`UPDATE user_session SET expires_at = now() - interval '1 second' WHERE id = $1`,
			*creds.Principal.SessionID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Authenticate(ctx, creds.SessionSecret); !errors.Is(err, auth.ErrInvalidToken) {
			t.Fatalf("error = %v, want ErrInvalidToken for an expired session", err)
		}
	})

	t.Run("an unknown secret does not authenticate", func(t *testing.T) {
		if _, err := svc.Authenticate(ctx, "not-a-real-token"); !errors.Is(err, auth.ErrInvalidToken) {
			t.Fatalf("error = %v, want ErrInvalidToken", err)
		}
		if _, err := svc.Authenticate(ctx, ""); !errors.Is(err, auth.ErrInvalidToken) {
			t.Fatalf("error for an empty secret = %v, want ErrInvalidToken", err)
		}
	})

	// The secret is never stored, only its digest, so a database disclosure
	// does not hand out working sessions.
	t.Run("the session secret is not stored", func(t *testing.T) {
		creds := h.signup(t, svc, "digest")
		var n int
		if err := h.super.QueryRow(ctx,
			`SELECT count(*) FROM user_session WHERE encode(token_hash, 'escape') LIKE '%' || $1 || '%'`,
			creds.SessionSecret).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Error("the session secret appears verbatim in the database")
		}
	})

	t.Run("revoking a membership ends an open session", func(t *testing.T) {
		creds := h.signup(t, svc, "revoked")
		if _, err := h.super.Exec(ctx, `DELETE FROM org_member WHERE user_id = $1`, creds.Principal.User.ID); err != nil {
			t.Fatal(err)
		}

		if principal, err := svc.Authenticate(ctx, creds.SessionSecret); !errors.Is(err, auth.ErrInvalidToken) {
			t.Fatalf("a session whose membership was revoked authenticated as %+v, %v; want ErrInvalidToken", principal, err)
		}
	})
}

func upper(s string) string {
	out := []rune(s)
	for i, r := range out {
		if r >= 'a' && r <= 'z' {
			out[i] = r - ('a' - 'A')
		}
	}
	return string(out)
}

var (
	_ = uuid.Nil
	_ = db.LSN(0)
)
