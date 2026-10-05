package project

import (
	"errors"
	"testing"
)

func TestScrumPlansByTeamAndKanbanChooses(t *testing.T) {
	text := func(s string) *string { return &s }

	m, g, err := nextPlanning(MethodKanban, GroupByTeam, nil, text("person"))
	if err != nil || m != MethodKanban || g != GroupByPerson {
		t.Fatalf("kanban by person = %s %s %v", m, g, err)
	}
	// Turning to scrum takes the grouping back to teams rather than refusing.
	m, g, err = nextPlanning(MethodKanban, GroupByPerson, text("scrum"), nil)
	if err != nil || m != MethodScrum || g != GroupByTeam {
		t.Fatalf("to scrum = %s %s %v", m, g, err)
	}
	if _, _, err := nextPlanning(MethodScrum, GroupByTeam, nil, text("person")); !errors.Is(err, ErrScrumByTeam) {
		t.Fatalf("scrum by person: %v", err)
	}
	if _, _, err := nextPlanning(MethodKanban, GroupByTeam, text("scrum"), text("person")); !errors.Is(err, ErrScrumByTeam) {
		t.Fatalf("scrum by person in one request: %v", err)
	}
	m, g, err = nextPlanning(MethodScrum, GroupByTeam, text("kanban"), text("person"))
	if err != nil || m != MethodKanban || g != GroupByPerson {
		t.Fatalf("to kanban by person = %s %s %v", m, g, err)
	}
	for _, bad := range [][2]*string{{text("waterfall"), nil}, {nil, text("everyone")}} {
		if _, _, err := nextPlanning(MethodKanban, GroupByTeam, bad[0], bad[1]); !errors.Is(err, ErrBadPlanning) {
			t.Errorf("%v: %v", bad, err)
		}
	}
}
