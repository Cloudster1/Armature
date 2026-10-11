//go:build integration

package test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/workflow"
)

// branch is an epic with a story under it and a subtask under that: three
// levels, so a move that only carries direct children leaves one behind.
type branch struct {
	epic, story, subtask *issue.Issue
}

func (h *harness) buildBranch(t *testing.T, ws *workspace) branch {
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
	epic := create("the epic", bootstrap.TypeEpic, "")
	story := create("the story", bootstrap.TypeStory, epic.Key)
	subtask := create("the step", bootstrap.TypeSubtask, story.Key)
	return branch{epic, story, subtask}
}

func (h *harness) moveTarget(t *testing.T, ws *workspace, name string) *project.Project {
	t.Helper()
	target, _, err := ws.projects.Create(ws.ctx, project.CreateInput{Name: name, Key: "MV" + strings.ToUpper(uuid.New().String()[:4])}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create target project: %v", err)
	}
	return target
}

// crossProjectParents counts the issues among ids whose parent is in another
// project, read as the superuser so nothing is hidden from the count.
func (h *harness) crossProjectParents(t *testing.T, ids ...uuid.UUID) int {
	t.Helper()
	var n int
	if err := h.super.QueryRow(context.Background(), `
		SELECT count(*) FROM issue c JOIN issue p ON p.id = c.parent_id
		WHERE c.id = ANY($1) AND c.project_id <> p.project_id`, ids).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Moving an epic carries every level under it; each issue gets a key, a status
// and a history entry there, and its old key still finds it.
func TestMovingAnIssueCarriesItsWholeSubtree(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "subtree")
	br := h.buildBranch(t, ws)
	br.subtask = ws.move(t, h, br.subtask.Key, "Start progress")
	target := h.moveTarget(t, ws, "Subtree target")

	moved, _, err := ws.issues.Move(ws.ctx, br.epic.Key, issue.MoveInput{ProjectKey: target.Key}, ws.actor)
	if err != nil {
		t.Fatalf("move the epic: %v", err)
	}
	if moved.ProjectKey != target.Key {
		t.Fatalf("the epic is in %s, want %s", moved.ProjectKey, target.Key)
	}

	newKeys := map[string]bool{}
	for _, before := range []*issue.Issue{br.epic, br.story, br.subtask} {
		after, err := ws.issues.ByKey(ws.ctx, before.Key)
		if err != nil || after.ID != before.ID {
			t.Fatalf("the old key %s no longer finds its issue: %v", before.Key, err)
		}
		if after.ProjectKey != target.Key || !strings.HasPrefix(after.Key, target.Key+"-") {
			t.Errorf("%s is now %s in %s, want a key in %s", before.Key, after.Key, after.ProjectKey, target.Key)
		}
		if newKeys[after.Key] {
			t.Errorf("%s was given twice", after.Key)
		}
		newKeys[after.Key] = true
		if after.Status.ID != before.Status.ID {
			t.Errorf("%s went from %s to %s, want its status kept", before.Key, before.Status.Name, after.Status.Name)
		}
		var steps []uuid.UUID
		if err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			wf, err := ws.store.ForIssueType(ctx, tx, target.ID, after.Type.ID)
			if err != nil {
				return err
			}
			for _, step := range wf.Steps {
				steps = append(steps, step.Status.ID)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(steps, after.Status.ID) {
			t.Errorf("%s is in %s, which its workflow in %s does not have", after.Key, after.Status.Name, target.Key)
		}
		history, err := ws.issues.History(ws.ctx, after.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(history) == 0 || len(history[0].Changes) < 2 ||
			history[0].Changes[0] != (issue.Change{Field: "project", From: ws.project.Key, To: target.Key}) ||
			history[0].Changes[1] != (issue.Change{Field: "key", From: before.Key, To: after.Key}) {
			t.Errorf("%s history = %+v, want the project and key changes on top", after.Key, history)
		}
	}

	story, _ := ws.issues.ByKey(ws.ctx, br.story.Key)
	subtask, _ := ws.issues.ByKey(ws.ctx, br.subtask.Key)
	if story.ParentKey != moved.Key || subtask.ParentKey != story.Key {
		t.Errorf("parents = %s under %s, %s under %s; want the moved keys", story.Key, story.ParentKey, subtask.Key, subtask.ParentKey)
	}

	t.Run("no parent is left in another project, read straight from the table", func(t *testing.T) {
		if n := h.crossProjectParents(t, br.epic.ID, br.story.ID, br.subtask.ID); n != 0 {
			t.Errorf("%d issues of the moved branch hang off a parent in another project", n)
		}
		var left int
		if err := h.super.QueryRow(context.Background(),
			`SELECT count(*) FROM issue WHERE id = ANY($1) AND project_id <> $2`,
			[]uuid.UUID{br.epic.ID, br.story.ID, br.subtask.ID}, target.ID).Scan(&left); err != nil {
			t.Fatal(err)
		}
		if left != 0 {
			t.Errorf("%d issues of the branch stayed in %s", left, ws.project.Key)
		}
	})

	t.Run("the database refuses to put one of them back on its own", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(),
			`UPDATE issue SET project_id = $1 WHERE id = $2`, ws.project.ID, br.subtask.ID)
		if err == nil || !strings.Contains(err.Error(), "same project") {
			t.Errorf("error = %v, want the same-project guard", err)
		}
	})
}

