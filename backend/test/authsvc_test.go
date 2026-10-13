//go:build integration

package test

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestOrgSwitching(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	svc := h.authService()
	ctx := context.Background()

	member := h.signup(t, svc, "switcher")
	other := h.signup(t, svc, "elsewhere")

	t.Run("refuses an organization the user does not belong to", func(t *testing.T) {
		_, _, err := svc.SwitchOrg(ctx, *member.Principal.SessionID, member.Principal.User.ID, other.Principal.Org.Slug)
		if !errors.Is(err, auth.ErrNotAMember) {
			t.Fatalf("error = %v, want ErrNotAMember", err)
		}
	})

	t.Run("refuses an organization that does not exist", func(t *testing.T) {
		_, _, err := svc.SwitchOrg(ctx, *member.Principal.SessionID, member.Principal.User.ID, "no-such-org")
		if !errors.Is(err, auth.ErrNotAMember) {
			t.Fatalf("error = %v, want ErrNotAMember", err)
		}
	})

	t.Run("switches into a second organization once invited", func(t *testing.T) {
		// Invite the switcher into the other organization and accept it.
		otherCtx := auth.ContextForOrg(ctx, other.Principal)
		_, token, _, err := svc.CreateInvite(otherCtx, member.Principal.User.Email, auth.RoleMember, other.Principal.User.ID, time.Hour)
		if err != nil {
			t.Fatalf("create invite: %v", err)
		}
		if _, err := svc.AcceptInvite(ctx, auth.AcceptInviteInput{
			Secret: token,
			UserID: &member.Principal.User.ID,
			Proof:  auth.ProofPassword,
		}); err != nil {
			t.Fatalf("accept invite: %v", err)
		}

		org, lsn, err := svc.SwitchOrg(ctx, *member.Principal.SessionID, member.Principal.User.ID, other.Principal.Org.Slug)
		if err != nil {
			t.Fatalf("switch: %v", err)
		}
		if org.Slug != other.Principal.Org.Slug {
			t.Errorf("switched into %q, want %q", org.Slug, other.Principal.Org.Slug)
		}
		if lsn == 0 {
			t.Error("switching returned no log position")
		}

		// The change must be visible through the session, not merely returned.
		principal, err := svc.Authenticate(ctx, member.SessionSecret)
		if err != nil {
			t.Fatal(err)
		}
		if principal.Org == nil || principal.Org.Slug != other.Principal.Org.Slug {
			t.Errorf("the session is still in %v after switching", principal.Org)
		}
	})

	t.Run("memberships list every organization the user is in", func(t *testing.T) {
		memberships, err := svc.Memberships(ctx, member.Principal.User.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(memberships) != 2 {
			t.Fatalf("got %d memberships, want 2", len(memberships))
		}
		// Ordered by when they were joined, so the original organization is first
		// and stays the stable default.
		if memberships[0].OrgSlug != member.Principal.Org.Slug {
			t.Errorf("first membership is %q, want the original %q", memberships[0].OrgSlug, member.Principal.Org.Slug)
		}
		if memberships[0].Role != auth.RoleOwner || memberships[1].Role != auth.RoleMember {
			t.Errorf("roles = %q, %q; want owner then member", memberships[0].Role, memberships[1].Role)
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
