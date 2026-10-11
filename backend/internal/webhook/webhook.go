// Package webhook posts the organization's events to addresses it named,
// signed with a secret each endpoint was shown once, and keeps a delivery log
// with retries. A rule's "send a webhook" and a topic subscription both end
// here, so there is one way an event leaves the product over HTTP.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/config"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/netguard"
	"github.com/armature/armature/backend/internal/tenant"
)

const (
	// MaxAttempts is how many times one event is tried before it is given up.
	MaxAttempts = 6
	// sendTimeout bounds one request; a slow endpoint is a failed attempt.
	sendTimeout = 10 * time.Second
	// maxResponseBytes is the most of an answer that is read, for the log.
	maxResponseBytes = 4 << 10
	// TopicAny subscribes an endpoint to everything.
	TopicAny = "*"
	// TopicPing is what a test delivery carries.
	TopicPing = "ping"
	// secretPrefix marks a webhook secret so one is never mistaken for a token.
	secretPrefix = "armature_whs_"
	// DefaultDeliveries is how many log rows a page shows.
	DefaultDeliveries = 50
)

// Backoff is the wait before each retry, by attempt number; a sixth failure
// is the last.
var Backoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

// ErrNotFound is returned for an endpoint or delivery that is not here.
var ErrNotFound = errors.New("there is no such webhook")

// Endpoint is one address the organization posts to.
type Endpoint struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Topics    []string  `json:"topics"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Secret is set only on the answer that made or rotated it.
	Secret string `json:"secret,omitempty"`
}

// Delivery is one attempt at one event.
type Delivery struct {
	ID            uuid.UUID  `json:"id"`
	EndpointID    uuid.UUID  `json:"endpointId"`
	EventID       uuid.UUID  `json:"eventId"`
	Topic         string     `json:"topic"`
	Attempt       int        `json:"attempt"`
	Status        *int       `json:"status,omitempty"`
	Error         string     `json:"error,omitempty"`
	NextAttemptAt *time.Time `json:"nextAttemptAt,omitempty"`
	DeliveredAt   *time.Time `json:"deliveredAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
}

// Input is what makes or changes an endpoint.
type Input struct {
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Topics  []string `json:"topics"`
	Enabled *bool    `json:"enabled,omitempty"`
}

// Sign computes the header value for a body: sha256= and the hex HMAC, the
// shape GitHub uses, so a receiver written for one serves both.
func Sign(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Verify says whether a signature header matches the body.
func Verify(body []byte, secret, header string) bool {
	return hmac.Equal([]byte(Sign(body, secret)), []byte(header))
}

// The headers a delivery is signed in. The timestamped signature also covers
// when it was sent, so a receiver can refuse one replayed later.
const (
	HeaderSignature            = "X-Armature-Signature-256"
	HeaderTimestamp            = "X-Armature-Timestamp"
	HeaderTimestampedSignature = "X-Armature-Signature-Timestamped-256"
)

// SignTimestamped signs "{timestamp}.{body}", the time in Unix seconds as the
// timestamp header carries it.
func SignTimestamped(body []byte, secret string, at time.Time) string {
	return Sign(append([]byte(strconv.FormatInt(at.Unix(), 10)+"."), body...), secret)
}

// VerifyTimestamped says whether the timestamped signature matches and the time
// it names is within tolerance of now; a replay outside that window is refused.
func VerifyTimestamped(body []byte, secret, timestamp, header string, tolerance time.Duration, now time.Time) bool {
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || strconv.FormatInt(seconds, 10) != timestamp {
		return false
	}
	at := time.Unix(seconds, 0)
	if drift := now.Sub(at); drift > tolerance || drift < -tolerance {
		return false
	}
	return hmac.Equal([]byte(SignTimestamped(body, secret, at)), []byte(header))
}

// Service keeps endpoints and deliveries and does the posting.
type Service struct {
	db     *db.Cluster
	client *http.Client
	log    *slog.Logger
	now    func() time.Time
	// concurrency is how many deliveries one pass posts at once.
	concurrency int
}

func NewService(cluster *db.Cluster, log *slog.Logger) *Service {
	return &Service{
		db: cluster, client: netguard.Client(sendTimeout, netguard.FromEnv()), log: log, now: time.Now,
		concurrency: config.DefaultWebhookConcurrency,
	}
}

// WithClient swaps the HTTP client, which a test does to point at itself.
func (s *Service) WithClient(c *http.Client) *Service {
	s.client = c
	return s
}

// WithConcurrency sets how many deliveries are posted at once; the worker
// passes ARMATURE_WEBHOOK_CONCURRENCY, which configuration keeps at one or more.
func (s *Service) WithConcurrency(n int) *Service {
	s.concurrency = max(n, 1)
	return s
}

const selectEndpoints = `SELECT id, name, url, topics, enabled, created_at, updated_at FROM webhook_endpoint`

func scanEndpoint(row pgx.Row) (*Endpoint, error) {
	var e Endpoint
	if err := row.Scan(&e.ID, &e.Name, &e.URL, &e.Topics, &e.Enabled, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return nil, err
	}
	if e.Topics == nil {
		e.Topics = []string{}
	}
	return &e, nil
}

// List is the organization's endpoints, by name.
func (s *Service) List(ctx context.Context) ([]Endpoint, error) {
	out := []Endpoint{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectEndpoints+` ORDER BY lower(name)`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e, err := scanEndpoint(rows)
			if err != nil {
				return err
			}
			out = append(out, *e)
		}
		return rows.Err()
	})
	return out, err
}

