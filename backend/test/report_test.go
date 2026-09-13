//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/milestone"
	"github.com/armature/armature/backend/internal/nql"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/sprint"
)

func (ws *workspace) reports(h *harness) *report.Service {
	return report.NewService(h.cluster, ws.plans).WithSprints(ws.sprints)
}

func kindsOf(widgets []report.Widget) []report.Kind {
	out := make([]report.Kind, 0, len(widgets))
	for _, w := range widgets {
		out = append(out, w.Kind)
	}
	return out
}

func hasKind(widgets []report.Widget, kind report.Kind) bool {
	for _, w := range widgets {
		if w.Kind == kind {
			return true
		}
	}
	return false
}

func bucket(b *report.Breakdown, label string) int {
	for _, each := range b.Buckets {
		if each.Label == label {
			return each.Count
		}
	}
	return 0
}

func TestANewProjectHasADashboardThatSuitsIt(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "dashing")
	r := ws.reports(h)

	dashboards, err := r.Dashboards(ws.ctx, ws.project.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(dashboards) != 1 || dashboards[0].Name != report.DefaultName {
		t.Fatalf("dashboards = %+v, want one called %s", dashboards, report.DefaultName)
	}
	widgets := dashboards[0].Widgets
	for _, want := range []report.Kind{report.StatusBreakdown, report.Sprint, report.Burndown, report.Workload, report.Throughput, report.Velocity} {
		if !hasKind(widgets, want) {
			t.Errorf("a software project's dashboard lacks %s: %v", want, kindsOf(widgets))
		}
	}
	if hasKind(widgets, report.SLA) {
		t.Error("a software project's dashboard shows service goals it does not have")
	}

	t.Run("a project from before dashboards existed gets one on first sight", func(t *testing.T) {
		if _, err := h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `DELETE FROM dashboard WHERE project_id = $1`, ws.project.ID)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		again, err := r.Dashboards(ws.ctx, ws.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(again) != 1 || len(again[0].Widgets) == 0 {
			t.Errorf("dashboards = %+v, want the default made again", again)
		}
	})

	_, deskProject := ws.aDesk(t, h, "Dash desk")
	deskDashboards, err := r.Dashboards(ws.ctx, deskProject.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(deskDashboards) != 1 || !hasKind(deskDashboards[0].Widgets, report.SLA) || !hasKind(deskDashboards[0].Widgets, report.RequestTypes) {
		t.Errorf("a service desk's dashboard = %v, want goals and request types on it", kindsOf(deskDashboards[0].Widgets))
	}
	if hasKind(deskDashboards[0].Widgets, report.Velocity) {
		t.Error("a service desk's dashboard shows sprint velocity")
	}
}

func TestReportsCountTheProjectsWork(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "counting")
	r := ws.reports(h)

	waiting := ws.newIssue(t, "still to do")
	started := ws.newIssue(t, "under way")
	finished := ws.newIssue(t, "already closed")
	ws.move(t, h, started.Key, "Start progress")
	ws.move(t, h, finished.Key, "Close")

	get := func(kind report.Kind, p report.Params) any {
		t.Helper()
		out, err := r.Report(ws.ctx, ws.project.Key, kind, p)
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		return out
	}

	t.Run("status counts everything, priority only what is open", func(t *testing.T) {
		status := get(report.StatusBreakdown, report.Params{}).(*report.Breakdown)
		if status.Total != 3 || bucket(status, bootstrap.StatusToDo) != 1 || bucket(status, bootstrap.StatusInProgress) != 1 || bucket(status, bootstrap.StatusDone) != 1 {
			t.Errorf("status = %+v", status)
		}
		for _, b := range status.Buckets {
			if b.Category == "" {
				t.Errorf("status bucket %q carries no category to colour by", b.Label)
			}
		}
		priority := get(report.PriorityBreakdown, report.Params{}).(*report.Breakdown)
		if priority.Total != 2 || bucket(priority, "medium") != 2 {
			t.Errorf("priority = %+v, want the two open issues", priority)
		}
	})

	t.Run("workload knows who is carrying what", func(t *testing.T) {
		load := get(report.Workload, report.Params{}).(*report.WorkloadReport)
		var mine, nobody *report.Load
		for i := range load.Rows {
			switch {
			case load.Rows[i].ID != nil && *load.Rows[i].ID == ws.actor.UserID:
				mine = &load.Rows[i]
			case load.Rows[i].ID == nil:
				nobody = &load.Rows[i]
			}
		}
		// Starting progress assigned the issue to the actor.
		if mine == nil || mine.Open != 1 || mine.InProgress != 1 {
			t.Errorf("my load = %+v, want one open and in progress", mine)
		}
		if nobody == nil || nobody.Open != 1 || nobody.DoneRecently != 1 {
			t.Errorf("unassigned load = %+v, want one open (%s) and one finished lately (%s)", nobody, waiting.Key, finished.Key)
		}
	})

	t.Run("throughput and cycle time see the one resolved", func(t *testing.T) {
		through := get(report.Throughput, report.Params{Days: 7}).(*report.ThroughputReport)
		if through.Created != 3 || through.Resolved != 1 || len(through.Weeks) == 0 {
			t.Errorf("throughput = %+v", through)
		}
		cycle := get(report.CycleTime, report.Params{}).(*report.CycleTimeReport)
		if cycle.Resolved != 1 || cycle.MedianHours > 1 {
			t.Errorf("cycle time = %+v, want one resolved within the hour", cycle)
		}
	})

	t.Run("epics report their children", func(t *testing.T) {
		epic, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: ws.project.Key, Summary: "the epic", TypeID: h.issueTypeID(t, ws, bootstrap.TypeEpic)}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		for _, summary := range []string{"child one", "child two"} {
			if _, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: ws.project.Key, Summary: summary, ParentKey: epic.Key}, ws.actor); err != nil {
				t.Fatal(err)
			}
		}
		children, _ := ws.issues.List(ws.ctx, issue.Filter{ProjectKey: ws.project.Key, ParentID: &epic.ID}, issue.Page{Limit: 10})
		ws.move(t, h, children.Issues[0].Key, "Close")

		epics := get(report.Epics, report.Params{}).(*report.EpicsReport)
		if len(epics.Epics) != 1 || epics.Epics[0].Key != epic.Key || epics.Epics[0].Total != 2 || epics.Epics[0].Done != 1 {
			t.Errorf("epics = %+v, want %s at one of two", epics.Epics, epic.Key)
		}
	})

	t.Run("the running sprint and the closed ones", func(t *testing.T) {
		before := get(report.Sprint, report.Params{}).(*report.SprintReport)
		if before.Active {
			t.Error("a project with no sprint reports one running")
		}
		first := ws.runnable(t, ws.project.Key, "Sprint 1", nil)
		ws.commit(t, waiting.Key, &first.ID, size(5))
		ws.start(t, first.ID)

		running := get(report.Sprint, report.Params{}).(*report.SprintReport)
		if !running.Active || len(running.Plans) != 1 || running.Plans[0].Committed != 5 || running.Plans[0].DaysLeft <= 0 {
			t.Errorf("sprint = %+v, want Sprint 1 running with 5 points and days left", running)
		}
		if running.Plans[0].Issues != 1 || running.Plans[0].IssuesDone != 0 {
			t.Errorf("sprint counts %d issues, %d done, want one and none", running.Plans[0].Issues, running.Plans[0].IssuesDone)
		}

		// The burndown reads the day the start wrote and today live; on the
		// first day those are the same day, and the live one wins.
		burndown := get(report.Burndown, report.Params{}).(*report.BurndownReport)
		if !burndown.Active || len(burndown.Sprints) != 1 || !burndown.Sprints[0].Live {
			t.Fatalf("burndown = %+v, want the running sprint with a live point", burndown)
		}
		points := burndown.Sprints[0].Points
		if len(points) != 1 || points[0].Scope != 5 || points[0].Remaining != 5 {
			t.Errorf("points = %+v, want today at 5 of 5", points)
		}
		ws.move(t, h, waiting.Key, "Close")
		burndown = get(report.Burndown, report.Params{}).(*report.BurndownReport)
		if last := burndown.Sprints[0].Points[len(burndown.Sprints[0].Points)-1]; last.Remaining != 0 || last.IssuesDone != 1 {
			t.Errorf("after closing the work, today = %+v, want nothing remaining", last)
		}

		if _, _, err := ws.sprints.Complete(ws.ctx, first.ID, sprint.CompleteInput{}, ws.actor.UserID); err != nil {
			t.Fatal(err)
		}
		velocity := get(report.Velocity, report.Params{}).(*report.VelocityReport)
		if len(velocity.Sprints) != 1 || velocity.Sprints[0].Name != "Sprint 1" || velocity.Sprints[0].Committed != 5 {
			t.Errorf("velocity = %+v", velocity)
		}
		history := get(report.SprintHistory, report.Params{}).(*report.SprintHistoryReport)
		if len(history.Sprints) != 1 || history.Sprints[0].Name != "Sprint 1" || history.Sprints[0].Finished != 1 || history.Sprints[0].Carried != 0 || history.Sprints[0].Completed != 5 {
			t.Errorf("history = %+v, want Sprint 1 with one issue finished and none carried", history)
		}
		frozen := get(report.Burndown, report.Params{SprintID: &first.ID}).(*report.BurndownReport)
		if frozen.Active || len(frozen.Sprints) != 1 || frozen.Sprints[0].Live || len(frozen.Sprints[0].Points) != 1 {
			t.Errorf("a finished sprint's burndown = %+v, want its written days and no live point", frozen)
		}
		if get(report.Burndown, report.Params{}).(*report.BurndownReport).Active {
			t.Error("with the sprint over, the burndown still says one is running")
		}
	})

	t.Run("a report nobody wrote is refused", func(t *testing.T) {
		if _, err := r.Report(ws.ctx, ws.project.Key, "pie", report.Params{}); !errors.Is(err, report.ErrBadKind) {
			t.Errorf("err = %v, want ErrBadKind", err)
		}
	})
}

