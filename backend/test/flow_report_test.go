//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/version"
)

// The flow is snapshotted, not rebuilt: today's counts are written once and
// replaced on a second write, and the days nobody wrote are rebuilt once from
// the changelog and say so.
func TestFlowIsSnapshottedAndRebuiltOnce(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "flows")
	reports := ws.reports(h)
	a := ws.newIssue(t, "One")
	ws.newIssue(t, "Two")
	ws.move(t, h, a.Key, "Start progress")
	// A change made yesterday, so the rebuilt day before it reads differently from today.
	if _, err := h.super.Exec(context.Background(), `UPDATE issue SET created_at = now() - interval '3 days' WHERE project_id = $1`, ws.project.ID); err != nil {
		t.Fatal(err)
	}

	got, err := reports.Report(ws.ctx, ws.project.Key, report.CumulativeFlow, report.Params{Days: 7})
	if err != nil {
		t.Fatal(err)
	}
	flow := got.(*report.CumulativeFlowReport)
	if !flow.Reconstructed || len(flow.Days) != 8 {
		t.Fatalf("flow = reconstructed %v, %d days, want rebuilt days for the whole window", flow.Reconstructed, len(flow.Days))
	}
	total := func(d report.FlowDay) int {
		sum := 0
		for _, c := range d.Counts {
			sum += c
		}
		return sum
	}
	today := flow.Days[len(flow.Days)-1]
	if total(today) != 2 {
		t.Errorf("today's counts add up to %d, want the project's 2 issues", total(today))
	}
	if total(flow.Days[0]) != 0 {
		t.Errorf("a day before the issues existed counts %d", total(flow.Days[0]))
	}

	// The worker writes today's row and replaces it on a second pass; the
	// rebuilt mark goes with it.
	day := time.Now().UTC().Truncate(24 * time.Hour)
	if err := reports.WriteFlowSnapshot(ws.ctx, ws.project.ID, day); err != nil {
		t.Fatal(err)
	}
	ws.newIssue(t, "Three")
	if err := reports.WriteFlowSnapshot(ws.ctx, ws.project.ID, day); err != nil {
		t.Fatal(err)
	}
	var rows, reconstructedToday int
	if err := h.cluster.Read(db.PinPrimary(ws.ctx), func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT (SELECT COALESCE(sum(count), 0) FROM project_flow_snapshot WHERE project_id = $1 AND day = current_date),
			       (SELECT count(*) FROM project_flow_snapshot WHERE project_id = $1 AND day = current_date AND reconstructed)`, ws.project.ID).Scan(&rows, &reconstructedToday)
	}); err != nil {
		t.Fatal(err)
	}
	if rows != 3 || reconstructedToday != 0 {
		t.Errorf("today's snapshot counts %d issues (%d rows rebuilt), want 3 written by the worker", rows, reconstructedToday)
	}

	// Another organization's rows are not this project's to see.
	other := h.newWorkspace(t, "otherflow")
	var theirs int
	if err := h.cluster.Read(db.PinPrimary(other.ctx), func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM project_flow_snapshot WHERE project_id = $1`, ws.project.ID).Scan(&theirs)
	}); err != nil {
		t.Fatal(err)
	}
	if theirs != 0 {
		t.Errorf("another organization sees %d snapshot rows", theirs)
	}
}

