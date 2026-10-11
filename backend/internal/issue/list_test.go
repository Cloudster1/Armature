package issue

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The sprints page reads each sprint and the backlog by these filters, so a
// sprint's work is found however many newer issues the project has.
func TestFilterNarrowsBySprintAndTeam(t *testing.T) {
	sprintID, teamID := uuid.New(), uuid.New()

	tests := []struct {
		name   string
		filter Filter
		clause string
		args   []any
	}{
		{"one sprint", Filter{SprintID: &sprintID}, "i.sprint_id = $1", []any{sprintID}},
		{"no sprint", Filter{NoSprint: true}, "i.sprint_id IS NULL", nil},
		{"one team", Filter{TeamID: &teamID}, "i.team_id = $1", []any{teamID}},
		{"no team", Filter{NoTeam: true}, "i.team_id IS NULL", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where, args := tt.filter.build()
			if !strings.Contains(where, tt.clause) {
				t.Errorf("where = %q, want it to hold %q", where, tt.clause)
			}
			if len(args) != len(tt.args) || (len(args) > 0 && args[0] != tt.args[0]) {
				t.Errorf("args = %v, want %v", args, tt.args)
			}
		})
	}

	t.Run("no sprint wins over one, so a backlog never reads as a sprint", func(t *testing.T) {
		where, _ := Filter{NoSprint: true, SprintID: &sprintID}.build()
		if strings.Contains(where, "i.sprint_id = ") {
			t.Errorf("where = %q, want only the backlog's clause", where)
		}
	})
}

// A backlog is read in the order the team ranked it, not by age.
func TestTheListOrdersByRank(t *testing.T) {
	if got := orderClause(Filter{}, Page{OrderBy: "rank"}); got != "i.rank ASC" {
		t.Errorf("order = %q, want the rank, lowest first", got)
	}
	if got := orderClause(Filter{}, Page{OrderBy: "rank", Desc: true}); got != "i.rank DESC" {
		t.Errorf("order = %q, want the rank turned round when asked", got)
	}
}
