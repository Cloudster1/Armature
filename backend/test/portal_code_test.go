//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

var sixDigits = regexp.MustCompile(`\b[0-9]{6}\b`)

// lastCode reads the code out of the last mail the fake mailer kept.
func (m *fakeMailer) lastCode(t *testing.T) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) == 0 {
		t.Fatal("no mail was sent")
	}
	code := sixDigits.FindString(m.sent[len(m.sent)-1].Body)
	if code == "" {
		t.Fatalf("no code in %q", m.sent[len(m.sent)-1].Body)
	}
	return code
}

// A mail address is enough to raise a request: a code proves the address, and
// whoever proves it is a customer of the desk with an ordinary session.
func TestAMailAddressIsEnoughToRaiseARequest(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	agent := api.client(t)
	signedUp := agent.signup(t, h, "deskdoor")
	slug := principalField(t, signedUp, "principal", "org", "slug").(string)
	want(t, agent.post("/api/v1/projects", map[string]any{"name": "Door", "key": "DOR", "template": "service-desk"}), http.StatusCreated, "desk")

	visitor := api.client(t)
	address := h.email(t, "ada")
	want(t, visitor.get("/api/v1/desk/"+slug), http.StatusOK, "the desk's door")
	want(t, visitor.get("/api/v1/desk/nobody-here-"+uuid.NewString()[:8]), http.StatusNotFound, "no desk there")
	want(t, visitor.post("/api/v1/desk/"+slug+"/codes", map[string]any{"email": "not an address"}), http.StatusUnprocessableEntity, "not an address")

	want(t, visitor.post("/api/v1/desk/"+slug+"/codes", map[string]any{"email": address}), http.StatusAccepted, "a code is mailed")
	want(t, visitor.post("/api/v1/desk/"+slug+"/codes", map[string]any{"email": address}), http.StatusTooManyRequests, "not another one straight away")
	code := api.mailer.lastCode(t)

	for i := 0; i < 5; i++ {
		want(t, visitor.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": address, "code": "000000"}), http.StatusUnauthorized, "a wrong code")
	}
	want(t, visitor.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": address, "code": code}), http.StatusUnauthorized, "the right code after five wrong ones is used up")

	// The cooldown is nudged aside through SQL, the way a minute passing would.
	if _, err := h.super.Exec(context.Background(), `UPDATE portal_code SET requested_at = requested_at - interval '2 minutes' WHERE email = $1`, address); err != nil {
		t.Fatal(err)
	}
	want(t, visitor.post("/api/v1/desk/"+slug+"/codes", map[string]any{"email": address}), http.StatusAccepted, "a fresh code")
	entered := want(t, visitor.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": address, "code": api.mailer.lastCode(t)}), http.StatusOK, "the code lets them in")
	if role := principalField(t, entered, "principal", "role"); role != "customer" {
		t.Errorf("role = %v, want customer", role)
	}
	requesterID := principalField(t, entered, "principal", "user", "id").(string)
	want(t, visitor.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": address, "code": code}), http.StatusUnauthorized, "a code is single use")

	// In: the portal works, the agents' side does not.
	desks := list(t, want(t, visitor.get("/api/v1/portal/desks"), http.StatusOK, "desks"), "desks")
	types := desks[0].(map[string]any)["requestTypes"].([]any)
	raised := want(t, visitor.post("/api/v1/portal/requests", map[string]any{
		"requestTypeId": types[0].(map[string]any)["id"], "summary": "The badge reader is dead",
	}), http.StatusCreated, "raise")
	key := obj(t, raised, "request")["key"].(string)
	want(t, visitor.get("/api/v1/portal/requests/"+key), http.StatusOK, "their request")
	want(t, visitor.get("/api/v1/projects"), http.StatusForbidden, "customers use the portal")

	// The receipt goes to the address, by the worker's notifier.
	var orgID uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM org WHERE slug = $1`, slug).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"key": key, "actorId": requesterID})
	before := len(api.mailer.sent)
	if err := api.notifier.Handle(context.Background(), events.Event{OrgID: orgID, Topic: events.TopicIssueCreated, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if len(api.mailer.sent) != before+1 {
		t.Fatalf("the receipt was not mailed: %+v", api.mailer.sent)
	}
	if receipt := api.mailer.sent[len(api.mailer.sent)-1]; receipt.To != address || !strings.Contains(receipt.Subject, key) || !strings.Contains(receipt.Body, "/desk/"+slug+"?next=") {
		t.Errorf("receipt = %+v, want it addressed to the requester, naming the key, linking to the door", receipt)
	}

	t.Run("the same address comes back as the same person", func(t *testing.T) {
		again := api.client(t)
		if _, err := h.super.Exec(context.Background(), `DELETE FROM portal_code WHERE email = $1`, address); err != nil {
			t.Fatal(err)
		}
		want(t, again.post("/api/v1/desk/"+slug+"/codes", map[string]any{"email": address}), http.StatusAccepted, "a code")
		entered := want(t, again.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": address, "code": api.mailer.lastCode(t)}), http.StatusOK, "in again")
		if id := principalField(t, entered, "principal", "user", "id"); id != requesterID {
			t.Errorf("a second entry made a second person: %v and %v", id, requesterID)
		}
		want(t, again.get("/api/v1/portal/requests/"+key), http.StatusOK, "and finds their request")
	})

	t.Run("an agent's address is refused after the mailbox is proven", func(t *testing.T) {
		owner := principalField(t, signedUp, "principal", "user", "email").(string)
		stranger := api.client(t)
		want(t, stranger.post("/api/v1/desk/"+slug+"/codes", map[string]any{"email": owner}), http.StatusAccepted, "asking says nothing")
		want(t, stranger.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": owner, "code": api.mailer.lastCode(t)}), http.StatusForbidden, "but the code is refused")
	})

	t.Run("a deactivated person stays out", func(t *testing.T) {
		if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET is_active = false WHERE id = $1`, requesterID); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = h.super.Exec(context.Background(), `UPDATE app_user SET is_active = true WHERE id = $1`, requesterID)
		})
		if _, err := h.super.Exec(context.Background(), `DELETE FROM portal_code WHERE email = $1`, address); err != nil {
			t.Fatal(err)
		}
		gone := api.client(t)
		want(t, gone.post("/api/v1/desk/"+slug+"/codes", map[string]any{"email": address}), http.StatusAccepted, "a code")
		want(t, gone.post("/api/v1/desk/"+slug+"/sessions", map[string]any{"email": address, "code": api.mailer.lastCode(t)}), http.StatusForbidden, "refused")
	})

	t.Run("codes are the organization's, through SQL", func(t *testing.T) {
		other := h.newWorkspace(t, "elsewhere")
		var mine, theirs int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM portal_code WHERE org_id = $1`, orgID).Scan(&mine); err != nil {
			t.Fatal(err)
		}
		if mine == 0 {
			t.Fatal("expected a code row to look at")
		}
		err := h.cluster.Read(other.ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM portal_code`).Scan(&theirs)
		})
		if err != nil {
			t.Fatal(err)
		}
		if theirs != 0 {
			t.Errorf("another organization sees %d codes", theirs)
		}
	})
}
