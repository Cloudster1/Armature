//go:build integration

package test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/component"
	"github.com/armature/armature/backend/internal/csvio"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/version"
)

// teamSprint plans a sprint in one team's stream; nil is the project's own.
func (ws *workspace) teamSprint(t *testing.T, name, from, to string, teamID *uuid.UUID) *sprint.Sprint {
	t.Helper()
	created, _, err := ws.sprints.Create(ws.ctx, ws.project.Key, sprint.CreateInput{
		Name: name, StartsOn: on(from), EndsOn: on(to), TeamID: teamID,
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create sprint %q: %v", name, err)
	}
	return created
}

// committedTo reads what the plan counts in one sprint.
func (ws *workspace) committedTo(t *testing.T, id uuid.UUID) float64 {
	t.Helper()
	for _, row := range ws.planOf(t).Sprints {
		if row.Sprint.ID == id {
			return row.Committed
		}
	}
	t.Fatalf("sprint %s is not on the plan", id)
	return 0
}

// leftSprint says whether the changelog shows the issue leaving a sprint.
func (ws *workspace) leftSprint(t *testing.T, key, from string) bool {
	t.Helper()
	history, err := ws.issues.History(ws.ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range history {
		for _, change := range entry.Changes {
			if change.Field == "sprint" && change.From == from && change.To == "" {
				return true
			}
		}
	}
	return false
}

// Handing work to another team takes it out of the sprint the old team planned
// it into: otherwise it shows in neither team's sprint and still counts in one.
func TestHandingWorkToAnotherTeamTakesItOutOfItsSprint(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "handover")

	alpha := ws.newTeam(t, "Alpha")
	beta := ws.newTeam(t, "Beta")
	running := ws.teamSprint(t, "Alpha 1", "2026-03-02", "2026-03-13", &alpha.ID)
	if _, _, err := ws.sprints.Start(ws.ctx, running.ID, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}

	moved := ws.typed(t, h, bootstrap.TypeTask, "handed to Beta")
	ws.hand(t, moved.Key, &alpha.ID)
	ws.commit(t, moved.Key, &running.ID, size(5))

	updated, _, err := ws.issues.SetTeam(ws.ctx, moved.Key, &beta.ID, ws.actor)
	if err != nil {
		t.Fatalf("hand the issue to Beta: %v", err)
	}
	if updated.Team == nil || updated.Team.ID != beta.ID {
		t.Errorf("team = %+v, want Beta", updated.Team)
	}
	if updated.SprintID != nil {
		t.Errorf("%s is still in %s, want it in Beta's backlog", moved.Key, updated.Sprint.Name)
	}

	t.Run("and the changelog says it left the sprint", func(t *testing.T) {
		if !ws.leftSprint(t, moved.Key, "Alpha 1") {
			t.Error("the issue left Alpha 1 with no trail in its changelog")
		}
	})

	t.Run("and Alpha's sprint no longer counts it", func(t *testing.T) {
		if got := ws.committedTo(t, running.ID); got != 0 {
			t.Errorf("Alpha 1 committed = %v, want 0 once the work is Beta's", got)
		}
	})

	t.Run("work given back to the project leaves a team's sprint too", func(t *testing.T) {
		task := ws.typed(t, h, bootstrap.TypeTask, "nobody's again")
		ws.hand(t, task.Key, &alpha.ID)
		ws.commit(t, task.Key, &running.ID, nil)
		ws.hand(t, task.Key, nil)
		found, err := ws.issues.ByKey(ws.ctx, task.Key)
		if err != nil {
			t.Fatal(err)
		}
		if found.SprintID != nil {
			t.Errorf("%s stayed in Alpha 1 after it went back to the project", task.Key)
		}
	})

	t.Run("work handed to a team leaves the project's own sprint", func(t *testing.T) {
		own := ws.teamSprint(t, "Project 1", "2026-03-02", "2026-03-13", nil)
		task := ws.typed(t, h, bootstrap.TypeTask, "taken on by Alpha")
		ws.commit(t, task.Key, &own.ID, nil)
		ws.hand(t, task.Key, &alpha.ID)
		found, err := ws.issues.ByKey(ws.ctx, task.Key)
		if err != nil {
			t.Fatal(err)
		}
		if found.SprintID != nil {
			t.Errorf("%s stayed in the project's sprint after Alpha took it", task.Key)
		}
		if !ws.leftSprint(t, task.Key, "Project 1") {
			t.Error("the issue left Project 1 with no trail in its changelog")
		}
	})

	// A closed sprint's report is written; the work in it is history and
	// stays there whoever carries the issue now.
	t.Run("a closed sprint keeps its work", func(t *testing.T) {
		past := ws.teamSprint(t, "Alpha 0", "2026-02-16", "2026-02-27", &alpha.ID)
		task := ws.typed(t, h, bootstrap.TypeTask, "done in Alpha 0")
		ws.hand(t, task.Key, &alpha.ID)
		ws.commit(t, task.Key, &past.ID, nil)
		ws.move(t, h, task.Key, "Close")
		if _, _, err := ws.sprints.Complete(ws.ctx, running.ID, sprint.CompleteInput{}, ws.actor.UserID); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ws.sprints.Start(ws.ctx, past.ID, ws.actor.UserID); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ws.sprints.Complete(ws.ctx, past.ID, sprint.CompleteInput{}, ws.actor.UserID); err != nil {
			t.Fatal(err)
		}
		ws.hand(t, task.Key, &beta.ID)
		found, err := ws.issues.ByKey(ws.ctx, task.Key)
		if err != nil {
			t.Fatal(err)
		}
		if found.SprintID == nil || *found.SprintID != past.ID {
			t.Errorf("%s left the closed sprint it was finished in", task.Key)
		}
	})
}

// A sprint plans its own team's work. Committing another team's work to it, by
// hand or by carrying it over, is refused by the service and by the database.
func TestASprintOnlyTakesItsOwnTeamsWork(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "streams")

	alpha := ws.newTeam(t, "Alpha")
	beta := ws.newTeam(t, "Beta")
	alphaSprint := ws.teamSprint(t, "Alpha 1", "2026-03-02", "2026-03-13", &alpha.ID)
	betaSprint := ws.teamSprint(t, "Beta 1", "2026-03-02", "2026-03-13", &beta.ID)

	betas := ws.typed(t, h, bootstrap.TypeTask, "Beta's work")
	ws.hand(t, betas.Key, &beta.ID)

	t.Run("the service refuses another team's sprint", func(t *testing.T) {
		_, _, err := ws.issues.SetSprint(ws.ctx, betas.Key, &alphaSprint.ID, ws.actor)
		if !errors.Is(err, issue.ErrSprintOtherTeam) {
			t.Errorf("error = %v, want it refused", err)
		}
		found, err := ws.issues.ByKey(ws.ctx, betas.Key)
		if err != nil {
			t.Fatal(err)
		}
		if found.SprintID != nil {
			t.Errorf("%s went into Alpha 1 anyway", betas.Key)
		}
	})

	t.Run("and a team's sprint refuses the project's own work", func(t *testing.T) {
		nobodys := ws.typed(t, h, bootstrap.TypeTask, "nobody's work")
		_, _, err := ws.issues.SetSprint(ws.ctx, nobodys.Key, &alphaSprint.ID, ws.actor)
		if !errors.Is(err, issue.ErrSprintOtherTeam) {
			t.Errorf("error = %v, want it refused", err)
		}
	})

	t.Run("and so does creating an issue straight into one", func(t *testing.T) {
		_, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
			ProjectKey: ws.project.Key, Summary: "born in the wrong sprint",
			TypeID: h.issueTypeID(t, ws, bootstrap.TypeTask), TeamID: &beta.ID, SprintID: &alphaSprint.ID,
		}, ws.actor)
		if !errors.Is(err, issue.ErrSprintOtherTeam) {
			t.Errorf("error = %v, want it refused", err)
		}
	})

	t.Run("its own team's sprint takes it", func(t *testing.T) {
		if _, _, err := ws.issues.SetSprint(ws.ctx, betas.Key, &betaSprint.ID, ws.actor); err != nil {
			t.Errorf("commit Beta's work to Beta 1: %v", err)
		}
	})

	t.Run("and SQL refuses another team's sprint too", func(t *testing.T) {
		other := ws.typed(t, h, bootstrap.TypeTask, "Beta's other work")
		ws.hand(t, other.Key, &beta.ID)
		_, err := h.super.Exec(context.Background(),
			`UPDATE issue SET sprint_id = $2 WHERE id = $1`, other.ID, alphaSprint.ID)
		if err == nil {
			t.Fatal("SQL put Beta's work in Alpha's sprint")
		}
		if !strings.Contains(err.Error(), "its own team") {
			t.Errorf("error = %v, want the guard's own message", err)
		}
	})

	t.Run("and SQL refuses handing work in a sprint to another team", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(),
			`UPDATE issue SET team_id = $2 WHERE id = $1`, betas.ID, alpha.ID)
		if err == nil {
			t.Fatal("SQL left Alpha's work in Beta's sprint")
		}
		if !strings.Contains(err.Error(), "its own team") {
			t.Errorf("error = %v, want the guard's own message", err)
		}
	})

	// The guard must not stand in the way of a project going away, where teams,
	// sprints and issues are deleted together in no promised order.
	t.Run("a project with planned team work can still be deleted", func(t *testing.T) {
		gone := h.newProject(t, "streams-gone")
		crew := gone.newTeam(t, "Crew")
		window := gone.teamSprint(t, "Crew 1", "2026-03-02", "2026-03-13", &crew.ID)
		task := gone.typed(t, h, bootstrap.TypeTask, "planned when the lights went out")
		gone.hand(t, task.Key, &crew.ID)
		gone.commit(t, task.Key, &window.ID, nil)
		if _, err := h.super.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, gone.project.ID); err != nil {
			t.Errorf("delete the project: %v", err)
		}
	})
}

