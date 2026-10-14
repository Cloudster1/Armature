package events

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/observability"
)

// StreamKey is the Redis stream every relayed event is appended to.
const StreamKey = "armature:events"

// Relay moves committed outbox rows onto Redis.
//
// Delivery is at least once, never exactly once: the row is marked published
// after the append succeeds, so a crash in between replays the event. Consumers
// must therefore be idempotent, which is a far easier property to hold than
// distributed exactly-once would be.
type Relay struct {
	db      *db.Cluster
	redis   *redis.Client
	log     *slog.Logger
	stream  string
	groups  []string
	batch   int
	idle    time.Duration
	maxLen  int64
	metrics *observability.Metrics
}

// StreamMaxLen caps how far the slowest group may fall behind before the relay
// waits; at 100,000 a run of 256 KB hook bodies once cost Redis 8 GB.
const StreamMaxLen = 10_000

// NewRelay constructs the relay. batch caps how many events one pass claims and
// idle is how long it waits when there is nothing to do.
func NewRelay(cluster *db.Cluster, rdb *redis.Client, log *slog.Logger) *Relay {
	return &Relay{
		db:      cluster,
		redis:   rdb,
		log:     log,
		stream:  StreamKey,
		groups:  Groups,
		batch:   100,
		idle:    time.Second,
		maxLen:  StreamMaxLen,
		metrics: observability.Current(),
	}
}

// Onto points the relay at another stream with another cap, waiting for every
// group on it, so a test can watch it against a slow group of its own.
func (r *Relay) Onto(stream string, maxLen int64) *Relay {
	r.stream, r.maxLen, r.groups = stream, maxLen, nil
	return r
}

// pendingLimit caps the count of what is waiting, since a backlog past it is
// a backlog whatever its exact size and counting it all would cost a scan.
const pendingLimit = 10_000

// holdBackPause is how long the relay waits for the slowest group to make
// room before asking again; short, since reading frees room in milliseconds.
const holdBackPause = 200 * time.Millisecond

// Run relays until ctx is cancelled.
func (r *Relay) Run(ctx context.Context) error {
	r.log.Info("outbox relay started", "stream", r.stream, "batch", r.batch, "cap", r.maxLen)
	for {
		limit := r.batch
		backlog, err := r.trim(ctx)
		switch {
		case ctx.Err() != nil:
			r.log.Info("outbox relay stopped")
			return nil
		case err != nil:
			// Nothing was trimmed, so relaying on loses nothing; if Redis is
			// down the publish fails too and is recorded against the event.
			r.log.Warn("could not trim the event stream", "error", err)
		default:
			r.metrics.StreamBacklog.Set(float64(backlog))
			limit = room(backlog, r.maxLen, r.batch)
		}

		// The slowest group is a full cap behind: the events wait in the
		// outbox, which is the record, until it has read some.
		if limit == 0 {
			r.metrics.RelayHeld.Set(1)
			if err := r.db.ReadAdmin(ctx, r.measure); err != nil && ctx.Err() == nil {
				r.log.Warn("could not measure the outbox", "error", err)
			}
			if !pause(ctx, holdBackPause) {
				return nil
			}
			continue
		}
		r.metrics.RelayHeld.Set(0)

		n, err := r.once(ctx, limit)
		switch {
		case errors.Is(err, context.Canceled):
			r.log.Info("outbox relay stopped")
			return nil
		case err != nil:
			r.log.Error("outbox relay pass failed", "error", err)
		}

		// Only sleep when the queue was empty: a full batch means there is
		// probably more waiting, so go straight round again.
		if n < limit {
			if !pause(ctx, r.idle) {
				return nil
			}
		} else if ctx.Err() != nil {
			return nil
		}
	}
}

// pause waits d, and says false when ctx ended first.
func pause(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// once claims and relays up to limit events, returning how many it handled.
//
// The claim uses FOR UPDATE SKIP LOCKED so several worker replicas can relay
// concurrently without any of them blocking on, or duplicating, another's rows.
func (r *Relay) once(ctx context.Context, limit int) (int, error) {
	var events []Event
	_, err := r.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT id, topic, payload, created_at, coalesce(trace_parent, '')
			FROM outbox_event
			WHERE published_at IS NULL
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT $1`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e Event
			if err := rows.Scan(&e.ID, &e.Topic, &e.Payload, &e.CreatedAt, &e.Trace); err != nil {
				return err
			}
			events = append(events, e)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}

		published := make([]uuid.UUID, 0, len(events))
		for _, e := range events {
			if err := r.publish(ctx, e); err != nil {
				// Record the failure against this event and leave it for a
				// later pass rather than losing the whole batch.
				r.log.Warn("failed to publish event", "event_id", e.ID, "topic", e.Topic, "error", err)
				if _, uErr := tx.Exec(ctx, `
					UPDATE outbox_event
					SET attempts = attempts + 1, last_error = $2
					WHERE id = $1`, e.ID, err.Error()); uErr != nil {
					return uErr
				}
				continue
			}
			published = append(published, e.ID)
		}

		if len(published) > 0 {
			if _, err := tx.Exec(ctx, `
				UPDATE outbox_event SET published_at = now()
				WHERE id = ANY($1)`, published); err != nil {
				return err
			}
		}
		r.metrics.OutboxSent.Add(float64(len(published)))
		r.metrics.OutboxFailed.Add(float64(len(events) - len(published)))

		return r.measure(ctx, tx)
	})
	return len(events), err
}

// measure records how much is waiting for the relay and how long the oldest
// of it has waited, which is the relay's lag.
func (r *Relay) measure(ctx context.Context, tx db.DBTX) error {
	var pending int
	var oldest float64
	if err := tx.QueryRow(ctx, `
		SELECT count(*), coalesce(extract(epoch FROM now() - min(created_at)), 0)::float8
		FROM (SELECT created_at FROM outbox_event WHERE published_at IS NULL ORDER BY created_at LIMIT $1) waiting`,
		pendingLimit).Scan(&pending, &oldest); err != nil {
		return err
	}
	r.metrics.OutboxPending.Set(float64(pending))
	r.metrics.OutboxAge.Set(oldest)
	return nil
}

func (r *Relay) publish(ctx context.Context, e Event) error {
	return r.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: r.stream,
		Values: map[string]any{
			"id":      e.ID.String(),
			"topic":   e.Topic,
			"payload": string(e.Payload),
			"trace":   e.Trace,
		},
	}).Err()
}

// FromMessage reads an event back off the stream, the inverse of publish. A
// field that does not parse is left zero rather than failing the whole read.
func FromMessage(msg redis.XMessage) Event {
	get := func(k string) string {
		if v, ok := msg.Values[k].(string); ok {
			return v
		}
		return ""
	}
	e := Event{Topic: get("topic"), Payload: json.RawMessage(get("payload")), Trace: get("trace")}
	e.ID, _ = uuid.Parse(get("id"))
	return e
}
