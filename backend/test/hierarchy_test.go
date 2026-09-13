//go:build integration

package test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
)

// tree builds an initiative, an epic under it, a story under that and a subtask
// under that, which is every level the hierarchy has.
type tree struct {
	initiative, epic, story, subtask *issue.Issue
}

func (h *harness) buildTree(t *testing.T, ws *workspace) tree {
	t.Helper()
	create := func(summary, typeName, parent string) *issue.Issue {
		created, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
			ProjectKey: ws.project.Key,
			Summary:    summary,
			TypeID:     h.issueTypeID(t, ws, typeName),
			ParentKey:  parent,
		}, ws.actor)
		if err != nil {
			t.Fatalf("create %s %q: %v", typeName, summary, err)
		}
		return created
	}

	initiative := create("the goal", bootstrap.TypeInitiative, "")
	epic := create("the epic", bootstrap.TypeEpic, initiative.Key)
	story := create("the story", bootstrap.TypeStory, epic.Key)
	subtask := create("the step", bootstrap.TypeSubtask, story.Key)
	return tree{initiative, epic, story, subtask}
}

func TestHierarchyLevels(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "levels")

	t.Run("every level can hold the one below it", func(t *testing.T) {
		tr := h.buildTree(t, ws)

		for _, c := range []struct{ child, parent *issue.Issue }{
			{tr.epic, tr.initiative},
			{tr.story, tr.epic},
			{tr.subtask, tr.story},
		} {
			if c.child.ParentKey != c.parent.Key {
				t.Errorf("%s's parent = %q, want %q", c.child.Key, c.child.ParentKey, c.parent.Key)
			}
			if c.child.Parent == nil || c.child.Parent.Summary != c.parent.Summary {
				t.Errorf("%s carries no parent summary; a breadcrumb would need a second request", c.child.Key)
			}
		}
	})

	t.Run("a level cannot be skipped", func(t *testing.T) {
		tr := h.buildTree(t, ws)

		_, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
			ProjectKey: ws.project.Key,
			Summary:    "straight under the initiative",
			TypeID:     h.issueTypeID(t, ws, bootstrap.TypeStory),
			ParentKey:  tr.initiative.Key,
		}, ws.actor)
		if !errors.Is(err, issue.ErrParentLevel) {
			t.Errorf("error = %v, want ErrParentLevel", err)
		}
		// The refusal has to name both types; "invalid parent" tells the user
		// nothing about what to do instead.
		if err != nil && !strings.Contains(err.Error(), "initiative") {
			t.Errorf("message %q does not say what the parent is", err)
		}
	})

	t.Run("two issues at the same level cannot hold each other", func(t *testing.T) {
		tr := h.buildTree(t, ws)
		other := h.buildTree(t, ws)

		if _, _, err := ws.issues.SetParent(ws.ctx, tr.epic.Key, &other.epic.Key, ws.actor); !errors.Is(err, issue.ErrParentLevel) {
			t.Errorf("error = %v, want ErrParentLevel", err)
		}
	})

	t.Run("an issue cannot be its own parent", func(t *testing.T) {
		tr := h.buildTree(t, ws)
		if _, _, err := ws.issues.SetParent(ws.ctx, tr.story.Key, &tr.story.Key, ws.actor); !errors.Is(err, issue.ErrParentIsSelf) {
			t.Errorf("error = %v, want ErrParentIsSelf", err)
		}
	})

	t.Run("a subtask cannot be left with nothing above it", func(t *testing.T) {
		tr := h.buildTree(t, ws)
		if _, _, err := ws.issues.SetParent(ws.ctx, tr.subtask.Key, nil, ws.actor); !errors.Is(err, issue.ErrNeedsParent) {
			t.Errorf("error = %v, want ErrNeedsParent", err)
		}
	})

	t.Run("a parent in another project is refused", func(t *testing.T) {
		tr := h.buildTree(t, ws)

		elsewhere, _, err := ws.projects.Create(ws.ctx, project.CreateInput{
			Name: "somewhere else",
			Key:  strings.ToUpper("OT" + uuid.New().String()[:3]),
		}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		stray, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
			ProjectKey: elsewhere.Key,
			Summary:    "somewhere else",
			TypeID:     h.issueTypeID(t, ws, bootstrap.TypeStory),
		}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}

		if _, _, err := ws.issues.SetParent(ws.ctx, stray.Key, &tr.epic.Key, ws.actor); !errors.Is(err, issue.ErrParentOtherProject) {
			t.Errorf("error = %v, want ErrParentOtherProject", err)
		}
	})
}

