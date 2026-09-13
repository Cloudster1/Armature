package report

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/nql"
	"github.com/armature/armature/backend/internal/plan"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/workflow"
)

// notSubtask is the condition that leaves subtasks out of a count.
const notSubtask = "it.hierarchy_level >= 0"

// The status categories as SQL literals, so a report and the workflow cannot
// drift apart about what "done" is called.
var (
	catDone       = sqlCategory(workflow.CategoryDone)
	catInProgress = sqlCategory(workflow.CategoryInProgress)
	catTodo       = sqlCategory(workflow.CategoryTodo)
)

func sqlCategory(c workflow.StatusCategory) string { return "'" + string(c) + "'" }

// Report computes one kind of report for a project.
func (s *Service) Report(ctx context.Context, projectKey string, kind Kind, p Params) (any, error) {
	if _, ok := infoFor(kind); !ok {
		return nil, fmt.Errorf("%w: %q", ErrBadKind, kind)
	}
	key := project.NormalizeKey(projectKey)

	switch kind {
	case Filter:
		return nil, ErrNoReport
	case Sprint:
		return s.sprint(ctx, key)
	case Burndown:
		return s.burndown(ctx, key, p)
	case CumulativeFlow:
		return s.flow(ctx, key, p)
	}

	var out any
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var projectID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, key).Scan(&projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return project.ErrNotFound
		}
		if err != nil {
			return err
		}
		sc := scope{projectID: projectID, teamID: p.TeamID, days: p.window(), narrow: p.Narrow}

		switch kind {
		case StatusBreakdown:
			out, err = breakdown(ctx, tx, sc, `st.name`, `st.category::text`, false)
		case PriorityBreakdown:
			out, err = breakdown(ctx, tx, sc, `i.priority::text`, `''`, true)
		case TypeBreakdown:
			out, err = breakdown(ctx, tx, sc, `it.name`, `''`, true)
		case RequestTypes:
			out, err = breakdown(ctx, tx, sc, `COALESCE(rt.name, 'Filed by an agent')`, `''`, true)
		case Throughput:
			out, err = throughput(ctx, tx, sc)
		case Workload:
			out, err = workload(ctx, tx, sc, false)
		case TeamWorkload:
			out, err = workload(ctx, tx, sc, true)
		case CycleTime:
			out, err = cycleTime(ctx, tx, sc)
		case Epics:
			out, err = epics(ctx, tx, sc)
		case Velocity:
			out, err = velocity(ctx, tx, sc)
		case SprintHistory:
			out, err = sprintHistory(ctx, tx, sc)
		case SLA:
			out, err = sla(ctx, tx, sc)
		case Milestones:
			out, err = milestones(ctx, tx, sc, p)
		case Versions:
			out, err = versions(ctx, tx, sc, p)
		case ControlChart:
			out, err = controlChart(ctx, tx, sc)
		case CreatedResolved:
			out, err = createdVsResolved(ctx, tx, sc)
		case AverageAge:
			out, err = averageAge(ctx, tx, sc)
		case ResolutionHistogram:
			out, err = resolutionHistogram(ctx, tx, sc)
		case ReleaseBurndown:
			out, err = releaseBurndown(ctx, tx, sc, p)
		case CSAT:
			out, err = csat(ctx, tx, sc)
		case Chart:
			if p.isLine() {
				out, err = chartLines(ctx, tx, sc, p)
			} else {
				out, err = chart(ctx, tx, sc, p)
			}
		default:
			err = fmt.Errorf("%w: %q", ErrBadKind, kind)
		}
		return err
	})
	return out, err
}

// scope is which issues a report counts.
type scope struct {
	projectID uuid.UUID
	teamID    *uuid.UUID
	days      int
	// narrow is the dashboard's query, or nil for everything in the project.
	narrow *nql.Compiled
}

// clause narrows to the project, to the team when one is asked for, and to
// the dashboard's query when there is one.
func (sc scope) clause(alias string, more ...any) (string, []any) {
	// alias is the issue table's name in the report, "i" almost everywhere. The
	// report's own arguments come first, so the query's are numbered after them.
	where := alias + `.project_id = $1 AND ($2::uuid IS NULL OR ` + alias + `.team_id = $2)`
	args := append([]any{sc.projectID, sc.teamID}, more...)
	// The query names the aliases the search catalog binds to, so it runs in a
	// subquery that owns them and leaves the report's own aliases alone.
	if q, qa := sc.narrow.SQL(len(args) + 1); q != "" {
		where += ` AND ` + alias + `.id IN (SELECT i.id FROM issue i
			JOIN project p ON p.id = i.project_id
			JOIN issue_type it ON it.id = i.issue_type_id
			JOIN issue_status s ON s.id = i.status_id
			WHERE ` + q + `)`
		args = append(args, qa...)
	}
	return where, args
}

