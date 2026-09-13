//go:build integration

package test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/plan"
	"github.com/armature/armature/backend/internal/sprint"
)

func on(s string) *time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return &t
}

func size(v float64) *float64 { return &v }

// planned creates a sprint with dates, which is what starting one needs.
func (ws *workspace) planned(t *testing.T, h *harness, name, from, to string, capacity *float64) *sprint.Sprint {
	t.Helper()
	created, _, err := ws.sprints.Create(ws.ctx, ws.project.Key, sprint.CreateInput{
		Name: name, StartsOn: on(from), EndsOn: on(to), Capacity: capacity,
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create sprint %q: %v", name, err)
	}
	return created
}

// commit puts an issue in a sprint and sizes it.
func (ws *workspace) commit(t *testing.T, key string, to *uuid.UUID, estimate *float64) {
	t.Helper()
	if _, _, err := ws.issues.SetSprint(ws.ctx, key, to, ws.actor); err != nil {
		t.Fatalf("put %s in a sprint: %v", key, err)
	}
	if estimate != nil {
		if _, _, err := ws.issues.SetEstimate(ws.ctx, key, estimate, ws.actor); err != nil {
			t.Fatalf("size %s: %v", key, err)
		}
	}
}

func (ws *workspace) planOf(t *testing.T) *plan.Plan {
	t.Helper()
	found, err := ws.plans.ForProject(ws.ctx, ws.project.Key)
	if err != nil {
		t.Fatalf("read the plan: %v", err)
	}
	return found
}

func TestSprintStartsOnlyWhenItCan(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "starting")

	t.Run("a sprint with no dates cannot start", func(t *testing.T) {
		vague, _, err := ws.sprints.Create(ws.ctx, ws.project.Key,
			sprint.CreateInput{Name: "Someday"}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = ws.sprints.Start(ws.ctx, vague.ID, ws.actor.UserID)
		if !errors.Is(err, sprint.ErrNotStartable) {
			t.Errorf("error = %v, want it refused", err)
		}
		if err != nil && !strings.Contains(err.Error(), "start and an end") {
			t.Errorf("error = %q, want it to say what is missing", err)
		}
	})

	first := ws.planned(t, h, "Sprint 1", "2026-03-02", "2026-03-13", size(20))
	if _, _, err := ws.sprints.Start(ws.ctx, first.ID, ws.actor.UserID); err != nil {
		t.Fatalf("start the first sprint: %v", err)
	}

	t.Run("a team runs one sprint at a time", func(t *testing.T) {
		second := ws.planned(t, h, "Sprint 2", "2026-03-16", "2026-03-27", size(20))
		_, _, err := ws.sprints.Start(ws.ctx, second.ID, ws.actor.UserID)
		if !errors.Is(err, sprint.ErrNotStartable) {
			t.Errorf("error = %v, want it refused", err)
		}
		if err != nil && !strings.Contains(err.Error(), "Sprint 1") {
			t.Errorf("error = %q, want it to name the sprint already running", err)
		}
	})

	// The service refuses first, so the last line of defence is the database.
	t.Run("nor by going round the service", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(), `
			INSERT INTO sprint (org_id, project_id, name, state, starts_on, ends_on)
			VALUES ($1, $2, 'Sneaky', 'active', '2026-04-01', '2026-04-14')`,
			ws.orgID, ws.project.ID)
		if err == nil {
			t.Fatal("SQL started a second sprint in one project")
		}
		if !strings.Contains(err.Error(), "sprint_one_active_idx") {
			t.Errorf("error = %v, want the index to have refused it", err)
		}
	})

	t.Run("and a running sprint needs both its dates", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(),
			`UPDATE sprint SET ends_on = NULL WHERE id = $1`, first.ID)
		if err == nil {
			t.Fatal("SQL left a running sprint without an end")
		}
		if !strings.Contains(err.Error(), "needs a start and an end") {
			t.Errorf("error = %v, want the guard's own message", err)
		}
	})
}

