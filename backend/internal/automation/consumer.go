package automation

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

// tickInterval is how often schedules are looked at; a schedule is never
// finer than an hour, so a minute is plenty.
const tickInterval = time.Minute

// Consumer feeds the stream to the rules and ticks the clock.
type Consumer struct {
	svc   *Service
	redis *redis.Client
	log   *slog.Logger
	group string
	block time.Duration
}

func NewConsumer(svc *Service, rdb *redis.Client, log *slog.Logger) *Consumer {
	return &Consumer{svc: svc, redis: rdb, log: log, group: "automation", block: 5 * time.Second}
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
			c.log.Warn("automation read failed", "error", err)
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
					c.log.Warn("automation could not act on an event", "id", msg.ID, "error", err)
				}
				if err := c.redis.XAck(ctx, events.StreamKey, c.group, msg.ID).Err(); err != nil {
					c.log.Warn("automation could not acknowledge", "id", msg.ID, "error", err)
				}
			}
		}
	}
}

// RunSchedules ticks the clock until the context ends.
func (c *Consumer) RunSchedules(ctx context.Context) error {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		if n, err := observability.Count(ctx, "automation-schedules", c.svc.Tick); err != nil {
			c.log.Warn("schedules failed", "error", err)
		} else if n > 0 {
			c.log.Info("scheduled rules ran", "count", n)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}