func validate(in Input) error {
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("a webhook needs a name")
	}
	if !strings.HasPrefix(in.URL, "http://") && !strings.HasPrefix(in.URL, "https://") {
		return errors.New("the address has to start with http:// or https://")
	}
	for _, t := range in.Topics {
		if t != TopicAny && (!events.Known(t) || strings.HasPrefix(t, "automation.")) {
			return fmt.Errorf("%q is not an event a webhook can take", t)
		}
	}
	return nil
}

// Create makes an endpoint and returns it with the secret, this once.
func (s *Service) Create(ctx context.Context, in Input) (*Endpoint, db.LSN, error) {
	if err := validate(in); err != nil {
		return nil, 0, err
	}
	secret, _, err := auth.GenerateToken()
	if err != nil {
		return nil, 0, err
	}
	secret = secretPrefix + secret
	if in.Topics == nil {
		in.Topics = []string{}
	}
	var out *Endpoint
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := scanEndpoint(tx.QueryRow(ctx, `
			INSERT INTO webhook_endpoint (org_id, name, url, secret, topics, enabled)
			VALUES (current_org_id(), $1, $2, $3, $4, true)
			RETURNING id, name, url, topics, enabled, created_at, updated_at`, strings.TrimSpace(in.Name), in.URL, secret, in.Topics))
		if isUnique(err) {
			return errors.New("a webhook with that name is already here")
		}
		if err != nil {
			return err
		}
		out = e
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	out.Secret = secret
	return out, lsn, nil
}

// Update changes name, address, topics or whether it is on.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) (*Endpoint, db.LSN, error) {
	if err := validate(in); err != nil {
		return nil, 0, err
	}
	if in.Topics == nil {
		in.Topics = []string{}
	}
	var out *Endpoint
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := scanEndpoint(tx.QueryRow(ctx, `
			UPDATE webhook_endpoint SET name = $2, url = $3, topics = $4, enabled = COALESCE($5, enabled)
			WHERE id = $1
			RETURNING id, name, url, topics, enabled, created_at, updated_at`, id, strings.TrimSpace(in.Name), in.URL, in.Topics, in.Enabled))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if isUnique(err) {
			return errors.New("a webhook with that name is already here")
		}
		if err != nil {
			return err
		}
		out = e
		return nil
	})
	return out, lsn, err
}

// RotateSecret issues a new secret; the old one stops working at once.
func (s *Service) RotateSecret(ctx context.Context, id uuid.UUID) (*Endpoint, db.LSN, error) {
	secret, _, err := auth.GenerateToken()
	if err != nil {
		return nil, 0, err
	}
	secret = secretPrefix + secret
	var out *Endpoint
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		e, err := scanEndpoint(tx.QueryRow(ctx, `
			UPDATE webhook_endpoint SET secret = $2 WHERE id = $1
			RETURNING id, name, url, topics, enabled, created_at, updated_at`, id, secret))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		out = e
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	out.Secret = secret
	return out, lsn, nil
}

// Delete removes the endpoint and its log.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM webhook_endpoint WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

const selectDeliveries = `
SELECT id, endpoint_id, event_id, topic, attempt, status, error, next_attempt_at, delivered_at, created_at
FROM webhook_delivery`

func scanDelivery(row pgx.Row) (*Delivery, error) {
	var d Delivery
	if err := row.Scan(&d.ID, &d.EndpointID, &d.EventID, &d.Topic, &d.Attempt, &d.Status, &d.Error, &d.NextAttemptAt, &d.DeliveredAt, &d.CreatedAt); err != nil {
		return nil, err
	}
	return &d, nil
}

