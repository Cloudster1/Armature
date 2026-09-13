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
	"github.com/armature/armature/backend/internal/workflow"
)

func (h *harness) loadWorkflow(t *testing.T, ws *workspace, id uuid.UUID) *workflow.Workflow {
	t.Helper()
	var out *workflow.Workflow
	if err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = ws.store.Load(ctx, tx, id)
		return err
	}); err != nil {
		t.Fatalf("load workflow: %v", err)
	}
	return out
}

func layoutOf(w *workflow.Workflow, statusID uuid.UUID) *workflow.Point {
	step, ok := w.StepByStatus(statusID)
	if !ok {
		return nil
	}
	return step.Layout
}

// The designer is only worth having if the picture comes back the way it was
// left, so where each status was drawn is part of the workflow.
func TestTheDesignerKeepsWhereStatusesWereDrawn(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "drawn")

	todo := h.statusID(t, ws, bootstrap.StatusToDo)
	progress := h.statusID(t, ws, bootstrap.StatusInProgress)
	done := h.statusID(t, ws, bootstrap.StatusDone)

	built, _, err := ws.admin.CreateWorkflow(ws.ctx, workflow.GraphInput{
		Name: "Drawn",
		Steps: []workflow.StepInput{
			{StatusID: todo, IsInitial: true, Layout: &workflow.Point{X: 40, Y: 80}},
			{StatusID: progress, Layout: &workflow.Point{X: 300, Y: 80}},
			{StatusID: done},
		},
		Transitions: []workflow.TransitionInput{
			{Name: "Start", FromStatusID: &todo, ToStatusID: progress},
		},
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if got := layoutOf(built, todo); got == nil || *got != (workflow.Point{X: 40, Y: 80}) {
		t.Errorf("To Do drawn at %v, want (40, 80)", got)
	}
	if got := layoutOf(built, done); got != nil {
		t.Errorf("Done drawn at %v, want it unplaced until somebody places it", got)
	}

	// Moving a status is a save with a new point; leaving the point out unplaces it.
	saved, _, err := ws.admin.SaveWorkflow(ws.ctx, built.ID, workflow.GraphInput{
		Name: "Drawn",
		Steps: []workflow.StepInput{
			{StatusID: todo, IsInitial: true, Layout: &workflow.Point{X: 60, Y: 120}},
			{StatusID: progress},
			{StatusID: done, Layout: &workflow.Point{X: 560, Y: 120}},
		},
		Transitions: []workflow.TransitionInput{
			{ID: built.Transitions[0].ID, Name: "Start", FromStatusID: &todo, ToStatusID: progress},
		},
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := layoutOf(saved, todo); got == nil || got.X != 60 {
		t.Errorf("To Do drawn at %v after the move, want x 60", got)
	}
	if got := layoutOf(saved, progress); got != nil {
		t.Errorf("In Progress drawn at %v, want it unplaced once its point was dropped", got)
	}
	if got := layoutOf(saved, done); got == nil || got.X != 560 {
		t.Errorf("Done drawn at %v, want x 560", got)
	}

	copied, _, err := ws.admin.CopyWorkflow(ws.ctx, built.ID, "Drawn again", ws.actor.UserID)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if got := layoutOf(copied, done); got == nil || *got != *layoutOf(saved, done) {
		t.Errorf("the copy drew Done at %v, want the original's %v", got, layoutOf(saved, done))
	}

	// Half a point is nothing anybody can draw, and the database says so.
	_, err = h.super.Exec(context.Background(), `
		UPDATE workflow_step SET layout_x = 10, layout_y = NULL WHERE workflow_id = $1 AND status_id = $2`,
		built.ID, todo)
	if err == nil || !strings.Contains(err.Error(), "workflow_step_layout_is_a_point") {
		t.Errorf("a lone coordinate was accepted (err = %v), want the check constraint to refuse it", err)
	}
}

func ruleTypes(t *workflow.Transition) []string {
	var out []string
	for _, r := range t.Rules {
		out = append(out, r.Type)
	}
	return out
}

// The designer edits rules in place, so what it sends is the whole set; an
// editor that never looked at the rules says nothing and changes nothing.
func TestSentRulesReplaceAndUnsentRulesStay(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "ruled")

	todo := h.statusID(t, ws, bootstrap.StatusToDo)
	done := h.statusID(t, ws, bootstrap.StatusDone)

	graph := func(id uuid.UUID, rules []workflow.RuleInput) workflow.GraphInput {
		return workflow.GraphInput{
			Name: "Ruled",
			Steps: []workflow.StepInput{
				{StatusID: todo, IsInitial: true},
				{StatusID: done},
			},
			Transitions: []workflow.TransitionInput{
				{ID: id, Name: "Finish", FromStatusID: &todo, ToStatusID: done, Rules: rules},
			},
		}
	}

	built, _, err := ws.admin.CreateWorkflow(ws.ctx, graph(uuid.Nil, []workflow.RuleInput{
		{Kind: workflow.KindValidator, Type: workflow.ValidatorCommentRequired},
	}), ws.actor.UserID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	finish := built.Transitions[0].ID

	t.Run("saying nothing about the rules keeps them", func(t *testing.T) {
		saved, _, err := ws.admin.SaveWorkflow(ws.ctx, built.ID, graph(finish, nil), ws.actor.UserID)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		if got := ruleTypes(&saved.Transitions[0]); len(got) != 1 || got[0] != workflow.ValidatorCommentRequired {
			t.Errorf("rules = %v, want the comment validator kept", got)
		}
	})

	t.Run("sending a set replaces what was there", func(t *testing.T) {
		saved, _, err := ws.admin.SaveWorkflow(ws.ctx, built.ID, graph(finish, []workflow.RuleInput{
			{Kind: workflow.KindPostFunction, Type: workflow.PostSetResolved},
			{Kind: workflow.KindPostFunction, Type: workflow.PostAddComment, Config: `{"text":"Done and dusted."}`},
		}), ws.actor.UserID)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		got := ruleTypes(&saved.Transitions[0])
		if len(got) != 2 || got[0] != workflow.PostSetResolved || got[1] != workflow.PostAddComment {
			t.Errorf("rules = %v, want the two post-functions in order", got)
		}
		if saved.Transitions[0].ID != finish {
			t.Errorf("the transition was re-made rather than kept")
		}
	})

	t.Run("sending an empty set clears them", func(t *testing.T) {
		saved, _, err := ws.admin.SaveWorkflow(ws.ctx, built.ID, graph(finish, []workflow.RuleInput{}), ws.actor.UserID)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		if got := ruleTypes(&saved.Transitions[0]); len(got) != 0 {
			t.Errorf("rules = %v, want none", got)
		}
		var count int
		if err := h.super.QueryRow(context.Background(), `
			SELECT count(*) FROM transition_rule WHERE transition_id = $1`, finish).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("%d rule rows survive in the database", count)
		}
	})

	t.Run("a rule the engine cannot run is refused by name", func(t *testing.T) {
		_, _, err := ws.admin.SaveWorkflow(ws.ctx, built.ID, graph(finish, []workflow.RuleInput{
			{Kind: workflow.KindCondition, Type: "condition.moon_phase"},
		}), ws.actor.UserID)
		if !errors.Is(err, workflow.ErrInvalid) {
			t.Fatalf("error = %v, want it refused as invalid", err)
		}
		if !strings.Contains(err.Error(), "Finish") || !strings.Contains(err.Error(), "condition.moon_phase") {
			t.Errorf("error = %q, want it to name the transition and the rule", err)
		}
		if got := ruleTypes(&h.loadWorkflow(t, ws, built.ID).Transitions[0]); len(got) != 0 {
			t.Errorf("a refused save changed the rules to %v", got)
		}
	})
}

// A status is the organization's, shared by every workflow, so it is coined
// once and kept unique by name whatever the case.
func TestAStatusIsCoinedOnce(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "coining")

	made, _, err := ws.admin.CreateStatus(ws.ctx, workflow.StatusInput{Name: " Reviewing ", Category: workflow.CategoryInProgress, Description: "Somebody else is looking."}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("coin a status: %v", err)
	}
	if made.Name != "Reviewing" || made.Category != workflow.CategoryInProgress || made.Position < 4 {
		t.Errorf("status = %+v, want the trimmed name, its category, and a place after the seeded four", made)
	}
	var listed []workflow.Status
	if err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		listed, err = workflow.NewStore().ListStatuses(ctx, tx)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 5 || listed[4].ID != made.ID {
		t.Errorf("statuses = %v, want the coined one last", listed)
	}

	t.Run("the same name in another case is refused", func(t *testing.T) {
		_, _, err := ws.admin.CreateStatus(ws.ctx, workflow.StatusInput{Name: "reviewing", Category: workflow.CategoryTodo}, ws.actor.UserID)
		if !errors.Is(err, workflow.ErrInvalid) || !strings.Contains(err.Error(), "already has a status called") {
			t.Errorf("err = %v, want the duplicate named", err)
		}
	})

	t.Run("a blank name or a made-up category is refused", func(t *testing.T) {
		if _, _, err := ws.admin.CreateStatus(ws.ctx, workflow.StatusInput{Name: "  ", Category: workflow.CategoryTodo}, ws.actor.UserID); !errors.Is(err, workflow.ErrInvalid) {
			t.Errorf("blank name: err = %v", err)
		}
		if _, _, err := ws.admin.CreateStatus(ws.ctx, workflow.StatusInput{Name: "Parked", Category: "paused"}, ws.actor.UserID); !errors.Is(err, workflow.ErrInvalid) {
			t.Errorf("bad category: err = %v", err)
		}
	})

	t.Run("and the database refuses the duplicate too", func(t *testing.T) {
		_, err := h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `INSERT INTO issue_status (org_id, name, category) VALUES (current_org_id(), 'REVIEWING', 'todo')`)
			return err
		})
		if err == nil {
			t.Error("the unique index let a second Reviewing through")
		}
	})

	t.Run("another organization may coin its own", func(t *testing.T) {
		other := h.newWorkspace(t, "coining-other")
		if _, _, err := other.admin.CreateStatus(other.ctx, workflow.StatusInput{Name: "Reviewing", Category: workflow.CategoryDone}, other.actor.UserID); err != nil {
			t.Errorf("another organization was refused its own Reviewing: %v", err)
		}
	})
}