func TestTheDeskReportsHowItKeptItsGoals(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "goalkeeping")
	d, p := ws.aDesk(t, h, "Report desk")
	r := ws.reports(h)
	customer := ws.customerOf(t, h, "reported")
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, _ := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "How?"}, customer)
	if _, _, err := ws.issues.AddComment(ws.ctx, raised.Key, issue.TextDocument("Like this."), ws.actor); err != nil {
		t.Fatal(err)
	}

	out, err := r.Report(ws.ctx, p.Key, report.SLA, report.Params{})
	if err != nil {
		t.Fatal(err)
	}
	slaReport := out.(*report.SLAReport)
	var first, resolution *report.SLAMetric
	for i := range slaReport.Metrics {
		switch slaReport.Metrics[i].Metric {
		case "first_response":
			first = &slaReport.Metrics[i]
		case "resolution":
			resolution = &slaReport.Metrics[i]
		}
	}
	if first == nil || first.Met != 1 || first.Breached != 0 {
		t.Errorf("first response = %+v, want one met", first)
	}
	if resolution == nil || resolution.Running != 1 {
		t.Errorf("resolution = %+v, want one still running", resolution)
	}

	kinds, err := r.Report(ws.ctx, p.Key, report.RequestTypes, report.Params{})
	if err != nil {
		t.Fatal(err)
	}
	if b := kinds.(*report.Breakdown); bucket(b, "Ask a question") != 1 {
		t.Errorf("request types = %+v", b)
	}
}

