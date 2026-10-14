package webhook

import (
	"encoding/json"
	"errors"
	"strconv"
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
	body, err := envelopeFor(1, installationID, events.Event{ID: eventID, Topic: events.TopicWatcherAdded, Payload: json.RawMessage(`{"key":"X-1","token":"unwatch"}`)}, at)
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

func TestAVersionNotOfferedBuildsNothing(t *testing.T) {
	for _, version := range []int{0, 2, -1} {
		if _, err := envelopeFor(version, uuid.New(), events.Event{ID: uuid.New(), Topic: TopicPing, Payload: json.RawMessage(`{}`)}, time.Now()); !errors.Is(err, ErrUnknownVersion) {
			t.Errorf("version %d: err = %v, want ErrUnknownVersion", version, err)
		}
	}
}
