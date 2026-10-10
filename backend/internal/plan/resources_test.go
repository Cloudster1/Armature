package plan

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/availability"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/team"
	"github.com/armature/armature/backend/internal/workflow"
)

// minutesIn is a number of hours as the issue stores them.
func minutesIn(hours int) *int {
	m := hours * minutesPerHour
	return &m
}

// work builds an open issue with hours and dates; an empty date is unset.
func work(key string, hours *int, start, due string) issue.Issue {
	i := issue.Issue{ID: uuid.New(), Key: key, Summary: "work " + key, TimeRemainingMinutes: hours}
	if start != "" {
		s := dayOf(start)
		i.StartDate = &s
	}
	if due != "" {
		d := dayOf(due)
		i.DueDate = &d
	}
	return i
}

func resourceRowNamed(t *testing.T, got ResourcePlan, name string) ResourceRow {
	t.Helper()
	for _, r := range got.Rows {
		if r.Name == name {
			return r
		}
	}
	t.Fatalf("no row %q in %+v", name, got.Rows)
	return ResourceRow{}
}

func hoursAre(p *float64, want float64) bool { return p != nil && *p == want }

// The fortnight from Monday 7 September 2026.
var fortnight = ResourceInput{From: dayOf("2026-09-07"), To: dayOf("2026-09-20")}

func TestTeamsCarryTheirMembersHoursAndTheirIssuesRemainingTime(t *testing.T) {
	alpha, nobody := uuid.New(), uuid.New()
	ada, bea := uuid.New(), uuid.New()
	people := map[uuid.UUID]availability.Person{
		ada: standardPerson(t, nil, availability.Absence{StartsOn: "2026-09-07", EndsOn: "2026-09-08"}),
		bea: standardPerson(t, []availability.Holiday{{Day: "2026-09-18", Name: "Fair day"}}),
	}
	alphas := work("PR-1", minutesIn(20), "2026-09-07", "2026-09-11")
	alphas.TeamID = &alpha
	alphas.TimeEstimateMinutes = minutesIn(30)
	in := fortnight
	in.Grouping = project.GroupByTeam
	in.Teams = []team.Team{{ID: alpha, Name: "Alpha"}, {ID: nobody, Name: "Nobody"}}
	in.Work = NewWorkdays(nil, map[uuid.UUID][]uuid.UUID{alpha: {ada, bea}}, people)
	in.Issues = []issue.Issue{alphas, work("PR-2", minutesIn(6), "2026-09-14", "2026-09-15")}
	got := ReadResources(in)

	if len(got.Weeks) != 2 || len(got.Rows) != 3 || got.Rows[0].Name != "Alpha" || got.Rows[1].Name != "Nobody" || got.Rows[2].Kind != LoadUnassigned {
		t.Fatalf("rows = %+v, want the teams in order and the unassigned row last", got.Rows)
	}
	first, second := got.Rows[0].Weeks[0], got.Rows[0].Weeks[1]
	// Ten days of eight hours, less the two Ada is away; the remaining time, not the estimate.
	if !hoursAre(first.CapacityHours, 64) || !hoursAre(first.NominalHours, 80) || first.DaysAway != 2 || first.LoadHours != 20 {
		t.Errorf("Alpha's first week = %+v, want 20 h against 64 of 80", first)
	}
	if len(first.Issues) != 1 || first.Issues[0].Key != "PR-1" || first.Issues[0].Hours != 20 {
		t.Errorf("the first week's issues = %+v", first.Issues)
	}
	if !hoursAre(second.CapacityHours, 72) || second.Holidays != 1 || second.LoadHours != 0 {
		t.Errorf("Alpha's second week = %+v, want 72 h after Bea's holiday", second)
	}
	if empty := got.Rows[1].Weeks[0]; !hoursAre(empty.CapacityHours, 0) {
		t.Errorf("a team of nobody = %+v, want no hours", empty)
	}
	unassigned := got.Rows[2]
	if unassigned.ID != nil || unassigned.Weeks[1].CapacityHours != nil || unassigned.Weeks[1].LoadHours != 6 {
		t.Errorf("the unassigned row = %+v, want 6 h and no capacity", unassigned)
	}
	if len(got.Warnings) != 0 {
		t.Errorf("warnings = %+v, want none", got.Warnings)
	}
}

