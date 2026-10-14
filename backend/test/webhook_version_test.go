//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/httpapi"
	"github.com/armature/armature/backend/internal/openapi"
)

// envelopeVersionHeader is spelled out rather than taken from the package, so
// renaming the constant cannot quietly change what receivers read.
const envelopeVersionHeader = "X-Armature-Webhook-Version"

// envelopeSchema is the body the document says a webhook delivery carries.
func envelopeSchema(t *testing.T) (*openapi.Document, *openapi.Schema) {
	t.Helper()
	doc := httpapi.Spec()
	encoded, err := doc.MarshalIndent()
	if err != nil {
		t.Fatal(err)
	}
	var described struct {
		Webhooks map[string]map[string]struct {
			RequestBody struct {
				Content map[string]struct {
					Schema *openapi.Schema `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
		} `json:"webhooks"`
	}
	if err := json.Unmarshal(encoded, &described); err != nil {
		t.Fatal(err)
	}
	schema := described.Webhooks["event"]["post"].RequestBody.Content["application/json"].Schema
	if schema == nil {
		t.Fatal("the document describes no webhook delivery")
	}
	return doc, schema
}

// sentInVersion checks one delivery a receiver kept against the document and
// the version it should be in, header and body alike.
func sentInVersion(t *testing.T, got received, version int) {
	t.Helper()
	doc, schema := envelopeSchema(t)
	if err := doc.ValidateJSON(schema, []byte(got.Body)); err != nil {
		t.Errorf("the delivery does not fit the document: %v: %s", err, got.Body)
	}
	var body struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal([]byte(got.Body), &body); err != nil {
		t.Fatal(err)
	}
	if body.Version != version || got.Version != strconv.Itoa(version) {
		t.Errorf("the delivery is in version %d with the header %q, want %d in both: %s", body.Version, got.Version, version, got.Body)
	}
}

// moveEndpointVersion puts an endpoint on another version behind the service's
// back, as a later release registering it would.
func moveEndpointVersion(t *testing.T, h *harness, id uuid.UUID, version int) {
	t.Helper()
	if _, err := h.super.Exec(context.Background(), `UPDATE webhook_endpoint SET version = $2 WHERE id = $1`, id, version); err != nil {
		t.Fatal(err)
	}
}

// An endpoint is on the current envelope version unless it asks, keeps its
// version through an edit that names none, and is refused one never sent.
func TestAWebhookKeepsTheEnvelopeVersionItWasRegisteredFor(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signIn(t, h.person(t, "hookversion", auth.RoleOwner))
	name := h.name(t, "Versioned")
	const address = "http://127.0.0.1:1/in"

	refused := want(t, c.post("/api/v1/webhooks", map[string]any{"name": name, "url": address, "topics": []string{}, "version": 7}), http.StatusUnprocessableEntity, "an unknown version")
	if !strings.Contains(refused.Raw, "version 7") || !strings.Contains(refused.Raw, "1") {
		t.Errorf("the refusal = %s, want it to name the version asked for and the ones offered", refused.Raw)
	}
	plain := want(t, c.post("/api/v1/webhooks", map[string]any{"name": name, "url": address, "topics": []string{}}), http.StatusCreated, "a webhook")
	hookID := idOf(t, plain, "webhook")
	t.Cleanup(func() { c.delete("/api/v1/webhooks/" + hookID) })
	if got := obj(t, plain, "webhook")["version"]; got != float64(1) {
		t.Errorf("a new webhook is on version %v, want the current 1", got)
	}
	named := want(t, c.post("/api/v1/webhooks", map[string]any{"name": h.name(t, "Pinned"), "url": address, "topics": []string{}, "version": 1}), http.StatusCreated, "a webhook on version 1")
	namedID := idOf(t, named, "webhook")
	t.Cleanup(func() { c.delete("/api/v1/webhooks/" + namedID) })
	if got := obj(t, named, "webhook")["version"]; got != float64(1) {
		t.Errorf("a webhook asked for version 1 is on %v", got)
	}

	// A later release may have put it on a version this one only stores; an
	// edit that names none, as turning it off does, keeps that.
	moveEndpointVersion(t, h, uuid.MustParse(hookID), 2)
	edited := want(t, c.patch("/api/v1/webhooks/"+hookID, map[string]any{"name": name, "url": address, "topics": []string{}, "enabled": false}), http.StatusOK, "turning it off")
	if got := obj(t, edited, "webhook")["version"]; got != float64(2) {
		t.Errorf("an edit without a version left the webhook on %v, want the 2 it had", got)
	}
	want(t, c.patch("/api/v1/webhooks/"+hookID, map[string]any{"name": name, "url": address, "topics": []string{}, "version": 0}), http.StatusUnprocessableEntity, "version 0")
	moved := want(t, c.patch("/api/v1/webhooks/"+hookID, map[string]any{"name": name, "url": address, "topics": []string{}, "version": 1}), http.StatusOK, "moving it to version 1")
	if got := obj(t, moved, "webhook")["version"]; got != float64(1) {
		t.Errorf("an edit asking for version 1 left the webhook on %v", got)
	}
}

// The database refuses a version below one as the service does, for an
// endpoint and for a delivery queued for it.
func TestTheDatabaseRefusesAnEnvelopeVersionBelowOne(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	var id uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO webhook_endpoint (name, url, secret) VALUES ($1, 'https://hooks.example.test/in', 'secret') RETURNING id`, h.name(t, "Guarded")).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = h.super.Exec(context.Background(), `DELETE FROM webhook_endpoint WHERE id = $1`, id) })

	for what, statement := range map[string]string{
		"webhook_endpoint_version_positive": `UPDATE webhook_endpoint SET version = 0 WHERE id = $1`,
		"webhook_delivery_version_positive": `INSERT INTO webhook_delivery (endpoint_id, event_id, topic, request_body, version) VALUES ($1, gen_random_uuid(), 'ping', '{}', 0)`,
	} {
		if _, err := h.super.Exec(ctx, statement, id); err == nil || !strings.Contains(err.Error(), what) {
			t.Errorf("%s: err = %v, want the check to refuse version 0", what, err)
		}
	}
}
