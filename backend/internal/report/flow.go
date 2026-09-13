package report

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/workflow"
)

// Flow reports: how work moves over time, read off daily snapshots of how
// many issues stood in each status, and off when issues were created and
// resolved.

const (
	// controlWindow is how many resolved issues the control chart's rolling
	// mean looks back over.
	controlWindow = 7
	// FlowDefaultDays is the window the flow reports look back when nobody said.
	FlowDefaultDays = 60
)

// resolutionBuckets are the bands the resolution histogram sorts into, in
// days; the last is open-ended.
var resolutionBuckets = []struct {
	label string
	upTo  float64
}{{"under a day", 1}, {"1 to 3 days", 3}, {"3 to 7 days", 7}, {"1 to 2 weeks", 14}, {"2 to 4 weeks", 28}, {"over 4 weeks", 0}}

// FlowStatus is one band of the cumulative flow.
type FlowStatus struct {
	ID       uuid.UUID               `json:"id"`
	Name     string                  `json:"name"`
	Category workflow.StatusCategory `json:"category"`
}

// FlowDay is one day's counts, aligned with the report's statuses.
type FlowDay struct {
	Day    time.Time `json:"day"`
	Counts []int     `json:"counts"`
}

// CumulativeFlowReport is a band per status over the window.
type CumulativeFlowReport struct {
	Statuses []FlowStatus `json:"statuses"`
	Days     []FlowDay    `json:"days"`
	// Reconstructed says some days were rebuilt from the changelog rather
	// than written as they passed.
	Reconstructed bool `json:"reconstructed"`
	Window        int  `json:"window"`
}

// ControlPoint is one resolved issue: when, and how long it took from being
// started to being resolved.
type ControlPoint struct {
	Key        string    `json:"key"`
	Summary    string    `json:"summary"`
	ResolvedAt time.Time `json:"resolvedAt"`
	Days       float64   `json:"days"`
	// Rolling is the mean of this and the points before it, up to the window.
	Rolling float64 `json:"rolling"`
}

// ControlChartReport is cycle time per resolved issue with a rolling mean.
type ControlChartReport struct {
	Points     []ControlPoint `json:"points"`
	MeanDays   float64        `json:"meanDays"`
	MedianDays float64        `json:"medianDays"`
	Window     int            `json:"window"`
}

// FlowCount is one day of arrivals and departures, with the running totals.
type FlowCount struct {
	Day           time.Time `json:"day"`
	Created       int       `json:"created"`
	Resolved      int       `json:"resolved"`
	CreatedTotal  int       `json:"createdTotal"`
	ResolvedTotal int       `json:"resolvedTotal"`
}

// CreatedResolvedReport is arrivals against departures, day by day, cumulative.
type CreatedResolvedReport struct {
	Days   []FlowCount `json:"days"`
	Window int         `json:"window"`
}

// AgeWeek is the average age of what was open at the start of a week.
type AgeWeek struct {
	Start   time.Time `json:"start"`
	Open    int       `json:"open"`
	Average float64   `json:"averageDays"`
}

// AverageAgeReport is how old the open work is, now and week by week.
type AverageAgeReport struct {
	Open        int       `json:"open"`
	AverageDays float64   `json:"averageDays"`
	P85Days     float64   `json:"p85Days"`
	OldestKey   string    `json:"oldestKey,omitempty"`
	OldestDays  float64   `json:"oldestDays"`
	Weeks       []AgeWeek `json:"weeks"`
	Window      int       `json:"window"`
}

// Band is one band of the resolution histogram.
type Band struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

// ResolutionReport is how long resolved issues took, in bands.
type ResolutionReport struct {
	Bands    []Band `json:"bands"`
	Resolved int    `json:"resolved"`
	Window   int    `json:"window"`
}

// BurndownDay is what was left of a version at the end of a day.
type BurndownDay struct {
	Day             time.Time `json:"day"`
	Remaining       int       `json:"remaining"`
	RemainingPoints float64   `json:"remainingPoints"`
}

