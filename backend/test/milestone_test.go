//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/milestone"
)

// A milestone's progress is a reading of the issues assigned to it, taken every
// time, so moving an issue moves the milestone.
func TestAMilestoneTracksTheIssuesAssignedToIt(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "milestone")
	milestones := milestone.NewService(h.cluster)
	task := h.issueTypeID(t, ws, bootstrap.TypeTask)

	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	release, _, err := milestones.Create(ws.ctx, ws.project.Key, milestone.Input{
		Name: "Release 1", Description: "The first cut.", DueOn: &due,
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if release.Progress.Issues != 0 || release.Progress.Percent != 0 {
		t.Errorf("a fresh milestone reads %+v, want nothing assigned", release.Progress)
	}

	var keys []string
	for _, summary := range []string{"first", "second", "third", "fourth"} {
		created, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
			ProjectKey: ws.project.Key, TypeID: task, Summary: summary,
		}, ws.actor)
		if err != nil {
			t.Fatalf("create issue: %v", err)
		}
		if _, _, err := ws.issues.SetMilestone(ws.ctx, created.Key, &release.ID, ws.actor); err != nil {
			t.Fatalf("assign %s: %v", created.Key, err)
		}
		keys = append(keys, created.Key)
	}

	read := func() milestone.Progress {
		m, err := milestones.ByID(ws.ctx, release.ID)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return m.Progress
	}
	if got := read(); got.Issues != 4 || got.Todo != 4 || got.Percent != 0 {
		t.Errorf("progress = %+v, want four to do", got)
	}

	// One started, one finished: a quarter done, a quarter in progress.
	start := h.transitionID(t, ws, keys[0], "Start progress")
	if _, _, err := ws.issues.Transition(ws.ctx, keys[0], issue.TransitionInput{TransitionID: start}, ws.actor); err != nil {
		t.Fatalf("start: %v", err)
	}
	closeID := h.transitionID(t, ws, keys[1], "Close")
	if _, _, err := ws.issues.Transition(ws.ctx, keys[1], issue.TransitionInput{TransitionID: closeID}, ws.actor); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := read(); got.Done != 1 || got.InProgress != 1 || got.Todo != 2 || got.Percent != 25 {
		t.Errorf("progress = %+v, want one done, one in progress, 25 percent", got)
	}

	// The issue says which milestone it counts towards, and history says so.
	one, err := ws.issues.ByKey(ws.ctx, keys[0])
	if err != nil {
		t.Fatal(err)
	}
	if one.Milestone == nil || one.Milestone.Name != "Release 1" {
		t.Errorf("issue carries milestone %+v, want Release 1", one.Milestone)
	}

	// Taking an issue out takes it out of the count.
	if _, _, err := ws.issues.SetMilestone(ws.ctx, keys[3], nil, ws.actor); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	if got := read(); got.Issues != 3 || got.Percent != 33 {
		t.Errorf("progress = %+v, want three issues at 33 percent", got)
	}

	t.Run("a closed milestone takes no more work but keeps what it had", func(t *testing.T) {
		closed, _, err := milestones.Close(ws.ctx, release.ID, ws.actor.UserID)
		if err != nil {
			t.Fatalf("close: %v", err)
		}
		if closed.Open() || closed.Progress.Issues != 3 {
			t.Errorf("closed milestone = %+v, want closed with its three issues", closed)
		}
		_, _, err = ws.issues.SetMilestone(ws.ctx, keys[3], &release.ID, ws.actor)
		if !errors.Is(err, issue.ErrMilestoneClosed) {
			t.Errorf("assigning to a closed milestone: %v, want it refused as closed", err)
		}
		// And the database refuses it without asking the service.
		_, err = h.super.Exec(context.Background(), `
			UPDATE issue SET milestone_id = $2 WHERE project_id = $3 AND milestone_id IS NULL AND summary = $1`,
			"fourth", release.ID, ws.project.ID)
		if err == nil || !strings.Contains(err.Error(), "closed") {
			t.Errorf("SQL accepted work on a closed milestone (err = %v)", err)
		}
		if _, _, err := milestones.Update(ws.ctx, release.ID, milestone.UpdateInput{Name: ptr("Renamed")}, ws.actor.UserID); !errors.Is(err, milestone.ErrClosed) {
			t.Errorf("editing a closed milestone: %v, want it refused", err)
		}
		open, err := milestones.List(ws.ctx, ws.project.Key, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(open) != 0 {
			t.Errorf("open milestones = %d, want none listed once closed", len(open))
		}
		if _, _, err := milestones.Reopen(ws.ctx, release.ID, ws.actor.UserID); err != nil {
			t.Fatalf("reopen: %v", err)
		}
	})

	t.Run("an issue cannot count towards another project's milestone", func(t *testing.T) {
		other := h.newWorkspace(t, "elsewhere")
		theirs, _, err := milestone.NewService(h.cluster).Create(other.ctx, other.project.Key, milestone.Input{Name: "Theirs"}, other.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = ws.issues.SetMilestone(ws.ctx, keys[3], &theirs.ID, ws.actor)
		if !errors.Is(err, issue.ErrMilestoneNotFound) {
			t.Errorf("error = %v, want the milestone not found here", err)
		}
		// Straight through SQL, as the superuser, the trigger still refuses.
		_, err = h.super.Exec(context.Background(), `
			UPDATE issue SET milestone_id = $1 WHERE project_id = $2 AND summary = 'fourth'`,
			theirs.ID, ws.project.ID)
		if err == nil || !strings.Contains(err.Error(), "own project") {
			t.Errorf("SQL accepted a milestone from another project (err = %v)", err)
		}
	})

	t.Run("two milestones cannot share a name in one project", func(t *testing.T) {
		_, _, err := milestones.Create(ws.ctx, ws.project.Key, milestone.Input{Name: "release 1"}, ws.actor.UserID)
		if !errors.Is(err, milestone.ErrDuplicateName) {
			t.Errorf("error = %v, want the duplicate refused", err)
		}
	})

	t.Run("deleting a milestone leaves its issues, unassigned", func(t *testing.T) {
		if _, err := milestones.Delete(ws.ctx, release.ID, ws.actor.UserID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		one, err := ws.issues.ByKey(ws.ctx, keys[0])
		if err != nil {
			t.Fatal(err)
		}
		if one.MilestoneID != nil {
			t.Errorf("issue still points at the deleted milestone")
		}
		var count int
		if err := h.super.QueryRow(context.Background(), `
			SELECT count(*) FROM issue WHERE project_id = $1`, ws.project.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 4 {
			t.Errorf("%d issues survive, want all four", count)
		}
	})
}

// The plan draws the milestones and says which work is due after them.
func TestThePlanCarriesMilestonesAndWarnsAboutLateWork(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "planms")

	created := owner.post("/api/v1/projects", map[string]string{"name": "Dated", "key": "DTD"})
	if created.Status != http.StatusCreated {
		t.Fatalf("create project returned %d: %s", created.Status, created.Raw)
	}
	types := owner.get("/api/v1/issue-types")
	task := find(t, types.Body["issueTypes"], "name", "Task")["id"].(string)

	ms := owner.post("/api/v1/projects/DTD/milestones", map[string]any{"name": "Beta", "dueOn": "2026-10-01"})
	if ms.Status != http.StatusCreated {
		t.Fatalf("returned %d: %s", ms.Status, ms.Raw)
	}
	msID := ms.Body["milestone"].(map[string]any)["id"].(string)

	late := owner.post("/api/v1/projects/DTD/issues", map[string]any{
		"summary": "lands after beta", "typeId": task, "startDate": "2026-09-20", "dueDate": "2026-10-10",
	})
	lateKey := late.Body["issue"].(map[string]any)["key"].(string)
	if resp := owner.put("/api/v1/issues/"+lateKey+"/milestone", map[string]any{"milestoneId": msID}); resp.Status != http.StatusOK {
		t.Fatalf("assign returned %d: %s", resp.Status, resp.Raw)
	}
	if got := late.Body; got == nil {
		t.Fatal("no issue")
	}

	plan := owner.get("/api/v1/projects/DTD/plan")
	if plan.Status != http.StatusOK {
		t.Fatalf("plan returned %d: %s", plan.Status, plan.Raw)
	}
	milestones, _ := plan.Body["milestones"].([]any)
	if len(milestones) != 1 || milestones[0].(map[string]any)["name"] != "Beta" {
		t.Fatalf("plan milestones = %v, want Beta", milestones)
	}
	progress := milestones[0].(map[string]any)["progress"].(map[string]any)
	if progress["issues"] != float64(1) {
		t.Errorf("progress = %v, want one issue", progress)
	}
	warnings, _ := plan.Body["warnings"].([]any)
	var found bool
	for _, w := range warnings {
		warning := w.(map[string]any)
		if warning["kind"] == "past-milestone" && warning["issueKey"] == lateKey {
			found = true
			if !strings.Contains(warning["message"].(string), "Beta") {
				t.Errorf("message = %v, want it to name the milestone", warning["message"])
			}
		}
	}
	if !found {
		t.Errorf("warnings = %v, want %s flagged as due after Beta", warnings, lateKey)
	}

	t.Run("the list filters by milestone", func(t *testing.T) {
		owner.post("/api/v1/projects/DTD/issues", map[string]any{"summary": "not for beta", "typeId": task})
		resp := owner.get("/api/v1/projects/DTD/issues?milestone=" + msID)
		if resp.Status != http.StatusOK {
			t.Fatalf("returned %d: %s", resp.Status, resp.Raw)
		}
		if resp.Body["total"] != float64(1) {
			t.Errorf("total = %v, want just the one counting towards Beta", resp.Body["total"])
		}
	})
}
