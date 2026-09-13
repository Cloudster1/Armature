package report

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/milestone"
)

// VersionRow is one version as a widget shows it: the issues that fix it by
// state, and the points behind them.
type VersionRow struct {
	ID         uuid.UUID          `json:"id"`
	Name       string             `json:"name"`
	ReleaseOn  *time.Time         `json:"releaseOn,omitempty"`
	ReleasedAt *time.Time         `json:"releasedAt,omitempty"`
	Progress   milestone.Progress `json:"progress"`
	Points     float64            `json:"points"`
	DonePoints float64            `json:"donePoints"`
}

// VersionsReport is the unreleased versions, or the one that was asked for.
type VersionsReport struct {
	Versions []VersionRow `json:"versions"`
}

// versions counts what fixes each unreleased version by status category, or
// one version's when asked by id, released or not.
func versions(ctx context.Context, tx db.DBTX, sc scope, p Params) (*VersionsReport, error) {
	where, args := sc.clause("i", p.VersionID)
	count := func(category string) string {
		return `(SELECT count(*) FROM issue_version iv JOIN issue i ON i.id = iv.issue_id JOIN issue_status s ON s.id = i.status_id
		          WHERE iv.version_id = v.id AND iv.role = 'fix' AND s.category = ` + category + ` AND ` + where + `)`
	}
	rows, err := tx.Query(ctx, `
		SELECT v.id, v.name, v.release_on, v.released_at,
		       `+count(catDone)+`, `+count(catInProgress)+`, `+count(catTodo)+`,
		       (SELECT COALESCE(sum(i.estimate), 0)::float8 FROM issue_version iv JOIN issue i ON i.id = iv.issue_id WHERE iv.version_id = v.id AND iv.role = 'fix' AND `+where+`),
		       (SELECT COALESCE(sum(i.estimate), 0)::float8 FROM issue_version iv JOIN issue i ON i.id = iv.issue_id JOIN issue_status s ON s.id = i.status_id
		         WHERE iv.version_id = v.id AND iv.role = 'fix' AND s.category = `+catDone+` AND `+where+`)
		FROM version v
		WHERE v.project_id = $1 AND v.archived_at IS NULL AND (($3::uuid IS NULL AND v.released_at IS NULL) OR v.id = $3)
		ORDER BY v.release_on NULLS LAST, v.position, v.created_at`, args...)
	if err != nil {
		return nil, fmt.Errorf("versions: %w", err)
	}
	defer rows.Close()
	out := &VersionsReport{Versions: []VersionRow{}}
	for rows.Next() {
		var row VersionRow
		var done, inProgress, todo int
		if err := rows.Scan(&row.ID, &row.Name, &row.ReleaseOn, &row.ReleasedAt, &done, &inProgress, &todo, &row.Points, &row.DonePoints); err != nil {
			return nil, err
		}
		row.Progress = milestone.Measure(done, inProgress, todo)
		out.Versions = append(out.Versions, row)
	}
	return out, rows.Err()
}
