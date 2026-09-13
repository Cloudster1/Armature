package issue

import (
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/workflow"
)

func TestCanParent(t *testing.T) {
	cases := []struct {
		name   string
		parent int
		child  int
		want   bool
	}{
		{"epic over a story", LevelEpic, LevelStandard, true},
		{"story over a subtask", LevelStandard, LevelSubtask, true},
		{"initiative over an epic", LevelInitiative, LevelEpic, true},
		{"initiative over a story skips a level", LevelInitiative, LevelStandard, false},
		{"story over a story", LevelStandard, LevelStandard, false},
		{"subtask over a story is upside down", LevelSubtask, LevelStandard, false},
		{"story over an epic is upside down", LevelStandard, LevelEpic, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CanParent(c.parent, c.child); got != c.want {
				t.Errorf("CanParent(%d, %d) = %v, want %v", c.parent, c.child, got, c.want)
			}
		})
	}
}

func TestNeedsParent(t *testing.T) {
	for level, want := range map[int]bool{
		LevelSubtask:    true,
		LevelStandard:   false,
		LevelEpic:       false,
		LevelInitiative: false,
	} {
		if got := NeedsParent(level); got != want {
			t.Errorf("NeedsParent(%d) = %v, want %v", level, got, want)
		}
	}
}

// A tree with a strict one-level rule cannot contain a cycle: every step up
// increases the level, and the levels are bounded.
func TestCanParentMakesCyclesImpossible(t *testing.T) {
	for level := LevelSubtask; level <= MaxLevel; level++ {
		if CanParent(level, level) {
			t.Errorf("level %d can parent itself, which allows a one-issue cycle", level)
		}
		for other := LevelSubtask; other <= MaxLevel; other++ {
			if CanParent(level, other) && CanParent(other, level) {
				t.Errorf("levels %d and %d can each parent the other", level, other)
			}
		}
	}
}

func TestProgressPercent(t *testing.T) {
	cases := []struct {
		name string
		p    Progress
		want int
	}{
		{"nothing underneath reports nothing", Progress{}, 0},
		{"half done", Progress{Total: 4, Done: 2}, 50},
		{"all done", Progress{Total: 3, Done: 3}, 100},
		{"rounds down rather than flattering", Progress{Total: 3, Done: 2}, 66},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.Percent(); got != c.want {
				t.Errorf("Percent() = %d, want %d", got, c.want)
			}
		})
	}
}

func TestArticle(t *testing.T) {
	for word, want := range map[string]string{
		"Epic":       "an epic",
		"Initiative": "an initiative",
		"Story":      "a story",
		"Bug":        "a bug",
		"":           "a",
	} {
		if got := article(word); got != want {
			t.Errorf("article(%q) = %q, want %q", word, got, want)
		}
	}
}

// mkIssue builds just enough of an issue for the forest tests.
func mkIssue(key string, level int, parent *uuid.UUID, category workflow.StatusCategory) Issue {
	return Issue{
		ID:       uuid.New(),
		Key:      key,
		Type:     TypeRef{Name: "T", Level: level},
		ParentID: parent,
		Status:   workflow.Status{Category: category},
	}
}

func TestBuildForest(t *testing.T) {
	epic := mkIssue("PR-1", LevelEpic, nil, workflow.CategoryTodo)
	storyA := mkIssue("PR-2", LevelStandard, &epic.ID, workflow.CategoryDone)
	storyB := mkIssue("PR-3", LevelStandard, &epic.ID, workflow.CategoryInProgress)
	subtask := mkIssue("PR-4", LevelSubtask, &storyA.ID, workflow.CategoryDone)
	loose := mkIssue("PR-5", LevelStandard, nil, workflow.CategoryTodo)

	// Deliberately out of order: a child before its parent must still find it.
	forest := BuildForest([]Issue{subtask, storyB, loose, epic, storyA})

	if len(forest) != 2 {
		t.Fatalf("roots = %d, want 2", len(forest))
	}
	if forest[0].Issue.Key != "PR-1" {
		t.Errorf("first root = %s, want the epic PR-1: higher levels sort first", forest[0].Issue.Key)
	}
	if forest[1].Issue.Key != "PR-5" {
		t.Errorf("second root = %s, want PR-5", forest[1].Issue.Key)
	}

	children := forest[0].Children
	if len(children) != 2 || children[0].Issue.Key != "PR-2" || children[1].Issue.Key != "PR-3" {
		t.Fatalf("epic children = %v, want PR-2 then PR-3", keysOf(children))
	}
	if got := forest[0].Progress; got.Total != 2 || got.Done != 1 || got.InProgress != 1 {
		t.Errorf("epic progress = %+v, want 2 total, 1 done, 1 in progress", got)
	}

	if len(children[0].Children) != 1 || children[0].Children[0].Issue.Key != "PR-4" {
		t.Errorf("story children = %v, want the subtask PR-4", keysOf(children[0].Children))
	}
	if got := children[0].Progress; got.Total != 1 || got.Done != 1 {
		t.Errorf("story progress = %+v, want 1 of 1 done", got)
	}
	// The epic's roll-up counts its own children, not its grandchildren, so a
	// finely split story cannot outweigh a coarse one.
	if forest[0].Progress.Total != 2 {
		t.Errorf("epic total = %d, want 2: grandchildren must not be counted", forest[0].Progress.Total)
	}
}

// A filtered list must still show everything it matched, even when the parent
// it hangs off was filtered out.
func TestBuildForestKeepsOrphans(t *testing.T) {
	absent := uuid.New()
	orphan := mkIssue("PR-9", LevelStandard, &absent, workflow.CategoryTodo)

	forest := BuildForest([]Issue{orphan})
	if len(forest) != 1 || forest[0].Issue.Key != "PR-9" {
		t.Fatalf("forest = %v, want the orphan as a root", keysOf(forest))
	}
}

// Sorting keys as text would put P-10 before P-2.
func TestBuildForestOrdersByIssueNumber(t *testing.T) {
	epic := mkIssue("PR-1", LevelEpic, nil, workflow.CategoryTodo)
	tenth := mkIssue("PR-10", LevelStandard, &epic.ID, workflow.CategoryTodo)
	second := mkIssue("PR-2", LevelStandard, &epic.ID, workflow.CategoryTodo)

	forest := BuildForest([]Issue{epic, tenth, second})
	if got := keysOf(forest[0].Children); got[0] != "PR-2" || got[1] != "PR-10" {
		t.Errorf("children = %v, want PR-2 before PR-10", got)
	}
}

func TestBuildForestOfNothing(t *testing.T) {
	forest := BuildForest(nil)
	if forest == nil {
		t.Fatal("forest is nil; an empty result must still marshal as []")
	}
	if len(forest) != 0 {
		t.Errorf("forest = %v, want empty", keysOf(forest))
	}
}

func keysOf(nodes []Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = n.Issue.Key
	}
	return out
}
