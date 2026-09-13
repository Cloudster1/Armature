package plan

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/workflow"
)

func date(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return &t
}

// node builds an issue tree node with only the fields the plan reads.
func node(key string, level int, start, due *time.Time, children ...issue.Node) issue.Node {
	return issue.Node{
		Issue: issue.Issue{
			ID:        uuid.New(),
			Key:       key,
			Type:      issue.TypeRef{Name: "T", Level: level},
			Status:    workflow.Status{Category: workflow.CategoryTodo},
			StartDate: start,
			DueDate:   due,
		},
		Children: children,
	}
}

func rangeOf(item Item) string {
	if !item.Scheduled() {
		return "unscheduled"
	}
	return item.Start.Format("2006-01-02") + ".." + item.Due.Format("2006-01-02")
}

func find(items []Item, key string) Item {
	for _, item := range Flatten(items) {
		if item.Issue.Key == key {
			return item
		}
	}
	return Item{}
}

func TestRollupDerivesParentDates(t *testing.T) {
	tree := []issue.Node{
		node("PR-1", issue.LevelInitiative, nil, nil,
			node("PR-2", issue.LevelEpic, nil, nil,
				node("PR-3", issue.LevelStandard, date("2026-03-10"), date("2026-03-20")),
				node("PR-4", issue.LevelStandard, date("2026-03-02"), date("2026-03-08")),
			),
		),
	}

	items := FromTree(tree)

	if got := rangeOf(find(items, "PR-2")); got != "2026-03-02..2026-03-20" {
		t.Errorf("epic = %s, want the span of its children", got)
	}
	// A grandparent sees its grandchildren through its children.
	if got := rangeOf(find(items, "PR-1")); got != "2026-03-02..2026-03-20" {
		t.Errorf("initiative = %s, want the span of everything beneath it", got)
	}
	if !find(items, "PR-1").Derived || !find(items, "PR-2").Derived {
		t.Error("derived ranges are not marked as derived, so the client cannot tell them apart")
	}
	if find(items, "PR-3").Derived {
		t.Error("a leaf with its own dates is marked derived")
	}
}

func TestRollupLeavesExplicitDatesAlone(t *testing.T) {
	tree := []issue.Node{
		node("PR-1", issue.LevelEpic, date("2026-01-01"), date("2026-06-30"),
			node("PR-2", issue.LevelStandard, date("2026-03-10"), date("2026-03-20")),
		),
	}

	items := FromTree(tree)
	if got := rangeOf(find(items, "PR-1")); got != "2026-01-01..2026-06-30" {
		t.Errorf("epic = %s, want the dates it was given", got)
	}
	if find(items, "PR-1").Derived {
		t.Error("an epic with its own dates is marked derived")
	}
}

// A half-scheduled parent keeps the end it was given and fills in the other.
func TestRollupFillsOnlyTheMissingEnd(t *testing.T) {
	tree := []issue.Node{
		node("PR-1", issue.LevelEpic, date("2026-01-01"), nil,
			node("PR-2", issue.LevelStandard, date("2026-03-10"), date("2026-03-20")),
		),
	}

	items := FromTree(tree)
	if got := rangeOf(find(items, "PR-1")); got != "2026-01-01..2026-03-20" {
		t.Errorf("epic = %s, want its own start and its children's end", got)
	}
}

func TestRollupOfNothingScheduled(t *testing.T) {
	tree := []issue.Node{
		node("PR-1", issue.LevelEpic, nil, nil, node("PR-2", issue.LevelStandard, nil, nil)),
	}

	items := FromTree(tree)
	if find(items, "PR-1").Scheduled() {
		t.Error("an epic whose children have no dates was given some anyway")
	}
	if got := Unscheduled(items); got != 2 {
		t.Errorf("unscheduled = %d, want 2", got)
	}
}

func TestCheckBlockedTooEarly(t *testing.T) {
	tree := []issue.Node{
		node("PR-1", issue.LevelStandard, date("2026-03-01"), date("2026-03-10")),
		node("PR-2", issue.LevelStandard, date("2026-03-05"), date("2026-03-12")),
		node("PR-3", issue.LevelStandard, date("2026-03-11"), date("2026-03-20")),
	}
	items := FromTree(tree)

	warnings := Check(items, []issue.Blocker{
		{BlockerKey: "PR-1", BlockedKey: "PR-2"},
		{BlockerKey: "PR-1", BlockedKey: "PR-3"},
	})

	if len(warnings) != 1 {
		t.Fatalf("warnings = %+v, want only the overlapping pair", warnings)
	}
	if warnings[0].IssueKey != "PR-2" || warnings[0].Kind != WarnBlockedTooEarly {
		t.Errorf("warning = %+v, want PR-2 blocked too early", warnings[0])
	}
	// The message has to name the date and the blocker, or it is not actionable.
	if !strings.Contains(warnings[0].Message, "PR-1") {
		t.Errorf("message %q does not name the blocker", warnings[0].Message)
	}
}

