package webhook

import (
	"context"
	"log/slog"
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
}

func NewConsumer(svc *Service, rdb *redis.Client, log *slog.Logger) *Consumer {
	return &Consumer{svc: svc, redis: rdb, log: log, group: "webhooks"}
}

// Run consumes the stream until the context ends.
func (c *Consumer) Run(ctx context.Context) error {
	return events.NewConsumer(c.svc.db, c.redis, c.log, c.group, c.svc.Handle).Run(ctx)
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