func TestHierarchyReparenting(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "reparent")

	t.Run("a story moves between epics and the change is recorded", func(t *testing.T) {
		tr := h.buildTree(t, ws)
		second, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
			ProjectKey: ws.project.Key,
			Summary:    "the other epic",
			TypeID:     h.issueTypeID(t, ws, bootstrap.TypeEpic),
			ParentKey:  tr.initiative.Key,
		}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}

		moved, _, err := ws.issues.SetParent(ws.ctx, tr.story.Key, &second.Key, ws.actor)
		if err != nil {
			t.Fatalf("move the story: %v", err)
		}
		if moved.ParentKey != second.Key {
			t.Errorf("parent = %q, want %q", moved.ParentKey, second.Key)
		}

		history, err := ws.issues.History(ws.ctx, tr.story.Key)
		if err != nil {
			t.Fatal(err)
		}
		var recorded *issue.Change
		for _, entry := range history {
			for i, c := range entry.Changes {
				if c.Field == "parent" {
					recorded = &entry.Changes[i]
				}
			}
		}
		if recorded == nil {
			t.Fatal("the move is not in the changelog")
		}
		if recorded.From != tr.epic.Key || recorded.To != second.Key {
			t.Errorf("change = %s -> %s, want %s -> %s", recorded.From, recorded.To, tr.epic.Key, second.Key)
		}
	})

	t.Run("a story can be taken out of its epic", func(t *testing.T) {
		tr := h.buildTree(t, ws)

		detached, _, err := ws.issues.SetParent(ws.ctx, tr.story.Key, nil, ws.actor)
		if err != nil {
			t.Fatalf("detach: %v", err)
		}
		if detached.ParentID != nil || detached.Parent != nil {
			t.Errorf("parent = %v, want none", detached.Parent)
		}

		// The subtask under it comes along rather than being orphaned.
		children, err := ws.issues.Children(ws.ctx, tr.story.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(children) != 1 || children[0].Key != tr.subtask.Key {
			t.Errorf("children = %v, want the subtask to have moved with its parent", children)
		}
	})

	t.Run("setting the parent it already has changes nothing", func(t *testing.T) {
		tr := h.buildTree(t, ws)

		before, err := ws.issues.History(ws.ctx, tr.story.Key)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := ws.issues.SetParent(ws.ctx, tr.story.Key, &tr.epic.Key, ws.actor); err != nil {
			t.Fatal(err)
		}
		after, err := ws.issues.History(ws.ctx, tr.story.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before) {
			t.Errorf("history grew from %d to %d entries for a move that did not happen", len(before), len(after))
		}
	})
}

