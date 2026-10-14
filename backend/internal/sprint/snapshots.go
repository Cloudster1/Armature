package sprint

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/observability"
)

// Snapshots is the worker's half of the burndown: it writes every running
// sprint's standing for today, on a timer, so that the day has a last word
// even when nobody touched the sprint.
type Snapshots struct {
	db       *db.Cluster
	sprints  *Service
	log      *slog.Logger
	interval time.Duration
	now      func() time.Time
}

func NewSnapshots(cluster *db.Cluster, sprints *Service, log *slog.Logger) *Snapshots {
	return &Snapshots{db: cluster, sprints: sprints, log: log, interval: time.Hour, now: time.Now}
}

// Run snapshots on start and then every interval until the context ends. The
// interval is well inside a day, so a day is never missed for a restart.
func (w *Snapshots) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		if n, err := observability.Count(ctx, "sprint-snapshots", w.Once); err != nil {
			w.log.Warn("sprint snapshots failed", "error", err)
		} else if n > 0 {
			w.log.Info("sprint snapshots written", "count", n)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Once snapshots every running sprint for today, and returns how many.
func (w *Snapshots) Once(ctx context.Context) (int, error) {
	var found []uuid.UUID
	err := w.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT id FROM sprint WHERE state = 'active' ORDER BY id`)
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

	today := midnight(w.now())
	written := 0
	for _, sprintID := range found {
		_, err := w.sprints.Snapshot(db.PinPrimary(ctx), sprintID, today)
		if errors.Is(err, ErrNotRunning) {
			// Completed since it was listed; completion wrote its last point.
			continue
		}
		if err != nil {
			w.log.Warn("could not snapshot a sprint", "sprint", sprintID, "error", err)
			continue
		}
		written++
	}
	return written, nil
}
