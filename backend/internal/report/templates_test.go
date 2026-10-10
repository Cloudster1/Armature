package report

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// pinnedConfig is a widget's stored configuration pinned to one project's
// team, sprint, milestone and version, with a filter that names things.
func pinnedConfig(t *testing.T) json.RawMessage {
	t.Helper()
	team, sprint, milestone, version := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	raw, err := json.Marshal(Params{
		Days: 90, TeamID: &team, SprintID: &sprint, MilestoneID: &milestone, VersionID: &version,
		GroupBy: "status", Filter: &FilterSpec{Team: "Backend", Milestone: "Release 1", Query: "priority = High"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func assertNothingPinned(t *testing.T, p Params) {
	t.Helper()
	if p.TeamID != nil || p.SprintID != nil || p.MilestoneID != nil || p.VersionID != nil {
		t.Errorf("params = %+v, want no team, sprint, milestone or version id", p)
	}
	if p.Days != 90 || p.GroupBy != "status" {
		t.Errorf("params = %+v, want the window and the grouping kept", p)
	}
	if p.Filter == nil || p.Filter.Team != "Backend" || p.Filter.Milestone != "Release 1" || p.Filter.Query != "priority = High" {
		t.Errorf("filter = %+v, want the names in the filter to travel", p.Filter)
	}
}

func TestATemplatedWidgetKeepsNoIDOfItsProject(t *testing.T) {
	assertNothingPinned(t, ParamsFrom(pinnedConfig(t)).templated())
}

func TestATemplateSavedWithAVersionReadsAsUnpinned(t *testing.T) {
	stored, err := json.Marshal([]map[string]any{{"kind": string(Versions), "config": pinnedConfig(t)}})
	if err != nil {
		t.Fatal(err)
	}
	widgets, err := templateWidgets(stored)
	if err != nil {
		t.Fatal(err)
	}
	if len(widgets) != 1 || widgets[0].Kind != Versions {
		t.Fatalf("widgets = %+v, want the one releases widget", widgets)
	}
	assertNothingPinned(t, widgets[0].Config)
}
