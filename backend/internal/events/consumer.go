package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/armature/armature/backend/internal/db"
)

// Handler acts on one event: nil when it is done with, Permanent when another
// attempt cannot help, and any other error to be handed the event again.
type Handler func(ctx context.Context, e Event) error

type permanent struct{ err error }

func (p permanent) Error() string { return p.err.Error() }
func (p permanent) Unwrap() error { return p.err }

// Permanent marks a failure another attempt would not cure, or would make
// worse by repeating what already went out. The event is set aside at once.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanent{err}
}

// IsPermanent says whether a handler gave up on an event for good.
func IsPermanent(err error) bool {
	var p permanent
	return errors.As(err, &p)
}

// ActionDeadLettered is the audit action an event set aside is recorded under,
// so the organization's administrators read on the audit page what was lost.
const ActionDeadLettered = "event.dead_lettered"

// Policy is how a consumer retries and how it stops.
type Policy struct {
	// MinIdle is how long an entry stays unacknowledged before it counts as
	// abandoned; a handler that runs longer may be handed it twice.
	MinIdle time.Duration
	// ClaimEvery is how often the group's abandoned entries are looked for.
	ClaimEvery time.Duration
	// MaxDeliveries is how many attempts an event gets before it is set aside.
	MaxDeliveries int64
	// Grace is how long the event in hand still has once a shutdown begins.
	Grace time.Duration
}

// DefaultPolicy spreads five attempts over about five minutes, which outlasts
// a failover or a rolling restart; Grace fits the chart's 20s for the worker.
var DefaultPolicy = Policy{MinIdle: time.Minute, ClaimEvery: 30 * time.Second, MaxDeliveries: 5, Grace: 10 * time.Second}

const (
	// consumerName is shared by every replica: whoever claims an entry is
	// the one to finish it, so a name per process would only add bookkeeping.
	consumerName = "worker"
	batchSize    = 50
	// readBlock is how long one read waits for news, and so how quickly a
	// shutdown is noticed between batches.
	readBlock  = 5 * time.Second
	readPause  = time.Second
	claimStart = "0-0"
)

// Consumer reads the stream for one group, acknowledging an event only once
// it was handled or set aside; everything else is claimed again and retried.
type Consumer struct {
	db     *db.Cluster
	redis  *redis.Client
	log    *slog.Logger
	group  string
	handle Handler
	policy Policy
}

// NewConsumer builds the loop every group of the worker runs.
func NewConsumer(cluster *db.Cluster, rdb *redis.Client, log *slog.Logger, group string, handle Handler) *Consumer {
	return &Consumer{db: cluster, redis: rdb, log: log, group: group, handle: handle, policy: DefaultPolicy}
}

// WithPolicy replaces the default retry policy.
func (c *Consumer) WithPolicy(p Policy) *Consumer {
	c.policy = p
	return c
}

// Run consumes the stream until the context ends. What is left unacknowledged
// then is claimed by the next worker to start.
func (c *Consumer) Run(ctx context.Context) error {
	err := c.redis.XGroupCreateMkStream(ctx, StreamKey, c.group, "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create consumer group %s: %w", c.group, err)
	}
	var claimed time.Time
	for ctx.Err() == nil {
		if time.Since(claimed) >= c.policy.ClaimEvery {
			c.claim(ctx)
			claimed = time.Now()
		}
		streams, err := c.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: c.group, Consumer: consumerName, Streams: []string{StreamKey, ">"},
			Count: batchSize, Block: min(readBlock, c.policy.ClaimEvery),
		}).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			c.log.Warn("the event stream could not be read", "group", c.group, "error", err)
			select {
			case <-ctx.Done():
			case <-time.After(readPause):
			}
			continue
		}
		for _, stream := range streams {
			c.batch(ctx, stream.Messages)
		}
	}
	return nil
}

