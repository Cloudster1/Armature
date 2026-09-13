package plan

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/workflow"
)

// pts is a pointer to an estimate, which is how an unestimated issue stays
// distinguishable from one estimated at zero.
func pts(v float64) *float64 { return &v }

// sized is a node carrying an estimate and a sprint, which is all capacity reads.
func sized(key string, level int, estimate *float64, in *uuid.UUID, children ...issue.Node) issue.Node {
	n := node(key, level, nil, nil, children...)
	n.Issue.Estimate = estimate
	n.Issue.SprintID = in
	return n
}

func done(n issue.Node) issue.Node {
	n.Issue.Status = workflow.Status{Category: workflow.CategoryDone}
	return n
}

func sprintOf(id uuid.UUID, name string, capacity *float64) sprint.Sprint {
	return sprint.Sprint{ID: id, Name: name, State: sprint.StateActive, Capacity: capacity}
}

func TestEstimatesRollUpToParentsWithoutOne(t *testing.T) {
	tree := []issue.Node{
		sized("PR-1", issue.LevelEpic, nil, nil,
			sized("PR-2", issue.LevelStandard, pts(3), nil),
			sized("PR-3", issue.LevelStandard, pts(5), nil),
		),
	}
	items := FromTree(tree)

	epic := find(items, "PR-1")
	if epic.Estimate == nil || *epic.Estimate != 8 {
		t.Errorf("epic = %v, want the 8 points beneath it", epic.Estimate)
	}
	if !epic.EstimateDerived {
		t.Error("a derived estimate is not marked as derived, so the client cannot tell")
	}
}

// The same rule the dates follow: a number somebody typed is a decision.
func TestAnEstimateOfItsOwnIsLeftAlone(t *testing.T) {
	tree := []issue.Node{
		sized("PR-1", issue.LevelEpic, pts(13), nil,
			sized("PR-2", issue.LevelStandard, pts(3), nil),
		),
	}
	epic := find(FromTree(tree), "PR-1")
	if epic.Estimate == nil || *epic.Estimate != 13 {
		t.Errorf("epic = %v, want the 13 it was given", epic.Estimate)
	}
	if epic.EstimateDerived {
		t.Error("an estimate that was typed in is marked as derived")
	}
}

func TestUnestimatedWorkStaysUnestimated(t *testing.T) {
	tree := []issue.Node{
		sized("PR-1", issue.LevelEpic, nil, nil, sized("PR-2", issue.LevelStandard, nil, nil)),
	}
	if got := find(FromTree(tree), "PR-1").Estimate; got != nil {
		t.Errorf("epic = %v, want no estimate rather than zero", got)
	}
}

func TestSprintCountsItsWork(t *testing.T) {
	id := uuid.New()
	tree := []issue.Node{
		sized("PR-1", issue.LevelStandard, pts(3), &id),
		done(sized("PR-2", issue.LevelStandard, pts(5), &id)),
		sized("PR-3", issue.LevelStandard, pts(8), nil),
	}

	plans := Sprints(FromTree(tree), []sprint.Sprint{sprintOf(id, "Sprint 1", pts(10))})
	got := plans[0]

	if got.Committed != 8 {
		t.Errorf("committed = %v, want the 8 points in the sprint and not the backlog's", got.Committed)
	}
	if got.Completed != 5 {
		t.Errorf("completed = %v, want the 5 that are done", got.Completed)
	}
	if got.Issues != 2 {
		t.Errorf("issues = %d, want 2", got.Issues)
	}
}

// Issues are counted done whether or not they were sized: an unestimated issue
// that is finished is still finished.
func TestSprintsCountFinishedIssues(t *testing.T) {
	id := uuid.New()
	tree := []issue.Node{
		sized("PR-1", issue.LevelStandard, pts(3), &id),
		done(sized("PR-2", issue.LevelStandard, pts(5), &id)),
		done(sized("PR-3", issue.LevelStandard, nil, &id)),
	}
	got := Sprints(FromTree(tree), []sprint.Sprint{sprintOf(id, "Sprint 1", nil)})[0]
	if got.Issues != 3 || got.IssuesDone != 2 {
		t.Errorf("issues = %d done %d, want 3 and 2", got.Issues, got.IssuesDone)
	}
	if got.Unestimated != 1 || got.Completed != 5 {
		t.Errorf("unestimated = %d completed = %v, want the unsized one counted and not guessed", got.Unestimated, got.Completed)
	}
}

// A story of five points broken into subtasks that add up to five is five
// points of work, not ten.
func TestWorkInASprintIsCountedOnce(t *testing.T) {
	id := uuid.New()
	tree := []issue.Node{
		sized("PR-1", issue.LevelStandard, pts(5), &id,
			sized("PR-2", issue.LevelSubtask, pts(2), &id),
			sized("PR-3", issue.LevelSubtask, pts(3), &id),
		),
	}

	plans := Sprints(FromTree(tree), []sprint.Sprint{sprintOf(id, "Sprint 1", nil)})
	if plans[0].Committed != 5 {
		t.Errorf("committed = %v, want the parent's 5 counted once", plans[0].Committed)
	}
	if plans[0].Issues != 1 {
		t.Errorf("issues = %d, want only the row that carries the work", plans[0].Issues)
	}
}

