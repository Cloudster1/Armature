//go:build integration

package test

import (
	"errors"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/sprint"
)

// The worker lists the running sprints before it snapshots each, so one can be
// completed in between; a late snapshot leaves what completion wrote alone.
func TestASnapshotLeavesASprintCompletedInBetweenAlone(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "snapshot-late")
	done := ws.newIssue(t, "three points done")
	open := ws.newIssue(t, "five points still open")
	sp := ws.planned(t, h, "Sprint L", "2026-09-07", "2026-09-18", nil)
	ws.commit(t, done.Key, &sp.ID, size(3))
	ws.commit(t, open.Key, &sp.ID, size(5))
	ws.start(t, sp.ID)
	ws.move(t, h, done.Key, "Close")

	if _, _, err := ws.sprints.Complete(ws.ctx, sp.ID, sprint.CompleteInput{}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	closed, err := ws.sprints.ByID(ws.ctx, sp.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := ws.sprints.Snapshots(ws.ctx, sp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(before) != 1 || before[0].Scope != 8 || before[0].Done != 3 || before[0].Remaining != 5 {
		t.Fatalf("completion point = %+v, want 8 in scope, 3 done, 5 remaining", before)
	}

	// The sprint now holds only its done work: counted again it would claim
	// that everything it held was finished.
	snap, err := ws.sprints.Snapshot(ws.ctx, sp.ID, time.Now())
	if !errors.Is(err, sprint.ErrNotRunning) {
		t.Errorf("snapshot of a completed sprint = %+v, %v; want it refused as not running", snap, err)
	}

	t.Run("the completion point is unchanged", func(t *testing.T) {
		after, err := ws.sprints.Snapshots(ws.ctx, sp.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != 1 {
			t.Fatalf("%d rows after the late snapshot, want the one completion wrote", len(after))
		}
		if after[0] != before[0] {
			t.Errorf("last point = %+v, want it as completion wrote it: %+v", after[0], before[0])
		}
	})

	t.Run("the report is unchanged", func(t *testing.T) {
		now, err := ws.sprints.ByID(ws.ctx, sp.ID)
		if err != nil {
			t.Fatal(err)
		}
		if now.State != sprint.StateClosed || *now.Committed != *closed.Committed || *now.Completed != *closed.Completed ||
			*now.Finished != *closed.Finished || *now.Carried != *closed.Carried {
			t.Errorf("sprint after the late snapshot = %+v, want the report completion wrote: %+v", now, closed)
		}
	})

	t.Run("a later day gets no point either", func(t *testing.T) {
		if _, err := ws.sprints.Snapshot(ws.ctx, sp.ID, time.Now().AddDate(0, 0, 1)); !errors.Is(err, sprint.ErrNotRunning) {
			t.Errorf("snapshot of a completed sprint for tomorrow = %v, want it refused as not running", err)
		}
		after, err := ws.sprints.Snapshots(ws.ctx, sp.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != 1 {
			t.Errorf("%d rows, want only the completion point", len(after))
		}
	})
}

// A sprint that has not begun has no burndown yet: its first point is the one
// starting it writes.
func TestASnapshotWaitsForTheSprintToStart(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "snapshot-early")
	sp := ws.planned(t, h, "Sprint E", "2026-09-07", "2026-09-18", nil)
	ws.commit(t, ws.newIssue(t, "planned work").Key, &sp.ID, size(2))

	if _, err := ws.sprints.Snapshot(ws.ctx, sp.ID, time.Now()); !errors.Is(err, sprint.ErrNotRunning) {
		t.Errorf("snapshot of a planned sprint = %v, want it refused as not running", err)
	}
	days, err := ws.sprints.Snapshots(ws.ctx, sp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 0 {
		t.Errorf("a planned sprint has %d snapshots, want none", len(days))
	}
}
