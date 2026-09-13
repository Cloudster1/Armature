package sprint

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/observability"
	"github.com/armature/armature/backend/internal/tenant"
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

// Once snapshots every running sprint across every organization for today, and
// returns how many. Finding them crosses tenants; each write is done inside
// its own organization, which is what lets the row carry its org.
func (w *Snapshots) Once(ctx context.Context) (int, error) {
	type running struct {
		orgID, sprintID uuid.UUID
	}
	var found []running
	err := w.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT org_id, id FROM sprint WHERE state = 'active' ORDER BY org_id, id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r running
			if err := rows.Scan(&r.orgID, &r.sprintID); err != nil {
				return err
			}
			found = append(found, r)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}

	today := midnight(w.now())
	written := 0
	for _, r := range found {
		orgCtx := db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: r.orgID}))
		if _, err := w.sprints.Snapshot(orgCtx, r.sprintID, today); err != nil {
			w.log.Warn("could not snapshot a sprint", "sprint", r.sprintID, "error", err)
			continue
		}
		written++
	}
	return written, nil
}
