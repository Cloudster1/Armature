//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// Each test here is a takeover that worked before sessions carried their proof:
// a stranger uses a door of their own organization to reach the victim's.

// victim signs up an organization with a project in it and returns its
// client and address.
func victim(t *testing.T, h *harness, api *apiServer) (*client, string) {
	t.Helper()
	c := api.client(t)
	signed := c.signup(t, h, "victim")
	want(t, c.post("/api/v1/projects", map[string]any{"name": "Secrets", "key": "SEC"}), http.StatusCreated, "the victim's project")
	return c, principalField(t, signed, "principal", "user", "email").(string)
}

func wantCode(t *testing.T, r response, code string) {
	t.Helper()
	if r.ErrorCode() != code {
		t.Fatalf("error code = %q, want %q: %s", r.ErrorCode(), code, r.Raw)
	}
}

func TestAnInvitationCannotHandOverAnAccount(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	victimClient, address := victim(t, h, api)

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
	victimClient, address := victim(t, h, api)

	attacker := api.client(t)
	slug := principalField(t, attacker.signup(t, h, "doorman"), "principal", "org", "slug").(string)
	wlk, hlp := deskKey("WLK"), deskKey("HLP")
	want(t, attacker.post("/api/v1/projects", map[string]any{"name": "Walk in", "key": wlk, "template": "service-desk"}), http.StatusCreated, "the attacker's desk")
	want(t, attacker.patch("/api/v1/projects/"+wlk, map[string]any{"portalVerifies": false}), http.StatusOK, "an open door")

	stranger := api.client(t)
	wantCode(t, want(t, stranger.post("/api/v1/desk/sessions", map[string]any{"email": address, "desk": wlk, "name": "Victim"}),
		http.StatusForbidden, "an address with a password at an open door"), "door_asks_for_code")
	want(t, stranger.get("/api/v1/auth/me"), http.StatusUnauthorized, "no session came of it")

	// A customer of the victim's desk has no password, so the door lets their
	// address in, but the session stays at the attacker's desk.
	want(t, victimClient.post("/api/v1/projects", map[string]any{"name": "Help", "key": hlp, "template": "service-desk"}), http.StatusCreated, "the victim's desk")
	customerAddress := h.email(t, "customer")
	customer := api.client(t)
	want(t, customer.post("/api/v1/desk/codes", map[string]any{"email": customerAddress, "desk": hlp}), http.StatusAccepted, "a code at the victim's desk")
	want(t, customer.post("/api/v1/desk/sessions", map[string]any{"email": customerAddress, "code": api.mailer.lastCode(t), "desk": hlp}), http.StatusOK, "the customer is in")

	walkIn := api.client(t)
	want(t, walkIn.post("/api/v1/desk/sessions", map[string]any{"email": customerAddress, "desk": wlk}), http.StatusOK, "the customer's address at the open door")
	at := want(t, walkIn.get("/api/v1/auth/me"), http.StatusOK, "where the walk-in is")
	if principalField(t, at, "principal", "org", "slug") != slug || principalField(t, at, "principal", "portalDesk") != wlk {
		t.Fatalf("the walk-in left the door's desk: %s", at.Raw)
	}
	want(t, walkIn.get("/api/v1/auth/me/export"), http.StatusForbidden, "exporting the whole person through an open door")
	want(t, walkIn.delete("/api/v1/auth/me"), http.StatusForbidden, "erasing the whole person through an open door")
	want(t, customer.get("/api/v1/portal/requests"), http.StatusOK, "and the real customer is untouched")

	t.Run("a session keeps its proof, through SQL", func(t *testing.T) {
		ctx := context.Background()
		var customerID uuid.UUID
		if err := h.super.QueryRow(ctx, `SELECT id FROM app_user WHERE email = $1`, customerAddress).Scan(&customerID); err != nil {
			t.Fatal(err)
		}
		var pgErr *pgconn.PgError
		_, err := h.super.Exec(ctx, `UPDATE user_session SET proof = 'password' WHERE user_id = $1 AND proof = 'portal_code'`, customerID)
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("a session's proof was rewritten: %v", err)
		}
		_, err = h.super.Exec(ctx, `UPDATE user_session SET portal_project_id = NULL WHERE user_id = $1 AND proof = 'open_door'`, customerID)
		if err != nil {
			t.Fatalf("an open door session could not leave its desk: %v", err)
		}
	})
}

