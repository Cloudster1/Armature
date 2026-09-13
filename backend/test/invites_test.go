//go:build integration

package test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
)

func TestInvites(t *testing.T) {
	h := newHarness(t)
	svc := h.authService()
	ctx := context.Background()

	owner := h.signup(t, svc, "inviter")
	orgCtx := auth.ContextForOrg(ctx, owner.Principal)

	t.Run("a new account can join through an invitation", func(t *testing.T) {
		invitee := h.email(t, "joiner")
		invite, token, err := svc.CreateInvite(orgCtx, invitee, auth.RoleMember, owner.Principal.User.ID, time.Hour)
		if err != nil {
			t.Fatalf("create invite: %v", err)
		}
		if invite.OrgID != owner.Principal.Org.ID {
			t.Error("the invitation was created in the wrong organization")
		}
		if token == "" {
			t.Fatal("no invitation secret was returned")
		}

		creds, err := svc.AcceptInvite(ctx, auth.AcceptInviteInput{
			Secret:   token,
			Name:     "New Joiner",
			Password: testPassword,
		})
		if err != nil {
			t.Fatalf("accept invite: %v", err)
		}
		if creds.Principal.Role != auth.RoleMember {
			t.Errorf("role = %q, want member as the invitation specified", creds.Principal.Role)
		}
		if creds.Principal.Org.ID != owner.Principal.Org.ID {
			t.Error("the invitee landed in a different organization")
		}
		// They are signed in straight away, with no separate login step.
		if _, err := svc.Authenticate(ctx, creds.SessionSecret); err != nil {
			t.Errorf("the session opened by accepting an invitation does not work: %v", err)
		}
	})

	t.Run("an invitation cannot be used twice", func(t *testing.T) {
		invitee := h.email(t, "reuse")
		_, token, err := svc.CreateInvite(orgCtx, invitee, auth.RoleMember, owner.Principal.User.ID, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.AcceptInvite(ctx, auth.AcceptInviteInput{
			Secret: token, Name: "First Use", Password: testPassword,
		}); err != nil {
			t.Fatalf("first acceptance: %v", err)
		}

		_, err = svc.AcceptInvite(ctx, auth.AcceptInviteInput{
			Secret: token, Name: "Second Use", Password: testPassword,
		})
		if !errors.Is(err, auth.ErrInviteInvalid) {
			t.Fatalf("error on reuse = %v, want ErrInviteInvalid", err)
		}
	})

	t.Run("an expired invitation is refused", func(t *testing.T) {
		invitee := h.email(t, "stale")
		invite, token, err := svc.CreateInvite(orgCtx, invitee, auth.RoleMember, owner.Principal.User.ID, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.super.Exec(ctx,
			`UPDATE org_invite SET expires_at = now() - interval '1 second' WHERE id = $1`, invite.ID); err != nil {
			t.Fatal(err)
		}

		_, err = svc.AcceptInvite(ctx, auth.AcceptInviteInput{
			Secret: token, Name: "Too Late", Password: testPassword,
		})
		if !errors.Is(err, auth.ErrInviteInvalid) {
			t.Fatalf("error = %v, want ErrInviteInvalid", err)
		}
	})

	t.Run("an unknown invitation secret is refused", func(t *testing.T) {
		_, err := svc.AcceptInvite(ctx, auth.AcceptInviteInput{
			Secret: "not-a-real-invitation", Name: "Nobody", Password: testPassword,
		})
		if !errors.Is(err, auth.ErrInviteInvalid) {
			t.Fatalf("error = %v, want ErrInviteInvalid", err)
		}
	})

	t.Run("re-inviting the same address replaces the pending invitation", func(t *testing.T) {
		invitee := h.email(t, "reinvite")
		_, firstToken, err := svc.CreateInvite(orgCtx, invitee, auth.RoleMember, owner.Principal.User.ID, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		_, secondToken, err := svc.CreateInvite(orgCtx, invitee, auth.RoleAdmin, owner.Principal.User.ID, time.Hour)
		if err != nil {
			t.Fatalf("second invite: %v", err)
		}

		// The superseded link must stop working, or a withdrawn invitation
		// would still be usable by whoever received the first email.
		if _, err := svc.AcceptInvite(ctx, auth.AcceptInviteInput{
			Secret: firstToken, Name: "Old Link", Password: testPassword,
		}); !errors.Is(err, auth.ErrInviteInvalid) {
			t.Errorf("the superseded invitation still works: %v", err)
		}

		creds, err := svc.AcceptInvite(ctx, auth.AcceptInviteInput{
			Secret: secondToken, Name: "New Link", Password: testPassword,
		})
		if err != nil {
			t.Fatalf("the replacement invitation failed: %v", err)
		}
		if creds.Principal.Role != auth.RoleAdmin {
			t.Errorf("role = %q, want the admin role from the replacement", creds.Principal.Role)
		}
	})

	t.Run("revoking an invitation stops it working", func(t *testing.T) {
		invitee := h.email(t, "revokeinv")
		invite, token, err := svc.CreateInvite(orgCtx, invitee, auth.RoleMember, owner.Principal.User.ID, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.RevokeInvite(orgCtx, invite.ID); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		if _, err := svc.AcceptInvite(ctx, auth.AcceptInviteInput{
			Secret: token, Name: "Revoked", Password: testPassword,
		}); !errors.Is(err, auth.ErrInviteInvalid) {
			t.Errorf("a revoked invitation still works: %v", err)
		}
	})

	t.Run("an existing account joins with the account it already has", func(t *testing.T) {
		existing := h.signup(t, svc, "existing")
		_, token, err := svc.CreateInvite(orgCtx, existing.Principal.User.Email, auth.RoleMember, owner.Principal.User.ID, time.Hour)
		if err != nil {
			t.Fatal(err)
		}

		creds, err := svc.AcceptInvite(ctx, auth.AcceptInviteInput{
			Secret: token,
			UserID: &existing.Principal.User.ID,
			Proof:  auth.ProofPassword,
		})
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		if creds.Principal.User.ID != existing.Principal.User.ID {
			t.Error("accepting created a second account instead of reusing the existing one")
		}

		memberships, err := svc.Memberships(ctx, existing.Principal.User.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(memberships) != 2 {
			t.Errorf("got %d memberships, want the original plus the invited one", len(memberships))
		}
	})

	t.Run("nobody can be invited as an owner", func(t *testing.T) {
		if _, _, err := svc.CreateInvite(orgCtx, h.email(t, "usurper"), auth.RoleOwner, owner.Principal.User.ID, time.Hour); err == nil {
			t.Error("an owner invitation was accepted, which would let anyone hand over the organization by email")
		}
	})

	t.Run("pending invitations are listed, accepted ones are not", func(t *testing.T) {
		// A fresh organization, so the listing is not polluted by the subtests
		// above running in any order.
		lister := h.signup(t, svc, "lister")
		// Pinned to the primary: the accept below is read back at once, and
		// a replica a beat behind would still list the accepted one.
		listCtx := db.PinPrimary(auth.ContextForOrg(ctx, lister.Principal))

		pending := h.email(t, "pending")
		if _, _, err := svc.CreateInvite(listCtx, pending, auth.RoleMember, lister.Principal.User.ID, time.Hour); err != nil {
			t.Fatal(err)
		}
		accepted := h.email(t, "accepted")
		_, token, err := svc.CreateInvite(listCtx, accepted, auth.RoleMember, lister.Principal.User.ID, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.AcceptInvite(ctx, auth.AcceptInviteInput{Secret: token, Name: "Accepted", Password: testPassword}); err != nil {
			t.Fatal(err)
		}

		invites, err := svc.ListInvites(listCtx)
		if err != nil {
			t.Fatal(err)
		}
		if len(invites) != 1 {
			t.Fatalf("got %d pending invitations, want 1", len(invites))
		}
		if invites[0].Email != pending {
			t.Errorf("listed %q, want the still-pending %q", invites[0].Email, pending)
		}
	})
}

func TestAPITokens(t *testing.T) {
	h := newHarness(t)
	svc := h.authService()
	ctx := context.Background()

	owner := h.signup(t, svc, "tokenowner")
	orgCtx := auth.ContextForOrg(ctx, owner.Principal)
	userID := owner.Principal.User.ID

	t.Run("a token authenticates as its owner in its organization", func(t *testing.T) {
		token, _, err := svc.CreateAPIToken(orgCtx, userID, "continuous integration", nil, nil, nil)
		if err != nil {
			t.Fatalf("create token: %v", err)
		}
		if !strings.HasPrefix(token.Secret, auth.APITokenPrefix) {
			t.Errorf("secret %q lacks the recognisable prefix", token.Secret)
		}

		principal, err := svc.Authenticate(ctx, token.Secret)
		if err != nil {
			t.Fatalf("authenticate with token: %v", err)
		}
		if principal.User.ID != userID {
			t.Error("the token resolved to a different user")
		}
		if principal.Org == nil || principal.Org.ID != owner.Principal.Org.ID {
			t.Error("the token is not bound to the organization it was created in")
		}
		if principal.TokenID == nil || principal.SessionID != nil {
			t.Error("a token principal should carry a token id and no session id")
		}
	})

	t.Run("the secret is never readable again", func(t *testing.T) {
		if _, _, err := svc.CreateAPIToken(orgCtx, userID, "write only", nil, nil, nil); err != nil {
			t.Fatal(err)
		}
		tokens, err := svc.ListAPITokens(orgCtx, userID)
		if err != nil {
			t.Fatal(err)
		}
		for _, tok := range tokens {
			if tok.Secret != "" {
				t.Errorf("token %q was listed with its secret attached", tok.Name)
			}
		}
	})

	t.Run("revoking a token stops it working immediately", func(t *testing.T) {
		token, _, err := svc.CreateAPIToken(orgCtx, userID, "short lived", nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Authenticate(ctx, token.Secret); err != nil {
			t.Fatalf("token does not work before revocation: %v", err)
		}
		if _, err := svc.RevokeAPIToken(orgCtx, userID, token.ID); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		if _, err := svc.Authenticate(ctx, token.Secret); !errors.Is(err, auth.ErrInvalidToken) {
			t.Fatalf("error after revocation = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("an expired token does not authenticate", func(t *testing.T) {
		past := time.Now().Add(-time.Minute)
		token, _, err := svc.CreateAPIToken(orgCtx, userID, "already expired", nil, nil, &past)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Authenticate(ctx, token.Secret); !errors.Is(err, auth.ErrInvalidToken) {
			t.Fatalf("error = %v, want ErrInvalidToken", err)
		}
	})

	t.Run("a token stops working when its owner loses their membership", func(t *testing.T) {
		member := h.signup(t, svc, "departing")
		memberCtx := auth.ContextForOrg(ctx, member.Principal)
		token, _, err := svc.CreateAPIToken(memberCtx, member.Principal.User.ID, "outlives membership", nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.super.Exec(ctx, `DELETE FROM org_member WHERE user_id = $1`, member.Principal.User.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Authenticate(ctx, token.Secret); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("a token still works after its owner left the organization: %v", err)
		}
	})

	t.Run("a token cannot be revoked by another user", func(t *testing.T) {
		token, _, err := svc.CreateAPIToken(orgCtx, userID, "not yours", nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		stranger := h.signup(t, svc, "stranger")
		if _, err := svc.RevokeAPIToken(orgCtx, stranger.Principal.User.ID, token.ID); err == nil {
			t.Error("another user revoked a token that was not theirs")
		}
		if _, err := svc.Authenticate(ctx, token.Secret); err != nil {
			t.Errorf("the token stopped working after a failed revocation attempt: %v", err)
		}
	})

	t.Run("a token name is required", func(t *testing.T) {
		if _, _, err := svc.CreateAPIToken(orgCtx, userID, "   ", nil, nil, nil); err == nil {
			t.Error("a blank token name was accepted")
		}
	})
}