func TestSprintCapacityCountsTheWorkInIt(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "capacity")

	running := ws.planned(t, h, "Sprint 1", "2026-03-02", "2026-03-13", size(10))
	if _, _, err := ws.sprints.Start(ws.ctx, running.ID, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}

	story := ws.typed(t, h, bootstrap.TypeStory, "committed work")
	other := ws.typed(t, h, bootstrap.TypeTask, "more committed work")
	backlog := ws.typed(t, h, bootstrap.TypeTask, "nobody has taken this on")

	ws.commit(t, story.Key, &running.ID, size(5))
	ws.commit(t, other.Key, &running.ID, size(3))
	ws.commit(t, backlog.Key, nil, size(8))

	found := ws.planOf(t)
	if len(found.Sprints) != 1 {
		t.Fatalf("sprints = %d, want 1", len(found.Sprints))
	}
	got := found.Sprints[0]
	if got.Committed != 8 {
		t.Errorf("committed = %v, want the 8 points in the sprint", got.Committed)
	}
	if got.Issues != 2 {
		t.Errorf("issues = %d, want 2", got.Issues)
	}
	if got.Completed != 0 {
		t.Errorf("completed = %v, want nothing done yet", got.Completed)
	}

	t.Run("finishing work moves it into completed", func(t *testing.T) {
		ws.move(t, h, story.Key, "Start progress")
		ws.move(t, h, story.Key, "Ready for review")
		ws.move(t, h, story.Key, "Approve")

		got := ws.planOf(t).Sprints[0]
		if got.Completed != 5 {
			t.Errorf("completed = %v, want the finished 5", got.Completed)
		}
		if got.Committed != 8 {
			t.Errorf("committed = %v, want it unchanged at 8", got.Committed)
		}
	})

	t.Run("committing past capacity warns and is not refused", func(t *testing.T) {
		heavy := ws.typed(t, h, bootstrap.TypeTask, "this tips it over")
		ws.commit(t, heavy.Key, &running.ID, size(6))

		found := ws.planOf(t)
		if found.Sprints[0].Committed != 14 {
			t.Fatalf("committed = %v, want 14", found.Sprints[0].Committed)
		}
		var over *plan.Warning
		for i, w := range found.Warnings {
			if w.Kind == plan.WarnOverCapacity {
				over = &found.Warnings[i]
			}
		}
		if over == nil {
			t.Fatalf("warnings = %+v, want one about capacity", found.Warnings)
		}
		if over.Sprint != "Sprint 1" {
			t.Errorf("warning names %q, want the sprint", over.Sprint)
		}
	})
}

// A subtask inside a story that is in the same sprint is the story's work, and
// counting both would double it.
func TestWorkInASprintIsCountedOnceEndToEnd(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "once")

	running := ws.planned(t, h, "Sprint 1", "2026-03-02", "2026-03-13", size(10))
	story := ws.typed(t, h, bootstrap.TypeStory, "a story with parts")
	child, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
		ProjectKey: ws.project.Key,
		Summary:    "one of the parts",
		TypeID:     h.issueTypeID(t, ws, bootstrap.TypeSubtask),
		ParentKey:  story.Key,
	}, ws.actor)
	if err != nil {
		t.Fatal(err)
	}

	ws.commit(t, story.Key, &running.ID, size(5))
	ws.commit(t, child.Key, &running.ID, size(2))

	got := ws.planOf(t).Sprints[0]
	if got.Committed != 5 {
		t.Errorf("committed = %v, want the story's 5 counted once", got.Committed)
	}
	if got.Issues != 1 {
		t.Errorf("issues = %d, want only the row carrying the work", got.Issues)
	}
}

