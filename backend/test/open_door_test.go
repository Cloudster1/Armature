//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// An address taken on trust at one desk's door is a customer of that desk and
// sees nothing else: not the other desks, not its own proven requests there.
func TestADeskMayLetItsCallersInWithoutACode(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	signedUp := owner.signup(t, h, "opendoor")
	slug := principalField(t, signedUp, "principal", "org", "slug").(string)

	// Two desks and a software project. Only a desk has a door.
	trusting := obj(t, want(t, owner.post("/api/v1/projects", map[string]any{"name": "IT desk", "key": "ITD", "template": "service-desk"}), http.StatusCreated, "a desk"), "project")
	careful := obj(t, want(t, owner.post("/api/v1/projects", map[string]any{"name": "HR desk", "key": "HRD", "template": "service-desk"}), http.StatusCreated, "another desk"), "project")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Code", "key": "COD"}), http.StatusCreated, "a software project")
	if trusting["portalVerifies"] != true || careful["portalVerifies"] != true {
		t.Fatalf("a new desk asks for a code by default: %v %v", trusting["portalVerifies"], careful["portalVerifies"])
	}
	want(t, owner.patch("/api/v1/projects/COD", map[string]any{"portalVerifies": false}), http.StatusConflict, "a software project has no door")

	// A member who does not administer the desk cannot open its door.
	invite := want(t, owner.post("/api/v1/invites", map[string]string{"email": h.email(t, "plain"), "role": "member"}), http.StatusCreated, "invite")
	member := api.client(t)
	want(t, member.post("/api/v1/auth/invites/accept", map[string]string{"token": invite.Body["token"].(string), "name": "Plain", "password": testPassword}), http.StatusOK, "accept")
	want(t, member.patch("/api/v1/projects/ITD", map[string]any{"portalVerifies": false}), http.StatusForbidden, "not theirs to open")

	// Before the door is opened, walking in is refused, and the door page says nothing is open.
	visitor := api.client(t)
	address := h.email(t, "walkin")
	want(t, visitor.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": address, "name": "Walk In", "desk": "ITD"}), http.StatusForbidden, "the door asks for a code")
	if open := list(t, want(t, visitor.get("/api/v1/desk/"+slug), http.StatusOK, "the door"), "open"); len(open) != 0 {
		t.Fatalf("open doors before any was opened: %v", open)
	}

	opened := obj(t, want(t, owner.patch("/api/v1/projects/ITD", map[string]any{"portalVerifies": false}), http.StatusOK, "open the door"), "project")
	if opened["portalVerifies"] != false {
		t.Fatalf("portalVerifies after opening = %v", opened["portalVerifies"])
	}
	open := list(t, want(t, visitor.get("/api/v1/desk/"+slug), http.StatusOK, "the door"), "open")
	if len(open) != 1 || open[0].(map[string]any)["key"] != "ITD" {
		t.Fatalf("open doors = %v, want the IT desk alone", open)
	}

	want(t, visitor.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": address, "code": "123456", "desk": "ITD"}), http.StatusBadRequest, "a code and a desk at once")
	want(t, visitor.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": "not an address", "desk": "ITD"}), http.StatusUnprocessableEntity, "not an address")
	entered := want(t, visitor.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": address, "name": "Walk In", "desk": "ITD"}), http.StatusOK, "in without a code")
	if role := principalField(t, entered, "principal", "role"); role != "customer" {
		t.Errorf("role = %v, want customer", role)
	}
	if desk := principalField(t, entered, "principal", "portalDesk"); desk != "ITD" {
		t.Errorf("portalDesk = %v, want ITD", desk)
	}
	if name := principalField(t, entered, "principal", "user", "name"); name != "Walk In" {
		t.Errorf("name = %v, want the one given at the door", name)
	}
	walkerID := principalField(t, entered, "principal", "user", "id").(string)

	// The session is the desk's: one desk listed, requests raised there only.
	desks := list(t, want(t, visitor.get("/api/v1/portal/desks"), http.StatusOK, "desks"), "desks")
	if len(desks) != 1 || desks[0].(map[string]any)["projectKey"] != "ITD" {
		t.Fatalf("a walk-in sees %d desks, want the one they walked into", len(desks))
	}
	itTypes := desks[0].(map[string]any)["requestTypes"].([]any)
	raised := want(t, visitor.post("/api/v1/portal/requests", map[string]any{"requestTypeId": itTypes[0].(map[string]any)["id"], "summary": "The printer is on fire"}), http.StatusCreated, "raise at the IT desk")
	itKey := obj(t, raised, "request")["key"].(string)
	want(t, visitor.get("/api/v1/portal/requests/"+itKey), http.StatusOK, "their request")

	// The same address, having proven itself at the HR desk earlier, has a
	// request there. The walk-in must not see it, nor raise one there.
	hrTypes := list(t, want(t, owner.get("/api/v1/projects/HRD/request-types"), http.StatusOK, "HR request types"), "requestTypes")
	hrKey, _ := raiseAsCustomer(t, h, api, slug, address, hrTypes[0].(map[string]any)["id"].(string))
	want(t, visitor.get("/api/v1/portal/requests/"+hrKey), http.StatusNotFound, "the HR request is not there for a walk-in")
	want(t, visitor.post("/api/v1/portal/requests", map[string]any{"requestTypeId": hrTypes[0].(map[string]any)["id"], "summary": "Sneaking in"}), http.StatusNotFound, "nor is the HR request type")
	mine := list(t, want(t, visitor.get("/api/v1/portal/requests"), http.StatusOK, "my requests"), "requests")
	for _, r := range mine {
		if r.(map[string]any)["key"] == hrKey {
			t.Fatalf("a walk-in's list holds the HR request: %v", mine)
		}
	}
	if len(mine) != 1 {
		t.Fatalf("a walk-in's list = %d requests, want the IT one alone", len(mine))
	}

	t.Run("an agent's address is refused at an open door too", func(t *testing.T) {
		ownerAddress := principalField(t, signedUp, "principal", "user", "email").(string)
		want(t, api.client(t).post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": ownerAddress, "desk": "ITD"}), http.StatusForbidden, "sign in instead")
	})

	t.Run("a session without a code belongs to an open desk, through SQL", func(t *testing.T) {
		var hrID uuid.UUID
		if err := h.super.QueryRow(context.Background(), `SELECT id FROM project WHERE key = 'HRD' AND org_id = (SELECT id FROM org WHERE slug = $1)`, slug).Scan(&hrID); err != nil {
			t.Fatal(err)
		}
		_, err := h.super.Exec(context.Background(), `
			INSERT INTO user_session (user_id, token_hash, expires_at, portal_project_id)
			VALUES ($1, $2, now() + interval '1 hour', $3)`, walkerID, []byte(uuid.NewString()), hrID)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("a code-less session for a desk that asks for a code was accepted: %v", err)
		}
		_, err = h.super.Exec(context.Background(), `UPDATE project SET portal_verifies = false WHERE key = 'COD' AND org_id = (SELECT id FROM org WHERE slug = $1)`, slug)
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Fatalf("a software project was allowed a door: %v", err)
		}
	})

	t.Run("turning the code back on ends the sessions that skipped it", func(t *testing.T) {
		want(t, owner.patch("/api/v1/projects/ITD", map[string]any{"portalVerifies": true}), http.StatusOK, "close the door")
		want(t, visitor.get("/api/v1/auth/me"), http.StatusUnauthorized, "the walk-in is out")
		want(t, visitor.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": address, "desk": "ITD"}), http.StatusForbidden, "and cannot walk back in")
	})
}

// raiseAsCustomer proves an address with a code and raises a request through
// a request type, returning its key and the customer's client.
func raiseAsCustomer(t *testing.T, h *harness, api *apiServer, slug, address, requestTypeID string) (string, *client) {
	t.Helper()
	c := api.client(t)
	if _, err := h.super.Exec(context.Background(), `DELETE FROM portal_code WHERE email = $1`, address); err != nil {
		t.Fatal(err)
	}
	want(t, c.post("/api/v1/desk/"+slug+"/codes", map[string]any{"email": address}), http.StatusAccepted, "a code")
	want(t, c.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": address, "code": api.mailer.lastCode(t)}), http.StatusOK, "in with the code")
	raised := want(t, c.post("/api/v1/portal/requests", map[string]any{"requestTypeId": requestTypeID, "summary": "A proven request"}), http.StatusCreated, "raise")
	return obj(t, raised, "request")["key"].(string), c
}
