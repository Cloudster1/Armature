package sprint

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
)

// Totals is what a sprint holds at one moment, counted the way the plan counts:
// each issue once, points only where somebody typed or the children add up.
type Totals struct {
	Committed   float64
	Completed   float64
	Issues      int
	IssuesDone  int
	Unestimated int
}

// Snapshot is one day's last word on a sprint: what was in it and how much of
// that was done. Remaining is the burndown's own number.
type Snapshot struct {
	SprintID    uuid.UUID `json:"sprintId"`
	Day         time.Time `json:"day"`
	Scope       float64   `json:"scope"`
	Done        float64   `json:"done"`
	Remaining   float64   `json:"remaining"`
	Issues      int       `json:"issues"`
	IssuesDone  int       `json:"issuesDone"`
	Unestimated int       `json:"unestimated"`
	TakenAt     time.Time `json:"takenAt"`
}

// Snapshot writes the sprint's standing for a day, replacing what that day
// already said. It is the worker's call on a timer and the lifecycle's at
// start and completion; nothing is asked of the person running the sprint.
func (s *Service) Snapshot(ctx context.Context, id uuid.UUID, day time.Time) (*Snapshot, error) {
	if s.counter == nil {
		return nil, errors.New("snapshots need a counter to read the sprint's totals")
	}
	found, err := s.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	totals, err := s.counter.SprintTotals(ctx, found.ProjectKey, id)
	if err != nil {
		return nil, err
	}
	var out *Snapshot
	_, err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		out, err = writeSnapshot(ctx, tx, found, day, totals)
		return err
	})
	return out, err
}

// Snapshots reads a sprint's days, oldest first, which is the order a chart
// reads them in.
func (s *Service) Snapshots(ctx context.Context, id uuid.UUID) ([]Snapshot, error) {
	out := []Snapshot{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT sprint_id, day, scope, done, remaining, issues, issues_done, unestimated, taken_at
			FROM sprint_snapshot WHERE sprint_id = $1 ORDER BY day`, id)
		if err != nil {
			return fmt.Errorf("read snapshots: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var snap Snapshot
			if err := rows.Scan(&snap.SprintID, &snap.Day, &snap.Scope, &snap.Done, &snap.Remaining,
				&snap.Issues, &snap.IssuesDone, &snap.Unestimated, &snap.TakenAt); err != nil {
				return err
			}
			out = append(out, snap)
		}
		return rows.Err()
	})
	return out, err
}

// writeSnapshot upserts the day's row inside the caller's transaction.
func writeSnapshot(ctx context.Context, tx db.DBTX, sprint *Sprint, day time.Time, t Totals) (*Snapshot, error) {
	var out Snapshot
	err := tx.QueryRow(ctx, `
		INSERT INTO sprint_snapshot (org_id, project_id, sprint_id, day, scope, done, issues, issues_done, unestimated)
		VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (sprint_id, day) DO UPDATE SET
		    scope = EXCLUDED.scope, done = EXCLUDED.done, issues = EXCLUDED.issues,
		    issues_done = EXCLUDED.issues_done, unestimated = EXCLUDED.unestimated, taken_at = now()
		RETURNING sprint_id, day, scope, done, remaining, issues, issues_done, unestimated, taken_at`,
		sprint.ProjectID, sprint.ID, midnight(day), t.Committed, t.Completed, t.Issues, t.IssuesDone, t.Unestimated,
	).Scan(&out.SprintID, &out.Day, &out.Scope, &out.Done, &out.Remaining, &out.Issues, &out.IssuesDone, &out.Unestimated, &out.TakenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("write snapshot: %w", err)
	}
	return &out, nil
}

// midnight is the day a moment falls on, in UTC, which is the day the row
// is keyed by.
func midnight(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