// A project with sprints and nothing in them yet still has sprints, which is
// exactly the state a team is in the moment before they plan one.
func TestAnEmptyProjectStillShowsItsSprints(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "empty")

	ws.planned(t, h, "Sprint 1", "2026-03-02", "2026-03-13", size(10))

	found := ws.planOf(t)
	if len(found.Items) != 0 {
		t.Fatalf("items = %d, want a project with no issues", len(found.Items))
	}
	if len(found.Sprints) != 1 {
		t.Fatalf("sprints = %d, want the one that was planned", len(found.Sprints))
	}
	if got := found.Sprints[0]; got.Committed != 0 || got.Issues != 0 {
		t.Errorf("sprint = %+v, want it empty rather than absent", got)
	}
}

func TestCompletingASprintCarriesTheUnfinishedWork(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "completing")

	running := ws.planned(t, h, "Sprint 1", "2026-03-02", "2026-03-13", size(10))
	next := ws.planned(t, h, "Sprint 2", "2026-03-16", "2026-03-27", size(10))
	if _, _, err := ws.sprints.Start(ws.ctx, running.ID, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}

	finished := ws.typed(t, h, bootstrap.TypeTask, "this one gets done")
	unfinished := ws.typed(t, h, bootstrap.TypeTask, "this one does not")
	ws.commit(t, finished.Key, &running.ID, size(3))
	ws.commit(t, unfinished.Key, &running.ID, size(5))
	ws.move(t, h, finished.Key, "Close")

	report, _, err := ws.sprints.Complete(ws.ctx, running.ID,
		sprint.CompleteInput{MoveTo: &next.ID}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("complete the sprint: %v", err)
	}

	if report.Committed != 8 || report.Completed != 3 {
		t.Errorf("report = %v of %v, want 3 of 8", report.Completed, report.Committed)
	}
	if report.Carried != 1 || report.CarriedTo != "Sprint 2" {
		t.Errorf("carried %d to %q, want 1 to Sprint 2", report.Carried, report.CarriedTo)
	}
	if report.Sprint.State != sprint.StateClosed {
		t.Errorf("state = %s, want closed", report.Sprint.State)
	}

	t.Run("the finished work stays where it was", func(t *testing.T) {
		found, err := ws.issues.ByKey(ws.ctx, finished.Key)
		if err != nil {
			t.Fatal(err)
		}
		if found.SprintID == nil || *found.SprintID != running.ID {
			t.Errorf("%s left the sprint it was completed in", finished.Key)
		}
	})

	t.Run("the unfinished work went to the next sprint", func(t *testing.T) {
		found, err := ws.issues.ByKey(ws.ctx, unfinished.Key)
		if err != nil {
			t.Fatal(err)
		}
		if found.SprintID == nil || *found.SprintID != next.ID {
			t.Errorf("%s did not move to Sprint 2", unfinished.Key)
		}
	})

	// A carried issue is an issue that moved, and the changelog says so.
	t.Run("and the move is in the changelog", func(t *testing.T) {
		history, err := ws.issues.History(ws.ctx, unfinished.Key)
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, entry := range history {
			for _, change := range entry.Changes {
				if change.Field == "sprint" && change.From == "Sprint 1" && change.To == "Sprint 2" {
					found = true
				}
			}
		}
		if !found {
			t.Error("the carry-over left no trail in the changelog")
		}
	})
}