func TestDashboardsAreArranged(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "arranging")
	other := h.newWorkspace(t, "elsewhere")
	r := ws.reports(h)

	made, _, err := r.CreateDashboardFrom(ws.ctx, ws.project.Key, report.CreateInput{Name: "Release readiness"}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.CreateDashboardFrom(ws.ctx, ws.project.Key, report.CreateInput{Name: "release readiness"}, ws.actor.UserID); !errors.Is(err, report.ErrNameTaken) {
		t.Errorf("a second dashboard by the same name gave %v", err)
	}

	if _, _, err := r.AddWidget(ws.ctx, made.ID, report.WidgetInput{Kind: "pie"}); !errors.Is(err, report.ErrBadKind) {
		t.Errorf("an unknown widget kind gave %v", err)
	}
	a, _, err := r.AddWidget(ws.ctx, made.ID, report.WidgetInput{Kind: report.Epics})
	if err != nil {
		t.Fatal(err)
	}
	if a.Title != "Epics" || a.Width != 1 {
		t.Errorf("widget = %+v, want the kind's own title and width", a)
	}
	b, _, err := r.AddWidget(ws.ctx, made.ID, report.WidgetInput{Kind: report.Throughput, Title: "Flow", Config: report.Params{Days: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if b.Width != 2 || report.ParamsFrom(b.Config).Days != 7 {
		t.Errorf("widget = %+v, want two wide with a seven day window", b)
	}

	if _, err := r.ReorderWidgets(ws.ctx, made.ID, []uuid.UUID{b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	dashboards, _ := r.Dashboards(ws.ctx, ws.project.Key)
	var found *report.Dashboard
	for i := range dashboards {
		if dashboards[i].ID == made.ID {
			found = &dashboards[i]
		}
	}
	if found == nil || len(found.Widgets) != 2 || found.Widgets[0].ID != b.ID {
		t.Errorf("after reordering = %+v, want the throughput first", found)
	}

	width := 3
	if _, _, err := r.UpdateWidget(ws.ctx, a.ID, report.UpdateWidgetInput{Width: &width}); err == nil {
		t.Error("a three column widget was accepted")
	}
	if _, err := r.RemoveWidget(ws.ctx, a.ID); err != nil {
		t.Fatal(err)
	}

	t.Run("the last dashboard stays", func(t *testing.T) {
		if _, err := r.DeleteDashboard(ws.ctx, made.ID); err != nil {
			t.Fatal(err)
		}
		remaining, _ := r.Dashboards(ws.ctx, ws.project.Key)
		if len(remaining) != 1 {
			t.Fatalf("dashboards = %d, want the default one left", len(remaining))
		}
		if _, err := r.DeleteDashboard(ws.ctx, remaining[0].ID); !errors.Is(err, report.ErrLastDashboard) {
			t.Errorf("deleting the last dashboard gave %v", err)
		}
	})

	t.Run("another organization sees nothing", func(t *testing.T) {
		theirs := report.NewService(h.cluster, other.plans)
		if got, _ := theirs.Dashboards(other.ctx, ws.project.Key); len(got) != 0 {
			t.Errorf("another organization read %d dashboards", len(got))
		}
		if _, _, err := theirs.AddWidget(other.ctx, made.ID, report.WidgetInput{Kind: report.Epics}); !errors.Is(err, report.ErrNotFound) {
			t.Errorf("another organization added a widget: %v", err)
		}
	})
}

func TestDashboardsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "dashapi")
	if made := c.post("/api/v1/projects", map[string]any{"name": "Dashed", "key": "DSH"}); made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}

	listed := c.get("/api/v1/projects/DSH/dashboards")
	if listed.Status != http.StatusOK || len(listed.Body["dashboards"].([]any)) != 1 {
		t.Fatalf("dashboards: %d %s", listed.Status, listed.Raw)
	}
	kinds := c.get("/api/v1/projects/DSH/report-kinds")
	if kinds.Status != http.StatusOK || len(kinds.Body["kinds"].([]any)) == 0 {
		t.Errorf("kinds: %d %s", kinds.Status, kinds.Raw)
	}
	status := c.get("/api/v1/projects/DSH/reports/status_breakdown")
	if status.Status != http.StatusOK || status.Body["total"].(float64) != 0 {
		t.Errorf("status report: %d %s", status.Status, status.Raw)
	}
	if bad := c.get("/api/v1/projects/DSH/reports/pie"); bad.Status != http.StatusBadRequest {
		t.Errorf("an unknown report over the api: %d %s", bad.Status, bad.Raw)
	}
	if bad := c.get("/api/v1/projects/DSH/reports/throughput?team=not-a-uuid"); bad.Status != http.StatusBadRequest {
		t.Errorf("a bad team id over the api: %d", bad.Status)
	}
}

// A dashboard's filter is an NQL clause handed to every report that counts
// issues; the reports about sprints count sprints and are left alone.
func TestAQueryNarrowsWhatAReportCounts(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "narrowed")
	r := ws.reports(h)

	ws.typed(t, h, bootstrap.TypeBug, "a bug")
	ws.typed(t, h, bootstrap.TypeTask, "a task")
	ws.typed(t, h, bootstrap.TypeTask, "another task")

	compile := func(text string) *nql.Compiled {
		t.Helper()
		q, err := nql.Parse(text)
		if err != nil {
			t.Fatalf("parse %q: %v", text, err)
		}
		compiled, err := q.Compile(nql.Env{UserID: ws.actor.UserID, Now: time.Now()})
		if err != nil {
			t.Fatalf("compile %q: %v", text, err)
		}
		return compiled
	}
	bugs := report.Params{Narrow: compile(`type = Bug`)}

	t.Run("a breakdown counts only what the query names", func(t *testing.T) {
		all, err := r.Report(ws.ctx, ws.project.Key, report.StatusBreakdown, report.Params{})
		if err != nil {
			t.Fatal(err)
		}
		narrowed, err := r.Report(ws.ctx, ws.project.Key, report.StatusBreakdown, bugs)
		if err != nil {
			t.Fatal(err)
		}
		if all.(*report.Breakdown).Total != 3 || narrowed.(*report.Breakdown).Total != 1 {
			t.Errorf("total = %d unfiltered, %d for bugs; want 3 and 1", all.(*report.Breakdown).Total, narrowed.(*report.Breakdown).Total)
		}
	})

	t.Run("the reports with their own arguments agree", func(t *testing.T) {
		through, err := r.Report(ws.ctx, ws.project.Key, report.Throughput, bugs)
		if err != nil {
			t.Fatal(err)
		}
		if got := through.(*report.ThroughputReport).Created; got != 1 {
			t.Errorf("throughput created = %d for bugs, want 1", got)
		}
		load, err := r.Report(ws.ctx, ws.project.Key, report.Workload, bugs)
		if err != nil {
			t.Fatal(err)
		}
		var open int
		for _, row := range load.(*report.WorkloadReport).Rows {
			open += row.Open
		}
		if open != 1 {
			t.Errorf("workload open = %d for bugs, want 1", open)
		}
		if _, err := r.Report(ws.ctx, ws.project.Key, report.Epics, bugs); err != nil {
			t.Errorf("epics with a query: %v", err)
		}
		// A sprint report counts sprints; the query does not reach it and does not break it.
		if _, err := r.Report(ws.ctx, ws.project.Key, report.Velocity, bugs); err != nil {
			t.Errorf("velocity with a query: %v", err)
		}
	})

	t.Run("a query that names nothing here counts nothing", func(t *testing.T) {
		none, err := r.Report(ws.ctx, ws.project.Key, report.TypeBreakdown, report.Params{Narrow: compile(`labels = nothing-here`)})
		if err != nil {
			t.Fatal(err)
		}
		if none.(*report.Breakdown).Total != 0 {
			t.Errorf("total = %d, want 0", none.(*report.Breakdown).Total)
		}
	})

	t.Run("the filter tile itself has no report", func(t *testing.T) {
		if _, err := r.Report(ws.ctx, ws.project.Key, report.Filter, report.Params{}); !errors.Is(err, report.ErrNoReport) {
			t.Errorf("err = %v, want ErrNoReport", err)
		}
	})
}

func TestADashboardKeepsOneFilter(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "onefilter")
	r := ws.reports(h)

	boards, err := r.Dashboards(ws.ctx, ws.project.Key)
	if err != nil || len(boards) == 0 {
		t.Fatalf("dashboards: %v", err)
	}
	dash := boards[0]
	first, _, err := r.AddWidget(ws.ctx, dash.ID, report.WidgetInput{Kind: report.Filter, Config: report.Params{Filter: &report.FilterSpec{Types: []string{"Bug"}, Days: 30}}})
	if err != nil {
		t.Fatalf("add the filter: %v", err)
	}
	if first.Kind != report.Filter || first.Width != 2 {
		t.Errorf("filter widget = %+v, want the filter kind at full width", first)
	}
	if _, _, err := r.AddWidget(ws.ctx, dash.ID, report.WidgetInput{Kind: report.Filter}); !errors.Is(err, report.ErrOneFilter) {
		t.Errorf("a second filter: err = %v, want ErrOneFilter", err)
	}
	// The saved defaults come back as they went in.
	again, err := r.Dashboards(ws.ctx, ws.project.Key)
	if err != nil {
		t.Fatal(err)
	}
	var stored report.Params
	for _, w := range again[0].Widgets {
		if w.ID == first.ID {
			if err := json.Unmarshal(w.Config, &stored); err != nil {
				t.Fatal(err)
			}
		}
	}
	if stored.Filter == nil || len(stored.Filter.Types) != 1 || stored.Filter.Days != 30 {
		t.Errorf("stored filter = %+v, want the types and the window kept", stored.Filter)
	}

	t.Run("and SQL refuses a second one too", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(), `
			INSERT INTO dashboard_widget (org_id, dashboard_id, kind, title, width, position, config)
			VALUES ($1, $2, 'filter', 'Sneaky', 2, 99, '{}')`, ws.orgID, dash.ID)
		if err == nil {
			t.Fatal("SQL let a dashboard have two filters")
		}
		if !strings.Contains(err.Error(), "dashboard_widget_one_filter_idx") {
			t.Errorf("error = %v, want the index's own name", err)
		}
	})
}

func TestReportsNarrowOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "narrowapi")
	if made := c.post("/api/v1/projects", map[string]any{"name": "Narrowed", "key": "NRW"}); made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}
	types := c.get("/api/v1/issue-types")
	bug := find(t, types.Body["issueTypes"], "name", "Bug")["id"].(string)
	c.post("/api/v1/projects/NRW/issues", map[string]any{"summary": "a bug", "typeId": bug})
	c.post("/api/v1/projects/NRW/issues", map[string]any{"summary": "a task"})

	narrowed := c.get("/api/v1/projects/NRW/reports/status_breakdown?q=" + url.QueryEscape("type = Bug"))
	if narrowed.Status != http.StatusOK || narrowed.Body["total"].(float64) != 1 {
		t.Errorf("narrowed report: %d %s", narrowed.Status, narrowed.Raw)
	}
	bad := c.get("/api/v1/projects/NRW/reports/status_breakdown?q=" + url.QueryEscape("type = "))
	if bad.Status != http.StatusBadRequest {
		t.Fatalf("a query that does not parse: %d %s", bad.Status, bad.Raw)
	}
	if _, has := bad.Error()["position"]; !has {
		t.Errorf("the refusal does not say where: %s", bad.Raw)
	}
	if noReport := c.get("/api/v1/projects/NRW/reports/filter"); noReport.Status != http.StatusBadRequest {
		t.Errorf("the filter tile as a report: %d %s", noReport.Status, noReport.Raw)
	}
}

