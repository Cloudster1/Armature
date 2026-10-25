//go:build integration

package test

import (
	"errors"
	"testing"

	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/events"
)

// The transition used to commit on its own before the rank was placed, so a
// drop between cards no longer in that order changed the status and then failed.
func TestBoardMoveAcrossLanesIsAllOrNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "halfmoved")

	first := ws.newIssue(t, "ranked first")
	second := ws.newIssue(t, "ranked second")
	card := ws.newIssue(t, "dropped between them the wrong way round")
	inProgress := ws.swimlane(t, bootstrap.StatusInProgress)

	rankOf := func(key string) string {
		t.Helper()
		var value string
		if err := h.super.QueryRow(ws.ctx, `
			SELECT i.rank FROM issue i JOIN project p ON p.id = i.project_id
			WHERE p.key || '-' || i.key_num = $1`, key).Scan(&value); err != nil {
			t.Fatalf("read the rank of %s: %v", key, err)
		}
		return value
	}
	if rankOf(first.Key) >= rankOf(second.Key) {
		t.Fatalf("%s is not ranked above %s", first.Key, second.Key)
	}
	rankBefore := rankOf(card.Key)

	_, _, err := ws.boards.Move(ws.ctx, ws.project.Key, board.MoveInput{
		IssueKey:   card.Key,
		SwimlaneID: inProgress.ID,
		AfterKey:   second.Key,
		BeforeKey:  first.Key,
	}, ws.actor)
	if !errors.Is(err, board.ErrBoardChanged) {
		t.Fatalf("error = %v, want ErrBoardChanged", err)
	}

	after, err := ws.issues.ByKey(ws.ctx, card.Key)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status.Name != bootstrap.StatusToDo {
		t.Errorf("status = %q after a refused move, want To Do", after.Status.Name)
	}
	// Start progress assigns the card, so an assignee means the post-function ran.
	if after.Assignee != nil {
		t.Errorf("assignee = %v after a refused move, want the post-function undone", after.Assignee)
	}
	if got := rankOf(card.Key); got != rankBefore {
		t.Errorf("rank = %q after a refused move, want %q", got, rankBefore)
	}

	history, err := ws.issues.History(ws.ctx, card.Key)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range history {
		for _, c := range entry.Changes {
			if c.Field == "status" {
				t.Errorf("the changelog records a status change %q to %q for a refused move", c.From, c.To)
			}
		}
	}

	var announced int
	if err := h.super.QueryRow(ws.ctx, `SELECT count(*) FROM outbox_event WHERE topic = $1 AND payload->>'key' = $2`,
		events.TopicIssueTransitioned, card.Key).Scan(&announced); err != nil {
		t.Fatal(err)
	}
	if announced != 0 {
		t.Errorf("%d transition events went out for a refused move, want none", announced)
	}

	// The same drop with the neighbours the right way round moves the whole card.
	result, _, err := ws.boards.Move(ws.ctx, ws.project.Key, board.MoveInput{
		IssueKey:   card.Key,
		SwimlaneID: inProgress.ID,
		AfterKey:   first.Key,
		BeforeKey:  second.Key,
	}, ws.actor)
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if result.Issue.Status.Name != bootstrap.StatusInProgress {
		t.Errorf("status = %q, want In Progress", result.Issue.Status.Name)
	}
	if moved := rankOf(card.Key); moved != result.Rank || moved <= rankOf(first.Key) || moved >= rankOf(second.Key) {
		t.Errorf("rank = %q, want %q, between %s and %s", moved, result.Rank, first.Key, second.Key)
	}
}