// Unfinished work stays with its team when a sprint completes: carrying it into
// another team's sprint would hide it from both of them.
func TestUnfinishedWorkIsOnlyCarriedIntoItsOwnTeamsSprint(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "carrying")

	alpha := ws.newTeam(t, "Alpha")
	beta := ws.newTeam(t, "Beta")
	running := ws.teamSprint(t, "Alpha 1", "2026-03-02", "2026-03-13", &alpha.ID)
	next := ws.teamSprint(t, "Alpha 2", "2026-03-16", "2026-03-27", &alpha.ID)
	theirs := ws.teamSprint(t, "Beta 2", "2026-03-16", "2026-03-27", &beta.ID)
	own := ws.teamSprint(t, "Project 2", "2026-03-16", "2026-03-27", nil)
	if _, _, err := ws.sprints.Start(ws.ctx, running.ID, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}

	unfinished := ws.typed(t, h, bootstrap.TypeTask, "not done yet")
	ws.hand(t, unfinished.Key, &alpha.ID)
	ws.commit(t, unfinished.Key, &running.ID, size(3))

	for _, destination := range []*sprint.Sprint{theirs, own} {
		t.Run("the service refuses "+destination.Name, func(t *testing.T) {
			_, _, err := ws.sprints.Complete(ws.ctx, running.ID,
				sprint.CompleteInput{MoveTo: &destination.ID}, ws.actor.UserID)
			if !errors.Is(err, sprint.ErrOtherTeam) {
				t.Errorf("error = %v, want it refused", err)
			}
		})
	}

	t.Run("and nothing moved", func(t *testing.T) {
		still, err := ws.sprints.ByID(ws.ctx, running.ID)
		if err != nil {
			t.Fatal(err)
		}
		if still.State != sprint.StateActive {
			t.Errorf("Alpha 1 is %s, want it still running after a refused completion", still.State)
		}
		found, err := ws.issues.ByKey(ws.ctx, unfinished.Key)
		if err != nil {
			t.Fatal(err)
		}
		if found.SprintID == nil || *found.SprintID != running.ID {
			t.Errorf("%s left Alpha 1 on a refused completion", unfinished.Key)
		}
	})

	t.Run("its own team's next sprint takes it", func(t *testing.T) {
		report, _, err := ws.sprints.Complete(ws.ctx, running.ID,
			sprint.CompleteInput{MoveTo: &next.ID}, ws.actor.UserID)
		if err != nil {
			t.Fatalf("complete into Alpha 2: %v", err)
		}
		if report.Carried != 1 || report.CarriedTo != "Alpha 2" {
			t.Errorf("carried %d to %q, want 1 to Alpha 2", report.Carried, report.CarriedTo)
		}
	})
}

