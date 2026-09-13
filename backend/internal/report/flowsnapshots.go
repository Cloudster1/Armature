package report

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/observability"
	"github.com/armature/armature/backend/internal/tenant"
)

// flowSnapshotInterval is well inside a day, so a day is never missed for a restart.
const flowSnapshotInterval = time.Hour

// FlowSnapshots writes today's status counts for every live project, on a timer.
type FlowSnapshots struct {
	db      *db.Cluster
	reports *Service
	log     *slog.Logger
	now     func() time.Time
}

func NewFlowSnapshots(cluster *db.Cluster, reports *Service, log *slog.Logger) *FlowSnapshots {
	return &FlowSnapshots{db: cluster, reports: reports, log: log, now: time.Now}
}

// Run snapshots on start and then every interval until the context ends.
func (w *FlowSnapshots) Run(ctx context.Context) error {
	ticker := time.NewTicker(flowSnapshotInterval)
	defer ticker.Stop()
	for {
		if n, err := observability.Count(ctx, "flow-snapshots", w.Once); err != nil {
			w.log.Warn("flow snapshots failed", "error", err)
		} else if n > 0 {
			w.log.Info("flow snapshots written", "count", n)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Once snapshots every unarchived project across organizations for today.
func (w *FlowSnapshots) Once(ctx context.Context) (int, error) {
	type live struct{ orgID, projectID uuid.UUID }
	var found []live
	err := w.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT org_id, id FROM project WHERE archived_at IS NULL ORDER BY org_id, id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var l live
			if err := rows.Scan(&l.orgID, &l.projectID); err != nil {
				return err
			}
			found = append(found, l)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	today := midnightUTC(w.now())
	written := 0
	for _, l := range found {
		orgCtx := db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: l.orgID}))
		if err := w.reports.WriteFlowSnapshot(orgCtx, l.projectID, today); err != nil {
			w.log.Warn("could not snapshot a project's flow", "project", l.projectID, "error", err)
			continue
		}
		written++
	}
	return written, nil
}
