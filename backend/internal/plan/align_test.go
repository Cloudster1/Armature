package plan

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/availability"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/workflow"
)

// alignToday is before every date the tests schedule, so nothing is under way unless a test says so.
var alignToday = dayOf("2026-10-01")

func task(key, start, due string, children ...issue.Node) issue.Node {
	return node(key, issue.LevelStandard, date(start), date(due), children...)
}

func blocks(pairs ...string) []issue.Blocker {
	var out []issue.Blocker
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, issue.Blocker{LinkID: uuid.New(), BlockerKey: pairs[i], BlockedKey: pairs[i+1]})
	}
	return out
}

func align(t *testing.T, tree []issue.Node, in AlignInput) []Move {
	t.Helper()
	if in.Today.IsZero() {
		in.Today = alignToday
	}
	return Align(FromTree(tree), in).Moves
}

// moveList is each move as KEY:start..due, in the order proposed.
func moveList(moves []Move) string {
	var parts []string
	for _, m := range moves {
		parts = append(parts, m.IssueKey+":"+m.To.Start.Format("2006-01-02")+".."+m.To.Due.Format("2006-01-02"))
	}
	return strings.Join(parts, " ")
}

func TestAlignStartsAChainAfterEachBlocker(t *testing.T) {
	// 2 November 2026 is a Monday.
	tree := []issue.Node{
		task("PR-1", "2026-11-02", "2026-11-06"),
		task("PR-2", "2026-11-04", "2026-11-05"),
		task("PR-3", "2026-11-09", "2026-11-11"),
	}
	moves := align(t, tree, AlignInput{Blockers: blocks("PR-1", "PR-2", "PR-2", "PR-3")})
	want := "PR-2:2026-11-09..2026-11-10 PR-3:2026-11-11..2026-11-13"
	if got := moveList(moves); got != want {
		t.Fatalf("moves = %s, want %s", got, want)
	}
	if moves[0].After != "PR-1" || moves[0].WorkingDays != 2 || moves[1].WorkingDays != 3 {
		t.Errorf("moves = %+v, want PR-2 after PR-1 keeping 2 working days and PR-3 keeping 3", moves)
	}
	if moves[0].From.Start != dayOf("2026-11-04") || moves[0].From.Due != dayOf("2026-11-05") {
		t.Errorf("PR-2 from = %+v, want where it was", moves[0].From)
	}
}

func TestAlignFollowsTheLatestOfSeveralBlockers(t *testing.T) {
	tree := []issue.Node{
		task("PR-1", "2026-11-02", "2026-11-06"),
		task("PR-2", "2026-11-09", "2026-11-09"),
		// Thursday to Tuesday: four working days.
		task("PR-3", "2026-11-05", "2026-11-10"),
		task("PR-4", "2026-11-11", "2026-11-11"),
	}
	moves := align(t, tree, AlignInput{Blockers: blocks("PR-1", "PR-2", "PR-1", "PR-3", "PR-2", "PR-4", "PR-3", "PR-4")})
	want := "PR-3:2026-11-09..2026-11-12 PR-4:2026-11-13..2026-11-13"
	if got := moveList(moves); got != want {
		t.Fatalf("moves = %s, want %s", got, want)
	}
	if moves[1].After != "PR-3" {
		t.Errorf("PR-4 follows %s, want PR-3, which finishes last", moves[1].After)
	}
}

func TestAlignTreatsStartingOnTheBlockersLastDayAsTooEarly(t *testing.T) {
	tree := []issue.Node{task("PR-1", "2026-11-02", "2026-11-04"), task("PR-2", "2026-11-04", "2026-11-04")}
	if got := moveList(align(t, tree, AlignInput{Blockers: blocks("PR-1", "PR-2")})); got != "PR-2:2026-11-05..2026-11-05" {
		t.Errorf("moves = %s, want PR-2 on the day after PR-1 ends", got)
	}
}

func TestAlignSkipsWeekendsAndHolidays(t *testing.T) {
	holidays := []availability.Holiday{{Day: "2026-11-10", Name: "Founders' Day"}}
	tree := []issue.Node{
		task("PR-1", "2026-11-02", "2026-11-06"),
		// Thursday, Friday and Monday: three working days.
		task("PR-2", "2026-11-05", "2026-11-09"),
	}
	moves := align(t, tree, AlignInput{Blockers: blocks("PR-1", "PR-2"), Work: NewWorkdays(holidays, nil, nil)})
	// Saturday and Sunday are skipped to Monday, then Tuesday's holiday.
	if got := moveList(moves); got != "PR-2:2026-11-09..2026-11-12" {
		t.Errorf("moves = %s, want Monday the 9th to Thursday the 12th", got)
	}
}