// The control chart excludes the unresolved, the histogram sorts what took how
// long, the age counts the open, and the release burndown drops as work is done.
func TestTheOtherFlowReports(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "flowreports")
	reports := ws.reports(h)
	done := ws.newIssue(t, "Done fast")
	ws.newIssue(t, "Still open")
	ws.move(t, h, done.Key, "Start progress")
	ws.move(t, h, done.Key, "Ready for review")
	ws.move(t, h, done.Key, "Approve")

	control, err := reports.Report(ws.ctx, ws.project.Key, report.ControlChart, report.Params{})
	if err != nil {
		t.Fatal(err)
	}
	cc := control.(*report.ControlChartReport)
	if len(cc.Points) != 1 || cc.Points[0].Key != done.Key || cc.Points[0].Rolling != cc.Points[0].Days {
		t.Errorf("control chart = %+v, want the one resolved issue", cc)
	}

	hist, err := reports.Report(ws.ctx, ws.project.Key, report.ResolutionHistogram, report.Params{})
	if err != nil {
		t.Fatal(err)
	}
	rh := hist.(*report.ResolutionReport)
	if rh.Resolved != 1 || rh.Bands[0].Count != 1 || rh.Bands[0].Label != "under a day" {
		t.Errorf("histogram = %+v, want one under a day", rh)
	}

	cvr, err := reports.Report(ws.ctx, ws.project.Key, report.CreatedResolved, report.Params{Days: 7})
	if err != nil {
		t.Fatal(err)
	}
	cr := cvr.(*report.CreatedResolvedReport)
	last := cr.Days[len(cr.Days)-1]
	if last.CreatedTotal != 2 || last.ResolvedTotal != 1 {
		t.Errorf("created vs resolved ends at %+v, want 2 created and 1 resolved", last)
	}

	age, err := reports.Report(ws.ctx, ws.project.Key, report.AverageAge, report.Params{})
	if err != nil {
		t.Fatal(err)
	}
	ar := age.(*report.AverageAgeReport)
	if ar.Open != 1 || ar.OldestKey == done.Key {
		t.Errorf("average age = %+v, want the one open issue", ar)
	}

	versions := version.NewService(h.cluster)
	v, _, err := versions.Create(ws.ctx, ws.project.Key, version.Input{Name: ptr("1.0")}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{done.Key} {
		if _, _, err := ws.issues.SetVersions(ws.ctx, key, []uuid.UUID{v.ID}, nil, ws.actor); err != nil {
			t.Fatal(err)
		}
	}
	pending := ws.newIssue(t, "Pending fix")
	if _, _, err := ws.issues.SetVersions(ws.ctx, pending.Key, []uuid.UUID{v.ID}, nil, ws.actor); err != nil {
		t.Fatal(err)
	}
	burn, err := reports.Report(ws.ctx, ws.project.Key, report.ReleaseBurndown, report.Params{VersionID: &v.ID})
	if err != nil {
		t.Fatal(err)
	}
	rb := burn.(*report.ReleaseBurndownReport)
	if rb.Total != 2 || len(rb.Days) < 1 || rb.Days[len(rb.Days)-1].Remaining != 1 || rb.VersionName != "1.0" {
		t.Errorf("release burndown = %+v, want 2 in all and 1 left today", rb)
	}
	if _, err := reports.Report(ws.ctx, ws.project.Key, report.ReleaseBurndown, report.Params{}); err == nil {
		t.Error("a release burndown without a version was answered")
	}
}

// Every flow report over the API, both ways.
func TestFlowReportsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "flowapi")
	project := want(t, c.post("/api/v1/projects", map[string]any{"name": "Flowing", "key": "FLW" + strings.ToUpper(uuid.New().String()[:3]), "template": "kanban"}), http.StatusCreated, "project")
	key := obj(t, project, "project")["key"].(string)
	want(t, c.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "Flowing work"}), http.StatusCreated, "issue")
	for _, kind := range []string{"cumulative_flow", "control_chart", "created_vs_resolved", "avg_age", "resolution_histogram"} {
		want(t, c.get("/api/v1/projects/"+key+"/reports/"+kind), http.StatusOK, kind)
	}
	want(t, c.get("/api/v1/projects/"+key+"/reports/release_burndown"), http.StatusBadRequest, "a burndown with no version")
	v := want(t, c.post("/api/v1/projects/"+key+"/versions", map[string]any{"name": "1.0"}), http.StatusCreated, "version")
	want(t, c.get("/api/v1/projects/"+key+"/reports/release_burndown?version="+idOf(t, v, "version")), http.StatusOK, "release burndown")
}
