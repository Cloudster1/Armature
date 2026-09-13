//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/oidc"
)

// Each test here is a takeover that worked before sessions carried their proof:
// a stranger uses a door of their own organization to reach the victim's.

// victim signs up an organization with a project in it and returns its
// client, address and slug.
func victim(t *testing.T, h *harness, api *apiServer) (*client, string, string) {
	t.Helper()
	c := api.client(t)
	signed := c.signup(t, h, "victim")
	want(t, c.post("/api/v1/projects", map[string]any{"name": "Secrets", "key": "SEC"}), http.StatusCreated, "the victim's project")
	return c, principalField(t, signed, "principal", "user", "email").(string), principalField(t, signed, "principal", "org", "slug").(string)
}

func wantCode(t *testing.T, r response, code string) {
	t.Helper()
	if r.ErrorCode() != code {
		t.Fatalf("error code = %q, want %q: %s", r.ErrorCode(), code, r.Raw)
	}
}

func TestAnInvitationCannotHandOverAnAccount(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	victimClient, address, _ := victim(t, h, api)

	attacker := api.client(t)
	attacker.signup(t, h, "inviter")
	invite := want(t, attacker.post("/api/v1/invites", map[string]string{"email": address, "role": "member"}), http.StatusCreated, "an invitation to the victim's address")
	token := invite.Body["token"].(string)

	stranger := api.client(t)
	wantCode(t, want(t, stranger.post("/api/v1/auth/invites/accept", map[string]string{"token": token, "name": "Not The Victim", "password": testPassword}),
		http.StatusConflict, "an anonymous acceptance for an address with an account"), "sign_in_to_accept")
	want(t, stranger.get("/api/v1/auth/me"), http.StatusUnauthorized, "no session came of it")

	bystander := api.client(t)
	bystander.signup(t, h, "bystander")
	wantCode(t, want(t, bystander.post("/api/v1/auth/invites/accept", map[string]string{"token": token}),
		http.StatusForbidden, "somebody else's invitation"), "invite_for_someone_else")

	// The invitation is still good for the person it was sent to.
	want(t, victimClient.post("/api/v1/auth/invites/accept", map[string]string{"token": token}), http.StatusOK, "the victim accepts their own")

	// And an invitation never rewrites the standing of somebody already here.
	wantCode(t, want(t, attacker.post("/api/v1/invites", map[string]string{"email": address, "role": "member"}),
		http.StatusConflict, "inviting a member again"), "already_member")
}

func TestAnOpenDoorCannotHandOverAnAccount(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	victimClient, address, victimSlug := victim(t, h, api)

	attacker := api.client(t)
	slug := principalField(t, attacker.signup(t, h, "doorman"), "principal", "org", "slug").(string)
	want(t, attacker.post("/api/v1/projects", map[string]any{"name": "Walk in", "key": "WLK", "template": "service-desk"}), http.StatusCreated, "the attacker's desk")
	want(t, attacker.patch("/api/v1/projects/WLK", map[string]any{"portalVerifies": false}), http.StatusOK, "an open door")

	stranger := api.client(t)
	wantCode(t, want(t, stranger.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": address, "desk": "WLK", "name": "Victim"}),
		http.StatusForbidden, "an address with a password at an open door"), "door_asks_for_code")
	want(t, stranger.get("/api/v1/auth/me"), http.StatusUnauthorized, "no session came of it")

	// A customer of the victim's desk has no password, so the door lets their
	// address in, but the session stays at the attacker's desk.
	want(t, victimClient.post("/api/v1/projects", map[string]any{"name": "Help", "key": "HLP", "template": "service-desk"}), http.StatusCreated, "the victim's desk")
	customerAddress := h.email(t, "customer")
	customer := api.client(t)
	want(t, customer.post("/api/v1/desk/"+victimSlug+"/codes", map[string]any{"email": customerAddress}), http.StatusAccepted, "a code at the victim's desk")
	want(t, customer.post("/api/v1/desk/"+victimSlug+"/sessions", map[string]any{"email": customerAddress, "code": api.mailer.lastCode(t)}), http.StatusOK, "the customer is in")

	walkIn := api.client(t)
	want(t, walkIn.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": customerAddress, "desk": "WLK"}), http.StatusOK, "the customer's address at the open door")
	wantCode(t, want(t, walkIn.post("/api/v1/auth/switch-org", map[string]string{"slug": victimSlug}), http.StatusConflict, "switching out of the door's organization"), "session_bound")
	wantCode(t, want(t, walkIn.get("/api/v1/auth/me/export"), http.StatusForbidden, "exporting the whole person"), "session_bound")
	wantCode(t, want(t, walkIn.delete("/api/v1/auth/me"), http.StatusForbidden, "erasing the whole person"), "session_bound")
	want(t, customer.get("/api/v1/portal/requests"), http.StatusOK, "and the real customer is untouched")

	t.Run("a session stays where its proof holds, through SQL", func(t *testing.T) {
		ctx := context.Background()
		var victimOrg, customerID uuid.UUID
		if err := h.super.QueryRow(ctx, `SELECT id FROM org WHERE slug = $1`, victimSlug).Scan(&victimOrg); err != nil {
			t.Fatal(err)
		}
		if err := h.super.QueryRow(ctx, `SELECT id FROM app_user WHERE email = $1`, customerAddress).Scan(&customerID); err != nil {
			t.Fatal(err)
		}
		var pgErr *pgconn.PgError
		_, err := h.super.Exec(ctx, `UPDATE user_session SET current_org_id = $2, portal_project_id = NULL WHERE user_id = $1 AND proof = 'open_door'`, customerID, victimOrg)
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("an open door session was moved to another organization: %v", err)
		}
		_, err = h.super.Exec(ctx, `UPDATE user_session SET proof = 'password' WHERE user_id = $1 AND proof = 'portal_code'`, customerID)
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("a session's proof was rewritten: %v", err)
		}
		_, err = h.super.Exec(ctx, `UPDATE user_session SET current_org_id = NULL, portal_project_id = NULL WHERE user_id = $1 AND proof = 'open_door'`, customerID)
		if err != nil {
			t.Fatalf("leaving an organization is always allowed: %v", err)
		}
	})
}