func TestAPersonsWorkIsSpreadOverTheDaysTheyAreThere(t *testing.T) {
	ada, bea := uuid.New(), uuid.New()
	people := map[uuid.UUID]availability.Person{
		// Away on Tuesday, and Thursday is a holiday on Ada's calendar.
		ada: standardPerson(t, []availability.Holiday{{Day: "2026-09-10", Name: "Ada's day"}},
			availability.Absence{StartsOn: "2026-09-08", EndsOn: "2026-09-08"}),
		bea: standardPerson(t, nil),
	}
	assigned := func(i issue.Issue, who uuid.UUID, name string) issue.Issue {
		i.Assignee = &issue.UserRef{ID: who, Name: name}
		return i
	}
	in := fortnight
	in.Grouping = project.GroupByPerson
	in.People = map[uuid.UUID]string{ada: "Ada", bea: "bea"}
	in.Work = NewWorkdays(nil, nil, people)
	in.Issues = []issue.Issue{
		// Monday to Friday is three days Ada is there: four hours each.
		assigned(work("PR-1", minutesIn(12), "2026-09-07", "2026-09-11"), ada, "Ada"),
		// The only day is the one she is away: it keeps its hours.
		assigned(work("PR-4", minutesIn(2), "2026-09-08", "2026-09-08"), ada, "Ada"),
		work("PR-3", minutesIn(5), "2026-09-07", "2026-09-07"),
	}
	got := ReadResources(in)

	if len(got.Rows) != 3 || got.Rows[0].Name != "Ada" || got.Rows[1].Name != "bea" || got.Rows[2].Kind != LoadUnassigned {
		t.Fatalf("rows = %+v, want people by name and the unassigned row last", got.Rows)
	}
	week := got.Rows[0].Weeks[0]
	if week.LoadHours != 14 || !hoursAre(week.CapacityHours, 24) || !hoursAre(week.NominalHours, 40) || week.DaysAway != 1 || week.Holidays != 1 {
		t.Errorf("Ada's week = %+v, want 14 h against 24 of 40", week)
	}
	if got.Rows[0].Kind != RowPerson || got.Rows[0].ID == nil || *got.Rows[0].ID != ada {
		t.Errorf("Ada's row = %+v", got.Rows[0])
	}
	if b := got.Rows[1].Weeks[0]; b.LoadHours != 0 || !hoursAre(b.CapacityHours, 40) {
		t.Errorf("Bea's week = %+v, want 40 h free", b)
	}
	if u := got.Rows[2].Weeks[0]; u.LoadHours != 5 {
		t.Errorf("the unassigned week = %+v, want 5 h", u)
	}
	// Fifty hours on one Monday is more than Ada's whole second week.
	in.Issues = append(in.Issues, assigned(work("PR-10", minutesIn(50), "2026-09-14", "2026-09-14"), ada, "Ada"))
	got = ReadResources(in)
	if len(got.Warnings) != 1 || got.Warnings[0].Kind != WarnOverAllocated || got.Warnings[0].Person != "Ada" || got.Warnings[0].Team != "" ||
		!strings.Contains(got.Warnings[0].Message, "Ada is allocated 50 h in the week of 14 Sep against 40 h available") {
		t.Errorf("warnings = %+v, want Ada over in the second week", got.Warnings)
	}
}

