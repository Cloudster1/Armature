//go:build integration

package test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/team"
)

func strPtr(s string) *string { return &s }
func intPtr(n int) *int       { return &n }

// A scrum board follows the running sprint; every sprint, running or not, can
// also be opened as a board of its own, showing only what is committed to it.
func TestEverySprintHasABoardOfItsOwn(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "sprintboard")
	task := h.issueTypeID(t, ws, bootstrap.TypeTask)

	var keys []string
	for _, summary := range []string{"in the next sprint", "in the backlog", "in the one after"} {
		created, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: ws.project.Key, TypeID: task, Summary: summary}, ws.actor)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		keys = append(keys, created.Key)
	}
	next, _, err := ws.sprints.Create(ws.ctx, ws.project.Key, sprint.CreateInput{Name: "Next", Goal: "Ship it"}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create sprint: %v", err)
	}
	after, _, err := ws.sprints.Create(ws.ctx, ws.project.Key, sprint.CreateInput{Name: "After"}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create sprint: %v", err)
	}
	for key, id := range map[string]uuid.UUID{keys[0]: next.ID, keys[2]: after.ID} {
		if _, _, err := ws.issues.SetSprint(ws.ctx, key, &id, ws.actor); err != nil {
			t.Fatalf("commit %s: %v", key, err)
		}
	}

	cardsOf := func(b *board.Board) []string {
		var out []string
		for _, lane := range b.Swimlanes {
			for _, card := range lane.Cards {
				out = append(out, card.Key)
			}
		}
		for _, card := range b.Unmapped {
			out = append(out, card.Key)
		}
		return out
	}

	shown, err := ws.boards.ForSprint(ws.ctx, next.ID)
	if err != nil {
		t.Fatalf("board for a future sprint: %v", err)
	}
	if shown.Sprint == nil || shown.Sprint.Name != "Next" || shown.Sprint.Goal != "Ship it" {
		t.Errorf("board is headed by %+v, want the Next sprint", shown.Sprint)
	}
	if got := cardsOf(shown); len(got) != 1 || got[0] != keys[0] {
		t.Errorf("cards = %v, want just %s", got, keys[0])
	}

	other, err := ws.boards.ForSprint(ws.ctx, after.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := cardsOf(other); len(got) != 1 || got[0] != keys[2] {
		t.Errorf("the other sprint's board shows %v, want just %s", got, keys[2])
	}

	if _, err := ws.boards.ForSprint(ws.ctx, uuid.New()); !errors.Is(err, board.ErrSprintNotFound) {
		t.Errorf("error = %v, want the sprint not found", err)
	}

	// A team's sprint has a board of its own too, showing the team's work
	// committed to it; a sprint only ever takes its own team's work.
	platform, _, err := ws.teams.Create(ws.ctx, ws.project.Key, team.CreateInput{Name: "Platform"}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create team: %v", err)
	}
	teamSprint, _, err := ws.sprints.Create(ws.ctx, ws.project.Key, sprint.CreateInput{Name: "Team sprint", TeamID: &platform.ID}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create team sprint: %v", err)
	}
	ws.hand(t, keys[1], &platform.ID)
	if _, _, err := ws.issues.SetSprint(ws.ctx, keys[1], &teamSprint.ID, ws.actor); err != nil {
		t.Fatalf("commit to the team sprint: %v", err)
	}
	teams, err := ws.boards.ForSprint(ws.ctx, teamSprint.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := cardsOf(teams); len(got) != 1 || got[0] != keys[1] {
		t.Errorf("the team sprint's board shows %v, want just %s", got, keys[1])
	}

	// Another organization's sprint does not exist here, whatever its id.
	elsewhere := h.newWorkspace(t, "elsewhere")
	theirs, _, err := elsewhere.sprints.Create(elsewhere.ctx, elsewhere.project.Key, sprint.CreateInput{Name: "Theirs"}, elsewhere.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.boards.ForSprint(ws.ctx, theirs.ID); !errors.Is(err, board.ErrSprintNotFound) {
		t.Errorf("error = %v, want another tenant's sprint not found", err)
	}
}
