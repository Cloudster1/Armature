package plan

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/team"
)

func dayOf(iso string) time.Time {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		panic(err)
	}
	return t
}

// teamed builds a scheduled, sized row on a team.
func teamed(key string, teamID *uuid.UUID, estimate *float64, start, due string, children ...issue.Node) issue.Node {
	n := sized(key, issue.LevelStandard, estimate, nil, children...)
	n.Issue.TeamID = teamID
	if start != "" {
		s, d := dayOf(start), dayOf(due)
		n.Issue.StartDate, n.Issue.DueDate = &s, &d
	}
	return n
}

func teamNamed(id uuid.UUID, name string, capacity *float64) team.Team {
	return team.Team{ID: id, Name: name, WeeklyCapacity: capacity}
}

func rowNamed(load Load, name string) TeamLoad {
	for _, r := range load.Rows {
		if r.Team == name {
			return r
		}
	}
	return TeamLoad{}
}

func TestWeeksStartOnMondayAndCoverTheWindow(t *testing.T) {
	// 2026-09-09 is a Wednesday; 2026-09-22 is a Tuesday.
	weeks := Weeks(dayOf("2026-09-09"), dayOf("2026-09-22"))
	if len(weeks) != 3 || weeks[0] != dayOf("2026-09-07") || weeks[2] != dayOf("2026-09-21") {
		t.Errorf("weeks = %v, want the three Mondays from the 7th", weeks)
	}
	if weeks[0].Weekday() != time.Monday {
		t.Error("a week does not start on Monday")
	}
}

func TestLoadSpreadsAnEstimateEvenlyAcrossItsDays(t *testing.T) {
	alpha := uuid.New()
	// Thursday the 10th to Monday the 14th: five days, four in week one.
	items := FromTree([]issue.Node{teamed("PR-1", &alpha, pts(10), "2026-09-10", "2026-09-14")})
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", pts(10))}, dayOf("2026-09-07"), dayOf("2026-09-20"))

	row := rowNamed(load, "Alpha")
	if len(row.Weeks) != 2 || row.Weeks[0].Load != 8 || row.Weeks[1].Load != 2 {
		t.Errorf("weeks = %+v, want 8 and 2 points", row.Weeks)
	}
	if row.Weeks[0].Issues != 1 || row.Weeks[1].Issues != 1 {
		t.Errorf("the issue touches both weeks: %+v", row.Weeks)
	}
}

func TestLoadCountsAParentOnceForItsTeam(t *testing.T) {
	alpha, beta := uuid.New(), uuid.New()
	story := teamed("PR-1", &alpha, pts(5), "2026-09-07", "2026-09-11",
		teamed("PR-2", &alpha, pts(2), "2026-09-07", "2026-09-08"),
		teamed("PR-3", &alpha, pts(3), "2026-09-09", "2026-09-11"),
		teamed("PR-4", &beta, pts(4), "2026-09-07", "2026-09-10"),
	)
	items := FromTree([]issue.Node{story})
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", nil), teamNamed(beta, "Beta", nil)}, dayOf("2026-09-07"), dayOf("2026-09-13"))

	if got := rowNamed(load, "Alpha").Weeks[0].Load; got != 5 {
		t.Errorf("Alpha = %v, want the story's 5 once, not 5 plus its subtasks", got)
	}
	if got := rowNamed(load, "Beta").Weeks[0].Load; got != 4 {
		t.Errorf("Beta = %v, want the subtask handed to it", got)
	}
	if got := rowNamed(load, "Total").Weeks[0].Load; got != 9 {
		t.Errorf("Total = %v, want 9", got)
	}
}

func TestTeamlessWorkIsUnassigned(t *testing.T) {
	items := FromTree([]issue.Node{teamed("PR-1", nil, pts(3), "2026-09-07", "2026-09-09")})
	load := Loads(items, nil, dayOf("2026-09-07"), dayOf("2026-09-13"))
	if len(load.Rows) != 2 || load.Rows[0].Kind != LoadUnassigned || load.Rows[0].Weeks[0].Load != 3 {
		t.Errorf("rows = %+v, want Unassigned then Total", load.Rows)
	}
	if load.Rows[1].Kind != LoadTotal || load.Rows[1].WeeklyCapacity != nil {
		t.Errorf("total = %+v, want no capacity when no team said one", load.Rows[1])
	}
}

