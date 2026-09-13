// Package report is the project's dashboards: a list of which views somebody
// wanted, with every number computed on read so it can never be stale.
package report

import (
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/nql"
	"github.com/armature/armature/backend/internal/plan"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/sprint"
)

// Kind is one report a widget can show.
type Kind string

const (
	StatusBreakdown   Kind = "status_breakdown"
	PriorityBreakdown Kind = "priority_breakdown"
	TypeBreakdown     Kind = "type_breakdown"
	Throughput        Kind = "throughput"
	Workload          Kind = "workload"
	TeamWorkload      Kind = "team_workload"
	CycleTime         Kind = "cycle_time"
	Epics             Kind = "epics"
	Sprint            Kind = "sprint"
	Velocity          Kind = "velocity"
	Burndown          Kind = "burndown"
	SprintHistory     Kind = "sprint_history"
	SLA               Kind = "sla"
	RequestTypes      Kind = "request_types"
	// Filter is the tile that narrows the others; it has no report of its own.
	Filter Kind = "filter"
	// Chart is issues grouped by a field, drawn as the widget's config says.
	Chart Kind = "chart"
	// Milestones is how far the open milestones have got, or one in full.
	Milestones Kind = "milestones"
	// Versions is how far the unreleased versions have got, or one in full.
	Versions Kind = "versions"
	// The flow reports: how work moves over time.
	CumulativeFlow      Kind = "cumulative_flow"
	ControlChart        Kind = "control_chart"
	CreatedResolved     Kind = "created_vs_resolved"
	AverageAge          Kind = "avg_age"
	ResolutionHistogram Kind = "resolution_histogram"
	ReleaseBurndown     Kind = "release_burndown"
	// CSAT is what customers said of resolved requests.
	CSAT Kind = "csat"
)