func TestWorkOfSomebodyWhoIsNotAmongThePeopleIsUnassigned(t *testing.T) {
	ada, gone := uuid.New(), uuid.New()
	in := fortnight
	in.Grouping = project.GroupByPerson
	// People are who works here now; whoever left is not among them.
	in.People = map[uuid.UUID]string{ada: "Ada"}
	in.Work = NewWorkdays(nil, nil, map[uuid.UUID]availability.Person{ada: standardPerson(t, nil)})
	theirs := work("PR-1", minutesIn(6), "2026-09-07", "2026-09-07")
	theirs.Assignee = &issue.UserRef{ID: gone, Name: "Alice"}
	in.Issues = []issue.Issue{theirs}
	got := ReadResources(in)
	if len(got.Rows) != 2 || got.Rows[0].Name != "Ada" || got.Rows[1].Kind != LoadUnassigned {
		t.Fatalf("rows = %+v, want Ada and the unassigned row, and no row for somebody who left", got.Rows)
	}
	if u := got.Rows[1].Weeks[0]; u.LoadHours != 6 || len(u.Issues) != 1 || u.Issues[0].Key != "PR-1" {
		t.Errorf("the unassigned week = %+v, want the 6 h of the work left behind", u)
	}
}

func TestAShareOfTheWeekScalesThePersonAndTheirTeam(t *testing.T) {
	alpha := uuid.New()
	ada, bea, cleo := uuid.New(), uuid.New(), uuid.New()
	people := map[uuid.UUID]availability.Person{
		ada:  standardPerson(t, nil),
		bea:  standardPerson(t, nil),
		cleo: standardPerson(t, nil, availability.Absence{StartsOn: "2026-09-07", EndsOn: "2026-09-07"}),
	}
	in := fortnight
	in.Grouping = project.GroupByPerson
	in.People = map[uuid.UUID]string{ada: "Ada", bea: "Bea", cleo: "Cleo"}
	in.Work = NewWorkdays(nil, map[uuid.UUID][]uuid.UUID{alpha: {ada, cleo}}, people)
	// Ada gives this project half her week and Bea none of it; Cleo, with no
	// share set, gives it all of hers.
	in.Shares = map[uuid.UUID]int{ada: 50, bea: 0}
	got := ReadResources(in)

	a, b, c := resourceRowNamed(t, got, "Ada"), resourceRowNamed(t, got, "Bea"), resourceRowNamed(t, got, "Cleo")
	if w := a.Weeks[0]; !hoursAre(w.CapacityHours, 20) || !hoursAre(w.NominalHours, 20) || a.SharePercent == nil || *a.SharePercent != 50 {
		t.Errorf("Ada = %+v %+v, want 20 of 20 h at 50%%", a.SharePercent, w)
	}
	if w := b.Weeks[0]; !hoursAre(w.CapacityHours, 0) || !hoursAre(w.NominalHours, 0) || b.SharePercent == nil || *b.SharePercent != 0 {
		t.Errorf("Bea = %+v %+v, want no hours at 0%%", b.SharePercent, w)
	}
	// The whole week is no share to speak of; the day away still counts.
	if w := c.Weeks[0]; !hoursAre(w.CapacityHours, 32) || !hoursAre(w.NominalHours, 40) || w.DaysAway != 1 || c.SharePercent != nil {
		t.Errorf("Cleo = %+v %+v, want 32 of 40 h and no share shown", c.SharePercent, w)
	}

	in.Grouping = project.GroupByTeam
	in.Teams = []team.Team{{ID: alpha, Name: "Alpha"}}
	got = ReadResources(in)
	// Half of Ada's forty and all of Cleo's thirty-two.
	if w := resourceRowNamed(t, got, "Alpha").Weeks[0]; !hoursAre(w.CapacityHours, 52) || !hoursAre(w.NominalHours, 60) || w.DaysAway != 1 {
		t.Errorf("Alpha's week = %+v, want 52 of 60 h", w)
	}
}