func TestTotalSumsEveryRowAndItsCapacities(t *testing.T) {
	alpha, beta := uuid.New(), uuid.New()
	items := FromTree([]issue.Node{
		teamed("PR-1", &alpha, pts(8), "2026-09-07", "2026-09-11"),
		teamed("PR-2", &beta, pts(6), "2026-09-07", "2026-09-11"),
		teamed("PR-3", nil, pts(3), "2026-09-07", "2026-09-11"),
	})
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", pts(10)), teamNamed(beta, "Beta", nil)}, dayOf("2026-09-07"), dayOf("2026-09-13"))
	total := rowNamed(load, "Total")
	if total.Weeks[0].Load != 17 || total.WeeklyCapacity == nil || *total.WeeklyCapacity != 10 {
		t.Errorf("total = %+v, want 17 points against the 10 the teams said", total)
	}
}

func TestOverLoadWarnsWithTheTeamAsSubject(t *testing.T) {
	alpha := uuid.New()
	items := FromTree([]issue.Node{
		teamed("PR-1", &alpha, pts(8), "2026-09-07", "2026-09-11"),
		teamed("PR-2", &alpha, pts(6), "2026-09-07", "2026-09-11"),
	})
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", pts(10))}, dayOf("2026-09-07"), dayOf("2026-09-13"))
	warnings := CheckLoad(load)
	if len(warnings) != 1 || warnings[0].Team != "Alpha" || warnings[0].Kind != WarnOverLoad || warnings[0].IssueKey != "" {
		t.Fatalf("warnings = %+v, want one about Alpha", warnings)
	}
	if !strings.Contains(warnings[0].Message, "14 points") || !strings.Contains(warnings[0].Message, "10 points") || !strings.Contains(warnings[0].Message, "7 Sep") {
		t.Errorf("message = %q", warnings[0].Message)
	}
}

func TestUnestimatedWorkIsCountedNotGuessed(t *testing.T) {
	alpha := uuid.New()
	items := FromTree([]issue.Node{teamed("PR-1", &alpha, nil, "2026-09-07", "2026-09-16")})
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", pts(1))}, dayOf("2026-09-07"), dayOf("2026-09-27"))
	row := rowNamed(load, "Alpha")
	if row.Weeks[0].Load != 0 || row.Weeks[0].Unestimated != 1 || row.Weeks[1].Unestimated != 1 || row.Weeks[2].Unestimated != 0 {
		t.Errorf("weeks = %+v, want no load and the unsized issue counted in the two weeks it touches", row.Weeks)
	}
	if len(CheckLoad(load)) != 0 {
		t.Error("unsized work read as over capacity")
	}
}

func TestUnscheduledWorkIsCountedNotSpread(t *testing.T) {
	alpha := uuid.New()
	items := FromTree([]issue.Node{teamed("PR-1", &alpha, pts(5), "", "")})
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", pts(1))}, dayOf("2026-09-07"), dayOf("2026-09-13"))
	row := rowNamed(load, "Alpha")
	if row.Unscheduled != 1 || row.Weeks[0].Load != 0 || rowNamed(load, "Total").Unscheduled != 1 {
		t.Errorf("row = %+v, want the work counted as unscheduled and in no week", row)
	}
}

func TestNoCapacityMeansNoWarning(t *testing.T) {
	alpha := uuid.New()
	items := FromTree([]issue.Node{teamed("PR-1", &alpha, pts(80), "2026-09-07", "2026-09-11")})
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", nil)}, dayOf("2026-09-07"), dayOf("2026-09-13"))
	if len(CheckLoad(load)) != 0 {
		t.Error("a team that said nothing about its capacity was warned about it")
	}
	if rowNamed(load, "Alpha").Weeks[0].OverBy() != 0 {
		t.Error("OverBy is not zero without a capacity")
	}
}

func TestWorkOutsideTheWindowFallsInNoWeek(t *testing.T) {
	alpha := uuid.New()
	items := FromTree([]issue.Node{teamed("PR-1", &alpha, pts(7), "2026-08-24", "2026-08-30")})
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", nil)}, dayOf("2026-09-07"), dayOf("2026-09-13"))
	if rowNamed(load, "Alpha").Weeks[0].Load != 0 {
		t.Error("work from before the window landed in it")
	}
}
