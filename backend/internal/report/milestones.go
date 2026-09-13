package report

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/milestone"
)

// MilestoneRow is one milestone as a widget shows it: the card's counts, and
// the points behind them. Whether it is overdue is the reader's date to judge.
type MilestoneRow struct {
	ID         uuid.UUID          `json:"id"`
	Name       string             `json:"name"`
	DueOn      *time.Time         `json:"dueOn,omitempty"`
	ClosedAt   *time.Time         `json:"closedAt,omitempty"`
	Progress   milestone.Progress `json:"progress"`
	Points     float64            `json:"points"`
	DonePoints float64            `json:"donePoints"`
}

// MilestonesReport is the open milestones, or the one that was asked for.
type MilestonesReport struct {
	Milestones []MilestoneRow `json:"milestones"`
}

// milestones counts each open milestone's issues by status category, or one
// milestone's when asked by id, closed or not.
func milestones(ctx context.Context, tx db.DBTX, sc scope, p Params) (*MilestonesReport, error) {
	where, args := sc.clause("i", p.MilestoneID)
	count := func(category string) string {
		return `(SELECT count(*) FROM issue i JOIN issue_status s ON s.id = i.status_id
		          WHERE i.milestone_id = m.id AND s.category = ` + category + ` AND ` + where + `)`
	}
	// Subtasks count here, unlike every other report: the widget must say the
	// number the milestone card says.
	rows, err := tx.Query(ctx, `
		SELECT m.id, m.name, m.due_on, m.closed_at,
		       `+count(catDone)+`, `+count(catInProgress)+`, `+count(catTodo)+`,
		       (SELECT COALESCE(sum(i.estimate), 0)::float8 FROM issue i WHERE i.milestone_id = m.id AND `+where+`),
		       (SELECT COALESCE(sum(i.estimate), 0)::float8 FROM issue i JOIN issue_status s ON s.id = i.status_id
		         WHERE i.milestone_id = m.id AND s.category = `+catDone+` AND `+where+`)
		FROM milestone m
		WHERE m.project_id = $1 AND (($3::uuid IS NULL AND m.closed_at IS NULL) OR m.id = $3)
		ORDER BY (m.closed_at IS NOT NULL), m.due_on NULLS LAST, m.position, m.created_at`, args...)
	if err != nil {
		return nil, fmt.Errorf("milestones: %w", err)
	}
	defer rows.Close()
	out := &MilestonesReport{Milestones: []MilestoneRow{}}
	for rows.Next() {
		var row MilestoneRow
		var done, inProgress, todo int
		if err := rows.Scan(&row.ID, &row.Name, &row.DueOn, &row.ClosedAt, &done, &inProgress, &todo, &row.Points, &row.DonePoints); err != nil {
			return nil, err
		}
		row.Progress = milestone.Measure(done, inProgress, todo)
		out.Milestones = append(out.Milestones, row)
	}
	return out, rows.Err()
}