func TestUnscheduledAndUnestimatedWorkIsListedNotGuessed(t *testing.T) {
	parent := work("PR-8", nil, "", "")
	child := work("PR-9", minutesIn(4), "2026-09-07", "2026-09-07")
	child.ParentID = &parent.ID
	in := fortnight
	in.Grouping = project.GroupByTeam
	in.Issues = []issue.Issue{
		work("PR-10", nil, "", ""),
		work("PR-5", minutesIn(3), "", "2026-09-09"),
		work("PR-6", nil, "", ""),
		work("PR-7", nil, "2027-01-04", "2027-01-08"),
		parent, child,
		work("PR-11", minutesIn(1), "2026-09-08", ""),
	}
	got := ReadResources(in)
	if len(got.Unscheduled) != 2 || got.Unscheduled[0].Key != "PR-5" || !hoursAre(got.Unscheduled[0].Hours, 3) || got.Unscheduled[1].Key != "PR-11" {
		t.Errorf("unscheduled = %+v, want PR-5 and PR-11 with their hours", got.Unscheduled)
	}
	// In key order, and neither the parent nor work far outside the window.
	if len(got.Unestimated) != 2 || got.Unestimated[0].Key != "PR-6" || got.Unestimated[1].Key != "PR-10" || got.Unestimated[0].Hours != nil {
		t.Errorf("unestimated = %+v, want PR-6 and PR-10", got.Unestimated)
	}
	if u := got.Rows[len(got.Rows)-1].Weeks[0]; u.LoadHours != 4 {
		t.Errorf("the child's hours = %+v, want 4", u)
	}
}

func TestWorkIsCountedOnceAtTheLevelThatCarriesItsHours(t *testing.T) {
	alpha, beta := uuid.New(), uuid.New()
	onTeam := func(i issue.Issue, team uuid.UUID, parent *issue.Issue) issue.Issue {
		i.TeamID = &team
		if parent != nil {
			i.ParentID = &parent.ID
		}
		return i
	}
	in := fortnight
	in.Grouping = project.GroupByTeam
	in.Teams = []team.Team{{ID: alpha, Name: "Alpha"}, {ID: beta, Name: "Beta"}}

	// A story sized at 16 h speaks for its subtasks: their 8 h each are inside it.
	story := onTeam(work("PR-1", minutesIn(16), "2026-09-07", "2026-09-11"), alpha, nil)
	in.Issues = []issue.Issue{
		story,
		onTeam(work("PR-2", minutesIn(8), "2026-09-07", "2026-09-08"), alpha, &story),
		onTeam(work("PR-3", minutesIn(8), "2026-09-09", "2026-09-11"), alpha, &story),
		// A subtask nobody sized is not missing an estimate when its story has one.
		onTeam(work("PR-4", nil, "", ""), alpha, &story),
		// Handed to another team, a subtask is that team's own work.
		onTeam(work("PR-5", minutesIn(4), "2026-09-07", "2026-09-07"), beta, &story),
	}
	got := ReadResources(in)
	if a := resourceRowNamed(t, got, "Alpha").Weeks[0]; a.LoadHours != 16 || len(a.Issues) != 1 || a.Issues[0].Key != "PR-1" {
		t.Errorf("Alpha's week = %+v, want the story's 16 h and not its subtasks' again", a)
	}
	if b := resourceRowNamed(t, got, "Beta").Weeks[0]; b.LoadHours != 4 {
		t.Errorf("Beta's week = %+v, want the 4 h of the subtask handed to it", b)
	}
	if len(got.Unestimated) != 0 {
		t.Errorf("unestimated = %+v, want nothing: the story carries its subtasks", got.Unestimated)
	}

	// A story nobody sized is the sum of its subtasks.
	unsized := onTeam(work("PR-6", nil, "2026-09-07", "2026-09-11"), alpha, nil)
	in.Issues = []issue.Issue{
		unsized,
		onTeam(work("PR-7", minutesIn(8), "2026-09-07", "2026-09-08"), alpha, &unsized),
		onTeam(work("PR-8", minutesIn(8), "2026-09-09", "2026-09-11"), alpha, &unsized),
	}
	got = ReadResources(in)
	if a := resourceRowNamed(t, got, "Alpha").Weeks[0]; a.LoadHours != 16 || len(a.Issues) != 2 {
		t.Errorf("Alpha's week = %+v, want the two subtasks' 16 h", a)
	}

	// A story with hours but no dates is listed with them, and its dated
	// subtasks are not counted a second time in the grid.
	undated := onTeam(work("PR-9", minutesIn(10), "", ""), alpha, nil)
	in.Issues = []issue.Issue{undated, onTeam(work("PR-10", minutesIn(6), "2026-09-07", "2026-09-07"), alpha, &undated)}
	got = ReadResources(in)
	if a := resourceRowNamed(t, got, "Alpha").Weeks[0]; a.LoadHours != 0 || len(got.Unscheduled) != 1 || got.Unscheduled[0].Key != "PR-9" {
		t.Errorf("Alpha's week = %+v, unscheduled = %+v; want the 10 h listed once, on the story", a, got.Unscheduled)
	}
}