// The chart widget is one grouped query with a short list of words for each
// question; anything off the lists is refused with the list.
func TestAChartGroupsIssuesByAField(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "charted")
	if made := c.post("/api/v1/projects", map[string]any{"name": "Charted", "key": "CHT"}); made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}
	types := c.get("/api/v1/issue-types")
	bug := find(t, types.Body["issueTypes"], "name", "Bug")["id"].(string)
	first := obj(t, want(t, c.post("/api/v1/projects/CHT/issues", map[string]any{"summary": "a bug", "typeId": bug}), http.StatusCreated, "bug"), "issue")["key"].(string)
	c.post("/api/v1/projects/CHT/issues", map[string]any{"summary": "a task"})
	c.post("/api/v1/projects/CHT/issues", map[string]any{"summary": "another task"})
	want(t, c.put("/api/v1/issues/"+first+"/labels", map[string]any{"labels": []string{"backend", "urgent"}}), http.StatusOK, "tag the bug twice")

	groups := func(resp response) map[string]float64 {
		t.Helper()
		if resp.Status != http.StatusOK {
			t.Fatalf("chart: %d %s", resp.Status, resp.Raw)
		}
		out := map[string]float64{}
		for _, row := range resp.Body["groups"].([]any) {
			g := row.(map[string]any)
			out[g["label"].(string)] = g["value"].(float64)
		}
		return out
	}

	t.Run("bars by type, largest first", func(t *testing.T) {
		resp := c.get("/api/v1/projects/CHT/reports/chart?groupBy=type&shape=bar")
		byType := groups(resp)
		if byType["Bug"] != 1 || byType["Task"] != 2 || resp.Body["total"].(float64) != 3 {
			t.Errorf("by type = %v, total %v", byType, resp.Body["total"])
		}
		if firstGroup := resp.Body["groups"].([]any)[0].(map[string]any)["label"]; firstGroup != "Task" {
			t.Errorf("first group = %v, want the largest", firstGroup)
		}
	})

	t.Run("a stack's parts add up to its bar", func(t *testing.T) {
		resp := c.get("/api/v1/projects/CHT/reports/chart?groupBy=type&splitBy=statusCategory&shape=stacked")
		if resp.Status != http.StatusOK {
			t.Fatalf("stacked: %d %s", resp.Status, resp.Raw)
		}
		for _, row := range resp.Body["groups"].([]any) {
			g := row.(map[string]any)
			var sum float64
			for _, part := range g["parts"].([]any) {
				sum += part.(map[string]any)["value"].(float64)
			}
			if sum != g["value"].(float64) {
				t.Errorf("%s: parts sum to %v, bar says %v", g["label"], sum, g["value"])
			}
		}
	})

	// A chart by label is a chart of labels: an issue with two counts in both.
	t.Run("labels count an issue once per label, and the total says so", func(t *testing.T) {
		resp := c.get("/api/v1/projects/CHT/reports/chart?groupBy=label")
		byLabel := groups(resp)
		if byLabel["backend"] != 1 || byLabel["urgent"] != 1 || byLabel["No label"] != 2 || resp.Body["total"].(float64) != 4 {
			t.Errorf("by label = %v, total %v; want backend 1, urgent 1, No label 2, total 4", byLabel, resp.Body["total"])
		}
	})

	t.Run("the dashboard's query narrows a chart like anything else", func(t *testing.T) {
		resp := c.get("/api/v1/projects/CHT/reports/chart?groupBy=status&q=" + url.QueryEscape("type = Bug"))
		if resp.Body["total"].(float64) != 1 {
			t.Errorf("narrowed total = %v, want 1", resp.Body["total"])
		}
	})

	t.Run("a line over time keeps its empty weeks and sums to the issues", func(t *testing.T) {
		resp := c.get("/api/v1/projects/CHT/reports/chart?shape=line&series=created&interval=week&days=30")
		if resp.Status != http.StatusOK {
			t.Fatalf("line: %d %s", resp.Status, resp.Raw)
		}
		lines := resp.Body["series"].([]any)
		if len(lines) != 1 {
			t.Fatalf("series = %d, want one line without a group", len(lines))
		}
		points := lines[0].(map[string]any)["points"].([]any)
		if len(points) < 4 {
			t.Errorf("points = %d, want a bucket per week of the window", len(points))
		}
		var sum float64
		for _, point := range points {
			sum += point.(map[string]any)["value"].(float64)
		}
		if sum != 3 {
			t.Errorf("created over the window = %v, want 3", sum)
		}
		split := c.get("/api/v1/projects/CHT/reports/chart?shape=line&series=open&groupBy=type&interval=month&days=90")
		if split.Status != http.StatusOK || len(split.Body["series"].([]any)) != 2 {
			t.Errorf("a line per type: %d %s", split.Status, split.Raw)
		}
	})

	t.Run("a word off the list is refused with the list", func(t *testing.T) {
		resp := c.get("/api/v1/projects/CHT/reports/chart?groupBy=summary")
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("groupBy=summary: %d %s", resp.Status, resp.Raw)
		}
		if message, _ := resp.Error()["message"].(string); !strings.Contains(message, "assignee") {
			t.Errorf("message = %q, want the allowed words", message)
		}
		for _, bad := range []string{"shape=pie", "measure=hours", "shape=line&interval=day", "shape=line&series=touched", "groupBy=type&splitBy=type"} {
			if resp := c.get("/api/v1/projects/CHT/reports/chart?" + bad); resp.Status != http.StatusBadRequest {
				t.Errorf("%s: %d, want 400", bad, resp.Status)
			}
		}
	})
}