// A descendant that cannot land in the target stops the whole move before
// anything is written, and the refusal names it.
func TestAMoveIsRefusedWhenPartOfTheSubtreeCannotCome(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "stranded")
	br := h.buildBranch(t, ws)
	br.subtask = ws.move(t, h, br.subtask.Key, "Start progress")
	target := h.moveTarget(t, ws, "Strict target")

	// The target runs subtasks through a workflow that has no In Progress.
	subtaskFlow := h.straightThrough(t, ws, "Subtask flow", bootstrap.StatusToDo, bootstrap.StatusDone)
	subtaskType := h.issueTypeID(t, ws, bootstrap.TypeSubtask)
	scheme, _, err := ws.admin.CreateScheme(ws.ctx, workflow.SchemeInput{
		Name:  "Strict subtasks",
		Items: []workflow.SchemeItemInput{{IssueTypeID: &subtaskType, WorkflowID: subtaskFlow.ID}},
	}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.projects.SetWorkflowScheme(ws.ctx, target.Key, &scheme.ID, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}

	_, _, err = ws.issues.Move(ws.ctx, br.epic.Key, issue.MoveInput{ProjectKey: target.Key}, ws.actor)
	if err == nil {
		t.Fatal("the epic moved although its subtask has no status in the target")
	}
	if msg := err.Error(); !strings.Contains(msg, br.subtask.Key) || !strings.Contains(msg, bootstrap.StatusInProgress) || !strings.HasSuffix(msg, ".") {
		t.Errorf("refusal = %q, want a sentence naming %s and its status", msg, br.subtask.Key)
	}

	for _, before := range []*issue.Issue{br.epic, br.story, br.subtask} {
		after, err := ws.issues.ByKey(ws.ctx, before.Key)
		if err != nil {
			t.Fatal(err)
		}
		if after.Key != before.Key || after.ProjectID != ws.project.ID || after.Status.ID != before.Status.ID {
			t.Errorf("%s became %s in %s after a refused move", before.Key, after.Key, after.ProjectKey)
		}
		if history, _ := ws.issues.History(ws.ctx, before.Key); len(history) > 0 && history[0].Changes[0].Field == "project" {
			t.Errorf("%s has a move in its history after a refused move", before.Key)
		}
	}
	// No key was spent either: the target's first issue is still number one.
	first, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: target.Key, Summary: "first"}, ws.actor)
	if err != nil {
		t.Fatal(err)
	}
	if first.Key != target.Key+"-1" {
		t.Errorf("the target's first issue is %s, want %s-1", first.Key, target.Key)
	}

	// Back in a status the target's subtask workflow has, the branch moves.
	ws.move(t, h, br.subtask.Key, "Stop progress")
	if _, _, err := ws.issues.Move(ws.ctx, br.epic.Key, issue.MoveInput{ProjectKey: target.Key}, ws.actor); err != nil {
		t.Fatalf("move once the subtask can come: %v", err)
	}
	subtask, err := ws.issues.ByKey(ws.ctx, br.subtask.Key)
	if err != nil || subtask.ProjectID != target.ID || subtask.Status.Name != bootstrap.StatusToDo {
		t.Errorf("subtask after the move = %+v, %v", subtask, err)
	}
	if n := h.crossProjectParents(t, br.epic.ID, br.story.ID, br.subtask.ID); n != 0 {
		t.Errorf("%d issues of the moved branch hang off a parent in another project", n)
	}
}
