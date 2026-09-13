package webhook

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/observability"
)

// sendInterval is how often due deliveries are looked at; the first attempt
// is due at once, so this is also the most a delivery waits.
const sendInterval = 5 * time.Second

// Consumer queues the stream's events for subscribed endpoints and sends
// what is due.
type Consumer struct {
	svc   *Service
	redis *redis.Client
	log   *slog.Logger
	group string
	block time.Duration
}

func NewConsumer(svc *Service, rdb *redis.Client, log *slog.Logger) *Consumer {
	return &Consumer{svc: svc, redis: rdb, log: log, group: "webhooks", block: 5 * time.Second}
}

// Run consumes the stream until the context ends.
func (c *Consumer) Run(ctx context.Context) error {
	err := c.redis.XGroupCreateMkStream(ctx, events.StreamKey, c.group, "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create consumer group: %w", err)
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		streams, err := c.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: c.group, Consumer: "worker", Streams: []string{events.StreamKey, ">"}, Count: 50, Block: c.block,
		}).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.log.Warn("webhook read failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				e := events.FromMessage(msg)
				hctx, done := events.Continue(ctx, e, c.group)
				err := c.svc.Handle(hctx, e)
				done(err)
				if err != nil {
					c.log.Warn("webhook could not queue an event", "id", msg.ID, "error", err)
				}
				if err := c.redis.XAck(ctx, events.StreamKey, c.group, msg.ID).Err(); err != nil {
					c.log.Warn("webhook could not acknowledge", "id", msg.ID, "error", err)
				}
			}
		}
	}
}

// RunSender posts due deliveries until the context ends.
func (c *Consumer) RunSender(ctx context.Context) error {
	ticker := time.NewTicker(sendInterval)
	defer ticker.Stop()
	for {
		if _, err := observability.Count(ctx, "webhook-sender", c.svc.SendDue); err != nil {
			c.log.Warn("webhook sending failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
