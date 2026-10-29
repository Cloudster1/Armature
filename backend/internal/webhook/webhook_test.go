package webhook

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/events"
)

func TestSignAndVerify(t *testing.T) {
	body := []byte(`{"id":"x"}`)
	sig := Sign(body, "secret")
	if len(sig) != len("sha256=")+64 || sig[:7] != "sha256=" {
		t.Fatalf("signature = %q", sig)
	}
	if !Verify(body, "secret", sig) || Verify(body, "other", sig) || Verify([]byte(`{}`), "secret", sig) {
		t.Fatal("verify disagrees with sign")
	}
}

// The timestamped signature binds the body to the moment it was sent: another
// time, another secret or another body is refused, and so is an old delivery.
func TestATimestampedSignatureRefusesAReplay(t *testing.T) {
	body := []byte(`{"id":"x"}`)
	sent := time.Unix(1_790_000_000, 0)
	stamp := strconv.FormatInt(sent.Unix(), 10)
	sig := SignTimestamped(body, "secret", sent)
	window := 5 * time.Minute

	if sig == Sign(body, "secret") {
		t.Fatal("the timestamped signature does not depend on the time")
	}
	if !VerifyTimestamped(body, "secret", stamp, sig, window, sent.Add(time.Minute)) {
		t.Fatal("a fresh delivery was refused")
	}
	for name, ok := range map[string]bool{
		"another secret":       VerifyTimestamped(body, "other", stamp, sig, window, sent),
		"another body":         VerifyTimestamped([]byte(`{}`), "secret", stamp, sig, window, sent),
		"a moved timestamp":    VerifyTimestamped(body, "secret", strconv.FormatInt(sent.Unix()+1, 10), sig, window, sent),
		"a padded timestamp":   VerifyTimestamped(body, "secret", "0"+stamp, sig, window, sent),
		"no timestamp":         VerifyTimestamped(body, "secret", "", sig, window, sent),
		"a replay much later":  VerifyTimestamped(body, "secret", stamp, sig, window, sent.Add(window+time.Second)),
		"a time in the future": VerifyTimestamped(body, "secret", stamp, sig, window, sent.Add(-window-time.Second)),
	} {
		if ok {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestBackoffCoversEveryRetry(t *testing.T) {
	if len(Backoff) != MaxAttempts-1 {
		t.Fatalf("%d waits for %d attempts", len(Backoff), MaxAttempts)
	}
	for i := 1; i < len(Backoff); i++ {
		if Backoff[i] <= Backoff[i-1] || Backoff[i] > 24*time.Hour {
			t.Errorf("backoff %d = %s", i, Backoff[i])
		}
	}
}

// An address can carry a credential in its path or query, as a chat service's
// incoming hook does, so the audit log keeps only where it points.
func TestAuditedAddressKeepsOnlyTheHost(t *testing.T) {
	for in, want := range map[string]string{
		"https://hooks.chat.example/services/T0/B0/hunter2": "https://hooks.chat.example",
		"http://10.0.0.5:8080/in?token=hunter2":             "http://10.0.0.5:8080",
		"https://user:hunter2@example.com/hook":             "https://example.com",
		"not an address":                                    "",
	} {
		if got := auditedAddress(in); got != want {
			t.Errorf("auditedAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

// Version 1 is the envelope receivers were written against before it had a
// number: the same fields, the number added, and a mail's token left out.
func TestVersionOneIsTheEnvelopeWithItsNumber(t *testing.T) {
	installationID, eventID := uuid.New(), uuid.New()
	at := time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC)
	body, err := envelopeFor(1, origin{installation: installationID, source: DefaultSource}, events.Event{ID: eventID, Topic: events.TopicWatcherAdded, Payload: json.RawMessage(`{"key":"X-1","token":"unwatch"}`)}, at)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"version":    `1`,
		"id":         `"` + eventID.String() + `"`,
		"topic":      `"watcher.added"`,
		"orgId":      `"` + installationID.String() + `"`,
		"occurredAt": `"2026-10-14T09:30:00Z"`,
		"payload":    `{"key":"X-1"}`,
	}
	if len(got) != len(want) {
		t.Errorf("the envelope has %d fields, want %d: %s", len(got), len(want), body)
	}
	for field, value := range want {
		if string(got[field]) != value {
			t.Errorf("%s = %s, want %s", field, got[field], value)
		}
	}
	if CurrentVersion != 1 {
		t.Errorf("a new endpoint gets version %d; a receiver written for 1 would not know it", CurrentVersion)
	}
}

// Version 2 is CloudEvents 1.0 in structured mode, as Exciter reads it: the
// version 1 payload as data, and none of version 1's own fields.
func TestVersionTwoIsACloudEvent(t *testing.T) {
	eventID := uuid.New()
	happened := time.Date(2026, 10, 14, 9, 29, 58, 0, time.FixedZone("CEST", 2*60*60))
	queued := time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC)
	from := origin{installation: uuid.New(), source: "https://armature.example.com"}
	event := events.Event{ID: eventID, Topic: events.TopicWatcherAdded, CreatedAt: happened, Payload: json.RawMessage(`{"key":"X-1","token":"unwatch"}`)}
	body, err := envelopeFor(2, from, event, queued)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"specversion":     `"1.0"`,
		"id":              `"` + eventID.String() + `"`,
		"source":          `"https://armature.example.com"`,
		"type":            `"dev.armature.watcher.added"`,
		"time":            `"2026-10-14T07:29:58Z"`,
		"datacontenttype": `"application/json"`,
		"dataversion":     `1`,
		"data":            `{"key":"X-1"}`,
	}
	if len(got) != len(want) {
		t.Errorf("the envelope has %d fields, want %d: %s", len(got), len(want), body)
	}
	for field, value := range want {
		if string(got[field]) != value {
			t.Errorf("%s = %s, want %s", field, got[field], value)
		}
	}
	if ContentType(2) != "application/cloudevents+json; charset=utf-8" || ContentType(1) != "application/json" {
		t.Errorf("content types: 1 is %q, 2 is %q", ContentType(1), ContentType(2))
	}

	// A test delivery has no time of its own, so it carries when it was queued.
	ping, err := envelopeFor(2, from, events.Event{ID: uuid.New(), Topic: TopicPing, Payload: json.RawMessage(`{}`)}, queued)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ping), `"time":"2026-10-14T09:30:00Z"`) || !strings.Contains(string(ping), `"type":"dev.armature.ping"`) {
		t.Errorf("a ping in version 2 is %s", ping)
	}
}

func TestAVersionNotOfferedBuildsNothing(t *testing.T) {
	for _, version := range []int{0, 3, -1} {
		if _, err := envelopeFor(version, origin{installation: uuid.New()}, events.Event{ID: uuid.New(), Topic: TopicPing, Payload: json.RawMessage(`{}`)}, time.Now()); !errors.Is(err, ErrUnknownVersion) {
			t.Errorf("version %d: err = %v, want ErrUnknownVersion", version, err)
		}
	}
}

// An endpoint takes what the desk publishes when a reply cannot reopen a request,
// and not a topic nothing publishes.
func TestAnEndpointTakesTheTopicsThatArePublished(t *testing.T) {
	in := Input{Name: "Desk", URL: "https://example.com/hook", Topics: []string{events.TopicRequestStaysDone}}
	if err := validate(in); err != nil {
		t.Errorf("request.stays_done was refused: %v", err)
	}
	in.Topics = []string{"ci.deployment.succeeded"}
	if err := validate(in); err == nil {
		t.Error("ci.deployment.succeeded, which nothing publishes, was taken")
	}
}