// claim takes over every entry of the group left unacknowledged for longer
// than MinIdle, whichever worker read it, and handles it.
func (c *Consumer) claim(ctx context.Context) {
	start := claimStart
	for ctx.Err() == nil {
		msgs, next, err := c.redis.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream: StreamKey, Group: c.group, Consumer: consumerName,
			MinIdle: c.policy.MinIdle, Start: start, Count: batchSize,
		}).Result()
		if err != nil {
			if ctx.Err() == nil {
				c.log.Warn("abandoned events could not be claimed", "group", c.group, "error", err)
			}
			return
		}
		c.batch(ctx, msgs)
		if next == claimStart || next == "" {
			return
		}
		start = next
	}
}

// batch handles messages in order until a shutdown; the rest stay pending.
func (c *Consumer) batch(ctx context.Context, msgs []redis.XMessage) {
	for _, msg := range msgs {
		if ctx.Err() != nil {
			return
		}
		c.one(ctx, msg)
	}
}

// one handles a message under a context a shutdown does not cancel at once, so
// the event in hand is finished and acknowledged rather than left half done.
func (c *Consumer) one(ctx context.Context, msg redis.XMessage) {
	work, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	stop := context.AfterFunc(ctx, func() { time.AfterFunc(c.policy.Grace, cancel) })
	defer stop()

	e := FromMessage(msg)
	hctx, done := Continue(work, e, c.group)
	err := c.handle(hctx, e)
	done(err)
	if err == nil {
		c.ack(work, msg.ID)
		return
	}
	attempts, pending := c.deliveries(work, msg.ID)
	switch {
	case !pending:
		// Somebody else finished it meanwhile, or nobody can say how often
		// it was tried; either way the next claim decides.
	case IsPermanent(err) || (attempts >= c.policy.MaxDeliveries && ctx.Err() == nil):
		c.setAside(work, msg.ID, e, attempts, err)
	default:
		c.log.Warn("an event failed and will be tried again", "group", c.group, "id", msg.ID,
			"topic", e.Topic, "attempt", attempts, "error", err)
	}
}

// deliveries asks how often the group has been handed the entry, and whether
// it is still waiting for an acknowledgement at all.
func (c *Consumer) deliveries(ctx context.Context, id string) (int64, bool) {
	p, err := c.redis.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: StreamKey, Group: c.group, Start: id, End: id, Count: 1,
	}).Result()
	if err != nil {
		c.log.Warn("an event's attempts could not be counted", "group", c.group, "id", id, "error", err)
		return 0, false
	}
	if len(p) == 0 {
		return 0, false
	}
	return p[0].RetryCount, true
}

// setAside records the event where the organization's administrators read,
// then acknowledges it. Without the record it stays pending, to be tried again.
func (c *Consumer) setAside(ctx context.Context, id string, e Event, attempts int64, cause error) {
	c.log.Error("an event was set aside unhandled", "group", c.group, "id", id, "event_id", e.ID,
		"topic", e.Topic, "attempts", attempts, "error", cause)
	if err := c.deadLetter(ctx, e, attempts); err != nil {
		c.log.Warn("an event set aside could not be recorded", "group", c.group, "id", id, "error", err)
		return
	}
	c.ack(ctx, id)
}

// deadLetter writes the audit entry, once per event and group however often a
// crash before the acknowledgement repeats it.
func (c *Consumer) deadLetter(ctx context.Context, e Event, attempts int64) error {
	data, err := json.Marshal(map[string]any{"group": c.group, "topic": e.Topic, "attempts": attempts})
	if err != nil {
		return err
	}
	var target *uuid.UUID
	if e.ID != uuid.Nil {
		target = &e.ID
	}
	_, err = c.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO audit_log (action, target_type, target_id, data)
			SELECT $1::text, 'event', $2::uuid, $3::jsonb
			WHERE NOT EXISTS (
			      SELECT 1 FROM audit_log
			      WHERE action = $1::text AND target_type = 'event'
			        AND target_id = $2::uuid AND data->>'group' = $4::text)`,
			ActionDeadLettered, target, data, c.group)
		return err
	})
	return err
}

func (c *Consumer) ack(ctx context.Context, id string) {
	if err := c.redis.XAck(ctx, StreamKey, c.group, id).Err(); err != nil {
		c.log.Warn("an event could not be acknowledged", "group", c.group, "id", id, "error", err)
	}
}