// ReleaseBurndownReport is a version's remaining work, day by day.
type ReleaseBurndownReport struct {
	VersionID   uuid.UUID     `json:"versionId"`
	VersionName string        `json:"versionName"`
	ReleaseOn   *time.Time    `json:"releaseOn,omitempty"`
	Total       int           `json:"total"`
	TotalPoints float64       `json:"totalPoints"`
	Days        []BurndownDay `json:"days"`
}

// flowWindow is the window a flow report looks back: the params' days, or the default.
func flowWindow(sc scope) int {
	if sc.days > 0 {
		return sc.days
	}
	return FlowDefaultDays
}

// reconstructMissingDays fills the snapshot table for every day of the range
// that has no rows, from the changelog: an issue's status on a day is the last
// status change written before the day ended, else the from of its first
// change, else its status now. Rebuilt rows are marked; the worker's own
// writes are not. It runs inside the report's read, in its own write.
func (s *Service) reconstructMissingDays(ctx context.Context, projectID uuid.UUID, from, to time.Time) (bool, error) {
	var missing int
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT count(*) FROM generate_series($2::date, $3::date, '1 day') AS d(day)
			WHERE NOT EXISTS (SELECT 1 FROM project_flow_snapshot s WHERE s.project_id = $1 AND s.day = d.day)`,
			projectID, from, to).Scan(&missing)
	})
	if err != nil || missing == 0 {
		return false, err
	}
	_, err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			WITH missing AS (
			    SELECT d.day::date AS day FROM generate_series($2::date, $3::date, '1 day') AS d(day)
			    WHERE NOT EXISTS (SELECT 1 FROM project_flow_snapshot s WHERE s.project_id = $1 AND s.day = d.day)
			),
			status_on AS (
			    SELECT m.day, i.id,
			           COALESCE(
			             (SELECT st.id FROM issue_history h
			                CROSS JOIN LATERAL jsonb_array_elements(h.changes) c
			                JOIN issue_status st ON lower(st.name) = lower(c->>'to')
			              WHERE h.issue_id = i.id AND c->>'field' = 'status' AND h.created_at < m.day + 1
			              ORDER BY h.created_at DESC LIMIT 1),
			             (SELECT st.id FROM issue_history h
			                CROSS JOIN LATERAL jsonb_array_elements(h.changes) c
			                JOIN issue_status st ON lower(st.name) = lower(c->>'from')
			              WHERE h.issue_id = i.id AND c->>'field' = 'status'
			              ORDER BY h.created_at LIMIT 1),
			             i.status_id) AS status_id
			    FROM missing m
			    JOIN issue i ON i.project_id = $1 AND i.created_at < m.day + 1
			    JOIN issue_type it ON it.id = i.issue_type_id AND `+notSubtask+`
			)
			INSERT INTO project_flow_snapshot (org_id, project_id, day, status_id, count, reconstructed)
			SELECT current_org_id(), $1, day, status_id, count(*), true FROM status_on GROUP BY day, status_id
			ON CONFLICT DO NOTHING`, projectID, from, to)
		return err
	})
	return true, err
}

