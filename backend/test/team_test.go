//go:build integration

package test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/team"
)

// newTeam forms a team in the workspace's project.
func (ws *workspace) newTeam(t *testing.T, name string) *team.Team {
	t.Helper()
	created, _, err := ws.teams.Create(ws.ctx, ws.project.Key,
		team.CreateInput{Name: name}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create team %q: %v", name, err)
	}
	return created
}

// boardFor makes a board that draws one team's work.
func (ws *workspace) boardFor(t *testing.T, name string, teamID *uuid.UUID) *board.Board {
	t.Helper()
	created, _, err := ws.boards.CreateBoard(ws.ctx, ws.project.Key,
		board.CreateInput{Name: name, TeamID: teamID}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create board %q: %v", name, err)
	}
	return created
}

// hand puts an issue on a team.
func (ws *workspace) hand(t *testing.T, key string, teamID *uuid.UUID) {
	t.Helper()
	if _, _, err := ws.issues.SetTeam(ws.ctx, key, teamID, ws.actor); err != nil {
		t.Fatalf("hand %s to a team: %v", key, err)
	}
}

// cardKeys reads the keys off a board, in whichever swimlane they landed.
func cardKeys(b *board.Board) []string {
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

func TestATeamIsMadeOfPeopleWhoAreAlreadyHere(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "forming")

	formed := ws.newTeam(t, "Platform")
	if formed.MemberCount != 0 {
		t.Errorf("a new team has %d members, want none until somebody joins", formed.MemberCount)
	}

	t.Run("somebody in the organization can join", func(t *testing.T) {
		updated, _, err := ws.teams.AddMember(ws.ctx, formed.ID, ws.actor.UserID, true, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if len(updated.Members) != 1 || !updated.Members[0].Lead {
			t.Errorf("members = %+v, want one lead", updated.Members)
		}
	})

	// A team is narrower than the organization, never wider: joining one is
	// never a way to gain access to something.
	t.Run("a stranger cannot", func(t *testing.T) {
		other := h.newWorkspace(t, "elsewhere")
		_, _, err := ws.teams.AddMember(ws.ctx, formed.ID, other.actor.UserID, false, ws.actor.UserID)
		if !errors.Is(err, team.ErrNotAMember) {
			t.Errorf("error = %v, want it refused", err)
		}
	})

	t.Run("and SQL refuses it too", func(t *testing.T) {
		other := h.newWorkspace(t, "outside")
		_, err := h.super.Exec(context.Background(), `
			INSERT INTO team_member (org_id, team_id, user_id) VALUES ($1, $2, $3)`,
			ws.orgID, formed.ID, other.actor.UserID)
		if err == nil {
			t.Fatal("SQL put a stranger on a team")
		}
		if !strings.Contains(err.Error(), "member of the organization") {
			t.Errorf("error = %v, want the guard's own message", err)
		}
	})

	t.Run("two teams in one project cannot share a name", func(t *testing.T) {
		_, _, err := ws.teams.Create(ws.ctx, ws.project.Key,
			team.CreateInput{Name: "platform"}, ws.actor.UserID)
		if !errors.Is(err, team.ErrNameTaken) {
			t.Errorf("error = %v, want it refused", err)
		}
	})
}

// Removing somebody from a team leaves their work with the team: the work does
// not follow a person off it.
func TestWorkStaysWithTheTeamWhenSomebodyLeaves(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "leaving")

	formed := ws.newTeam(t, "Platform")
	if _, _, err := ws.teams.AddMember(ws.ctx, formed.ID, ws.actor.UserID, false, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	task := ws.typed(t, h, bootstrap.TypeTask, "carried by the team")
	ws.hand(t, task.Key, &formed.ID)

	if _, _, err := ws.teams.RemoveMember(ws.ctx, formed.ID, ws.actor.UserID, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}

	after, err := ws.issues.ByKey(ws.ctx, task.Key)
	if err != nil {
		t.Fatal(err)
	}
	if after.TeamID == nil || *after.TeamID != formed.ID {
		t.Error("the work left the team along with the person")
	}
}

func TestABoardDrawsOnlyItsTeamsWork(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "drawing")

	platform := ws.newTeam(t, "Platform")
	product := ws.newTeam(t, "Product")

	mine := ws.typed(t, h, bootstrap.TypeTask, "platform work")
	theirs := ws.typed(t, h, bootstrap.TypeTask, "product work")
	nobodys := ws.typed(t, h, bootstrap.TypeTask, "nobody has taken this on")
	ws.hand(t, mine.Key, &platform.ID)
	ws.hand(t, theirs.Key, &product.ID)

	platformBoard := ws.boardFor(t, "Platform board", &platform.ID)
	productBoard := ws.boardFor(t, "Product board", &product.ID)

	t.Run("each team's board shows its own work", func(t *testing.T) {
		got := cardKeys(mustLoad(t, ws, platformBoard.ID))
		if len(got) != 1 || got[0] != mine.Key {
			t.Errorf("platform board = %v, want just %s", got, mine.Key)
		}
		got = cardKeys(mustLoad(t, ws, productBoard.ID))
		if len(got) != 1 || got[0] != theirs.Key {
			t.Errorf("product board = %v, want just %s", got, theirs.Key)
		}
	})

	// A board over the whole project is what a project without teams has always
	// had, and it does not stop working when teams appear.
	t.Run("the project's own board still sees everything", func(t *testing.T) {
		whole, err := ws.boards.ForProject(ws.ctx, ws.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		got := cardKeys(whole)
		if len(got) != 3 {
			t.Errorf("project board = %v, want all three issues including %s", got, nobodys.Key)
		}
	})

	t.Run("the project lists all of its boards", func(t *testing.T) {
		boards, err := ws.boards.List(ws.ctx, ws.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(boards) != 3 {
			t.Fatalf("boards = %d, want the project's own and one per team", len(boards))
		}
		// The project's own board reads first: it is the one without a team.
		if boards[0].TeamID != nil {
			t.Errorf("first board = %+v, want the project's own", boards[0])
		}
	})
}

func TestEachBoardHasItsOwnBacklog(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "backlogs")

	platform := ws.newTeam(t, "Platform")
	product := ws.newTeam(t, "Product")

	committed := ws.typed(t, h, bootstrap.TypeTask, "already in a sprint")
	waiting := ws.typed(t, h, bootstrap.TypeTask, "waiting in the platform backlog")
	elsewhere := ws.typed(t, h, bootstrap.TypeTask, "the other team's backlog")
	ws.hand(t, committed.Key, &platform.ID)
	ws.hand(t, waiting.Key, &platform.ID)
	ws.hand(t, elsewhere.Key, &product.ID)

	running, _, err := ws.sprints.Create(ws.ctx, ws.project.Key, sprint.CreateInput{
		Name: "Platform 1", StartsOn: on("2026-03-02"), EndsOn: on("2026-03-13"), TeamID: &platform.ID,
	}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	ws.commit(t, committed.Key, &running.ID, size(3))

	platformBoard := ws.boardFor(t, "Platform board", &platform.ID)
	productBoard := ws.boardFor(t, "Product board", &product.ID)

	backlog, err := ws.boards.Backlog(ws.ctx, platformBoard.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(backlog) != 1 || backlog[0].Key != waiting.Key {
		t.Errorf("platform backlog = %v, want just the uncommitted %s", keysOf(backlog), waiting.Key)
	}

	other, err := ws.boards.Backlog(ws.ctx, productBoard.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 1 || other[0].Key != elsewhere.Key {
		t.Errorf("product backlog = %v, want just %s", keysOf(other), elsewhere.Key)
	}
}

// Two teams in one project run their own sprints. That is the whole point of
// giving them their own backlogs.
func TestTwoTeamsRunSprintsAtTheSameTime(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "parallel")

	platform := ws.newTeam(t, "Platform")
	product := ws.newTeam(t, "Product")

	first, _, err := ws.sprints.Create(ws.ctx, ws.project.Key, sprint.CreateInput{
		Name: "Platform 1", StartsOn: on("2026-03-02"), EndsOn: on("2026-03-13"), TeamID: &platform.ID,
	}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := ws.sprints.Create(ws.ctx, ws.project.Key, sprint.CreateInput{
		Name: "Product 1", StartsOn: on("2026-03-02"), EndsOn: on("2026-03-13"), TeamID: &product.ID,
	}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := ws.sprints.Start(ws.ctx, first.ID, ws.actor.UserID); err != nil {
		t.Fatalf("start the platform sprint: %v", err)
	}
	if _, _, err := ws.sprints.Start(ws.ctx, second.ID, ws.actor.UserID); err != nil {
		t.Fatalf("start the product sprint alongside it: %v", err)
	}

	t.Run("but one team still runs one at a time", func(t *testing.T) {
		third, _, err := ws.sprints.Create(ws.ctx, ws.project.Key, sprint.CreateInput{
			Name: "Platform 2", StartsOn: on("2026-03-16"), EndsOn: on("2026-03-27"), TeamID: &platform.ID,
		}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = ws.sprints.Start(ws.ctx, third.ID, ws.actor.UserID)
		if !errors.Is(err, sprint.ErrNotStartable) {
			t.Errorf("error = %v, want it refused", err)
		}
		if err != nil && !strings.Contains(err.Error(), "Platform") {
			t.Errorf("error = %q, want it to name the team", err)
		}
	})

	// And the project's own work is a stream of its own, alongside both.
	t.Run("and the project's own sprint runs alongside them", func(t *testing.T) {
		own, _, err := ws.sprints.Create(ws.ctx, ws.project.Key, sprint.CreateInput{
			Name: "Unassigned 1", StartsOn: on("2026-03-02"), EndsOn: on("2026-03-13"),
		}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := ws.sprints.Start(ws.ctx, own.ID, ws.actor.UserID); err != nil {
			t.Errorf("start the project's own sprint: %v", err)
		}
	})
}

func TestATeamCannotBeBorrowedFromAnotherProject(t *testing.T) {
	h := newHarness(t)
	mine := h.newWorkspace(t, "mine")
	theirs := h.newWorkspace(t, "theirs")

	other := theirs.newTeam(t, "Theirs")
	task := mine.typed(t, h, bootstrap.TypeTask, "stays where it belongs")

	t.Run("the service says it does not exist", func(t *testing.T) {
		_, _, err := mine.issues.SetTeam(mine.ctx, task.Key, &other.ID, mine.actor)
		if !errors.Is(err, issue.ErrTeamNotFound) {
			t.Errorf("error = %v, want it reported as missing", err)
		}
	})

	t.Run("and SQL refuses it too", func(t *testing.T) {
		found, err := mine.issues.ByKey(mine.ctx, task.Key)
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.super.Exec(context.Background(),
			`UPDATE issue SET team_id = $2 WHERE id = $1`, found.ID, other.ID)
		if err == nil {
			t.Fatal("SQL put an issue on another project's team")
		}
		if !strings.Contains(err.Error(), "not in this project") {
			t.Errorf("error = %v, want the guard's own message", err)
		}
	})
}

// Deleting a team would leave its work as nobody's. Somebody has to decide.
func TestATeamCarryingWorkCannotJustBeDeleted(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "deleting")

	formed := ws.newTeam(t, "Platform")
	task := ws.typed(t, h, bootstrap.TypeTask, "carried by the team")
	ws.hand(t, task.Key, &formed.ID)

	_, err := ws.teams.Delete(ws.ctx, formed.ID)
	if !errors.Is(err, team.ErrInUse) {
		t.Fatalf("error = %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), "Platform") {
		t.Errorf("error = %q, want it to name the team", err)
	}

	ws.hand(t, task.Key, nil)
	if _, err := ws.teams.Delete(ws.ctx, formed.ID); err != nil {
		t.Errorf("deleting a team carrying nothing: %v", err)
	}
}

func TestAProjectKeepsAtLeastOneBoard(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "lastboard")

	extra := ws.boardFor(t, "A second view", nil)
	if _, err := ws.boards.DeleteBoard(ws.ctx, extra.ID); err != nil {
		t.Fatalf("deleting a spare board: %v", err)
	}

	first, err := ws.boards.ForProject(ws.ctx, ws.project.Key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.boards.DeleteBoard(ws.ctx, first.ID); !errors.Is(err, board.ErrLastBoard) {
		t.Errorf("error = %v, want the last board kept", err)
	}
}

func mustLoad(t *testing.T, ws *workspace, id uuid.UUID) *board.Board {
	t.Helper()
	found, err := ws.boards.ByID(ws.ctx, id)
	if err != nil {
		t.Fatalf("load board: %v", err)
	}
	return found
}

func keysOf(cards []board.Card) []string {
	out := make([]string, 0, len(cards))
	for _, card := range cards {
		out = append(out, card.Key)
	}
	return out
}

// A team says how much it can take on in a week, in the unit issues are
// estimated in; nothing means "has not said", which is not zero.
func TestATeamSaysHowMuchItCanTakeOnInAWeek(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "capacity")
	alpha := ws.newTeam(t, "Alpha")
	if alpha.WeeklyCapacity != nil {
		t.Fatalf("a new team already has a capacity: %v", *alpha.WeeklyCapacity)
	}

	ten := 10.0
	updated, _, err := ws.teams.Update(ws.ctx, alpha.ID, team.UpdateInput{WeeklyCapacity: &ten, SetCapacity: true}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.WeeklyCapacity == nil || *updated.WeeklyCapacity != 10 {
		t.Errorf("capacity = %v, want 10", updated.WeeklyCapacity)
	}

	t.Run("an edit that says nothing about it leaves it alone", func(t *testing.T) {
		name := "Alpha team"
		kept, _, err := ws.teams.Update(ws.ctx, alpha.ID, team.UpdateInput{Name: &name}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if kept.WeeklyCapacity == nil || *kept.WeeklyCapacity != 10 {
			t.Errorf("capacity = %v after a rename, want 10 kept", kept.WeeklyCapacity)
		}
	})

	t.Run("it can be taken back", func(t *testing.T) {
		cleared, _, err := ws.teams.Update(ws.ctx, alpha.ID, team.UpdateInput{SetCapacity: true}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if cleared.WeeklyCapacity != nil {
			t.Errorf("capacity = %v, want none", *cleared.WeeklyCapacity)
		}
	})

	t.Run("a negative one is refused, by the service and by the database", func(t *testing.T) {
		minus := -1.0
		if _, _, err := ws.teams.Update(ws.ctx, alpha.ID, team.UpdateInput{WeeklyCapacity: &minus, SetCapacity: true}, ws.actor.UserID); !errors.Is(err, team.ErrNegativeCapacity) {
			t.Errorf("err = %v, want ErrNegativeCapacity", err)
		}
		_, err := h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE team SET weekly_capacity = -1 WHERE id = $1`, alpha.ID)
			return err
		})
		if err == nil {
			t.Error("the check constraint let a negative capacity through")
		}
	})
}
