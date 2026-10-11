//go:build integration

package test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/version"
)

func widgetOfKind(t *testing.T, widgets []report.Widget, kind report.Kind) report.Widget {
	t.Helper()
	for _, w := range widgets {
		if w.Kind == kind {
			return w
		}
	}
	t.Fatalf("kinds = %v, want a %s widget", kindsOf(widgets), kind)
	return report.Widget{}
}

func versionNames(t *testing.T, out any) []string {
	t.Helper()
	var names []string
	for _, v := range out.(*report.VersionsReport).Versions {
		names = append(names, v.Name)
	}
	return names
}

// A template carries no id of the project it was saved in, so a widget
// pinned to a version there shows the next project's own versions.
func TestATemplateLeavesItsProjectsVersionBehind(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "tplversion")
	r := ws.reports(h)
	versions := version.NewService(h.cluster)

	pinned, _, err := versions.Create(ws.ctx, ws.project.Key, version.Input{Name: ptr("1.2")}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := ws.projects.Create(ws.ctx, project.CreateInput{Name: "Second", Key: "SEC"}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"2.0", "2.1", "1.9"} {
		v, _, err := versions.Create(ws.ctx, other.Key, version.Input{Name: ptr(name)}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if name == "1.9" {
			if _, _, err := versions.Release(ws.ctx, v.ID, ws.actor.UserID); err != nil {
				t.Fatal(err)
			}
		}
	}
	unreleased := []string{"2.0", "2.1"}

	// releasesIn reads the releases widget of a dashboard made from a template
	// in the second project, as the page asks for it.
	releasesIn := func(t *testing.T, made *report.Dashboard) []string {
		t.Helper()
		w := widgetOfKind(t, made.Widgets, report.Versions)
		out, err := r.Report(ws.ctx, other.Key, report.Versions, report.ParamsFrom(w.Config))
		if err != nil {
			t.Fatalf("releases widget: %v", err)
		}
		return versionNames(t, out)
	}

	t.Run("saving drops the version along with the team, sprint and milestone", func(t *testing.T) {
		board, _, err := r.CreateDashboardFrom(ws.ctx, ws.project.Key, report.CreateInput{Name: "Release 1.2"}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		team, sprint, milestone := uuid.New(), uuid.New(), uuid.New()
		for _, in := range []report.WidgetInput{
			{Kind: report.Versions, Config: report.Params{VersionID: &pinned.ID}},
			{Kind: report.ReleaseBurndown, Config: report.Params{VersionID: &pinned.ID}},
			{Kind: report.TeamWorkload, Config: report.Params{TeamID: &team, Days: 90}},
			{Kind: report.Burndown, Config: report.Params{SprintID: &sprint}},
			{Kind: report.Milestones, Config: report.Params{MilestoneID: &milestone}},
		} {
			if _, _, err := r.AddWidget(ws.ctx, board.ID, in); err != nil {
				t.Fatal(err)
			}
		}
		saved, _, err := r.SaveTemplate(ws.ctx, board.ID, "Release readiness", "", ws.actor.UserID)
		if err != nil {
			t.Fatalf("save template: %v", err)
		}
		for _, w := range saved.Widgets {
			c := w.Config
			if c.VersionID != nil || c.TeamID != nil || c.SprintID != nil || c.MilestoneID != nil {
				t.Errorf("%s widget config = %+v, want no id of the first project", w.Kind, c)
			}
		}

		made, _, err := r.CreateDashboardFrom(ws.ctx, other.Key, report.CreateInput{Name: "Release readiness", Template: saved.Key}, ws.actor.UserID)
		if err != nil {
			t.Fatalf("apply the template in the second project: %v", err)
		}
		if got := releasesIn(t, made); !slices.Equal(got, unreleased) {
			t.Errorf("releases widget lists %v, want every unreleased version of the second project %v", got, unreleased)
		}
		burn := report.ParamsFrom(widgetOfKind(t, made.Widgets, report.ReleaseBurndown).Config)
		if burn.VersionID != nil {
			t.Errorf("release burndown is pinned to %v, want it waiting for a version to be picked", *burn.VersionID)
		}
	})

	t.Run("a template saved with a version before reads as if it had none", func(t *testing.T) {
		stored, err := json.Marshal([]map[string]any{
			{"kind": report.Versions, "config": map[string]any{"versionId": pinned.ID}},
			{"kind": report.ReleaseBurndown, "config": map[string]any{"versionId": pinned.ID, "days": 14}},
		})
		if err != nil {
			t.Fatal(err)
		}
		var id uuid.UUID
		if err := h.super.QueryRow(context.Background(), `
			INSERT INTO dashboard_template (org_id, name, widgets) VALUES ($1, 'Pinned long ago', $2) RETURNING id`,
			ws.orgID, stored).Scan(&id); err != nil {
			t.Fatal(err)
		}

		templates, err := r.Templates(ws.ctx, project.KindSoftware)
		if err != nil {
			t.Fatal(err)
		}
		listed := false
		for _, tpl := range templates {
			if tpl.Key != id.String() {
				continue
			}
			listed = true
			for _, w := range tpl.Widgets {
				if w.Config.VersionID != nil {
					t.Errorf("the chooser shows the %s widget pinned to %v, want no version", w.Kind, *w.Config.VersionID)
				}
			}
		}
		if !listed {
			t.Fatal("the chooser does not list the template saved before")
		}

		made, _, err := r.CreateDashboardFrom(ws.ctx, other.Key, report.CreateInput{Name: "From long ago", Template: id.String()}, ws.actor.UserID)
		if err != nil {
			t.Fatalf("apply the old template: %v", err)
		}
		if got := releasesIn(t, made); !slices.Equal(got, unreleased) {
			t.Errorf("releases widget lists %v, want every unreleased version of the second project %v", got, unreleased)
		}
		burn := report.ParamsFrom(widgetOfKind(t, made.Widgets, report.ReleaseBurndown).Config)
		if burn.VersionID != nil || burn.Days != 14 {
			t.Errorf("release burndown config = %+v, want the window kept and no version", burn)
		}
	})
}
