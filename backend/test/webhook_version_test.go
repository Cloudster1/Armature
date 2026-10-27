//go:build integration

package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/httpapi"
	"github.com/armature/armature/backend/internal/openapi"
	"github.com/armature/armature/backend/internal/webhook"
)

// envelopeVersionHeader is spelled out rather than taken from the package, so
// renaming the constant cannot quietly change what receivers read.
const envelopeVersionHeader = "X-Armature-Webhook-Version"

// envelopeSchema is the body the document says a webhook delivery carries in
// the media type, one per envelope version.
func envelopeSchema(t *testing.T, mediaType string) (*openapi.Document, *openapi.Schema) {
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
	schema := described.Webhooks["event"]["post"].RequestBody.Content[mediaType].Schema
	if schema == nil {
		t.Fatalf("the document describes no webhook delivery as %s", mediaType)
	}
	return doc, schema
}

// sentInVersion checks one delivery a receiver kept against the document and
// the version it should be in, header and body alike.
func sentInVersion(t *testing.T, got received, version int) {
	t.Helper()
	if got.Version != strconv.Itoa(version) {
		t.Errorf("the delivery has the version header %q, want %d: %s", got.Version, version, got.Body)
	}
	mediaType, contentType := "application/json", "application/json"
	if version == 2 {
		mediaType, contentType = "application/cloudevents+json", "application/cloudevents+json; charset=utf-8"
	}
	if got.ContentType != contentType {
		t.Errorf("version %d is posted as %q, want %q", version, got.ContentType, contentType)
	}
	doc, schema := envelopeSchema(t, mediaType)
	if err := doc.ValidateJSON(schema, []byte(got.Body)); err != nil {
		t.Errorf("the delivery does not fit the document: %v: %s", err, got.Body)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal([]byte(got.Body), &body); err != nil {
		t.Fatal(err)
	}
	switch version {
	case 1:
		if string(body["version"]) != "1" {
			t.Errorf("a version 1 body says version %s: %s", body["version"], got.Body)
		}
	case 2:
		if string(body["specversion"]) != `"1.0"` {
			t.Errorf("a version 2 body is not CloudEvents 1.0: %s", got.Body)
		}
		for _, field := range []string{"version", "topic", "occurredAt", "payload", "orgId"} {
			if _, there := body[field]; there {
				t.Errorf("a version 2 body carries version 1's %s: %s", field, got.Body)
			}
		}
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
	moveEndpointVersion(t, h, uuid.MustParse(hookID), 3)
	edited := want(t, c.patch("/api/v1/webhooks/"+hookID, map[string]any{"name": name, "url": address, "topics": []string{}, "enabled": false}), http.StatusOK, "turning it off")
	if got := obj(t, edited, "webhook")["version"]; got != float64(3) {
		t.Errorf("an edit without a version left the webhook on %v, want the 3 it had", got)
	}
	want(t, c.patch("/api/v1/webhooks/"+hookID, map[string]any{"name": name, "url": address, "topics": []string{}, "version": 0}), http.StatusUnprocessableEntity, "version 0")
	moved := want(t, c.patch("/api/v1/webhooks/"+hookID, map[string]any{"name": name, "url": address, "topics": []string{}, "version": 2}), http.StatusOK, "moving it to version 2")
	if got := obj(t, moved, "webhook")["version"]; got != float64(2) {
		t.Errorf("an edit asking for version 2 left the webhook on %v", got)
	}
	catalog := want(t, c.get("/api/v1/automation/catalog"), http.StatusOK, "the catalog")
	if got := catalog.Body["versions"]; fmt.Sprint(got) != "[1 2]" {
		t.Errorf("the catalog offers the versions %v, want [1 2]", got)
	}
}

// A webhook on version 2 is posted a CloudEvent, signed as version 1 is, and a
// redelivery carries the same event (#286).
func TestAWebhookOnVersionTwoIsPostedACloudEvent(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signIn(t, h.person(t, "cloudevents", auth.RoleOwner))
	sink := &receiver{status: http.StatusOK}
	server := httptest.NewServer(sink)
	t.Cleanup(server.Close)

	made := want(t, c.post("/api/v1/webhooks", map[string]any{"name": h.name(t, "Broker"), "url": server.URL + "/in", "topics": []string{}, "version": 2}), http.StatusCreated, "a webhook on version 2")
	hookID := idOf(t, made, "webhook")
	t.Cleanup(func() { c.delete("/api/v1/webhooks/" + hookID) })
	secret, _ := obj(t, made, "webhook")["secret"].(string)
	pinged := want(t, c.post("/api/v1/webhooks/"+hookID+"/test", nil), http.StatusOK, "a test delivery")
	if len(sink.got) != 1 {
		t.Fatalf("the receiver was posted %d times, want once", len(sink.got))
	}
	got := sink.got[0]
	sentInVersion(t, got, 2)
	if !webhook.Verify([]byte(got.Body), secret, got.Signature) {
		t.Error("the version 2 body does not verify with the endpoint's secret")
	}
	var event struct {
		ID     string          `json:"id"`
		Source string          `json:"source"`
		Type   string          `json:"type"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(got.Body), &event); err != nil {
		t.Fatal(err)
	}
	if event.Source != "http://app.test" || event.Type != "dev.armature.ping" || !strings.Contains(string(event.Data), "Armature can reach this address.") {
		t.Errorf("the event = %s, want the installation as source, dev.armature.ping as type and the ping's payload as data", got.Body)
	}

	deliveryID, _ := obj(t, pinged, "delivery")["id"].(string)
	want(t, c.post("/api/v1/webhooks/"+hookID+"/deliveries/"+deliveryID+"/redeliver", nil), http.StatusOK, "a redelivery")
	if len(sink.got) != 2 {
		t.Fatalf("the receiver was posted %d times, want twice", len(sink.got))
	}
	if again := sink.got[1]; again.Body != got.Body {
		t.Errorf("the redelivery is %s, want the same event %s", again.Body, got.Body)
	}
	sentInVersion(t, sink.got[1], 2)
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