func TestAlignCountsOnTheIssuesTeamCalendar(t *testing.T) {
	weekend, weekender := uuid.New(), uuid.New()
	s := availability.Schedule{Week: map[string]int{"sat": 480, "sun": 480}}
	people := map[uuid.UUID]availability.Person{weekender: {Week: s.Week}}
	work := NewWorkdays(nil, map[uuid.UUID][]uuid.UUID{weekend: {weekender}}, people)
	blocked := task("PR-2", "2026-11-07", "2026-11-07")
	blocked.Issue.TeamID = &weekend
	tree := []issue.Node{task("PR-1", "2026-11-02", "2026-11-07"), blocked}
	if got := moveList(align(t, tree, AlignInput{Blockers: blocks("PR-1", "PR-2"), Work: work})); got != "PR-2:2026-11-08..2026-11-08" {
		t.Errorf("moves = %s, want the Sunday the team works", got)
	}
}

func TestAlignLeavesWhatIsAlreadyInOrder(t *testing.T) {
	tree := []issue.Node{
		task("PR-1", "2026-11-02", "2026-11-04"),
		// Slack after the blocker is kept, not closed.
		task("PR-2", "2026-11-16", "2026-11-18"),
	}
	if moves := align(t, tree, AlignInput{Blockers: blocks("PR-1", "PR-2")}); len(moves) != 0 {
		t.Errorf("moves = %s, want none", moveList(moves))
	}
}

func TestAlignMovesNothingThatIsFixed(t *testing.T) {
	blocker := task("PR-1", "2026-11-02", "2026-11-06")
	underWay := func(key, start, due string) issue.Node {
		n := task(key, start, due)
		n.Issue.Status = workflow.Status{Category: workflow.CategoryInProgress}
		return n
	}
	cases := []struct {
		name  string
		issue issue.Node
		in    AlignInput
	}{
		{"done", done(task("PR-2", "2026-11-03", "2026-11-04")), AlignInput{}},
		{"under way since before today", underWay("PR-2", "2026-11-03", "2026-11-04"), AlignInput{Today: dayOf("2026-11-04")}},
		{"outside the query", task("PR-2", "2026-11-03", "2026-11-04"), AlignInput{Matched: map[string]bool{"PR-1": true}}},
		{"half scheduled", node("PR-2", issue.LevelStandard, date("2026-11-03"), nil), AlignInput{}},
		{"derived from its children", node("PR-2", issue.LevelEpic, nil, nil, task("PR-3", "2026-11-03", "2026-11-04")), AlignInput{Matched: map[string]bool{"PR-2": true}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.in.Blockers = blocks("PR-1", "PR-2")
			if moves := align(t, []issue.Node{blocker, c.issue}, c.in); len(moves) != 0 {
				t.Errorf("moves = %s, want none", moveList(moves))
			}
		})
	}

	t.Run("in progress but not started yet", func(t *testing.T) {
		moves := align(t, []issue.Node{blocker, underWay("PR-2", "2026-11-03", "2026-11-04")}, AlignInput{Blockers: blocks("PR-1", "PR-2")})
		if got := moveList(moves); got != "PR-2:2026-11-09..2026-11-10" {
			t.Errorf("moves = %s, want it moved: it has not started", got)
		}
	})
}

func TestAFixedIssueStillHoldsBackWhatItBlocks(t *testing.T) {
	tree := []issue.Node{
		task("PR-1", "2026-11-02", "2026-11-06"),
		done(task("PR-2", "2026-11-04", "2026-11-09")),
		task("PR-3", "2026-11-09", "2026-11-09"),
	}
	moves := align(t, tree, AlignInput{Blockers: blocks("PR-1", "PR-2", "PR-2", "PR-3")})
	if got := moveList(moves); got != "PR-3:2026-11-10..2026-11-10" {
		t.Errorf("moves = %s, want only PR-3, after the done PR-2", got)
	}
}