// A closed sprint's report is a record of a moment. Editing the issues in it
// afterwards must not rewrite what it says.
func TestAClosedSprintsReportDoesNotDrift(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "record")

	running := ws.planned(t, h, "Sprint 1", "2026-03-02", "2026-03-13", size(10))
	if _, _, err := ws.sprints.Start(ws.ctx, running.ID, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	task := ws.typed(t, h, bootstrap.TypeTask, "sized once, resized later")
	ws.commit(t, task.Key, &running.ID, size(5))
	ws.move(t, h, task.Key, "Close")

	report, _, err := ws.sprints.Complete(ws.ctx, running.ID, sprint.CompleteInput{}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if report.Committed != 5 {
		t.Fatalf("committed = %v, want 5", report.Committed)
	}

	if _, _, err := ws.issues.SetEstimate(ws.ctx, task.Key, size(13), ws.actor); err != nil {
		t.Fatal(err)
	}

	after, err := ws.sprints.ByID(ws.ctx, running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Committed == nil || *after.Committed != 5 {
		t.Errorf("committed = %v, want the 5 it was when the sprint closed", after.Committed)
	}
}

func TestASprintOnlyMovesForwards(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "forwards")

	running := ws.planned(t, h, "Sprint 1", "2026-03-02", "2026-03-13", nil)
	if _, _, err := ws.sprints.Start(ws.ctx, running.ID, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.sprints.Complete(ws.ctx, running.ID, sprint.CompleteInput{}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}

	t.Run("a closed sprint cannot be started again", func(t *testing.T) {
		_, _, err := ws.sprints.Start(ws.ctx, running.ID, ws.actor.UserID)
		if !errors.Is(err, sprint.ErrNotStartable) {
			t.Errorf("error = %v, want it refused", err)
		}
	})

	t.Run("nor completed twice", func(t *testing.T) {
		_, _, err := ws.sprints.Complete(ws.ctx, running.ID, sprint.CompleteInput{}, ws.actor.UserID)
		if !errors.Is(err, sprint.ErrNotRunning) {
			t.Errorf("error = %v, want it refused", err)
		}
	})

	t.Run("nor edited", func(t *testing.T) {
		name := "Renamed after the fact"
		_, _, err := ws.sprints.Update(ws.ctx, running.ID, sprint.UpdateInput{Name: &name}, ws.actor.UserID)
		if !errors.Is(err, sprint.ErrClosed) {
			t.Errorf("error = %v, want it refused", err)
		}
	})

	t.Run("nor reopened by going round the service", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(),
			`UPDATE sprint SET state = 'active' WHERE id = $1`, running.ID)
		if err == nil {
			t.Fatal("SQL reopened a completed sprint")
		}
		if !strings.Contains(err.Error(), "cannot be reopened") {
			t.Errorf("error = %v, want the guard's own message", err)
		}
	})

	t.Run("and no more work can be committed to it", func(t *testing.T) {
		late := ws.typed(t, h, bootstrap.TypeTask, "too late")
		_, _, err := ws.issues.SetSprint(ws.ctx, late.Key, &running.ID, ws.actor)
		if !errors.Is(err, issue.ErrSprintClosed) {
			t.Errorf("error = %v, want it refused", err)
		}
	})
}

func TestDeletingAPlannedSprintReturnsItsWork(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "deleting")

	planned := ws.planned(t, h, "Sprint 1", "2026-03-02", "2026-03-13", nil)
	task := ws.typed(t, h, bootstrap.TypeTask, "committed to a sprint that goes away")
	ws.commit(t, task.Key, &planned.ID, size(3))

	if _, err := ws.sprints.Delete(ws.ctx, planned.ID); err != nil {
		t.Fatalf("delete the sprint: %v", err)
	}

	found, err := ws.issues.ByKey(ws.ctx, task.Key)
	if err != nil {
		t.Fatal(err)
	}
	if found.SprintID != nil {
		t.Error("the work went with the sprint instead of back to the backlog")
	}
	if found.Estimate == nil || *found.Estimate != 3 {
		t.Errorf("estimate = %v, want it kept", found.Estimate)
	}
}