// Deliveries is an endpoint's log, newest first.
func (s *Service) Deliveries(ctx context.Context, endpointID uuid.UUID, limit int) ([]Delivery, error) {
	if limit <= 0 {
		limit = DefaultDeliveries
	}
	out := []Delivery{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var found bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM webhook_endpoint WHERE id = $1)`, endpointID).Scan(&found); err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		rows, err := tx.Query(ctx, selectDeliveries+` WHERE endpoint_id = $1 ORDER BY created_at DESC LIMIT $2`, endpointID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanDelivery(rows)
			if err != nil {
				return err
			}
			out = append(out, *d)
		}
		return rows.Err()
	})
	return out, err
}

// envelope is what an endpoint receives.
type envelope struct {
	ID         uuid.UUID       `json:"id"`
	Topic      string          `json:"topic"`
	OrgID      uuid.UUID       `json:"orgId"`
	OccurredAt time.Time       `json:"occurredAt"`
	Payload    json.RawMessage `json:"payload"`
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

// Enqueue records an event for one endpoint, due now. The sender picks it up;
// a second call for the same event and endpoint is a no-op.
func (s *Service) Enqueue(ctx context.Context, endpointID uuid.UUID, e events.Event) (db.LSN, error) {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if len(e.Payload) == 0 {
		e.Payload = json.RawMessage(`{}`)
	}
	body, _ := json.Marshal(envelope{ID: e.ID, Topic: e.Topic, OrgID: e.OrgID, OccurredAt: s.now().UTC(), Payload: withoutSecrets(e.Payload)})
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO webhook_delivery (org_id, endpoint_id, event_id, topic, attempt, request_body, next_attempt_at)
			VALUES (current_org_id(), $1, $2, $3, 1, $4, now())
			ON CONFLICT (endpoint_id, event_id, attempt) DO NOTHING`, endpointID, e.ID, e.Topic, body)
		return err
	})
}

// Handle is the consumer's half: every enabled endpoint subscribed to the
// topic gets the event queued. The automation's own topics stay inside: an
// endpoint that took them and posted to an incoming hook would feed itself.
func (s *Service) Handle(ctx context.Context, e events.Event) error {
	if e.OrgID == uuid.Nil || e.Topic == "" || strings.HasPrefix(e.Topic, "automation.") {
		return nil
	}
	ctx = db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: e.OrgID}))
	var ids []uuid.UUID
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT id FROM webhook_endpoint WHERE enabled AND ($1 = ANY(topics) OR $2 = ANY(topics))`, e.Topic, TopicAny)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := s.Enqueue(ctx, id, e); err != nil {
			return err
		}
	}
	return nil
}

// Test posts a ping to the endpoint now and returns the attempt as logged.
func (s *Service) Test(ctx context.Context, endpointID uuid.UUID) (*Delivery, db.LSN, error) {
	lsn, err := s.Enqueue(ctx, endpointID, events.Event{ID: uuid.New(), OrgID: mustOrg(ctx), Topic: TopicPing, Payload: json.RawMessage(`{"message":"Armature can reach this address."}`)})
	if err != nil {
		return nil, 0, err
	}
	// The delivery was written a moment ago; a replica may not have it yet.
	ctx = db.PinPrimary(ctx)
	var d *Delivery
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		d, err = scanDelivery(tx.QueryRow(ctx, selectDeliveries+` WHERE endpoint_id = $1 AND topic = $2 ORDER BY created_at DESC LIMIT 1`, endpointID, TopicPing))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	sent, outcome, err := s.attempt(ctx, d.ID)
	if err != nil {
		return nil, 0, err
	}
	return sent, max(lsn, outcome), nil
}

// Redeliver tries a logged delivery again, now, as a fresh first attempt.
func (s *Service) Redeliver(ctx context.Context, deliveryID uuid.UUID) (*Delivery, db.LSN, error) {
	var made uuid.UUID
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO webhook_delivery (org_id, endpoint_id, event_id, topic, attempt, request_body, next_attempt_at)
			SELECT org_id, endpoint_id, event_id, topic,
			       (SELECT max(attempt) FROM webhook_delivery d2 WHERE d2.endpoint_id = d.endpoint_id AND d2.event_id = d.event_id) + 1,
			       request_body, now()
			FROM webhook_delivery d WHERE id = $1
			RETURNING id`, deliveryID).Scan(&made)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	sent, outcome, err := s.attempt(db.PinPrimary(ctx), made)
	return sent, max(lsn, outcome), err
}

func mustOrg(ctx context.Context) uuid.UUID {
	org, _ := tenant.FromContext(ctx)
	return org.ID
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "webhook_endpoint_name_idx")
}