func TestABlockerInAnotherProjectConstrainsAndNeverMoves(t *testing.T) {
	tree := []issue.Node{task("PR-1", "2026-11-03", "2026-11-03")}
	outside := map[string]Span{"OT-7": {Start: dayOf("2026-11-02"), Due: dayOf("2026-11-04")}}
	moves := align(t, tree, AlignInput{Blockers: blocks("OT-7", "PR-1"), Outside: outside})
	if got := moveList(moves); got != "PR-1:2026-11-05..2026-11-05" || moves[0].After != "OT-7" {
		t.Errorf("moves = %+v, want PR-1 after OT-7", moves)
	}

	// One the person may not read is not in Outside, and so holds nothing back.
	if moves := align(t, tree, AlignInput{Blockers: blocks("OT-7", "PR-1")}); len(moves) != 0 {
		t.Errorf("moves = %s, want none without the blocker's dates", moveList(moves))
	}
}

func TestAlignProposesTheSameMovesEveryTime(t *testing.T) {
	tree := []issue.Node{
		task("PR-1", "2026-11-02", "2026-11-06"),
		task("PR-3", "2026-11-03", "2026-11-03"),
		task("PR-2", "2026-11-04", "2026-11-04"),
	}
	in := AlignInput{Blockers: blocks("PR-1", "PR-3", "PR-1", "PR-2")}
	first := moveList(align(t, tree, in))
	for i := 0; i < 20; i++ {
		if got := moveList(align(t, tree, in)); got != first {
			t.Fatalf("moves = %s, then %s", first, got)
		}
	}
	if first != "PR-2:2026-11-09..2026-11-09 PR-3:2026-11-09..2026-11-09" {
		t.Errorf("moves = %s, want them in key order", first)
	}
}