func TestASprintCannotBeBorrowedFromAnotherProject(t *testing.T) {
	h := newHarness(t)
	mine := h.newWorkspace(t, "mine")
	theirs := h.newWorkspace(t, "theirs")

	other := theirs.planned(t, h, "Their sprint", "2026-03-02", "2026-03-13", nil)
	task := mine.typed(t, h, bootstrap.TypeTask, "stays where it belongs")

	t.Run("the service says it does not exist", func(t *testing.T) {
		_, _, err := mine.issues.SetSprint(mine.ctx, task.Key, &other.ID, mine.actor)
		if !errors.Is(err, issue.ErrSprintNotFound) {
			t.Errorf("error = %v, want it reported as missing", err)
		}
	})

	t.Run("and SQL refuses it too", func(t *testing.T) {
		found, err := mine.issues.ByKey(mine.ctx, task.Key)
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.super.Exec(context.Background(),
			`UPDATE issue SET sprint_id = $2 WHERE id = $1`, found.ID, other.ID)
		if err == nil {
			t.Fatal("SQL put an issue in another project's sprint")
		}
		if !strings.Contains(err.Error(), "its own project") {
			t.Errorf("error = %v, want the guard's own message", err)
		}
	})
}

func TestWorkScheduledOutsideItsSprintIsReportedOnThePlan(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "outside")

	running := ws.planned(t, h, "Sprint 1", "2026-03-02", "2026-03-13", nil)
	task := ws.typed(t, h, bootstrap.TypeTask, "runs past the sprint")
	ws.commit(t, task.Key, &running.ID, size(3))

	if _, _, err := ws.issues.Schedule(ws.ctx, task.Key, issue.ScheduleInput{
		Start: ptrTo(on("2026-03-09")), Due: ptrTo(on("2026-03-25")),
	}, ws.actor); err != nil {
		t.Fatal(err)
	}

	found := ws.planOf(t)
	var outside *plan.Warning
	for i, w := range found.Warnings {
		if w.Kind == plan.WarnOutsideSprint {
			outside = &found.Warnings[i]
		}
	}
	if outside == nil {
		t.Fatalf("warnings = %+v, want one about the sprint", found.Warnings)
	}
	if outside.IssueKey != task.Key || !strings.Contains(outside.Message, "Sprint 1") {
		t.Errorf("warning = %+v, want it to name the issue and the sprint", outside)
	}

	// The dates were kept: a plan is a draft, not a promise.
	after, err := ws.issues.ByKey(ws.ctx, task.Key)
	if err != nil {
		t.Fatal(err)
	}
	if after.DueDate == nil || after.DueDate.Format("2006-01-02") != "2026-03-25" {
		t.Errorf("due = %v, want it saved despite the warning", after.DueDate)
	}
}

func ptrTo[T any](v T) *T { return &v }

