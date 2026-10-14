package desk

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/observability"
)

// Watch is the worker's half of the clocks: it notices when a running one has
// run out, which nothing in the request path would, since nobody is making a
// request at the moment a goal is missed.
type Watch struct {
	db       *db.Cluster
	desk     *Service
	log      *slog.Logger
	interval time.Duration
}

func NewWatch(cluster *db.Cluster, desk *Service, log *slog.Logger) *Watch {
	return &Watch{db: cluster, desk: desk, log: log, interval: 30 * time.Second}
}

// Run checks every interval until the context ends.
func (w *Watch) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		if n, err := observability.Count(ctx, "sla-watch", w.Once); err != nil {
			w.log.Warn("sla watch failed", "error", err)
		} else if n > 0 {
			w.log.Info("sla breaches recorded", "count", n)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Once records every breach that has happened since the last look, and returns
// how many.
func (w *Watch) Once(ctx context.Context) (int, error) {
	var found []uuid.UUID
	err := w.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT id FROM sla_timer
			WHERE running_since IS NOT NULL AND completed_at IS NULL AND breached_at IS NULL
			  AND elapsed_seconds + EXTRACT(EPOCH FROM now() - running_since) > goal_minutes * 60`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			found = append(found, id)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}

	recorded := 0
	for _, timerID := range found {
		var happened bool
		_, err := w.db.Write(db.PinPrimary(ctx), func(ctx context.Context, tx db.DBTX) (err error) {
			happened, err = w.desk.breach(ctx, tx, timerID)
			return err
		})
		if err != nil {
			w.log.Warn("could not record a breach", "timer", timerID, "error", err)
			continue
		}
		// A clock that only counts open hours may be found by wall time and
		// still have time left; that is not a breach recorded.
		if happened {
			recorded++
		}
	}
	return recorded, nil
}