// Starting the same day a blocker ends is still too early: the blocker is not
// finished until the end of that day.
func TestCheckTreatsSameDayAsTooEarly(t *testing.T) {
	tree := []issue.Node{
		node("PR-1", issue.LevelStandard, date("2026-03-01"), date("2026-03-10")),
		node("PR-2", issue.LevelStandard, date("2026-03-10"), date("2026-03-20")),
	}
	warnings := Check(FromTree(tree), []issue.Blocker{{BlockerKey: "PR-1", BlockedKey: "PR-2"}})
	if len(warnings) != 1 {
		t.Errorf("warnings = %+v, want one", warnings)
	}
}

func TestCheckChildOutsideItsParent(t *testing.T) {
	tree := []issue.Node{
		node("PR-1", issue.LevelEpic, date("2026-03-01"), date("2026-03-31"),
			node("PR-2", issue.LevelStandard, date("2026-03-10"), date("2026-04-15")),
			node("PR-3", issue.LevelStandard, date("2026-03-10"), date("2026-03-20")),
		),
	}
	warnings := Check(FromTree(tree), nil)

	if len(warnings) != 1 || warnings[0].IssueKey != "PR-2" || warnings[0].Kind != WarnOutsideParent {
		t.Fatalf("warnings = %+v, want only PR-2 outside its parent", warnings)
	}
}

// A derived parent is made of its children, so it cannot disagree with them.
func TestCheckIgnoresDerivedParents(t *testing.T) {
	tree := []issue.Node{
		node("PR-1", issue.LevelEpic, nil, nil,
			node("PR-2", issue.LevelStandard, date("2026-03-10"), date("2026-04-15")),
		),
	}
	if warnings := Check(FromTree(tree), nil); len(warnings) != 0 {
		t.Errorf("warnings = %+v, want none", warnings)
	}
}

func TestCheckHalfScheduled(t *testing.T) {
	tree := []issue.Node{node("PR-1", issue.LevelStandard, date("2026-03-01"), nil)}
	warnings := Check(FromTree(tree), nil)

	if len(warnings) != 1 || warnings[0].Kind != WarnHalfScheduled {
		t.Errorf("warnings = %+v, want one half-scheduled", warnings)
	}
}

// An unscheduled issue is not a disagreement, it is work nobody has planned.
func TestCheckSaysNothingAboutUnscheduledWork(t *testing.T) {
	tree := []issue.Node{
		node("PR-1", issue.LevelStandard, nil, nil),
		node("PR-2", issue.LevelStandard, date("2026-03-01"), date("2026-03-10")),
	}
	if warnings := Check(FromTree(tree), []issue.Blocker{{BlockerKey: "PR-1", BlockedKey: "PR-2"}}); len(warnings) != 0 {
		t.Errorf("warnings = %+v, want none", warnings)
	}
}

func TestWindowCoversTheWorkWithRoomAround(t *testing.T) {
	tree := []issue.Node{node("PR-1", issue.LevelStandard, date("2026-03-10"), date("2026-03-20"))}
	from, to := Window(FromTree(tree), *date("2026-03-15"))

	if from.Format("2006-01-02") != "2026-03-03" || to.Format("2006-01-02") != "2026-03-27" {
		t.Errorf("window = %s to %s, want a week either side", from.Format("2006-01-02"), to.Format("2006-01-02"))
	}
}

// Today is the one date a person always looks for, so it is always on screen.
func TestWindowAlwaysIncludesToday(t *testing.T) {
	tree := []issue.Node{node("PR-1", issue.LevelStandard, date("2026-03-10"), date("2026-03-20"))}

	from, to := Window(FromTree(tree), *date("2026-09-01"))
	if !from.Before(*date("2026-03-10")) || !to.After(*date("2026-08-31")) {
		t.Errorf("window %s to %s does not cover both the work and today", from, to)
	}

	from, to = Window(FromTree(tree), *date("2025-01-01"))
	if !from.Before(*date("2025-01-01")) || !to.After(*date("2026-03-20")) {
		t.Errorf("window %s to %s does not reach back to today", from, to)
	}
}

func TestWindowOfAnEmptyPlan(t *testing.T) {
	from, to := Window(nil, *date("2026-03-15"))
	if from.After(*date("2026-03-01")) || to.Before(*date("2026-04-15")) {
		t.Errorf("empty window = %s to %s, want a stretch around today", from, to)
	}
}

func TestFlattenIsDisplayOrder(t *testing.T) {
	tree := []issue.Node{
		node("PR-1", issue.LevelEpic, nil, nil,
			node("PR-2", issue.LevelStandard, nil, nil,
				node("PR-3", issue.LevelSubtask, nil, nil),
			),
		),
		node("PR-4", issue.LevelStandard, nil, nil),
	}

	var keys []string
	var depths []int
	for _, item := range Flatten(FromTree(tree)) {
		keys = append(keys, item.Issue.Key)
		depths = append(depths, item.Depth)
	}

	want := []string{"PR-1", "PR-2", "PR-3", "PR-4"}
	for i, key := range want {
		if keys[i] != key {
			t.Fatalf("order = %v, want %v", keys, want)
		}
	}
	wantDepths := []int{0, 1, 2, 0}
	for i, depth := range wantDepths {
		if depths[i] != depth {
			t.Errorf("depths = %v, want %v", depths, wantDepths)
		}
	}
}
