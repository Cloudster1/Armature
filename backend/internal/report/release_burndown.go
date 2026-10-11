package report

import (
	"context"
	"fmt"
	"time"

	"github.com/armature/armature/backend/internal/db"
)

// releaseBurndown counts an issue from the day it was filed, so work added to a
// version later lifts the line instead of hiding in its first day.
func releaseBurndown(ctx context.Context, tx db.DBTX, sc scope, p Params) (*ReleaseBurndownReport, error) {
	if p.VersionID == nil {
		return nil, fmt.Errorf("%w: choose a version.", ErrBadChart)
	}
	var (
		out        = &ReleaseBurndownReport{VersionID: *p.VersionID, Days: []BurndownDay{}}
		startOn    *time.Time
		releasedAt *time.Time
		createdAt  time.Time
	)
	err := tx.QueryRow(ctx, `SELECT name, start_on, release_on, released_at, created_at FROM version WHERE id = $1 AND project_id = $2`, *p.VersionID, sc.projectID).
		Scan(&out.VersionName, &startOn, &out.ReleaseOn, &releasedAt, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("%w: that version is not one of this project's.", ErrBadChart)
	}
	from, to := burndownSpan(createdAt, startOn, releasedAt, time.Now())
	where, args := sc.clause("i", *p.VersionID, from, to)
	rows, err := tx.Query(ctx, `
		WITH days AS (SELECT generate_series($4::date, $5::date, '1 day') AS day),
		     fixing AS (
		         SELECT i.id, i.estimate, i.created_at, i.resolved_at FROM issue_version iv
		         JOIN issue i ON i.id = iv.issue_id
		         JOIN issue_type it ON it.id = i.issue_type_id
		         WHERE iv.version_id = $3 AND iv.role = 'fix' AND `+where+` AND `+notSubtask+`),
		     open_on AS (
		         SELECT d.day, f.estimate FROM days d JOIN fixing f
		           ON f.created_at < d.day + interval '1 day'
		          AND (f.resolved_at IS NULL OR f.resolved_at >= d.day + interval '1 day'))
		SELECT d.day,
		       (SELECT count(*) FROM open_on o WHERE o.day = d.day),
		       (SELECT COALESCE(sum(o.estimate), 0)::float8 FROM open_on o WHERE o.day = d.day),
		       (SELECT count(*) FROM fixing), (SELECT COALESCE(sum(estimate), 0)::float8 FROM fixing)
		FROM days d ORDER BY d.day`, args...)
	if err != nil {
		return nil, fmt.Errorf("release burndown: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var d BurndownDay
		if err := rows.Scan(&d.Day, &d.Remaining, &d.RemainingPoints, &out.Total, &out.TotalPoints); err != nil {
			return nil, err
		}
		out.Days = append(out.Days, d)
	}
	return out, rows.Err()
}

// burndownSpan is the first and last day a version's burndown draws. A released
// version has nothing left to burn after its release day, so the series stops there.
func burndownSpan(createdAt time.Time, startOn, releasedAt *time.Time, now time.Time) (from, to time.Time) {
	from = midnightUTC(createdAt)
	if startOn != nil {
		from = midnightUTC(*startOn)
	}
	to = midnightUTC(now)
	if releasedAt != nil && midnightUTC(*releasedAt).Before(to) {
		to = midnightUTC(*releasedAt)
	}
	if from.After(to) {
		from = to
	}
	return from, to
}