func TestATeamWithNobodyIsOverWithAnyWork(t *testing.T) {
	nobody := uuid.New()
	busy := work("PR-1", minutesIn(8), "2026-09-07", "2026-09-07")
	busy.TeamID = &nobody
	in := fortnight
	in.Grouping = project.GroupByTeam
	in.Teams = []team.Team{{ID: nobody, Name: "Nobody"}}
	in.Issues = []issue.Issue{busy, work("PR-2", minutesIn(80), "2026-09-07", "2026-09-07")}
	got := ReadResources(in)
	if len(got.Warnings) != 1 || got.Warnings[0].Team != "Nobody" || !strings.Contains(got.Warnings[0].Message, "8 h in the week of 7 Sep against 0 h available") {
		t.Errorf("warnings = %+v, want the empty team over and nothing for the unassigned work", got.Warnings)
	}
}

func TestOnlyOpenIssuesAreRead(t *testing.T) {
	done := issue.Node{Issue: work("PR-1", minutesIn(1), "", "")}
	done.Issue.Status = workflow.Status{Category: workflow.CategoryDone}
	done.Children = []issue.Node{{Issue: work("PR-2", minutesIn(1), "", "")}}
	got := openIssues([]issue.Node{done, {Issue: work("PR-3", nil, "", "")}})
	if len(got) != 2 || got[0].Key != "PR-2" || got[1].Key != "PR-3" {
		t.Errorf("open = %+v, want the open child and the open root", got)
	}
}

func TestTheResourceWindowIsWholeWeeks(t *testing.T) {
	wednesday := dayOf("2026-09-09")
	from, to, err := ResourceWindow("", "", wednesday)
	if err != nil || from != dayOf("2026-09-07") || to != dayOf("2026-11-01") || len(Weeks(from, to)) != ResourceDefaultWeeks {
		t.Errorf("the default window = %v to %v (%v), want eight weeks from this Monday", from, to, err)
	}
	from, to, err = ResourceWindow("2026-09-10", "2026-09-16", wednesday)
	if err != nil || from != dayOf("2026-09-07") || to != dayOf("2026-09-20") {
		t.Errorf("a window inside two weeks = %v to %v (%v), want the whole two", from, to, err)
	}
	from, to, err = ResourceWindow("", "2026-11-04", wednesday)
	if err != nil || to != dayOf("2026-11-08") || from != dayOf("2026-09-14") {
		t.Errorf("a window by its end = %v to %v (%v), want eight weeks up to it", from, to, err)
	}
	for _, bad := range [][2]string{{"2026-09-14", "2026-09-07"}, {"2026-01-05", "2026-12-31"}, {"next week", ""}, {"", "2026-13-01"}} {
		if _, _, err := ResourceWindow(bad[0], bad[1], wednesday); !errors.Is(err, ErrBadWindow) {
			t.Errorf("%v: %v, want a refused window", bad, err)
		}
	}
	if _, _, err := ResourceWindow("2026-01-05", "2026-07-05", wednesday); err != nil {
		t.Errorf("the most weeks one reading covers: %v", err)
	}
}