// breakdown counts issues per label. Subtasks are left out: they move with
// their parent and would count the same work twice.
func breakdown(ctx context.Context, tx db.DBTX, sc scope, label, category string, openOnly bool) (*Breakdown, error) {
	open := ""
	if openOnly {
		open = ` AND st.category <> ` + catDone
	}
	where, args := sc.clause("i")
	rows, err := tx.Query(ctx, `
		SELECT `+label+`, `+category+`, count(*)
		FROM issue i
		JOIN issue_status st ON st.id = i.status_id
		JOIN issue_type it ON it.id = i.issue_type_id
		LEFT JOIN request_type rt ON rt.id = i.request_type_id
		WHERE `+where+` AND `+notSubtask+open+`
		GROUP BY 1, 2
		ORDER BY count(*) DESC, 1`, args...)
	if err != nil {
		return nil, fmt.Errorf("breakdown: %w", err)
	}
	defer rows.Close()
	out := &Breakdown{Buckets: []Bucket{}}
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.Label, &b.Category, &b.Count); err != nil {
			return nil, err
		}
		out.Buckets = append(out.Buckets, b)
		out.Total += b.Count
	}
	return out, rows.Err()
}

// throughput is issues created against issues resolved, per week. Weeks with
// nothing in them are still weeks, so the chart has gaps where the gaps were.
func throughput(ctx context.Context, tx db.DBTX, sc scope) (*ThroughputReport, error) {
	where, args := sc.clause("i", sc.days)
	rows, err := tx.Query(ctx, `
		WITH weeks AS (
		    SELECT generate_series(
		        date_trunc('week', now() - make_interval(days => $3)),
		        date_trunc('week', now()), '1 week') AS start
		)
		SELECT w.start,
		       (SELECT count(*) FROM issue i JOIN issue_type it ON it.id = i.issue_type_id
		         WHERE `+where+` AND `+notSubtask+`
		           AND i.created_at >= w.start AND i.created_at < w.start + interval '1 week'),
		       (SELECT count(*) FROM issue i JOIN issue_type it ON it.id = i.issue_type_id
		         WHERE `+where+` AND `+notSubtask+`
		           AND i.resolved_at >= w.start AND i.resolved_at < w.start + interval '1 week')
		FROM weeks w ORDER BY w.start`, args...)
	if err != nil {
		return nil, fmt.Errorf("throughput: %w", err)
	}
	defer rows.Close()
	out := &ThroughputReport{Weeks: []Week{}, Days: sc.days}
	for rows.Next() {
		var w Week
		if err := rows.Scan(&w.Start, &w.Created, &w.Resolved); err != nil {
			return nil, err
		}
		out.Weeks = append(out.Weeks, w)
		out.Created += w.Created
		out.Resolved += w.Resolved
	}
	return out, rows.Err()
}

// workload is what each person, or each team, is carrying. Unassigned work is
// a row too: it is somebody's problem even before it is anybody's.
func workload(ctx context.Context, tx db.DBTX, sc scope, byTeam bool) (*WorkloadReport, error) {
	who, name, join := `i.assignee_id`, `COALESCE(u.name, 'Unassigned')`, `LEFT JOIN app_user u ON u.id = i.assignee_id`
	if byTeam {
		who, name, join = `i.team_id`, `COALESCE(t.name, 'No team')`, `LEFT JOIN team t ON t.id = i.team_id`
	}
	where, args := sc.clause("i", sc.days)
	rows, err := tx.Query(ctx, `
		SELECT `+who+`, `+name+`,
		       count(*) FILTER (WHERE st.category <> `+catDone+`),
		       count(*) FILTER (WHERE st.category = `+catInProgress+`),
		       COALESCE(sum(i.estimate) FILTER (WHERE st.category <> `+catDone+`), 0),
		       count(*) FILTER (WHERE i.resolved_at >= now() - make_interval(days => $3))
		FROM issue i
		JOIN issue_status st ON st.id = i.status_id
		JOIN issue_type it ON it.id = i.issue_type_id
		`+join+`
		WHERE `+where+` AND `+notSubtask+`
		  AND (st.category <> `+catDone+` OR i.resolved_at >= now() - make_interval(days => $3))
		GROUP BY 1, 2
		ORDER BY 3 DESC, 2`, args...)
	if err != nil {
		return nil, fmt.Errorf("workload: %w", err)
	}
	defer rows.Close()
	out := &WorkloadReport{Rows: []Load{}, Days: sc.days}
	for rows.Next() {
		var l Load
		if err := rows.Scan(&l.ID, &l.Name, &l.Open, &l.InProgress, &l.Points, &l.DoneRecently); err != nil {
			return nil, err
		}
		out.Rows = append(out.Rows, l)
	}
	return out, rows.Err()
}

