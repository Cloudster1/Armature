package attachment

import (
	"context"
	"log/slog"
	"time"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/observability"
)

// Reaper removes the bytes behind tombstones: objects whose rows went with a
// deleted issue, or whose removal failed right after a delete. It runs in the
// worker, across every organization, because the rows it cleans up after are
// already gone and no request will ever ask for them.
type Reaper struct {
	service  *Service
	log      *slog.Logger
	interval time.Duration
}

// reapBatch is how many objects one pass removes before the next look.
const reapBatch = 200

func NewReaper(service *Service, log *slog.Logger) *Reaper {
	return &Reaper{service: service, log: log, interval: time.Minute}
}

// Run reaps every interval until the context ends.
func (r *Reaper) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		if n, err := observability.Count(ctx, "attachment-reaper", r.Once); err != nil {
			r.log.Warn("attachment reaper failed", "error", err)
		} else if n > 0 {
			r.log.Info("attachment objects removed", "count", n)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Once removes the objects behind up to one batch of tombstones and returns
// how many went. A failure on one object leaves its tombstone for next time.
func (r *Reaper) Once(ctx context.Context) (int, error) {
	var keys []string
	err := r.service.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT object_key FROM attachment_tombstone ORDER BY created_at LIMIT $1`, reapBatch)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				return err
			}
			keys = append(keys, key)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, key := range keys {
		if err := r.service.reap(ctx, key, r.service.db.WriteAdmin); err != nil {
			r.log.Warn("attachment object could not be removed yet", "key", key, "error", err)
			continue
		}
		removed++
	}
	return removed, nil
}