// WriteFlowSnapshot records how many issues stand in each status today, for
// one project, replacing what the day already had.
func (s *Service) WriteFlowSnapshot(ctx context.Context, projectID uuid.UUID, day time.Time) error {
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `DELETE FROM project_flow_snapshot WHERE project_id = $1 AND day = $2`, projectID, day); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO project_flow_snapshot (org_id, project_id, day, status_id, count, reconstructed)
			SELECT current_org_id(), $1, $2, i.status_id, count(*), false
			FROM issue i JOIN issue_type it ON it.id = i.issue_type_id AND `+notSubtask+`
			WHERE i.project_id = $1 GROUP BY i.status_id`, projectID, day)
		return err
	})
	return err
}

// flow answers the cumulative flow: the days nobody wrote are rebuilt first,
// in their own write, then the bands are read.
func (s *Service) flow(ctx context.Context, key string, p Params) (*CumulativeFlowReport, error) {
	var projectID uuid.UUID
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, key).Scan(&projectID)
	})
	if err != nil {
		return nil, project.ErrNotFound
	}
	window := p.window()
	if window <= 0 {
		window = FlowDefaultDays
	}
	today := midnightUTC(time.Now())
	if _, err := s.reconstructMissingDays(ctx, projectID, today.AddDate(0, 0, -window), today); err != nil {
		return nil, fmt.Errorf("rebuild flow days: %w", err)
	}
	var out *CumulativeFlowReport
	err = s.db.Read(db.PinPrimary(ctx), func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = s.cumulativeFlow(ctx, tx, scope{projectID: projectID, days: window})
		return err
	})
	return out, err
}

// cumulativeFlow reads the bands. Snapshots count the whole project, so the
// dashboard's team and query narrowing do not reach here.
func (s *Service) cumulativeFlow(ctx context.Context, tx db.DBTX, sc scope) (*CumulativeFlowReport, error) {
	window := flowWindow(sc)
	today := midnightUTC(time.Now())
	from := today.AddDate(0, 0, -window)
	out := &CumulativeFlowReport{Statuses: []FlowStatus{}, Days: []FlowDay{}, Window: window}

	rows, err := tx.Query(ctx, `
		SELECT s.day, s.status_id, st.name, st.category, st.position, s.count, s.reconstructed
		FROM project_flow_snapshot s JOIN issue_status st ON st.id = s.status_id
		WHERE s.project_id = $1 AND s.day BETWEEN $2 AND $3
		ORDER BY s.day, st.category, st.position`, sc.projectID, from, today)
	if err != nil {
		return nil, fmt.Errorf("cumulative flow: %w", err)
	}
	defer rows.Close()
	index := map[uuid.UUID]int{}
	byDay := map[time.Time]map[uuid.UUID]int{}
	for rows.Next() {
		var (
			day           time.Time
			id            uuid.UUID
			name          string
			category      string
			position      int
			count         int
			reconstructed bool
		)
		if err := rows.Scan(&day, &id, &name, &category, &position, &count, &reconstructed); err != nil {
			return nil, err
		}
		if _, ok := index[id]; !ok {
			index[id] = len(out.Statuses)
			out.Statuses = append(out.Statuses, FlowStatus{ID: id, Name: name, Category: workflow.StatusCategory(category)})
		}
		day = midnightUTC(day)
		if _, ok := byDay[day]; !ok {
			byDay[day] = map[uuid.UUID]int{}
		}
		byDay[day][id] = count
		out.Reconstructed = out.Reconstructed || reconstructed
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Every day of the window is a row, zeros where nothing stood: a chart
	// with a gap would read as a day the project vanished.
	for day := from; !day.After(today); day = day.AddDate(0, 0, 1) {
		counts := make([]int, len(out.Statuses))
		for id, n := range byDay[day] {
			counts[index[id]] = n
		}
		out.Days = append(out.Days, FlowDay{Day: day, Counts: counts})
	}
	return out, nil
}

// controlChart is cycle time per issue resolved in the window: from the first
// step into an in-progress status, or from creation, to resolution.
func controlChart(ctx context.Context, tx db.DBTX, sc scope) (*ControlChartReport, error) {
	window := flowWindow(sc)
	where, args := sc.clause("i", window)
	rows, err := tx.Query(ctx, `
		SELECT p.key || '-' || i.key_num, i.summary, i.resolved_at,
		       EXTRACT(EPOCH FROM i.resolved_at - COALESCE(
		         (SELECT min(h.created_at) FROM issue_history h
		            CROSS JOIN LATERAL jsonb_array_elements(h.changes) c
		            JOIN issue_status st ON lower(st.name) = lower(c->>'to')
		          WHERE h.issue_id = i.id AND c->>'field' = 'status' AND st.category = `+catInProgress+`),
		         i.created_at)) / 86400
		FROM issue i
		JOIN project p ON p.id = i.project_id
		JOIN issue_type it ON it.id = i.issue_type_id
		WHERE `+where+` AND `+notSubtask+` AND i.resolved_at >= now() - make_interval(days => $3)
		ORDER BY i.resolved_at`, args...)
	if err != nil {
		return nil, fmt.Errorf("control chart: %w", err)
	}
	defer rows.Close()
	out := &ControlChartReport{Points: []ControlPoint{}, Window: window}
	var all []float64
	for rows.Next() {
		var pt ControlPoint
		if err := rows.Scan(&pt.Key, &pt.Summary, &pt.ResolvedAt, &pt.Days); err != nil {
			return nil, err
		}
		if pt.Days < 0 {
			pt.Days = 0
		}
		all = append(all, pt.Days)
		pt.Rolling = mean(all[max(0, len(all)-controlWindow):])
		out.Points = append(out.Points, pt)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out.MeanDays = mean(all)
	out.MedianDays = percentile(all, 0.5)
	return out, nil
}

// createdVsResolved counts arrivals and departures per day, cumulative.
func createdVsResolved(ctx context.Context, tx db.DBTX, sc scope) (*CreatedResolvedReport, error) {
	window := flowWindow(sc)
	where, args := sc.clause("i", window)
	rows, err := tx.Query(ctx, `
		WITH days AS (SELECT generate_series(date_trunc('day', now() - make_interval(days => $3)), date_trunc('day', now()), '1 day') AS day)
		SELECT d.day,
		       (SELECT count(*) FROM issue i JOIN issue_type it ON it.id = i.issue_type_id
		         WHERE `+where+` AND `+notSubtask+` AND i.created_at >= d.day AND i.created_at < d.day + interval '1 day'),
		       (SELECT count(*) FROM issue i JOIN issue_type it ON it.id = i.issue_type_id
		         WHERE `+where+` AND `+notSubtask+` AND i.resolved_at >= d.day AND i.resolved_at < d.day + interval '1 day')
		FROM days d ORDER BY d.day`, args...)
	if err != nil {
		return nil, fmt.Errorf("created vs resolved: %w", err)
	}
	defer rows.Close()
	out := &CreatedResolvedReport{Days: []FlowCount{}, Window: window}
	created, resolved := 0, 0
	for rows.Next() {
		var d FlowCount
		if err := rows.Scan(&d.Day, &d.Created, &d.Resolved); err != nil {
			return nil, err
		}
		created += d.Created
		resolved += d.Resolved
		d.CreatedTotal, d.ResolvedTotal = created, resolved
		out.Days = append(out.Days, d)
	}
	return out, rows.Err()
}

// averageAge is how old the open work is: now, and at the start of each week
// of the window, counting what was open then by created and resolved times.
func averageAge(ctx context.Context, tx db.DBTX, sc scope) (*AverageAgeReport, error) {
	window := flowWindow(sc)
	where, args := sc.clause("i", window)
	out := &AverageAgeReport{Weeks: []AgeWeek{}, Window: window}

	// The window rides along as $3 so the same clause serves both queries.
	rows, err := tx.Query(ctx, `
		SELECT p.key || '-' || i.key_num, EXTRACT(EPOCH FROM now() - i.created_at) / 86400
		FROM issue i JOIN project p ON p.id = i.project_id
		JOIN issue_type it ON it.id = i.issue_type_id
		JOIN issue_status st ON st.id = i.status_id
		WHERE `+where+` AND `+notSubtask+` AND st.category <> `+catDone+` AND $3::int >= 0
		ORDER BY i.created_at`, args...)
	if err != nil {
		return nil, fmt.Errorf("average age: %w", err)
	}
	var ages []float64
	for rows.Next() {
		var (
			key string
			age float64
		)
		if err := rows.Scan(&key, &age); err != nil {
			rows.Close()
			return nil, err
		}
		if out.OldestKey == "" {
			out.OldestKey, out.OldestDays = key, age
		}
		ages = append(ages, age)
	}
	rows.Close()
	out.Open = len(ages)
	out.AverageDays = mean(ages)
	out.P85Days = percentile(ages, 0.85)

	weeks, err := tx.Query(ctx, `
		WITH weeks AS (SELECT generate_series(date_trunc('week', now() - make_interval(days => $3)), date_trunc('week', now()), '1 week') AS start)
		SELECT w.start,
		       count(i.id),
		       COALESCE(avg(EXTRACT(EPOCH FROM w.start - i.created_at) / 86400), 0)
		FROM weeks w
		LEFT JOIN issue i ON i.created_at < w.start AND (i.resolved_at IS NULL OR i.resolved_at >= w.start) AND `+where+`
		LEFT JOIN issue_type it ON it.id = i.issue_type_id
		WHERE i.id IS NULL OR `+notSubtask+`
		GROUP BY w.start ORDER BY w.start`, args...)
	if err != nil {
		return nil, fmt.Errorf("average age by week: %w", err)
	}
	defer weeks.Close()
	for weeks.Next() {
		var w AgeWeek
		if err := weeks.Scan(&w.Start, &w.Open, &w.Average); err != nil {
			return nil, err
		}
		out.Weeks = append(out.Weeks, w)
	}
	return out, weeks.Err()
}

// resolutionHistogram sorts what was resolved in the window into bands of
// how long it took from filed to resolved.
func resolutionHistogram(ctx context.Context, tx db.DBTX, sc scope) (*ResolutionReport, error) {
	window := flowWindow(sc)
	where, args := sc.clause("i", window)
	rows, err := tx.Query(ctx, `
		SELECT EXTRACT(EPOCH FROM i.resolved_at - i.created_at) / 86400
		FROM issue i JOIN issue_type it ON it.id = i.issue_type_id
		WHERE `+where+` AND `+notSubtask+` AND i.resolved_at >= now() - make_interval(days => $3)`, args...)
	if err != nil {
		return nil, fmt.Errorf("resolution histogram: %w", err)
	}
	defer rows.Close()
	out := &ResolutionReport{Bands: make([]Band, len(resolutionBuckets)), Window: window}
	for i, b := range resolutionBuckets {
		out.Bands[i].Label = b.label
	}
	for rows.Next() {
		var days float64
		if err := rows.Scan(&days); err != nil {
			return nil, err
		}
		out.Resolved++
		placed := false
		for i, b := range resolutionBuckets {
			if b.upTo > 0 && days < b.upTo {
				out.Bands[i].Count++
				placed = true
				break
			}
		}
		if !placed {
			out.Bands[len(out.Bands)-1].Count++
		}
	}
	return out, rows.Err()
}

// releaseBurndown is what is left of a version at the end of each day, from
// its start (or the window's) to today or its release, counted off the issues
// that fix it and when they were resolved.
func releaseBurndown(ctx context.Context, tx db.DBTX, sc scope, p Params) (*ReleaseBurndownReport, error) {
	if p.VersionID == nil {
		return nil, fmt.Errorf("%w: choose a version.", ErrBadChart)
	}
	var (
		out       = &ReleaseBurndownReport{VersionID: *p.VersionID, Days: []BurndownDay{}}
		startOn   *time.Time
		createdAt time.Time
	)
	err := tx.QueryRow(ctx, `SELECT name, start_on, release_on, created_at FROM version WHERE id = $1 AND project_id = $2`, *p.VersionID, sc.projectID).
		Scan(&out.VersionName, &startOn, &out.ReleaseOn, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("%w: that version is not one of this project's.", ErrBadChart)
	}
	from := midnightUTC(createdAt)
	if startOn != nil {
		from = midnightUTC(*startOn)
	}
	today := midnightUTC(time.Now())
	if from.After(today) {
		from = today
	}
	where, args := sc.clause("i", *p.VersionID, from, today)
	rows, err := tx.Query(ctx, `
		WITH days AS (SELECT generate_series($4::date, $5::date, '1 day') AS day),
		     fixing AS (
		         SELECT i.id, i.estimate, i.resolved_at FROM issue_version iv
		         JOIN issue i ON i.id = iv.issue_id
		         JOIN issue_type it ON it.id = i.issue_type_id
		         WHERE iv.version_id = $3 AND iv.role = 'fix' AND `+where+` AND `+notSubtask+`)
		SELECT d.day,
		       (SELECT count(*) FROM fixing f WHERE f.resolved_at IS NULL OR f.resolved_at >= d.day + interval '1 day'),
		       (SELECT COALESCE(sum(f.estimate), 0)::float8 FROM fixing f WHERE f.resolved_at IS NULL OR f.resolved_at >= d.day + interval '1 day'),
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
