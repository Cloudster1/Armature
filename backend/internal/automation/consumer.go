package automation

import (
	"context"
	"log/slog"
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
}

func NewConsumer(svc *Service, rdb *redis.Client, log *slog.Logger) *Consumer {
	return &Consumer{svc: svc, redis: rdb, log: log, group: "automation"}
}

// Run consumes the stream until the context ends.
func (c *Consumer) Run(ctx context.Context) error {
	return events.NewConsumer(c.svc.db, c.redis, c.log, c.group, c.svc.Handle).Run(ctx)
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