// A desk entered without a code takes the address on trust, so its session may
// not take or erase the account behind it, even in an installation of one desk.
func TestAnOpenDoorCannotActForTheWholePerson(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	agent := api.client(t)
	agent.signup(t, h, "walkin")
	wli := deskKey("WLI")
	want(t, agent.post("/api/v1/projects", map[string]any{"name": "Walk in", "key": wli, "template": "service-desk"}), http.StatusCreated, "a desk")
	want(t, agent.patch("/api/v1/projects/"+wli, map[string]any{"portalVerifies": false}), http.StatusOK, "an open door")

	address := h.email(t, "trusted")
	walkIn := api.client(t)
	want(t, walkIn.post("/api/v1/desk/sessions", map[string]any{"email": address, "desk": wli}), http.StatusOK, "in through the open door")
	for what, got := range map[string]response{
		"export": walkIn.get("/api/v1/auth/me/export"),
		"erase":  walkIn.delete("/api/v1/auth/me"),
	} {
		if got.Status != http.StatusForbidden || !strings.Contains(got.Error()["message"].(string), "code") {
			t.Errorf("%s through an open door answered %d %s, want 403 pointing at a mailed code", what, got.Status, got.Raw)
		}
	}

	byCode := api.client(t)
	want(t, byCode.post("/api/v1/desk/codes", map[string]any{"email": address, "desk": wli}), http.StatusAccepted, "a code")
	want(t, byCode.post("/api/v1/desk/sessions", map[string]any{"email": address, "code": api.mailer.lastCode(t), "desk": wli}), http.StatusOK, "in with the code")
	want(t, byCode.get("/api/v1/auth/me/export"), http.StatusOK, "a proven mailbox takes its data")
}

func TestAPortalCodeCannotBeGuessedByAskingAgain(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	agent := api.client(t)
	agent.signup(t, h, "guessdesk")
	gsd := deskKey("GSD")
	want(t, agent.post("/api/v1/projects", map[string]any{"name": "Desk", "key": gsd, "template": "service-desk"}), http.StatusCreated, "a desk")

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
		want(t, visitor.post("/api/v1/desk/codes", map[string]any{"email": address, "desk": gsd}), http.StatusAccepted, "a code")
		for i := 0; i < 5; i++ {
			want(t, visitor.post("/api/v1/desk/sessions", map[string]any{"email": address, "code": "000000", "desk": gsd}), http.StatusUnauthorized, "a wrong code")
		}
	}
	nudge(`UPDATE portal_code SET requested_at = requested_at - interval '2 minutes' WHERE email = $1`)
	want(t, visitor.post("/api/v1/desk/codes", map[string]any{"email": address, "desk": gsd}), http.StatusAccepted, "a third code")
	want(t, visitor.post("/api/v1/desk/sessions", map[string]any{"email": address, "code": api.mailer.lastCode(t), "desk": gsd}),
		http.StatusUnauthorized, "ten wrong guesses in the hour spend the right code too")

	// An hour later the address starts afresh.
	nudge(`UPDATE portal_code SET requested_at = requested_at - interval '2 minutes', window_started = window_started - interval '2 hours' WHERE email = $1`)
	want(t, visitor.post("/api/v1/desk/codes", map[string]any{"email": address, "desk": gsd}), http.StatusAccepted, "a code an hour on")
	want(t, visitor.post("/api/v1/desk/sessions", map[string]any{"email": address, "code": api.mailer.lastCode(t), "desk": gsd}), http.StatusOK, "the right code works again")
}
