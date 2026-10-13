//go:build integration

package test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/webhook"
)

// An event names no organization, and what takes one off the stream acts in
// the installation; a webhook still tells its receiver the installation's id.
func TestAnEventThatNamesNoOrganizationIsActedOn(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "unnamed")
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)

	t.Run("the audit log copies it into the installation", func(t *testing.T) {
		eventID := uuid.New()
		raw, _ := json.Marshal(map[string]any{"projectId": uuid.New(), "actorId": ws.actor.UserID})
		if err := audit.NewConsumer(h.cluster, nil, log).Handle(ctx, events.Event{ID: eventID, Topic: "project.archived", Payload: raw}); err != nil {
			t.Fatal(err)
		}
		var landed uuid.UUID
		if err := h.super.QueryRow(ctx, `SELECT org_id FROM audit_log WHERE event_id = $1`, eventID).Scan(&landed); err != nil {
			t.Fatalf("find the copied entry: %v", err)
		}
		if landed != shared.installation.ID {
			t.Errorf("the entry landed in %s, want the installation %s", landed, shared.installation.ID)
		}
	})

	// Receivers read orgId, so it stays for a release, deprecated, holding the
	// installation's id; a later release removes it.
	t.Run("a webhook's envelope names the installation", func(t *testing.T) {
		hooks := webhook.NewService(h.cluster, log)
		made, _, err := hooks.Create(ws.ctx, webhook.Input{Name: h.name(t, "Unnamed"), URL: "http://127.0.0.1:1/in", Topics: []string{events.TopicIssueCreated}}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = hooks.Delete(ws.ctx, made.ID, ws.actor.UserID) })

		eventID := uuid.New()
		if _, err := hooks.Enqueue(ws.ctx, made.ID, events.Event{ID: eventID, Topic: events.TopicIssueCreated, Payload: json.RawMessage(`{"key":"X-1"}`)}); err != nil {
			t.Fatal(err)
		}
		var body []byte
		if err := h.super.QueryRow(ctx, `SELECT request_body FROM webhook_delivery WHERE endpoint_id = $1 AND event_id = $2`, made.ID, eventID).Scan(&body); err != nil {
			t.Fatalf("find the delivery: %v", err)
		}
		var envelope struct {
			OrgID uuid.UUID `json:"orgId"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.OrgID != shared.installation.ID {
			t.Errorf("the envelope's orgId = %s, want the installation's %s: %s", envelope.OrgID, shared.installation.ID, body)
		}
	})
}