// KindInfo describes a kind for the chooser: what it shows, and which projects
// it makes sense in.
type KindInfo struct {
	Kind        Kind   `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Width is the columns the widget takes by default, of two.
	Width int `json:"width"`
	// Kinds are the project kinds the report is offered for; empty is all.
	Kinds []project.Kind `json:"projectKinds,omitempty"`
	// Narrows says whether the dashboard's filter reaches this report. Reports
	// about sprints count sprints, not issues, and are left alone.
	Narrows bool `json:"narrows"`
}

var kinds = []KindInfo{
	{Filter, "Filter", "Narrows every widget that counts issues to the ones it names.", 2, nil, false},
	{Chart, "Chart", "Issues grouped by a field, as bars, a stack, a donut or a line over time.", 1, nil, true},
	{Milestones, "Milestones", "How far each open milestone has got, or one milestone in full.", 1, nil, true},
	{Versions, "Releases", "How far each unreleased version has got, or one version in full.", 1, nil, true},
	{CumulativeFlow, "Cumulative flow", "How many issues stood in each status, day by day; a band that widens is where work piles up.", 2, nil, false},
	{ControlChart, "Control chart", "How long each resolved issue took from started to resolved, with a rolling average.", 2, nil, true},
	{CreatedResolved, "Created vs resolved", "Arrivals against departures, day by day, cumulative; a gap that grows is a backlog growing.", 2, nil, true},
	{AverageAge, "Average age", "How old the open work is, now and week by week.", 1, nil, true},
	{ResolutionHistogram, "Resolution time", "How long resolved issues took, in bands from under a day to over four weeks.", 1, nil, true},
	{CSAT, "Satisfaction", "What customers said of resolved requests: the average, how the scores fall, how many answered.", 1, []project.Kind{project.KindService}, true},
	{ReleaseBurndown, "Release burndown", "What is left of a version, day by day, counted off the issues that fix it.", 2, nil, true},
	{StatusBreakdown, "Status", "Every issue by the state it is in.", 1, nil, true},
	{PriorityBreakdown, "Priority", "Open issues by how urgent they are.", 1, nil, true},
	{TypeBreakdown, "Issue types", "Open issues by what kind of work they are.", 1, nil, true},
	{Throughput, "Created and resolved", "How much arrives and how much gets finished, week by week.", 2, nil, true},
	{Workload, "Workload", "Who is carrying what: open, in progress, points, finished lately.", 2, nil, true},
	{TeamWorkload, "Teams", "The same, by team.", 1, nil, true},
	{CycleTime, "Cycle time", "How long an issue takes from filed to resolved.", 1, nil, true},
	{Epics, "Epics", "How far each epic has got.", 1, []project.Kind{project.KindSoftware, project.KindBusiness}, true},
	{Sprint, "Current sprint", "What was committed to the running sprint and how much of it is done.", 1, []project.Kind{project.KindSoftware}, false},
	{Velocity, "Velocity", "What the last sprints committed to and what they finished.", 1, []project.Kind{project.KindSoftware}, false},
	{Burndown, "Burndown", "How much of the running sprint is left, day by day, against the line it should follow.", 2, []project.Kind{project.KindSoftware}, false},
	{SprintHistory, "Sprint history", "What each finished sprint committed, completed and carried over, with its burndown on request.", 2, []project.Kind{project.KindSoftware}, false},
	{SLA, "Goals met", "How often the desk answered and resolved within its goals.", 1, []project.Kind{project.KindService}, true},
	{RequestTypes, "Request types", "Open requests by what customers asked for.", 1, []project.Kind{project.KindService}, true},
}

// Kinds returns the reports on offer for a project of a kind.
func Kinds(kind project.Kind) []KindInfo {
	out := []KindInfo{}
	for _, k := range kinds {
		if k.suits(kind) {
			out = append(out, k)
		}
	}
	return out
}

func (k KindInfo) suits(kind project.Kind) bool { return suitsKind(k.Kinds, kind) }

// suitsKind says whether a list of project kinds admits one; empty is every kind.
func suitsKind(kinds []project.Kind, kind project.Kind) bool {
	return len(kinds) == 0 || slices.Contains(kinds, kind)
}

// MaxWidth is the columns a dashboard is laid out in.
const MaxWidth = 2

func validWidth(width int) bool { return width >= 1 && width <= MaxWidth }

// widthOr keeps a width the layout can draw, falling back to the kind's own.
func widthOr(width int, info KindInfo) int {
	if !validWidth(width) {
		return info.Width
	}
	return width
}

func infoFor(kind Kind) (KindInfo, bool) {
	for _, k := range kinds {
		if k.Kind == kind {
			return k, true
		}
	}
	return KindInfo{}, false
}

// Dashboard is one arrangement of widgets over a project.
type Dashboard struct {
	ID         uuid.UUID `json:"id"`
	ProjectID  uuid.UUID `json:"projectId"`
	ProjectKey string    `json:"projectKey"`
	Name       string    `json:"name"`
	Position   int       `json:"position"`
	Widgets    []Widget  `json:"widgets"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Widget is one report on a dashboard, with how it is asked and drawn.
type Widget struct {
	ID          uuid.UUID       `json:"id"`
	DashboardID uuid.UUID       `json:"dashboardId"`
	Kind        Kind            `json:"kind"`
	Title       string          `json:"title"`
	Width       int             `json:"width"`
	Position    int             `json:"position"`
	Config      json.RawMessage `json:"config"`
}

// Params is what a report is asked with. Every report reads what it needs
// and ignores the rest.
type Params struct {
	// Days is the window for anything that looks back: throughput, cycle
	// time, what was finished lately.
	Days int `json:"days,omitempty"`
	// TeamID narrows to one team's issues.
	TeamID *uuid.UUID `json:"teamId,omitempty"`
	// SprintID asks the burndown for one sprint, running or over, instead of
	// every running one.
	SprintID *uuid.UUID `json:"sprintId,omitempty"`
	// MilestoneID asks the milestones widget for one milestone, open or
	// closed, instead of every open one.
	MilestoneID *uuid.UUID `json:"milestoneId,omitempty"`
	// VersionID asks the releases widget for one version, released or not.
	VersionID *uuid.UUID `json:"versionId,omitempty"`
	// Filter is what a filter tile starts out saying. The server stores it and
	// never reads it: the tile composes a query from it on the page.
	Filter *FilterSpec `json:"filter,omitempty"`
	// Narrow is the dashboard's query, compiled at the edge; it is asked with,
	// never stored.
	Narrow *nql.Compiled `json:"-"`

	// The chart widget's questions: what to group by, what to split each group
	// by, what to measure, how to draw it, and for a line, how to bucket time
	// and what each bucket counts. Each is one of a short list in chart.go.
	GroupBy  string `json:"groupBy,omitempty"`
	SplitBy  string `json:"splitBy,omitempty"`
	Measure  string `json:"measure,omitempty"`
	Shape    string `json:"shape,omitempty"`
	Interval string `json:"interval,omitempty"`
	Series   string `json:"series,omitempty"`
}

// ChartPart is one slice of a group when a chart is split by a second field.
type ChartPart struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

// ChartGroup is one bar, slice or stack: a label, its measure and its parts.
type ChartGroup struct {
	Label string `json:"label"`
	// Category is the status category behind a status label, so the page can
	// use the learned colours; empty for other groupings.
	Category string      `json:"category,omitempty"`
	Value    float64     `json:"value"`
	Parts    []ChartPart `json:"parts"`
}

// ChartReport is a chart over the scoped issues, largest group first.
type ChartReport struct {
	Groups  []ChartGroup `json:"groups"`
	Total   float64      `json:"total"`
	GroupBy string       `json:"groupBy"`
	SplitBy string       `json:"splitBy,omitempty"`
	Measure string       `json:"measure"`
}

// ChartPoint is one bucket of a line.
type ChartPoint struct {
	Start time.Time `json:"start"`
	Value float64   `json:"value"`
}

// ChartLine is one series over time.
type ChartLine struct {
	Name   string       `json:"name"`
	Points []ChartPoint `json:"points"`
}

// ChartSeriesReport is a chart over time: one line, or one per group.
type ChartSeriesReport struct {
	Series   []ChartLine `json:"series"`
	Interval string      `json:"interval"`
	Days     int         `json:"days"`
	Measure  string      `json:"measure"`
	GroupBy  string      `json:"groupBy,omitempty"`
	// Counts says what each bucket counts: created, resolved or open.
	Counts string `json:"counts"`
}

// FilterSpec is a filter tile's saved defaults, in the tile's own terms.
type FilterSpec struct {
	Types    []string `json:"types,omitempty"`
	Team     string   `json:"team,omitempty"`
	Assignee string   `json:"assignee,omitempty"`
	Category string   `json:"category,omitempty"`
	// Field is the date the window counts by: created, updated or resolved.
	Field string `json:"field,omitempty"`
	Days  int    `json:"days,omitempty"`
	// Milestone is a name, because that is what the search language matches.
	Milestone string `json:"milestone,omitempty"`
	Query     string `json:"query,omitempty"`
	// FilterID names a saved filter whose query the tile reads live.
	FilterID string `json:"filterId,omitempty"`
}

// DefaultDays is the window a widget looks back over unless told otherwise.
const DefaultDays = 30

// MaxDays bounds the window, so a widget cannot ask for the whole history.
const MaxDays = 365

func (p Params) window() int {
	switch {
	case p.Days <= 0:
		return DefaultDays
	case p.Days > MaxDays:
		return MaxDays
	}
	return p.Days
}

// Bucket is one slice of a breakdown.
type Bucket struct {
	Label string `json:"label"`
	Count int    `json:"count"`
	// Category is set for statuses, so a client can colour by meaning.
	Category string `json:"category,omitempty"`
}

// Breakdown is a count per label.
type Breakdown struct {
	Buckets []Bucket `json:"buckets"`
	Total   int      `json:"total"`
}

// Week is one week of created and resolved.
type Week struct {
	Start    time.Time `json:"start"`
	Created  int       `json:"created"`
	Resolved int       `json:"resolved"`
}

// ThroughputReport is arrivals against departures.
type ThroughputReport struct {
	Weeks    []Week `json:"weeks"`
	Created  int    `json:"created"`
	Resolved int    `json:"resolved"`
	Days     int    `json:"days"`
}

// Load is what one person or team is carrying.
type Load struct {
	ID   *uuid.UUID `json:"id,omitempty"`
	Name string     `json:"name"`
	// Open counts every unfinished issue, InProgress the part somebody is on.
	Open       int     `json:"open"`
	InProgress int     `json:"inProgress"`
	Points     float64 `json:"points"`
	// DoneRecently is what was resolved inside the window.
	DoneRecently int `json:"doneRecently"`
}

// WorkloadReport is the load per person or per team, heaviest first.
type WorkloadReport struct {
	Rows []Load `json:"rows"`
	Days int    `json:"days"`
}

// CycleWeek is the average time to resolve for issues resolved in one week.
type CycleWeek struct {
	Start        time.Time `json:"start"`
	Resolved     int       `json:"resolved"`
	AverageHours float64   `json:"averageHours"`
}

// CycleTimeReport is how long resolving takes.
type CycleTimeReport struct {
	Resolved     int         `json:"resolved"`
	AverageHours float64     `json:"averageHours"`
	MedianHours  float64     `json:"medianHours"`
	P90Hours     float64     `json:"p90Hours"`
	Weeks        []CycleWeek `json:"weeks"`
	Days         int         `json:"days"`
}

// EpicProgress is how far one epic has got, counted over its children.
type EpicProgress struct {
	Key        string `json:"key"`
	Summary    string `json:"summary"`
	Status     string `json:"status"`
	Category   string `json:"category"`
	Total      int    `json:"total"`
	Done       int    `json:"done"`
	InProgress int    `json:"inProgress"`
}

// EpicsReport is every open epic, least finished first.
type EpicsReport struct {
	Epics []EpicProgress `json:"epics"`
}

// SprintReport is the running sprint and where it stands.
type SprintReport struct {
	Active bool `json:"active"`
	// Plans has one entry per running sprint: a project with teams has one
	// per team.
	Plans []SprintStanding `json:"plans"`
}

// SprintStanding is one running sprint's numbers and the days it has left.
type SprintStanding struct {
	plan.SprintPlan
	DaysLeft  int `json:"daysLeft"`
	DaysTotal int `json:"daysTotal"`
}

// BurndownPoint is a sprint's standing on one day.
type BurndownPoint struct {
	Day         time.Time `json:"day"`
	Scope       float64   `json:"scope"`
	Done        float64   `json:"done"`
	Remaining   float64   `json:"remaining"`
	Issues      int       `json:"issues"`
	IssuesDone  int       `json:"issuesDone"`
	Unestimated int       `json:"unestimated"`
}

// SprintBurndown is one sprint's course: the days written down, and for a
// running sprint today read live, so the chart never waits for the worker.
type SprintBurndown struct {
	Sprint sprint.Sprint   `json:"sprint"`
	Points []BurndownPoint `json:"points"`
	// Live says the last point is today, computed now rather than stored.
	Live bool `json:"live"`
}

// BurndownReport is the running sprints' burndowns, or one sprint's when asked.
type BurndownReport struct {
	Active  bool             `json:"active"`
	Sprints []SprintBurndown `json:"sprints"`
}

// SprintOutcome is what a finished sprint came to, in points and in issues.
type SprintOutcome struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Team        string     `json:"team,omitempty"`
	StartsOn    *time.Time `json:"startsOn,omitempty"`
	EndsOn      *time.Time `json:"endsOn,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
	Committed   float64    `json:"committed"`
	Completed   float64    `json:"completed"`
	Finished    int        `json:"finished"`
	Carried     int        `json:"carried"`
}

// SprintHistoryReport is the last finished sprints, oldest first.
type SprintHistoryReport struct {
	Sprints []SprintOutcome `json:"sprints"`
}

// SprintResult is what a closed sprint turned out to be.
type SprintResult struct {
	Name        string     `json:"name"`
	Team        string     `json:"team,omitempty"`
	Committed   float64    `json:"committed"`
	Completed   float64    `json:"completed"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

// VelocityReport is the last sprints, oldest first, so a chart reads forwards.
type VelocityReport struct {
	Sprints []SprintResult `json:"sprints"`
}

// SLAMetric is how one goal has been kept.
type SLAMetric struct {
	Metric       string  `json:"metric"`
	Name         string  `json:"name"`
	Met          int     `json:"met"`
	Breached     int     `json:"breached"`
	Running      int     `json:"running"`
	AverageHours float64 `json:"averageHours"`
}

// SLAReport is how the desk has kept its goals inside the window.
type SLAReport struct {
	Metrics []SLAMetric `json:"metrics"`
	Days    int         `json:"days"`
}

var (
	// ErrNotFound is returned for a dashboard or widget that is not in the
	// caller's organization.
	ErrNotFound = errors.New("dashboard not found")
	// ErrNameTaken is returned when the project already has that dashboard.
	ErrNameTaken = errors.New("this project already has a dashboard by that name")
	// ErrBadKind is returned for a report nobody has written.
	ErrBadKind = errors.New("that is not a kind of report")
	// ErrLastDashboard is returned when deleting the only dashboard, since a
	// project's dashboard page needs something to show.
	ErrLastDashboard = errors.New("a project keeps at least one dashboard")
	// ErrNoReport is returned for a kind that is a widget but not a report.
	ErrNoReport = errors.New("a filter narrows the other widgets and has nothing of its own to show")
	// ErrOneFilter is returned when a dashboard would get a second filter.
	ErrOneFilter = errors.New("this dashboard already has a filter; change that one instead")
	// ErrBadChart is returned for a chart asked with a word off its short lists.
	ErrBadChart = errors.New("that is not a chart this dashboard can draw")
	// ErrNoTemplate is returned for a template key nobody has, built in or saved.
	ErrNoTemplate = errors.New("that is not a dashboard template")
	// ErrTemplateName is returned when a template is saved without a name.
	ErrTemplateName = errors.New("a template needs a name")
	// ErrTemplateNameTaken is returned when the organization has that template.
	ErrTemplateNameTaken = errors.New("this organization already has a template by that name")
)
