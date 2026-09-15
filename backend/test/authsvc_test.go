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

func TestSignup(t *testing.T) {
	h := newHarness(t)
	svc := h.authService()
	ctx := context.Background()

	t.Run("creates the user, the organization and the owner membership together", func(t *testing.T) {
		creds := h.signup(t, svc, "founder")

		if creds.Principal.Org == nil {
			t.Fatal("signup returned a principal with no organization")
		}
		if creds.Principal.Role != auth.RoleOwner {
			t.Errorf("role = %q, want owner", creds.Principal.Role)
		}
		if creds.SessionSecret == "" {
			t.Error("signup did not open a session")
		}
		if creds.LSN == 0 {
			t.Error("signup returned no log position, so the new user's first read cannot be pinned")
		}
		if !creds.ExpiresAt.After(time.Now()) {
			t.Error("session expiry is not in the future")
		}
	})

	t.Run("the new session authenticates immediately", func(t *testing.T) {
		creds := h.signup(t, svc, "immediate")

		principal, err := svc.Authenticate(ctx, creds.SessionSecret)
		if err != nil {
			t.Fatalf("the session returned by signup does not authenticate: %v", err)
		}
		if principal.User.ID != creds.Principal.User.ID {
			t.Error("the session resolved to a different user")
		}
		if principal.Org == nil || principal.Org.ID != creds.Principal.Org.ID {
			t.Error("the session is not scoped to the organization that was just created")
		}
	})

	t.Run("rejects a duplicate email", func(t *testing.T) {
		first := h.signup(t, svc, "duplicate")

		_, err := svc.Signup(ctx, auth.SignupInput{
			Email:    first.Principal.User.Email,
			Password: testPassword,
			Name:     "Someone Else",
			OrgName:  "Another Company",
			OrgSlug:  h.orgSlug(t, "another"),
		})
		if !errors.Is(err, auth.ErrEmailTaken) {
			t.Fatalf("error = %v, want ErrEmailTaken", err)
		}
	})

	t.Run("rejects a duplicate organization address", func(t *testing.T) {
		first := h.signup(t, svc, "sameslug")

		_, err := svc.Signup(ctx, auth.SignupInput{
			Email:    h.email(t, "other"),
			Password: testPassword,
			Name:     "Other Person",
			OrgName:  "Other Company",
			OrgSlug:  first.Principal.Org.Slug,
		})
		if !errors.Is(err, auth.ErrSlugTaken) {
			t.Fatalf("error = %v, want ErrSlugTaken", err)
		}
	})

	// A failed signup must leave nothing behind. Creating the user, the
	// organization and the membership in one transaction is what guarantees it.
	t.Run("a rejected signup leaves no orphaned account", func(t *testing.T) {
		taken := h.signup(t, svc, "orphan")
		email := h.email(t, "wouldbe")

		_, err := svc.Signup(ctx, auth.SignupInput{
			Email:    email,
			Password: testPassword,
			Name:     "Would Be",
			OrgName:  "Would Be Company",
			OrgSlug:  taken.Principal.Org.Slug, // collides, so the whole thing rolls back
		})
		if !errors.Is(err, auth.ErrSlugTaken) {
			t.Fatalf("error = %v, want ErrSlugTaken", err)
		}

		var n int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM app_user WHERE email = $1`, email).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("the rolled back signup left %d user rows behind", n)
		}
	})

	t.Run("validates its input", func(t *testing.T) {
		cases := map[string]auth.SignupInput{
			"no email":        {Password: testPassword, Name: "A", OrgName: "B"},
			"no name":         {Email: "a@armature.test", Password: testPassword, OrgName: "B"},
			"no organization": {Email: "a@armature.test", Password: testPassword, Name: "A"},
			"short password":  {Email: "a@armature.test", Password: "short", Name: "A", OrgName: "B"},
			"unusable slug":   {Email: "a@armature.test", Password: testPassword, Name: "A", OrgName: "B", OrgSlug: "Not A Slug"},
		}
		for name, in := range cases {
			if _, err := svc.Signup(ctx, in); err == nil {
				t.Errorf("%s: signup succeeded, want a validation error", name)
			}
		}
	})

	t.Run("derives an organization address when none is given", func(t *testing.T) {
		slug := h.orgSlug(t, "derived")
		creds, err := svc.Signup(ctx, auth.SignupInput{
			Email:    h.email(t, "derive"),
			Password: testPassword,
			Name:     "Deriver",
			// Slugify lowercases and hyphenates, so this yields the slug above.
			OrgName: slug,
		})
		if err != nil {
			t.Fatalf("signup: %v", err)
		}
		if creds.Principal.Org.Slug != slug {
			t.Errorf("slug = %q, want %q derived from the organization name", creds.Principal.Org.Slug, slug)
		}
	})
}

func TestLogin(t *testing.T) {
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

	t.Run("revoking a membership locks an open session out of the organization", func(t *testing.T) {
		creds := h.signup(t, svc, "revoked")
		if _, err := h.super.Exec(ctx, `DELETE FROM org_member WHERE user_id = $1`, creds.Principal.User.ID); err != nil {
			t.Fatal(err)
		}

		principal, err := svc.Authenticate(ctx, creds.SessionSecret)
		if err != nil {
			t.Fatalf("authenticate: %v", err)
		}
		if principal.InOrg() {
			t.Error("a user whose membership was revoked still has an organization on their session")
		}
	})
}

func TestOrgSwitching(t *testing.T) {
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
		_, token, err := svc.CreateInvite(otherCtx, member.Principal.User.Email, auth.RoleMember, other.Principal.User.ID, time.Hour)
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
