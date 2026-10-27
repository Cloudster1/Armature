package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/events"
)

// CurrentVersion is the envelope a new endpoint is registered for. It rises
// only for a change that breaks a receiver, as docs/webhooks.md says.
const CurrentVersion = 1

// ErrUnknownVersion is an envelope version this release does not send.
var ErrUnknownVersion = errors.New("there is no such webhook envelope version")

// DefaultSource is the source of an envelope from a service not told the
// installation's address, as in a test.
const DefaultSource = "/armature"

// TypePrefix is what a version 2 type puts before the version 1 topic, the
// reverse domain the suite's tools agreed on (#286).
const TypePrefix = "dev.armature."

// The media types of a body: version 2 is CloudEvents' structured mode.
const (
	ContentTypeV1 = "application/json"
	ContentTypeV2 = "application/cloudevents+json; charset=utf-8"
)

// PayloadVersion is the version of the payload itself, which version 2 states
// as dataversion; it rises when a payload breaks, apart from the envelope.
const PayloadVersion = 1

// EnvelopeV1 is what an endpoint on version 1 receives. Its orgId goes with
// version 2, so an endpoint on 1 keeps it (#218).
type EnvelopeV1 struct {
	Version    int             `json:"version"`
	ID         uuid.UUID       `json:"id"`
	Topic      string          `json:"topic"`
	OrgID      uuid.UUID       `json:"orgId" deprecated:"The installation's stable id, the same in every delivery. Envelope version 2 leaves it out; read nothing from it."`
	OccurredAt time.Time       `json:"occurredAt"`
	Payload    json.RawMessage `json:"payload"`
}

// EnvelopeV2 is a CloudEvents 1.0 event in structured mode, so a receiver or a
// broker that knows CloudEvents reads it without code for Armature.
type EnvelopeV2 struct {
	SpecVersion     string          `json:"specversion"`
	ID              uuid.UUID       `json:"id"`
	Source          string          `json:"source"`
	Type            string          `json:"type"`
	Time            time.Time       `json:"time"`
	DataContentType string          `json:"datacontenttype"`
	DataVersion     int             `json:"dataversion"`
	Data            json.RawMessage `json:"data"`
}

// origin is where an envelope says its event comes from.
type origin struct {
	installation uuid.UUID
	source       string
}

// envelopes builds each version's body. A later version is a row beside the
// others, and an endpoint keeps getting the one it was registered for.
var envelopes = map[int]func(from origin, e events.Event, at time.Time) any{
	1: func(from origin, e events.Event, at time.Time) any {
		return EnvelopeV1{Version: 1, ID: e.ID, Topic: e.Topic, OrgID: from.installation, OccurredAt: at, Payload: e.Payload}
	},
	2: func(from origin, e events.Event, at time.Time) any {
		happened := at
		if !e.CreatedAt.IsZero() {
			happened = e.CreatedAt.UTC()
		}
		return EnvelopeV2{
			SpecVersion: "1.0", ID: e.ID, Source: from.source, Type: TypePrefix + e.Topic, Time: happened,
			DataContentType: "application/json", DataVersion: PayloadVersion, Data: e.Payload,
		}
	},
}

// Versions lists the envelope versions this release sends, lowest first.
func Versions() []int {
	out := make([]int, 0, len(envelopes))
	for v := range envelopes {
		out = append(out, v)
	}
	slices.Sort(out)
	return out
}

// ContentType is the media type a body of the version is posted as.
func ContentType(version int) string {
	if version >= 2 {
		return ContentTypeV2
	}
	return ContentTypeV1
}

// envelopeFor is the body an endpoint on the version is posted for the event.
func envelopeFor(version int, from origin, e events.Event, at time.Time) ([]byte, error) {
	build, ok := envelopes[version]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrUnknownVersion, version)
	}
	e.Payload = withoutSecrets(e.Payload)
	return json.Marshal(build(from, e, at.UTC()))
}

// withoutSecrets drops what an event carries for a mail rather than for a
// reader: an unwatch link's token is one, and an endpoint has no use for it.
func withoutSecrets(payload json.RawMessage) json.RawMessage {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(payload, &body); err != nil {
		return payload
	}
	if _, carried := body["token"]; !carried {
		return payload
	}
	delete(body, "token")
	cleaned, err := json.Marshal(body)
	if err != nil {
		return payload
	}
	return cleaned
}