// cycleTime is filed-to-resolved for what was resolved inside the window.
func cycleTime(ctx context.Context, tx db.DBTX, sc scope) (*CycleTimeReport, error) {
	out := &CycleTimeReport{Weeks: []CycleWeek{}, Days: sc.days}
	var hours []float64
	where, args := sc.clause("i", sc.days)
	rows, err := tx.Query(ctx, `
		SELECT EXTRACT(EPOCH FROM i.resolved_at - i.created_at) / 3600, date_trunc('week', i.resolved_at)
		FROM issue i JOIN issue_type it ON it.id = i.issue_type_id
		WHERE `+where+` AND `+notSubtask+`
		  AND i.resolved_at IS NOT NULL AND i.resolved_at >= now() - make_interval(days => $3)
		ORDER BY 2`, args...)
	if err != nil {
		return nil, fmt.Errorf("cycle time: %w", err)
	}
	defer rows.Close()
	byWeek := map[time.Time]*CycleWeek{}
	var order []time.Time
	for rows.Next() {
		var (
			h    float64
			week time.Time
		)
		if err := rows.Scan(&h, &week); err != nil {
			return nil, err
		}
		hours = append(hours, h)
		w, ok := byWeek[week]
		if !ok {
			w = &CycleWeek{Start: week}
			byWeek[week] = w
			order = append(order, week)
		}
		w.Resolved++
		w.AverageHours += h
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, week := range order {
		w := byWeek[week]
		w.AverageHours = round1(w.AverageHours / float64(w.Resolved))
		out.Weeks = append(out.Weeks, *w)
	}
	out.Resolved = len(hours)
	out.AverageHours = round1(mean(hours))
	out.MedianHours = round1(percentile(hours, 0.5))
	out.P90Hours = round1(percentile(hours, 0.9))
	return out, nil
}

// epics is each unfinished epic and how far its children have got.
func epics(ctx context.Context, tx db.DBTX, sc scope) (*EpicsReport, error) {
	const epicLevel = 1
	where, args := sc.clause("e", epicLevel)
	rows, err := tx.Query(ctx, `
		SELECT p.key || '-' || e.key_num, e.summary, st.name, st.category::text,
		       (SELECT count(*) FROM issue c WHERE c.parent_id = e.id),
		       (SELECT count(*) FROM issue c JOIN issue_status cs ON cs.id = c.status_id WHERE c.parent_id = e.id AND cs.category = `+catDone+`),
		       (SELECT count(*) FROM issue c JOIN issue_status cs ON cs.id = c.status_id WHERE c.parent_id = e.id AND cs.category = `+catInProgress+`)
		FROM issue e
		JOIN project p ON p.id = e.project_id
		JOIN issue_status st ON st.id = e.status_id
		JOIN issue_type it ON it.id = e.issue_type_id
		WHERE `+where+`
		  AND it.hierarchy_level = $3 AND st.category <> `+catDone+`
		ORDER BY e.created_at`, args...)
	if err != nil {
		return nil, fmt.Errorf("epics: %w", err)
	}
	defer rows.Close()
	out := &EpicsReport{Epics: []EpicProgress{}}
	for rows.Next() {
		var e EpicProgress
		if err := rows.Scan(&e.Key, &e.Summary, &e.Status, &e.Category, &e.Total, &e.Done, &e.InProgress); err != nil {
			return nil, err
		}
		out.Epics = append(out.Epics, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Least finished first, so what needs attention is at the top.
	sort.SliceStable(out.Epics, func(a, b int) bool {
		return fraction(out.Epics[a]) < fraction(out.Epics[b])
	})
	return out, nil
}

func fraction(e EpicProgress) float64 {
	if e.Total == 0 {
		return 0
	}
	return float64(e.Done) / float64(e.Total)
}

// sprint is the running sprint's standing, from the same plan the sprints
// page and the completion report use, so the numbers agree everywhere.
func (s *Service) sprint(ctx context.Context, projectKey string) (*SprintReport, error) {
	out := &SprintReport{Plans: []SprintStanding{}}
	if s.plans == nil {
		return out, nil
	}
	p, err := s.plans.ForProject(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	for _, sp := range p.Sprints {
		if sp.Sprint.State != sprint.StateActive {
			continue
		}
		standing := SprintStanding{SprintPlan: sp}
		if sp.Sprint.EndsOn != nil {
			standing.DaysLeft = int(math.Ceil(sp.Sprint.EndsOn.Sub(today).Hours() / 24))
			if standing.DaysLeft < 0 {
				standing.DaysLeft = 0
			}
		}
		if sp.Sprint.StartsOn != nil && sp.Sprint.EndsOn != nil {
			standing.DaysTotal = int(sp.Sprint.EndsOn.Sub(*sp.Sprint.StartsOn).Hours()/24) + 1
		}
		out.Plans = append(out.Plans, standing)
	}
	out.Active = len(out.Plans) > 0
	return out, nil
}

// lastSprints is how far back the sprint reports look: enough to see a trend,
// few enough to read at a glance.
const lastSprints = 8

// burndown is each sprint's course, day by day, from the snapshots that were
// written; a running sprint's today is read live, so the chart never waits.
func (s *Service) burndown(ctx context.Context, projectKey string, p Params) (*BurndownReport, error) {
	out := &BurndownReport{Sprints: []SprintBurndown{}}
	if s.plans == nil || s.sprints == nil {
		return out, nil
	}
	// One sprint by id may be over, which the plan's own list leaves out.
	var found []sprint.Sprint
	if p.SprintID != nil {
		one, err := s.sprints.ByID(ctx, *p.SprintID)
		if err != nil {
			return nil, err
		}
		if one.ProjectKey != project.NormalizeKey(projectKey) {
			return nil, sprint.ErrNotFound
		}
		found = []sprint.Sprint{*one}
	} else {
		all, err := s.sprints.ForProject(ctx, projectKey)
		if err != nil {
			return nil, err
		}
		for _, sp := range all {
			if sp.State == sprint.StateActive {
				found = append(found, sp)
			}
		}
	}
	var live map[uuid.UUID]plan.SprintPlan
	for _, sp := range found {
		curve := SprintBurndown{Sprint: sp, Points: []BurndownPoint{}}
		days, err := s.sprints.Snapshots(ctx, sp.ID)
		if err != nil {
			return nil, err
		}
		for _, d := range days {
			curve.Points = append(curve.Points, pointOf(d.Day, d.Scope, d.Done, d.Issues, d.IssuesDone, d.Unestimated))
		}
		if sp.State == sprint.StateActive {
			if live == nil {
				if live, err = s.livePlans(ctx, projectKey); err != nil {
					return nil, err
				}
			}
			if standing, ok := live[sp.ID]; ok {
				today := midnightUTC(time.Now())
				point := pointOf(today, standing.Committed, standing.Completed, standing.Issues, standing.IssuesDone, standing.Unestimated)
				if n := len(curve.Points); n > 0 && curve.Points[n-1].Day.Equal(today) {
					curve.Points[n-1] = point
				} else {
					curve.Points = append(curve.Points, point)
				}
				curve.Live = true
			}
		}
		out.Sprints = append(out.Sprints, curve)
	}
	for _, c := range out.Sprints {
		if c.Sprint.State == sprint.StateActive {
			out.Active = true
		}
	}
	return out, nil
}

// livePlans is the plan's count of every sprint, keyed by sprint.
func (s *Service) livePlans(ctx context.Context, projectKey string) (map[uuid.UUID]plan.SprintPlan, error) {
	p, err := s.plans.ForProject(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID]plan.SprintPlan{}
	for _, sp := range p.Sprints {
		out[sp.Sprint.ID] = sp
	}
	return out, nil
}

func pointOf(day time.Time, committed, done float64, issues, issuesDone, unestimated int) BurndownPoint {
	return BurndownPoint{Day: day, Scope: committed, Done: done, Remaining: committed - done, Issues: issues, IssuesDone: issuesDone, Unestimated: unestimated}
}

func midnightUTC(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// closedSprints is what the last finished sprints came to, from the numbers
// written when each closed, oldest first so a chart reads forwards.
func closedSprints(ctx context.Context, tx db.DBTX, sc scope, what string) ([]SprintOutcome, error) {
	rows, err := tx.Query(ctx, `
		SELECT s.id, s.name, COALESCE(t.name, ''), s.starts_on, s.ends_on, s.completed_at,
		       COALESCE(s.committed, 0), COALESCE(s.completed, 0), COALESCE(s.finished, 0), COALESCE(s.carried, 0)
		FROM sprint s LEFT JOIN team t ON t.id = s.team_id
		WHERE s.project_id = $1 AND ($2::uuid IS NULL OR s.team_id = $2) AND s.state = 'closed'
		ORDER BY s.completed_at DESC NULLS LAST LIMIT $3`, sc.projectID, sc.teamID, lastSprints)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	defer rows.Close()
	out := []SprintOutcome{}
	for rows.Next() {
		var o SprintOutcome
		if err := rows.Scan(&o.ID, &o.Name, &o.Team, &o.StartsOn, &o.EndsOn, &o.CompletedAt, &o.Committed, &o.Completed, &o.Finished, &o.Carried); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	reverse(out)
	return out, nil
}

// reverse turns a newest-first list into an oldest-first one, in place.
func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// sprintHistory is the last finished sprints in full.
func sprintHistory(ctx context.Context, tx db.DBTX, sc scope) (*SprintHistoryReport, error) {
	found, err := closedSprints(ctx, tx, sc, "sprint history")
	if err != nil {
		return nil, err
	}
	return &SprintHistoryReport{Sprints: found}, nil
}

// velocity is the same sprints read as a trend: what each committed to and
// what it finished.
func velocity(ctx context.Context, tx db.DBTX, sc scope) (*VelocityReport, error) {
	found, err := closedSprints(ctx, tx, sc, "velocity")
	if err != nil {
		return nil, err
	}
	out := &VelocityReport{Sprints: make([]SprintResult, 0, len(found))}
	for _, s := range found {
		out.Sprints = append(out.Sprints, SprintResult{
			Name: s.Name, Team: s.Team, Committed: s.Committed, Completed: s.Completed, CompletedAt: s.CompletedAt,
		})
	}
	return out, nil
}

// sla is how the desk kept each goal for clocks that ran inside the window.
func sla(ctx context.Context, tx db.DBTX, sc scope) (*SLAReport, error) {
	where, args := sc.clause("i", sc.days)
	rows, err := tx.Query(ctx, `
		SELECT sp.metric::text, sp.name,
		       count(*) FILTER (WHERE t.completed_at IS NOT NULL AND t.breached_at IS NULL),
		       count(*) FILTER (WHERE t.breached_at IS NOT NULL),
		       count(*) FILTER (WHERE t.completed_at IS NULL AND t.running_since IS NOT NULL),
		       COALESCE(avg(t.elapsed_seconds) FILTER (WHERE t.completed_at IS NOT NULL), 0) / 3600
		FROM sla_timer t
		JOIN sla_policy sp ON sp.id = t.policy_id
		JOIN issue i ON i.id = t.issue_id
		WHERE `+where+` AND t.started_at >= now() - make_interval(days => $3)
		GROUP BY sp.metric, sp.name ORDER BY sp.metric`, args...)
	if err != nil {
		return nil, fmt.Errorf("sla: %w", err)
	}
	defer rows.Close()
	out := &SLAReport{Metrics: []SLAMetric{}, Days: sc.days}
	for rows.Next() {
		var m SLAMetric
		if err := rows.Scan(&m.Metric, &m.Name, &m.Met, &m.Breached, &m.Running, &m.AverageHours); err != nil {
			return nil, err
		}
		m.AverageHours = round1(m.AverageHours)
		out.Metrics = append(out.Metrics, m)
	}
	return out, rows.Err()
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// percentile is the nearest-rank percentile of the values, which is the one
// people mean by "the median" and "p90" when they check by hand.
func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	rank := int(math.Ceil(p*float64(len(sorted)))) - 1
	if rank < 0 {
		rank = 0
	}
	if rank >= len(sorted) {
		rank = len(sorted) - 1
	}
	return sorted[rank]
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
