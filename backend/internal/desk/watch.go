package desk

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/observability"
	"github.com/armature/armature/backend/internal/tenant"
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

// Once records every breach that has happened since the last look, across
// every organization, and returns how many.
func (w *Watch) Once(ctx context.Context) (int, error) {
	type due struct {
		orgID, timerID uuid.UUID
	}
	var found []due
	// Finding them crosses tenants, which is what the admin role is for; the
	// recording of each is done inside its own organization.
	err := w.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT org_id, id FROM sla_timer
			WHERE running_since IS NOT NULL AND completed_at IS NULL AND breached_at IS NULL
			  AND elapsed_seconds + EXTRACT(EPOCH FROM now() - running_since) > goal_minutes * 60`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d due
			if err := rows.Scan(&d.orgID, &d.timerID); err != nil {
				return err
			}
			found = append(found, d)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}

	recorded := 0
	for _, d := range found {
		orgCtx := db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: d.orgID}))
		_, err := w.db.Write(orgCtx, func(ctx context.Context, tx db.DBTX) error {
			return w.desk.breach(ctx, tx, d.timerID)
		})
		if err != nil {
			w.log.Warn("could not record a breach", "timer", d.timerID, "error", err)
			continue
		}
		recorded++
	}
	return recorded, nil
}