// A running sprint's standing is written down day by day, so its burndown can
// be drawn after the issues in it have moved on. The row for a day is the
// day's last word: writing again replaces it.
func TestASprintIsSnapshottedDayByDay(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "snapshots")
	first := ws.newIssue(t, "five points of it")
	second := ws.newIssue(t, "three points of it")
	sp := ws.planned(t, h, "Sprint S", "2026-09-07", "2026-09-18", nil)
	ws.commit(t, first.Key, &sp.ID, size(5))
	ws.commit(t, second.Key, &sp.ID, size(3))
	ws.start(t, sp.ID)

	rowsFor := func() []sprint.Snapshot {
		t.Helper()
		days, err := ws.sprints.Snapshots(ws.ctx, sp.ID)
		if err != nil {
			t.Fatal(err)
		}
		return days
	}

	t.Run("starting writes the first point", func(t *testing.T) {
		days := rowsFor()
		if len(days) != 1 || days[0].Scope != 8 || days[0].Done != 0 || days[0].Remaining != 8 || days[0].Issues != 2 {
			t.Fatalf("snapshots after start = %+v, want one row of 8 points, none done", days)
		}
	})

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	worker := sprint.NewSnapshots(h.cluster, ws.sprints, log)

	t.Run("the worker rewrites today's row rather than adding one", func(t *testing.T) {
		before := rowsFor()[0].TakenAt
		time.Sleep(20 * time.Millisecond)
		if n, err := worker.Once(context.Background()); err != nil || n < 1 {
			t.Fatalf("once = %d, %v", n, err)
		}
		if _, err := worker.Once(context.Background()); err != nil {
			t.Fatal(err)
		}
		days := rowsFor()
		if len(days) != 1 {
			t.Fatalf("%d rows for one day, want one", len(days))
		}
		if !days[0].TakenAt.After(before) {
			t.Error("the row was not rewritten")
		}
	})

	t.Run("finishing work moves today's numbers", func(t *testing.T) {
		ws.move(t, h, second.Key, "Close")
		if _, err := worker.Once(context.Background()); err != nil {
			t.Fatal(err)
		}
		days := rowsFor()
		if days[0].Done != 3 || days[0].Remaining != 5 || days[0].IssuesDone != 1 {
			t.Errorf("today = %+v, want 3 done, 5 remaining, one issue finished", days[0])
		}
	})

	t.Run("completing keeps what it counted", func(t *testing.T) {
		report, _, err := ws.sprints.Complete(ws.ctx, sp.ID, sprint.CompleteInput{}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if report.Sprint.Finished == nil || *report.Sprint.Finished != 1 || report.Sprint.Carried == nil || *report.Sprint.Carried != 1 {
			t.Errorf("sprint = finished %v carried %v, want 1 and 1 kept on the row", report.Sprint.Finished, report.Sprint.Carried)
		}
		days := rowsFor()
		if len(days) != 1 || days[0].Done != 3 || days[0].Scope != 8 {
			t.Errorf("the last point = %+v, want the standing before the carry-over", days)
		}
	})

	t.Run("and the database holds the same lines", func(t *testing.T) {
		var projectID uuid.UUID
		if err := h.super.QueryRow(context.Background(), `SELECT project_id FROM sprint WHERE id = $1`, sp.ID).Scan(&projectID); err != nil {
			t.Fatal(err)
		}
		_, err := h.super.Exec(context.Background(), `
			INSERT INTO sprint_snapshot (org_id, project_id, sprint_id, day, scope, done, issues, issues_done, unestimated)
			VALUES ($1, $2, $3, current_date, 1, 0, 1, 0, 0)`, ws.orgID, projectID, sp.ID)
		if err == nil {
			t.Error("a second row for the same day was accepted")
		}

		other := h.newWorkspace(t, "snapshots-other")
		_, err = h.cluster.Write(other.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO sprint_snapshot (org_id, project_id, sprint_id, day, scope, done, issues, issues_done, unestimated)
				VALUES ($1, $2, $3, '2020-01-01', 1, 0, 1, 0, 0)`, ws.orgID, projectID, sp.ID)
			return err
		})
		if err == nil {
			t.Error("another organization wrote a snapshot into this one")
		}
		theirs, err := other.sprints.Snapshots(other.ctx, sp.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(theirs) != 0 {
			t.Errorf("another organization reads %d of this one's snapshots", len(theirs))
		}
	})
}

func TestTheWorkerSnapshotsEveryOrganizationsSprints(t *testing.T) {
	h := newHarness(t)
	a := h.newWorkspace(t, "snapshots-a")
	b := h.newWorkspace(t, "snapshots-b")
	for _, ws := range []*workspace{a, b} {
		sp := ws.planned(t, h, "Sprint W", "2026-09-07", "2026-09-18", nil)
		ws.commit(t, ws.newIssue(t, "work").Key, &sp.ID, size(2))
		ws.start(t, sp.ID)
	}

	worker := sprint.NewSnapshots(h.cluster, a.sprints, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	n, err := worker.Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Errorf("once wrote %d snapshots, want at least the two running sprints", n)
	}
	for _, ws := range []*workspace{a, b} {
		sprints, err := ws.sprints.List(ws.ctx, ws.project.Key, false)
		if err != nil || len(sprints) != 1 {
			t.Fatalf("sprints = %v, %v", sprints, err)
		}
		days, err := ws.sprints.Snapshots(ws.ctx, sprints[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(days) != 1 || days[0].Scope != 2 {
			t.Errorf("%s sees %+v, want its own one row of 2 points", ws.project.Key, days)
		}
	}
}