func TestAPortalCodeCannotBeGuessedByAskingAgain(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	agent := api.client(t)
	slug := principalField(t, agent.signup(t, h, "guessdesk"), "principal", "org", "slug").(string)
	want(t, agent.post("/api/v1/projects", map[string]any{"name": "Desk", "key": "GSD", "template": "service-desk"}), http.StatusCreated, "a desk")

	visitor := api.client(t)
	address := h.email(t, "guessed")
	nudge := func(sql string) {
		t.Helper()
		if _, err := h.super.Exec(context.Background(), sql, address); err != nil {
			t.Fatal(err)
		}
	}
	for round := 0; round < 2; round++ {
		if round > 0 {
			nudge(`UPDATE portal_code SET requested_at = requested_at - interval '2 minutes' WHERE email = $1`)
		}
		want(t, visitor.post("/api/v1/desk/"+slug+"/codes", map[string]any{"email": address}), http.StatusAccepted, "a code")
		for i := 0; i < 5; i++ {
			want(t, visitor.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": address, "code": "000000"}), http.StatusUnauthorized, "a wrong code")
		}
	}
	nudge(`UPDATE portal_code SET requested_at = requested_at - interval '2 minutes' WHERE email = $1`)
	want(t, visitor.post("/api/v1/desk/"+slug+"/codes", map[string]any{"email": address}), http.StatusAccepted, "a third code")
	want(t, visitor.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": address, "code": api.mailer.lastCode(t)}),
		http.StatusUnauthorized, "ten wrong guesses in the hour spend the right code too")

	// An hour later the address starts afresh.
	nudge(`UPDATE portal_code SET requested_at = requested_at - interval '2 minutes', window_started = window_started - interval '2 hours' WHERE email = $1`)
	want(t, visitor.post("/api/v1/desk/"+slug+"/codes", map[string]any{"email": address}), http.StatusAccepted, "a code an hour on")
	want(t, visitor.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": address, "code": api.mailer.lastCode(t)}), http.StatusOK, "the right code works again")
}

func TestASingleSignOnReachesOnlyItsOrganization(t *testing.T) {
	h := newHarness(t)
	home := h.newWorkspace(t, "ssohome")
	away := h.newWorkspace(t, "ssoaway")
	elsewhere := h.newWorkspace(t, "ssoelse")
	provider := newIDP(t)
	svc, clientID := configured(t, h, home, provider, false)

	email := h.email(t, "traveller")
	h.joinExisting(t, home, email, "member")
	h.joinExisting(t, away, email, "member")

	if _, err := signInWith(t, h, svc, home, provider, clientID, map[string]any{"email": email, "email_verified": false}); !errors.Is(err, oidc.ErrEmailUnverified) {
		t.Fatalf("an address the provider never verified signed in: %v", err)
	}
	session, err := signInWith(t, h, svc, home, provider, clientID, map[string]any{"email": email, "email_verified": true})
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}

	ctx := context.Background()
	accounts := h.authService()
	principal, err := accounts.Authenticate(ctx, session.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if principal.Proof != auth.ProofOIDC {
		t.Fatalf("proof = %q, want oidc", principal.Proof)
	}
	var awaySlug string
	if err := h.super.QueryRow(ctx, `SELECT slug FROM org WHERE id = $1`, away.orgID).Scan(&awaySlug); err != nil {
		t.Fatal(err)
	}
	if _, _, err := accounts.SwitchOrg(ctx, *principal.SessionID, principal.User.ID, awaySlug); !errors.Is(err, auth.ErrSessionStaysHome) {
		t.Fatalf("a provider's sign-in switched into another organization: %v", err)
	}

	_, token, err := accounts.CreateInvite(elsewhere.ctx, email, auth.RoleMember, elsewhere.actor.UserID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, err = accounts.AcceptInvite(ctx, auth.AcceptInviteInput{Secret: token, UserID: &principal.User.ID, Proof: principal.Proof, SessionOrg: &principal.Org.ID})
	if !errors.Is(err, auth.ErrSessionStaysHome) {
		t.Fatalf("a provider's sign-in took an invitation to another organization: %v", err)
	}
}