// An import that names a team and a sprint makes the sprint in that team's
// stream, so the rows land in it rather than being refused.
func TestAnImportedSprintBelongsToTheTeamOfItsRows(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "importstream")
	alpha := ws.newTeam(t, "Alpha")
	files := csvio.NewService(h.cluster, ws.issues, label.NewService(h.cluster), field.NewService(h.cluster), h.authService()).
		WithPlanning(ws.sprints, version.NewService(h.cluster), component.NewService(h.cluster), ws.teams)

	data := []byte("Summary,Issue Type,Status,Sprint,Custom field (Team)\n" +
		"Wire the gate,Task,To Do,Gate 1,Alpha\n" +
		"Paint the gate,Task,To Do,Gate 1,Alpha\n")
	ask := csvio.ImportRequest{
		Mapping: csvio.Mapping{"summary": {0}, "type": {1}, "status": {2}, "sprint": {3}, "team": {4}},
		Source:  "gate.csv",
	}
	report, _, err := files.Import(ws.ctx, ws.project.Key, data, ask, ws.actor)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(report.Imported) != 2 {
		t.Fatalf("imported %d of 2: refused %+v", len(report.Imported), report.Refused)
	}

	for _, key := range []string{ws.project.Key + "-1", ws.project.Key + "-2"} {
		made, err := ws.issues.ByKey(ws.ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if made.Team == nil || made.Team.ID != alpha.ID {
			t.Errorf("%s team = %+v, want Alpha", key, made.Team)
		}
		if made.Sprint == nil || made.Sprint.Name != "Gate 1" {
			t.Fatalf("%s sprint = %+v, want Gate 1", key, made.Sprint)
		}
		gate, err := ws.sprints.ByID(ws.ctx, made.Sprint.ID)
		if err != nil {
			t.Fatal(err)
		}
		if gate.TeamID == nil || *gate.TeamID != alpha.ID {
			t.Errorf("Gate 1 belongs to %v, want Alpha's stream", gate.TeamID)
		}
	}
}
