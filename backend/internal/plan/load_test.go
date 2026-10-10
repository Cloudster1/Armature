package plan

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/availability"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/sprint"
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

func TestTheLoadIsReadOverABoundedStretchOfTheWindow(t *testing.T) {
	today := dayOf("2026-10-08")
	from, to := LoadSpan(dayOf("1990-01-01"), dayOf("2199-12-31"), today)
	if from != today.AddDate(0, 0, -LoadBackDays) || to != from.AddDate(0, 0, LoadSpanDays) {
		t.Errorf("span = %v to %v, want two years back and ten years long", from, to)
	}
	if weeks := len(Weeks(from, to)); weeks > LoadSpanDays/7+2 {
		t.Errorf("%d weeks, want about ten years of them", weeks)
	}
	// Work planned a few years ahead keeps its whole window.
	if from, to := LoadSpan(dayOf("2026-09-01"), dayOf("2031-03-31"), today); from != dayOf("2026-09-01") || to != dayOf("2031-03-31") {
		t.Errorf("span = %v to %v, want the window unchanged", from, to)
	}
}

func TestLoadSpreadsAnEstimateEvenlyAcrossItsWorkingDays(t *testing.T) {
	alpha := uuid.New()
	// Thursday the 10th to Tuesday the 15th: four working days, two in week one.
	items := FromTree([]issue.Node{teamed("PR-1", &alpha, pts(10), "2026-09-10", "2026-09-15")})
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", pts(10))}, dayOf("2026-09-07"), dayOf("2026-09-20"), Workdays{})

	row := rowNamed(load, "Alpha")
	if len(row.Weeks) != 2 || row.Weeks[0].Load != 5 || row.Weeks[1].Load != 5 {
		t.Errorf("weeks = %+v, want 5 and 5 points, the weekend taking none", row.Weeks)
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
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", nil), teamNamed(beta, "Beta", nil)}, dayOf("2026-09-07"), dayOf("2026-09-13"), Workdays{})

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
	load := Loads(items, nil, dayOf("2026-09-07"), dayOf("2026-09-13"), Workdays{})
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
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", pts(10)), teamNamed(beta, "Beta", nil)}, dayOf("2026-09-07"), dayOf("2026-09-13"), Workdays{})
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
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", pts(10))}, dayOf("2026-09-07"), dayOf("2026-09-13"), Workdays{})
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
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", pts(1))}, dayOf("2026-09-07"), dayOf("2026-09-27"), Workdays{})
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
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", pts(1))}, dayOf("2026-09-07"), dayOf("2026-09-13"), Workdays{})
	row := rowNamed(load, "Alpha")
	if row.Unscheduled != 1 || row.Weeks[0].Load != 0 || rowNamed(load, "Total").Unscheduled != 1 {
		t.Errorf("row = %+v, want the work counted as unscheduled and in no week", row)
	}
}

func TestNoCapacityMeansNoWarning(t *testing.T) {
	alpha := uuid.New()
	items := FromTree([]issue.Node{teamed("PR-1", &alpha, pts(80), "2026-09-07", "2026-09-11")})
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", nil)}, dayOf("2026-09-07"), dayOf("2026-09-13"), Workdays{})
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
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", nil)}, dayOf("2026-09-07"), dayOf("2026-09-13"), Workdays{})
	if rowNamed(load, "Alpha").Weeks[0].Load != 0 {
		t.Error("work from before the window landed in it")
	}
}

// standardPerson is somebody on the standard week over the fortnight from 7
// September, with whatever holidays and absences they have.
func standardPerson(t *testing.T, off []availability.Holiday, away ...availability.Absence) availability.Person {
	t.Helper()
	s := availability.Schedule{Week: availability.StandardWeek(), Off: availability.DaysOf(off), Away: away}
	return availability.Person{Week: s.Week, Days: s.Days(dayOf("2026-09-07"), dayOf("2026-09-20"))}
}

func TestADefaultHolidayTakesNoWorkAndAWeekendOnlyTicketKeepsItsPoints(t *testing.T) {
	alpha := uuid.New()
	holiday := []availability.Holiday{{Day: "2026-09-09", Name: "Founders' Day"}}
	work := NewWorkdays(holiday, nil, nil)
	items := FromTree([]issue.Node{
		// Monday to Friday with Wednesday off: four days of two points.
		teamed("PR-1", &alpha, pts(8), "2026-09-07", "2026-09-11"),
		// Saturday and Sunday only: spread over them rather than lost.
		teamed("PR-2", &alpha, pts(3), "2026-09-12", "2026-09-13"),
	})
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", nil)}, dayOf("2026-09-07"), dayOf("2026-09-13"), work)
	if got := rowNamed(load, "Alpha").Weeks[0].Load; got != 11 {
		t.Errorf("Alpha's week = %v, want every one of the 11 points", got)
	}

	// The holiday is the only day of a one-day ticket: it keeps its point too.
	lone := FromTree([]issue.Node{teamed("PR-3", nil, pts(1), "2026-09-09", "2026-09-09")})
	if got := Loads(lone, nil, dayOf("2026-09-07"), dayOf("2026-09-13"), work).Rows[0].Weeks[0].Load; got != 1 {
		t.Errorf("a ticket on a holiday alone = %v, want its point", got)
	}
}

func TestATeamWorksTheDaysItsMembersWork(t *testing.T) {
	alpha := uuid.New()
	weekender := uuid.New()
	// Somebody who works Saturday and Sunday only.
	s := availability.Schedule{Week: map[string]int{"sat": 480, "sun": 480}}
	people := map[uuid.UUID]availability.Person{weekender: {Week: s.Week, Days: s.Days(dayOf("2026-09-07"), dayOf("2026-09-13"))}}
	work := NewWorkdays(nil, map[uuid.UUID][]uuid.UUID{alpha: {weekender}}, people)
	items := FromTree([]issue.Node{teamed("PR-1", &alpha, pts(6), "2026-09-11", "2026-09-14")})
	load := Loads(items, []team.Team{teamNamed(alpha, "Alpha", nil)}, dayOf("2026-09-07"), dayOf("2026-09-20"), work)
	if row := rowNamed(load, "Alpha"); row.Weeks[0].Load != 6 || row.Weeks[1].Load != 0 {
		t.Errorf("weeks = %+v, want all six points on the weekend the team works", row.Weeks)
	}
}

func TestCapacityShrinksWithTheTeamsTimeAway(t *testing.T) {
	alpha, empty := uuid.New(), uuid.New()
	ada, bea := uuid.New(), uuid.New()
	people := map[uuid.UUID]availability.Person{
		// Ada is away Monday and Tuesday and half of Wednesday of the first week.
		ada: standardPerson(t, nil,
			availability.Absence{StartsOn: "2026-09-07", EndsOn: "2026-09-08"},
			availability.Absence{StartsOn: "2026-09-09", EndsOn: "2026-09-09", HalfDay: true}),
		// Bea's calendar has the Friday of the second week off.
		bea: standardPerson(t, []availability.Holiday{{Day: "2026-09-18", Name: "Fair day"}}),
	}
	work := NewWorkdays(nil, map[uuid.UUID][]uuid.UUID{alpha: {ada, bea}}, people)
	teams := []team.Team{teamNamed(alpha, "Alpha", pts(20)), teamNamed(empty, "Nobody", pts(5))}
	load := Loads(nil, teams, dayOf("2026-09-07"), dayOf("2026-09-20"), work)

	row := rowNamed(load, "Alpha")
	first, second := row.Weeks[0], row.Weeks[1]
	// Ten working days, two and a half of them away: 20 * 7.5 / 10.
	if first.Capacity == nil || *first.Capacity != 15 || *first.NominalCapacity != 20 || first.DaysAway != 2.5 || first.Holidays != 0 {
		t.Errorf("the first week = %+v, want 15 of 20 after two and a half days away", first)
	}
	if second.Capacity == nil || *second.Capacity != 18 || second.DaysAway != 0 || second.Holidays != 1 {
		t.Errorf("the second week = %+v, want 18 after one holiday", second)
	}
	if nobody := rowNamed(load, "Nobody").Weeks[0]; nobody.Capacity == nil || *nobody.Capacity != 5 {
		t.Errorf("a team of nobody = %+v, want the 5 it said", nobody)
	}
	if total := rowNamed(load, "Total"); *total.Weeks[0].Capacity != 20 || *total.Weeks[0].NominalCapacity != 25 || *total.WeeklyCapacity != 25 {
		t.Errorf("the total = %+v, want the teams' 20 of 25", total.Weeks[0])
	}

	busy := FromTree([]issue.Node{teamed("PR-1", &alpha, pts(16), "2026-09-07", "2026-09-11")})
	warnings := CheckLoad(Loads(busy, teams[:1], dayOf("2026-09-07"), dayOf("2026-09-13"), work))
	if len(warnings) != 1 || !strings.Contains(warnings[0].Message, "capacity of 15 points, 20 points before holidays and absences") {
		t.Errorf("warnings = %+v, want 16 against the 15 left of 20", warnings)
	}
}

func TestASprintCarriesItsTeamsHours(t *testing.T) {
	alpha := uuid.New()
	ada := uuid.New()
	people := map[uuid.UUID]availability.Person{ada: standardPerson(t, nil, availability.Absence{StartsOn: "2026-09-10", EndsOn: "2026-09-11"})}
	work := NewWorkdays(nil, map[uuid.UUID][]uuid.UUID{alpha: {ada}}, people)
	start, end := dayOf("2026-09-07"), dayOf("2026-09-18")
	plans := []SprintPlan{
		{Sprint: sprint.Sprint{Name: "Alpha's", TeamID: &alpha, StartsOn: &start, EndsOn: &end}},
		{Sprint: sprint.Sprint{Name: "the project's", StartsOn: &start, EndsOn: &end}},
		{Sprint: sprint.Sprint{Name: "undated", TeamID: &alpha}},
	}
	TeamHours(plans, work)
	if h := plans[0]; h.AvailableHours == nil || *h.AvailableHours != 64 || *h.NominalHours != 80 {
		t.Errorf("Alpha's sprint = %v of %v hours, want 64 of 80", h.AvailableHours, h.NominalHours)
	}
	if plans[1].AvailableHours != nil || plans[2].AvailableHours != nil {
		t.Error("a sprint with no team, or no dates, was given hours")
	}
}