// A child in a different sprint from its parent is work in that other sprint,
// so it does count there.
func TestAChildInAnotherSprintCountsThere(t *testing.T) {
	first, second := uuid.New(), uuid.New()
	tree := []issue.Node{
		sized("PR-1", issue.LevelStandard, pts(5), &first,
			sized("PR-2", issue.LevelSubtask, pts(2), &second),
		),
	}

	plans := Sprints(FromTree(tree), []sprint.Sprint{
		sprintOf(first, "Sprint 1", nil), sprintOf(second, "Sprint 2", nil),
	})
	if plans[0].Committed != 5 || plans[1].Committed != 2 {
		t.Errorf("committed = %v and %v, want 5 and 2", plans[0].Committed, plans[1].Committed)
	}
}

// A grandchild is still covered by a grandparent in the same sprint.
func TestAnAncestorAnywhereAboveClaimsTheWork(t *testing.T) {
	id := uuid.New()
	tree := []issue.Node{
		sized("PR-1", issue.LevelEpic, pts(20), &id,
			sized("PR-2", issue.LevelStandard, pts(5), nil,
				sized("PR-3", issue.LevelSubtask, pts(2), &id),
			),
		),
	}
	plans := Sprints(FromTree(tree), []sprint.Sprint{sprintOf(id, "Sprint 1", nil)})
	if plans[0].Committed != 20 {
		t.Errorf("committed = %v, want only the epic's 20", plans[0].Committed)
	}
}

func TestUnestimatedIssuesAreCountedNotGuessed(t *testing.T) {
	id := uuid.New()
	tree := []issue.Node{
		sized("PR-1", issue.LevelStandard, pts(3), &id),
		sized("PR-2", issue.LevelStandard, nil, &id),
		sized("PR-3", issue.LevelStandard, nil, &id),
	}

	got := Sprints(FromTree(tree), []sprint.Sprint{sprintOf(id, "Sprint 1", nil)})[0]
	if got.Committed != 3 {
		t.Errorf("committed = %v, want only what was estimated", got.Committed)
	}
	if got.Unestimated != 2 {
		t.Errorf("unestimated = %d, want 2", got.Unestimated)
	}
}

func TestOverCapacityIsAWarningAndNotARefusal(t *testing.T) {
	id := uuid.New()
	tree := []issue.Node{
		sized("PR-1", issue.LevelStandard, pts(13), &id),
	}
	items := FromTree(tree)
	plans := Sprints(items, []sprint.Sprint{sprintOf(id, "Sprint 1", pts(8))})

	if over := plans[0].OverBy(); over != 5 {
		t.Errorf("over by %v, want 5", over)
	}

	warnings := CheckSprints(items, plans)
	if len(warnings) != 1 || warnings[0].Kind != WarnOverCapacity {
		t.Fatalf("warnings = %+v, want one over-capacity", warnings)
	}
	if warnings[0].Sprint != "Sprint 1" {
		t.Errorf("warning names %q, want the sprint", warnings[0].Sprint)
	}
	// The message has to carry both numbers or it is not actionable.
	if !strings.Contains(warnings[0].Message, "13 points") || !strings.Contains(warnings[0].Message, "8 points") {
		t.Errorf("message %q does not name both totals", warnings[0].Message)
	}
}

func TestASprintWithNoCapacityIsNeverOver(t *testing.T) {
	id := uuid.New()
	items := FromTree([]issue.Node{sized("PR-1", issue.LevelStandard, pts(100), &id)})
	plans := Sprints(items, []sprint.Sprint{sprintOf(id, "Sprint 1", nil)})

	if plans[0].OverBy() != 0 {
		t.Error("a sprint nobody gave a capacity was reported as over it")
	}
	if warnings := CheckSprints(items, plans); len(warnings) != 0 {
		t.Errorf("warnings = %+v, want none", warnings)
	}
}

func TestWorkScheduledOutsideItsSprintIsReported(t *testing.T) {
	id := uuid.New()
	within := sized("PR-1", issue.LevelStandard, pts(3), &id)
	within.Issue.StartDate, within.Issue.DueDate = date("2026-03-02"), date("2026-03-06")

	beyond := sized("PR-2", issue.LevelStandard, pts(3), &id)
	beyond.Issue.StartDate, beyond.Issue.DueDate = date("2026-03-09"), date("2026-03-20")

	items := FromTree([]issue.Node{within, beyond})
	running := sprint.Sprint{
		ID: id, Name: "Sprint 1", State: sprint.StateActive,
		StartsOn: date("2026-03-02"), EndsOn: date("2026-03-13"),
	}

	warnings := CheckSprints(items, Sprints(items, []sprint.Sprint{running}))
	if len(warnings) != 1 || warnings[0].IssueKey != "PR-2" || warnings[0].Kind != WarnOutsideSprint {
		t.Fatalf("warnings = %+v, want only PR-2 outside the sprint", warnings)
	}
	if !strings.Contains(warnings[0].Message, "Sprint 1") {
		t.Errorf("message %q does not name the sprint", warnings[0].Message)
	}
}

// A sprint without dates cannot disagree with anything.
func TestASprintWithNoDatesReportsNothing(t *testing.T) {
	id := uuid.New()
	n := sized("PR-1", issue.LevelStandard, pts(3), &id)
	n.Issue.StartDate, n.Issue.DueDate = date("2026-03-09"), date("2026-03-20")

	items := FromTree([]issue.Node{n})
	plans := Sprints(items, []sprint.Sprint{{ID: id, Name: "Someday", State: sprint.StateFuture}})
	if warnings := CheckSprints(items, plans); len(warnings) != 0 {
		t.Errorf("warnings = %+v, want none", warnings)
	}
}