func TestAWorkingDaySearchEndsOnACalendarWithoutOne(t *testing.T) {
	nobody, idle := uuid.New(), uuid.New()
	work := NewWorkdays(nil, map[uuid.UUID][]uuid.UUID{nobody: {idle}}, map[uuid.UUID]availability.Person{idle: {Week: map[string]int{}}})
	blocked := task("PR-2", "2026-11-03", "2026-11-04")
	blocked.Issue.TeamID = &nobody
	result := make(chan []Move, 1)
	go func() {
		result <- align(t, []issue.Node{task("PR-1", "2026-11-02", "2026-11-04"), blocked}, AlignInput{Blockers: blocks("PR-1", "PR-2"), Work: work})
	}()
	select {
	case moves := <-result:
		// No working day at all: it keeps its calendar length from the day after.
		if got := moveList(moves); got != "PR-2:2026-11-05..2026-11-06" {
			t.Errorf("moves = %s, want the day after PR-1, two calendar days long", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the search for a working day did not end")
	}
}

func TestMovedPlanRaisesNoWarningTheMovesResolved(t *testing.T) {
	tree := []issue.Node{
		node("PR-1", issue.LevelEpic, nil, nil,
			task("PR-2", "2026-11-02", "2026-11-06"),
			task("PR-3", "2026-11-04", "2026-11-04"),
		),
	}
	items := FromTree(tree)
	b := blocks("PR-2", "PR-3")
	moves := Align(items, AlignInput{Blockers: b, Today: alignToday}).Moves
	after := Moved(items, moves)
	for _, w := range Check(after, b) {
		if w.Kind == WarnBlockedTooEarly {
			t.Errorf("after the moves: %+v", w)
		}
	}
	if got := rangeOf(find(after, "PR-1")); got != "2026-11-02..2026-11-09" {
		t.Errorf("the epic spans %s after the moves, want it rolled up again", got)
	}
	if got := rangeOf(find(items, "PR-3")); got != "2026-11-04..2026-11-04" {
		t.Errorf("the plan read was changed to %s", got)
	}
}

// undated is an issue without dates with this much time left, in minutes.
func undated(key string, minutes int) issue.Node {
	n := node(key, issue.LevelStandard, nil, nil)
	n.Issue.TimeRemainingMinutes = &minutes
	return n
}

func TestPlacingGivesWorkWithoutDatesItsFirstOnesAfterItsBlockers(t *testing.T) {
	const twoDays = 2 * availability.StandardDayMinutes
	tree := []issue.Node{
		task("PR-1", "2026-11-02", "2026-11-06"),
		undated("PR-2", twoDays),
		// A day and a bit is two days.
		undated("PR-3", availability.StandardDayMinutes+1),
	}
	proposal := Align(FromTree(tree), AlignInput{Blockers: blocks("PR-1", "PR-2", "PR-2", "PR-3"), Today: alignToday, Place: true})
	if got := moveList(proposal.Moves); got != "PR-2:2026-11-09..2026-11-10 PR-3:2026-11-11..2026-11-12" {
		t.Fatalf("moves = %s, want PR-2 after PR-1 and PR-3 after PR-2, two days each", got)
	}
	if m := proposal.Moves[0]; !m.Placed || m.From != nil || m.After != "PR-1" || m.WorkingDays != 2 {
		t.Errorf("PR-2's move = %+v, want it placed from nothing after PR-1", m)
	}
}

func TestPlacingStartsFromTodayWhenNothingHoldsTheWorkBack(t *testing.T) {
	// 1 October 2026 is a Thursday; one day of work lands on it.
	proposal := Align(FromTree([]issue.Node{undated("PR-1", availability.StandardDayMinutes)}), AlignInput{Today: alignToday, Place: true})
	if got := moveList(proposal.Moves); got != "PR-1:2026-10-01..2026-10-01" {
		t.Errorf("moves = %s, want today", got)
	}

	// A blocker that ended in the past does not put the work in the past.
	tree := []issue.Node{task("PR-1", "2026-09-01", "2026-09-02"), undated("PR-2", availability.StandardDayMinutes)}
	proposal = Align(FromTree(tree), AlignInput{Blockers: blocks("PR-1", "PR-2"), Today: alignToday, Place: true})
	if got := moveList(proposal.Moves); got != "PR-2:2026-10-01..2026-10-01" {
		t.Errorf("moves = %s, want today, not the day after a blocker long done", got)
	}
}

func TestPlacingCountsTheTeamsCalendar(t *testing.T) {
	holidays := []availability.Holiday{{Day: "2026-11-10", Name: "Founders' Day"}}
	tree := []issue.Node{task("PR-1", "2026-11-02", "2026-11-06"), undated("PR-2", 3*availability.StandardDayMinutes)}
	proposal := Align(FromTree(tree), AlignInput{Blockers: blocks("PR-1", "PR-2"), Today: alignToday, Place: true, Work: NewWorkdays(holidays, nil, nil)})
	if got := moveList(proposal.Moves); got != "PR-2:2026-11-09..2026-11-12" {
		t.Errorf("moves = %s, want three working days around the holiday", got)
	}
}

func TestPlacingLeavesOutWhatItCannotOrShouldNotPlace(t *testing.T) {
	estimated := node("PR-5", issue.LevelStandard, nil, nil)
	points := 3.0
	estimated.Issue.Estimate = &points
	tree := []issue.Node{
		undated("PR-1", availability.StandardDayMinutes),
		node("PR-2", issue.LevelStandard, nil, nil),
		done(undated("PR-3", availability.StandardDayMinutes)),
		node("PR-4", issue.LevelEpic, nil, nil, undated("PR-6", availability.StandardDayMinutes)),
		estimated,
		node("PR-7", issue.LevelStandard, date("2026-11-03"), nil),
	}
	proposal := Align(FromTree(tree), AlignInput{Today: alignToday, Place: true, Matched: map[string]bool{"PR-1": true, "PR-2": true, "PR-3": true, "PR-4": true, "PR-5": true, "PR-7": true}})
	if got := moveList(proposal.Moves); got != "PR-1:2026-10-01..2026-10-01" {
		t.Errorf("moves = %s, want only PR-1: the rest are done, a parent, half scheduled or outside the query", got)
	}
	var unplaced []string
	for _, u := range proposal.Unplaced {
		unplaced = append(unplaced, u.IssueKey)
		if u.Reason != "Not placed: it has no estimate." {
			t.Errorf("%s is not placed because %q", u.IssueKey, u.Reason)
		}
	}
	// Points say how big, not how long, so they place nothing.
	if strings.Join(unplaced, " ") != "PR-2 PR-5" {
		t.Errorf("unplaced = %v, want PR-2 and PR-5, which have no time estimate", unplaced)
	}

	if without := Align(FromTree(tree), AlignInput{Today: alignToday}); len(without.Moves) != 0 || len(without.Unplaced) != 0 {
		t.Errorf("without placing: %+v, want nothing", without)
	}
}