// The milestones widget says what the milestone card says, subtasks included,
// and narrows with the dashboard's query like anything else.
func TestAMilestonesWidgetCountsWhatTheCardCounts(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "mwidget")
	r := ws.reports(h)
	milestones := milestone.NewService(h.cluster)

	release, _, err := milestones.Create(ws.ctx, ws.project.Key, milestone.Input{Name: "Release 1"}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create milestone: %v", err)
	}
	later, _, err := milestones.Create(ws.ctx, ws.project.Key, milestone.Input{Name: "Release 2"}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create the second milestone: %v", err)
	}

	bug := ws.typed(t, h, bootstrap.TypeBug, "a bug for the release")
	task := ws.typed(t, h, bootstrap.TypeTask, "a task for the release")
	sub, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
		ProjectKey: ws.project.Key, Summary: "a step of the task", TypeID: h.issueTypeID(t, ws, bootstrap.TypeSubtask), ParentKey: task.Key,
	}, ws.actor)
	if err != nil {
		t.Fatalf("create the subtask: %v", err)
	}
	for _, key := range []string{bug.Key, task.Key, sub.Key} {
		if _, _, err := ws.issues.SetMilestone(ws.ctx, key, &release.ID, ws.actor); err != nil {
			t.Fatalf("assign %s: %v", key, err)
		}
	}
	ws.move(t, h, bug.Key, "Close")

	rowFor := func(t *testing.T, p report.Params, name string) report.MilestoneRow {
		t.Helper()
		out, err := r.Report(ws.ctx, ws.project.Key, report.Milestones, p)
		if err != nil {
			t.Fatalf("milestones report: %v", err)
		}
		for _, row := range out.(*report.MilestonesReport).Milestones {
			if row.Name == name {
				return row
			}
		}
		t.Fatalf("no row for %s in %+v", name, out)
		return report.MilestoneRow{}
	}

	t.Run("every open milestone, with the card's own numbers", func(t *testing.T) {
		card, err := milestones.ByID(ws.ctx, release.ID)
		if err != nil {
			t.Fatal(err)
		}
		row := rowFor(t, report.Params{}, "Release 1")
		if row.Progress != card.Progress {
			t.Errorf("widget says %+v, the card says %+v", row.Progress, card.Progress)
		}
		if row.Progress.Issues != 3 || row.Progress.Done != 1 {
			t.Errorf("progress = %+v, want 3 issues with 1 done, the subtask counted", row.Progress)
		}
		if empty := rowFor(t, report.Params{}, "Release 2"); empty.Progress.Issues != 0 {
			t.Errorf("an empty milestone counts %d", empty.Progress.Issues)
		}
	})

	t.Run("the dashboard's query narrows it", func(t *testing.T) {
		q, err := nql.Parse(`type = Bug`)
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := q.Compile(nql.Env{UserID: ws.actor.UserID, Now: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		row := rowFor(t, report.Params{Narrow: compiled}, "Release 1")
		if row.Progress.Issues != 1 || row.Progress.Done != 1 {
			t.Errorf("narrowed to bugs = %+v, want the one closed bug", row.Progress)
		}
	})

	t.Run("one milestone by id, even once it is closed", func(t *testing.T) {
		if _, _, err := milestones.Close(ws.ctx, later.ID, ws.actor.UserID); err != nil {
			t.Fatal(err)
		}
		out, err := r.Report(ws.ctx, ws.project.Key, report.Milestones, report.Params{})
		if err != nil {
			t.Fatal(err)
		}
		if got := len(out.(*report.MilestonesReport).Milestones); got != 1 {
			t.Errorf("open milestones = %d, want the closed one left out", got)
		}
		row := rowFor(t, report.Params{MilestoneID: &later.ID}, "Release 2")
		if row.ClosedAt == nil {
			t.Error("asked by id, the closed milestone comes back without its closed_at")
		}
	})
}

// A template is a copy: built in as code or saved as the organization's own
// rows, and a dashboard made from one never follows it.
func TestADashboardIsMadeFromATemplate(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "templated")
	r := ws.reports(h)

	t.Run("the built-in milestone template puts the filter first, and only one", func(t *testing.T) {
		made, _, err := r.CreateDashboardFrom(ws.ctx, ws.project.Key, report.CreateInput{Name: "Release readiness", Template: "milestone"}, ws.actor.UserID)
		if err != nil {
			t.Fatalf("create from template: %v", err)
		}
		kinds := kindsOf(made.Widgets)
		if len(kinds) < 5 || kinds[0] != report.Filter {
			t.Errorf("kinds = %v, want the filter first and the rest after", kinds)
		}
		filters := 0
		for _, k := range kinds {
			if k == report.Filter {
				filters++
			}
		}
		if filters != 1 {
			t.Errorf("filters = %d, want one", filters)
		}
	})

	t.Run("a word nobody has is refused", func(t *testing.T) {
		if _, _, err := r.CreateDashboardFrom(ws.ctx, ws.project.Key, report.CreateInput{Name: "Nowhere", Template: "nope"}, ws.actor.UserID); !errors.Is(err, report.ErrNoTemplate) {
			t.Errorf("err = %v, want ErrNoTemplate", err)
		}
	})

	var saved *report.Template
	t.Run("saving keeps the arrangement and drops what belongs to this project", func(t *testing.T) {
		boards, err := r.Dashboards(ws.ctx, ws.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		overview := boards[0]
		team := uuid.New()
		if _, _, err := r.AddWidget(ws.ctx, overview.ID, report.WidgetInput{Kind: report.TeamWorkload, Config: report.Params{TeamID: &team, Days: 90}}); err != nil {
			t.Fatal(err)
		}
		saved, _, err = r.SaveTemplate(ws.ctx, overview.ID, "Our overview", "What we look at on Mondays.", ws.actor.UserID)
		if err != nil {
			t.Fatalf("save template: %v", err)
		}
		if saved.ID == nil || saved.BuiltIn || saved.Key != saved.ID.String() {
			t.Errorf("saved = %+v, want a row keyed by its id", saved)
		}
		var kept *report.TemplateWidget
		for i := range saved.Widgets {
			if saved.Widgets[i].Kind == report.TeamWorkload {
				kept = &saved.Widgets[i]
			}
		}
		if kept == nil || kept.Config.TeamID != nil || kept.Config.Days != 90 {
			t.Errorf("team workload widget = %+v, want the window kept and the team id dropped", kept)
		}
		if _, _, err := r.SaveTemplate(ws.ctx, overview.ID, "our OVERVIEW", "", ws.actor.UserID); !errors.Is(err, report.ErrTemplateNameTaken) {
			t.Errorf("the same name again: err = %v, want ErrTemplateNameTaken", err)
		}
		if _, _, err := r.SaveTemplate(ws.ctx, overview.ID, "  ", "", ws.actor.UserID); !errors.Is(err, report.ErrTemplateName) {
			t.Errorf("a blank name: err = %v, want ErrTemplateName", err)
		}
	})

	t.Run("the chooser lists the built-ins that suit the project, then the saved ones", func(t *testing.T) {
		templates, err := r.Templates(ws.ctx, project.KindSoftware)
		if err != nil {
			t.Fatal(err)
		}
		var keys []string
		for _, tpl := range templates {
			keys = append(keys, tpl.Key)
		}
		if !slices.Contains(keys, "overview-software") || !slices.Contains(keys, "milestone") || slices.Contains(keys, "overview-service") {
			t.Errorf("keys = %v, want the software built-ins and not the service one", keys)
		}
		if keys[len(keys)-1] != saved.Key {
			t.Errorf("keys = %v, want the saved template last", keys)
		}
	})

	t.Run("a saved software template on a business project leaves its sprint widgets out", func(t *testing.T) {
		business, _, err := ws.projects.Create(ws.ctx, project.CreateInput{Name: "Ledger", Key: "LDG", Kind: project.KindBusiness}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		made, _, err := r.CreateDashboardFrom(ws.ctx, business.Key, report.CreateInput{Name: "Mondays", Template: saved.Key}, ws.actor.UserID)
		if err != nil {
			t.Fatalf("apply the saved template: %v", err)
		}
		kinds := kindsOf(made.Widgets)
		if slices.Contains(kinds, report.Sprint) || slices.Contains(kinds, report.Burndown) || !slices.Contains(kinds, report.TeamWorkload) {
			t.Errorf("kinds = %v, want the sprint widgets left out and the rest kept", kinds)
		}
	})

	t.Run("another organization neither sees nor names it", func(t *testing.T) {
		theirs := h.newWorkspace(t, "theirtemplates")
		templates, err := theirs.reports(h).Templates(theirs.ctx, project.KindSoftware)
		if err != nil {
			t.Fatal(err)
		}
		for _, tpl := range templates {
			if !tpl.BuiltIn {
				t.Errorf("another organization lists %q", tpl.Name)
			}
		}
		if _, _, err := theirs.reports(h).CreateDashboardFrom(theirs.ctx, theirs.project.Key, report.CreateInput{Name: "Borrowed", Template: saved.Key}, theirs.actor.UserID); !errors.Is(err, report.ErrNoTemplate) {
			t.Errorf("err = %v, want the saved template to be nobody's there", err)
		}
	})

	t.Run("SQL refuses the same name twice in one organization", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(), `
			INSERT INTO dashboard_template (org_id, name) VALUES ($1, 'OUR overview')`, ws.orgID)
		if err == nil {
			t.Fatal("SQL let a second template take the name")
		}
		if !strings.Contains(err.Error(), "dashboard_template_org_name_idx") {
			t.Errorf("error = %v, want the index's own name", err)
		}
	})

	t.Run("deleting a template leaves what was made from it", func(t *testing.T) {
		if _, err := r.DeleteTemplate(ws.ctx, *saved.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := r.DeleteTemplate(ws.ctx, *saved.ID); !errors.Is(err, report.ErrNotFound) {
			t.Errorf("deleting twice: err = %v, want ErrNotFound", err)
		}
		boards, err := r.Dashboards(ws.ctx, "LDG")
		if err != nil {
			t.Fatal(err)
		}
		if len(boards) != 2 {
			t.Errorf("the business project has %d dashboards, want its overview and the one made from the template", len(boards))
		}
	})
}

// A milestone's dashboard is a copy pinned to the milestone: the filter says
// its name and the widget carries its id, so the two cannot disagree.
func TestAMilestoneGetsADashboard(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "mdash")
	r := ws.reports(h)
	milestones := milestone.NewService(h.cluster)
	release, _, err := milestones.Create(ws.ctx, ws.project.Key, milestone.Input{Name: "Release 1"}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}

	made, _, err := r.CreateDashboardFrom(ws.ctx, ws.project.Key, report.CreateInput{Name: "Release 1", Template: "milestone", MilestoneID: &release.ID}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create the milestone dashboard: %v", err)
	}
	var pinnedFilter, pinnedWidget bool
	for _, w := range made.Widgets {
		p := report.ParamsFrom(w.Config)
		switch w.Kind {
		case report.Filter:
			pinnedFilter = p.Filter != nil && p.Filter.Milestone == "Release 1"
		case report.Milestones:
			pinnedWidget = p.MilestoneID != nil && *p.MilestoneID == release.ID
		}
	}
	if !pinnedFilter || !pinnedWidget {
		t.Errorf("filter pinned = %v, widget pinned = %v; want both", pinnedFilter, pinnedWidget)
	}

	t.Run("a milestone of another project is not found", func(t *testing.T) {
		other, _, err := ws.projects.Create(ws.ctx, project.CreateInput{Name: "Other", Key: "OTM"}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := r.CreateDashboardFrom(ws.ctx, other.Key, report.CreateInput{Name: "Borrowed", Template: "milestone", MilestoneID: &release.ID}, ws.actor.UserID); !errors.Is(err, milestone.ErrNotFound) {
			t.Errorf("err = %v, want the milestone not found from the other project", err)
		}
	})

	t.Run("closing the milestone leaves the dashboard as it is", func(t *testing.T) {
		if _, _, err := milestones.Close(ws.ctx, release.ID, ws.actor.UserID); err != nil {
			t.Fatal(err)
		}
		boards, err := r.Dashboards(ws.ctx, ws.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.ContainsFunc(boards, func(d report.Dashboard) bool { return d.ID == made.ID }) {
			t.Error("the dashboard went with the milestone's closing")
		}
	})
}
