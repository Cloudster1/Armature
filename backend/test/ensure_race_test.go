//go:build integration

package test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/workflow"
)

// ensureWait bounds how long the test waits for the second making of a name to
// queue behind the first one, which has not committed yet.
const ensureWait = 10 * time.Second

// ensureHeldOpen holds one ensure open and commits it only once a second one of
// the same name, whose lookup cannot see the first, is waiting on it.
func ensureHeldOpen(t *testing.T, h *harness, ensure func(context.Context, db.DBTX) (uuid.UUID, error)) (first, second uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	open, err := h.cluster.Primary().Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = open.Rollback(ctx) }()
	var heldPID int
	if err := open.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&heldPID); err != nil {
		t.Fatalf("pid: %v", err)
	}
	if first, err = ensure(ctx, open); err != nil {
		t.Fatalf("the first making: %v", err)
	}

	type outcome struct {
		id  uuid.UUID
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		var id uuid.UUID
		_, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
			var err error
			id, err = ensure(ctx, tx)
			return err
		})
		done <- outcome{id, err}
	}()

	deadline := time.Now().Add(ensureWait)
	for {
		var waiting int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))`, heldPID).Scan(&waiting); err != nil {
			t.Fatalf("look for the waiting making: %v", err)
		}
		if waiting > 0 {
			break
		}
		select {
		case got := <-done:
			t.Fatalf("the second making finished without meeting the open one: %v %v", got.id, got.err)
		case <-time.After(20 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("the second making never waited for the open one")
		}
	}
	if err := open.Commit(ctx); err != nil {
		t.Fatalf("commit the first making: %v", err)
	}
	got := <-done
	if got.err != nil {
		t.Fatalf("the second making of the same name was refused: %v", got.err)
	}
	return first, got.id
}

// Two projects made from one template at the same moment, the first of their
// kind, both get made: the second uses what the first brought (#458).
func TestWhatATemplateBringsIsMadeOnceWhenTwoMakeItAtOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "ensuring")

	countNamed := func(t *testing.T, table, name string) int {
		t.Helper()
		var n int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM `+table+` WHERE lower(name) = lower($1)`, name).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}
	same := func(t *testing.T, table, name string, first, second uuid.UUID) {
		t.Helper()
		if first != second {
			t.Errorf("the second making answered %s, want the first's %s", second, first)
		}
		if n := countNamed(t, table, name); n != 1 {
			t.Errorf("there are %d rows in %s called %s, want 1", n, table, name)
		}
	}

	t.Run("a status", func(t *testing.T) {
		name := h.name(t, "Waiting on")
		first, second := ensureHeldOpen(t, h, func(ctx context.Context, tx db.DBTX) (uuid.UUID, error) {
			return workflow.EnsureStatus(ctx, tx, workflow.StatusInput{Name: name, Category: workflow.CategoryInProgress})
		})
		same(t, "issue_status", name, first, second)
	})

	todo, done := h.statusID(t, ws, bootstrap.StatusToDo), h.statusID(t, ws, bootstrap.StatusDone)
	graph := func(name string) workflow.GraphInput {
		return workflow.GraphInput{
			Name:        name,
			Steps:       []workflow.StepInput{{StatusID: todo, IsInitial: true}, {StatusID: done}},
			Transitions: []workflow.TransitionInput{{Name: "Finish", FromStatusID: &todo, ToStatusID: done}},
		}
	}

	t.Run("a workflow", func(t *testing.T) {
		name := h.name(t, "Brought workflow")
		first, second := ensureHeldOpen(t, h, func(ctx context.Context, tx db.DBTX) (uuid.UUID, error) {
			return ws.admin.EnsureWorkflow(ctx, tx, graph(name))
		})
		same(t, "workflow", name, first, second)
	})

	t.Run("a scheme", func(t *testing.T) {
		var flow uuid.UUID
		_, err := h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			var err error
			flow, err = ws.admin.EnsureWorkflow(ctx, tx, graph(h.name(t, "Scheme workflow")))
			return err
		})
		if err != nil {
			t.Fatalf("make the scheme's workflow: %v", err)
		}
		name := h.name(t, "Brought scheme")
		first, second := ensureHeldOpen(t, h, func(ctx context.Context, tx db.DBTX) (uuid.UUID, error) {
			return ws.admin.EnsureScheme(ctx, tx, workflow.SchemeInput{Name: name, Items: []workflow.SchemeItemInput{{WorkflowID: flow}}})
		})
		same(t, "workflow_scheme", name, first, second)
	})
}