func TestHierarchyReading(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "reading")

	t.Run("ancestors read from the top down", func(t *testing.T) {
		tr := h.buildTree(t, ws)

		got, err := ws.issues.Hierarchy(ws.ctx, tr.subtask.Key)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{tr.initiative.Key, tr.epic.Key, tr.story.Key}
		if len(got.Ancestors) != len(want) {
			t.Fatalf("ancestors = %d, want %d", len(got.Ancestors), len(want))
		}
		for i, key := range want {
			if got.Ancestors[i].Key != key {
				t.Errorf("ancestor %d = %s, want %s", i, got.Ancestors[i].Key, key)
			}
		}
	})

	t.Run("progress counts direct children by category", func(t *testing.T) {
		tr := h.buildTree(t, ws)
		for i := 0; i < 2; i++ {
			if _, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
				ProjectKey: ws.project.Key,
				Summary:    "another step",
				TypeID:     h.issueTypeID(t, ws, bootstrap.TypeSubtask),
				ParentKey:  tr.story.Key,
			}, ws.actor); err != nil {
				t.Fatal(err)
			}
		}

		// Move one subtask along so the three categories are all represented.
		start := h.transitionID(t, ws, tr.subtask.Key, "Start progress")
		if _, _, err := ws.issues.Transition(ws.ctx, tr.subtask.Key, issue.TransitionInput{TransitionID: start}, ws.actor); err != nil {
			t.Fatal(err)
		}

		got, err := ws.issues.Hierarchy(ws.ctx, tr.story.Key)
		if err != nil {
			t.Fatal(err)
		}
		if got.Progress.Total != 3 || got.Progress.InProgress != 1 || got.Progress.Todo != 2 {
			t.Errorf("progress = %+v, want 3 total with 1 in progress and 2 to do", got.Progress)
		}
	})

	t.Run("the child types offered are the ones the server accepts", func(t *testing.T) {
		tr := h.buildTree(t, ws)

		epicView, err := ws.issues.Hierarchy(ws.ctx, tr.epic.Key)
		if err != nil {
			t.Fatal(err)
		}
		for _, option := range epicView.ChildTypes {
			if option.Level != issue.LevelStandard {
				t.Errorf("an epic is offered %s at level %d, which it cannot hold", option.Name, option.Level)
			}
		}
		if len(epicView.ChildTypes) == 0 {
			t.Error("an epic is offered nothing to put under it")
		}

		subtaskView, err := ws.issues.Hierarchy(ws.ctx, tr.subtask.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(subtaskView.ChildTypes) != 0 {
			t.Errorf("a subtask is offered %v to put under it; nothing goes below a subtask", subtaskView.ChildTypes)
		}
	})

	t.Run("a project's tree nests and rolls up", func(t *testing.T) {
		ws := h.newWorkspace(t, "treeview")
		tr := h.buildTree(t, ws)

		forest, err := ws.issues.Tree(ws.ctx, ws.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(forest) != 1 || forest[0].Issue.Key != tr.initiative.Key {
			t.Fatalf("roots = %v, want just the initiative", forest)
		}
		epic := forest[0].Children
		if len(epic) != 1 || epic[0].Issue.Key != tr.epic.Key {
			t.Fatalf("initiative children = %v, want the epic", epic)
		}
		story := epic[0].Children
		if len(story) != 1 || story[0].Issue.Key != tr.story.Key {
			t.Fatalf("epic children = %v, want the story", story)
		}
		if len(story[0].Children) != 1 || story[0].Children[0].Issue.Key != tr.subtask.Key {
			t.Fatalf("story children = %v, want the subtask", story[0].Children)
		}
		if story[0].Progress.Total != 1 {
			t.Errorf("story progress = %+v, want one child counted", story[0].Progress)
		}
	})
}

func TestHierarchyAndIssueType(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "typechange")

	t.Run("a type change that would strand the children is refused", func(t *testing.T) {
		tr := h.buildTree(t, ws)

		subtaskType := h.issueTypeID(t, ws, bootstrap.TypeSubtask)
		if _, _, err := ws.issues.Update(ws.ctx, tr.story.Key, issue.UpdateInput{TypeID: &subtaskType}, ws.actor); err == nil {
			t.Error("a story with a subtask under it was turned into a subtask")
		} else if !errors.Is(err, issue.ErrChildrenInTheWay) && !errors.Is(err, issue.ErrParentLevel) {
			t.Errorf("error = %v, want a refusal naming the children or the parent", err)
		}
	})

	t.Run("a type change that would strand the parent is refused", func(t *testing.T) {
		tr := h.buildTree(t, ws)

		epicType := h.issueTypeID(t, ws, bootstrap.TypeEpic)
		if _, _, err := ws.issues.Update(ws.ctx, tr.story.Key, issue.UpdateInput{TypeID: &epicType}, ws.actor); !errors.Is(err, issue.ErrParentLevel) {
			t.Errorf("error = %v, want ErrParentLevel: an epic cannot sit under an epic", err)
		}
	})

	t.Run("a type change within the level is allowed", func(t *testing.T) {
		tr := h.buildTree(t, ws)

		bugType := h.issueTypeID(t, ws, bootstrap.TypeBug)
		updated, _, err := ws.issues.Update(ws.ctx, tr.story.Key, issue.UpdateInput{TypeID: &bugType}, ws.actor)
		if err != nil {
			t.Fatalf("story to bug: %v", err)
		}
		if updated.Type.Name != bootstrap.TypeBug || updated.ParentKey != tr.epic.Key {
			t.Errorf("issue = %s under %s, want a bug still under its epic", updated.Type.Name, updated.ParentKey)
		}
	})
}

// The service writes the readable refusals; the trigger is what makes the rule
// true for anything that reaches the table another way.
func TestHierarchyIsEnforcedByTheDatabase(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "trigger")
	tr := h.buildTree(t, ws)

	t.Run("a level cannot be skipped by writing the column directly", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(),
			`UPDATE issue SET parent_id = $1 WHERE id = $2`, tr.initiative.ID, tr.story.ID)
		if err == nil {
			t.Fatal("the trigger let a story hang straight off an initiative")
		}
		if !strings.Contains(err.Error(), "cannot be the parent") {
			t.Errorf("error = %v, want the hierarchy guard", err)
		}
	})

	t.Run("a subtask cannot be orphaned by writing the column directly", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(),
			`UPDATE issue SET parent_id = NULL WHERE id = $1`, tr.subtask.ID)
		if err == nil {
			t.Fatal("the trigger let a subtask loose")
		}
	})

	t.Run("a type in use cannot change level", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(),
			`UPDATE issue_type SET hierarchy_level = 0 WHERE id = $1`,
			h.issueTypeID(t, ws, bootstrap.TypeEpic))
		if err == nil {
			t.Fatal("the epic type moved level while issues were using it")
		}
	})

	t.Run("the subtask flag follows the level rather than disagreeing with it", func(t *testing.T) {
		var flagged bool
		err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `
				SELECT bool_and(is_subtask = (hierarchy_level < 0)) FROM issue_type`).Scan(&flagged)
		})
		if err != nil {
			t.Fatal(err)
		}
		if !flagged {
			t.Error("some issue type's subtask flag disagrees with its level")
		}
	})
}

func TestHierarchyStaysInsideTheTenant(t *testing.T) {
	h := newHarness(t)
	alpha := h.newWorkspace(t, "halpha")
	beta := h.newWorkspace(t, "hbeta")

	alphaTree := h.buildTree(t, alpha)
	betaStory, _, err := beta.issues.Create(beta.ctx, issue.CreateInput{
		ProjectKey: beta.project.Key,
		Summary:    "beta's own work",
		TypeID:     h.issueTypeID(t, beta, bootstrap.TypeStory),
	}, beta.actor)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("another tenant's issue cannot become a parent", func(t *testing.T) {
		_, _, err := beta.issues.SetParent(beta.ctx, betaStory.Key, &alphaTree.epic.Key, beta.actor)
		// Not a level error and not a permission error: from beta's side that
		// issue simply is not there.
		if !errors.Is(err, issue.ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
	})

	t.Run("another tenant's tree is not readable", func(t *testing.T) {
		forest, err := beta.issues.Tree(beta.ctx, alpha.project.Key)
		if err == nil && len(forest) > 0 {
			t.Errorf("beta read %d roots of alpha's project", len(forest))
		}
	})

	t.Run("each tenant sees only its own tree", func(t *testing.T) {
		forest, err := alpha.issues.Tree(alpha.ctx, alpha.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(forest) != 1 || forest[0].Issue.Key != alphaTree.initiative.Key {
			t.Errorf("alpha's roots = %v, want just its own initiative", forest)
		}
	})
}

// The board shows the work a team moves. Before the hierarchy that meant
// "anything without a parent", which would now hide every story in an epic.
func TestBoardShowsIssuesInsideEpics(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "boardtree")
	tr := h.buildTree(t, ws)

	b, err := ws.boards.ForProject(ws.ctx, ws.project.Key)
	if err != nil {
		t.Fatal(err)
	}

	seen := map[string]bool{}
	for _, lane := range b.Swimlanes {
		for _, card := range lane.Cards {
			seen[card.Key] = true
			if card.Key == tr.story.Key {
				if card.Parent == nil || card.Parent.Key != tr.epic.Key {
					t.Errorf("the story's card does not name its epic: %+v", card.Parent)
				}
			}
		}
	}
	for _, card := range b.Unmapped {
		seen[card.Key] = true
	}

	if !seen[tr.story.Key] {
		t.Error("a story inside an epic vanished from the board")
	}
	if !seen[tr.epic.Key] {
		t.Error("the epic itself is not on the board")
	}
	if seen[tr.subtask.Key] {
		t.Error("a subtask is on the board; it moves with its parent")
	}
}
