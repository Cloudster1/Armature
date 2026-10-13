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
	"github.com/armature/armature/backend/internal/workflow"
)

// swimlane finds a lane by name, failing the test when it is not there.
func (ws *workspace) swimlane(t *testing.T, name string) board.Swimlane {
	t.Helper()
	b, err := ws.boards.ForProject(ws.ctx, ws.project.Key)
	if err != nil {
		t.Fatalf("load board: %v", err)
	}
	for _, lane := range b.Swimlanes {
		if lane.Name == name {
			return lane
		}
	}
	var names []string
	for _, lane := range b.Swimlanes {
		names = append(names, lane.Name)
	}
	t.Fatalf("no swimlane named %q; the board has %v", name, names)
	return board.Swimlane{}
}

// statusID looks up one of the organization's statuses by name.
func (h *harness) statusID(t *testing.T, ws *workspace, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT id FROM issue_status WHERE name = $1`, name).Scan(&id)
	})
	if err != nil {
		t.Fatalf("look up status %q: %v", name, err)
	}
	return id
}

func TestBoardCreatedWithProject(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "boarded")

	b, err := ws.boards.ForProject(ws.ctx, ws.project.Key)
	if err != nil {
		t.Fatalf("a new project has no board: %v", err)
	}

	// One swimlane per status of the workflow, in workflow order, so the board
	// is immediately recognisable rather than something to assemble by hand.
	var names []string
	for _, lane := range b.Swimlanes {
		names = append(names, lane.Name)
	}
	want := strings.Join([]string{
		bootstrap.StatusToDo, bootstrap.StatusInProgress,
		bootstrap.StatusInReview, bootstrap.StatusDone,
	}, ",")
	if got := strings.Join(names, ","); got != want {
		t.Errorf("swimlanes = %v, want %v", got, want)
	}

	for _, lane := range b.Swimlanes {
		if len(lane.Statuses) != 1 {
			t.Errorf("swimlane %q covers %d states, want exactly its own", lane.Name, len(lane.Statuses))
		}
		if lane.Statuses[0].Name != lane.Name {
			t.Errorf("swimlane %q covers %q", lane.Name, lane.Statuses[0].Name)
		}
	}
}

func TestBoardPlacesCardsByStatus(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "placing")

	waiting := ws.newIssue(t, "not started")
	started := ws.newIssue(t, "under way")
	ws.move(t, h, started.Key, "Start progress")

	b, err := ws.boards.ForProject(ws.ctx, ws.project.Key)
	if err != nil {
		t.Fatal(err)
	}

	where := map[string]string{}
	for _, lane := range b.Swimlanes {
		for _, card := range lane.Cards {
			where[card.Key] = lane.Name
		}
	}
	if where[waiting.Key] != bootstrap.StatusToDo {
		t.Errorf("%s is in %q, want To Do", waiting.Key, where[waiting.Key])
	}
	if where[started.Key] != bootstrap.StatusInProgress {
		t.Errorf("%s is in %q, want In Progress", started.Key, where[started.Key])
	}
	if len(b.Unmapped) != 0 {
		t.Errorf("%d cards are unmapped on a fully mapped board", len(b.Unmapped))
	}
}

// TestBoardMoveTakesAWorkflowTransition is the property that makes a board a
// view of the workflow rather than a second opinion about it.
func TestBoardMoveTakesAWorkflowTransition(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "dragging")

	t.Run("a legal drag moves the card and takes the transition", func(t *testing.T) {
		card := ws.newIssue(t, "drag me forward")
		inProgress := ws.swimlane(t, bootstrap.StatusInProgress)

		result, _, err := ws.boards.Move(ws.ctx, ws.project.Key, board.MoveInput{
			IssueKey:   card.Key,
			SwimlaneID: inProgress.ID,
		}, ws.actor)
		if err != nil {
			t.Fatalf("move: %v", err)
		}
		if result.Transition != "Start progress" {
			t.Errorf("transition = %q, want the workflow's own name for it", result.Transition)
		}
		if result.Issue.Status.Name != bootstrap.StatusInProgress {
			t.Errorf("status = %q, want In Progress", result.Issue.Status.Name)
		}
		// The transition's post-function must have run: dragging a card into
		// In Progress assigns it, exactly as pressing the button would.
		if result.Issue.Assignee == nil || result.Issue.Assignee.ID != ws.actor.UserID {
			t.Errorf("assignee = %v, want the post-function to have assigned it", result.Issue.Assignee)
		}
	})

	t.Run("the changelog records a drag as the transition it was", func(t *testing.T) {
		card := ws.newIssue(t, "logged drag")
		inProgress := ws.swimlane(t, bootstrap.StatusInProgress)

		if _, _, err := ws.boards.Move(ws.ctx, ws.project.Key, board.MoveInput{
			IssueKey: card.Key, SwimlaneID: inProgress.ID,
		}, ws.actor); err != nil {
			t.Fatal(err)
		}

		history, err := ws.issues.History(ws.ctx, card.Key)
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, entry := range history {
			for _, c := range entry.Changes {
				if c.Field == "status" && c.From == bootstrap.StatusToDo && c.To == bootstrap.StatusInProgress {
					found = true
				}
			}
		}
		if !found {
			t.Error("dragging a card left no status change in the changelog")
		}
	})

	// A board must not become a way around the workflow. Dragging from To Do
	// straight into In Review has no transition behind it, so it is refused.
	t.Run("a drag the workflow forbids is refused", func(t *testing.T) {
		card := ws.newIssue(t, "no shortcut")
		inReview := ws.swimlane(t, bootstrap.StatusInReview)

		_, _, err := ws.boards.Move(ws.ctx, ws.project.Key, board.MoveInput{
			IssueKey: card.Key, SwimlaneID: inReview.ID,
		}, ws.actor)
		if !errors.Is(err, board.ErrNoTransition) {
			t.Fatalf("error = %v, want ErrNoTransition", err)
		}
		if !strings.Contains(err.Error(), bootstrap.StatusToDo) {
			t.Errorf("the message does not say where the card is: %v", err)
		}

		after, err := ws.issues.ByKey(ws.ctx, card.Key)
		if err != nil {
			t.Fatal(err)
		}
		if after.Status.Name != bootstrap.StatusToDo {
			t.Errorf("status = %q after a refused drag, want it untouched", after.Status.Name)
		}
	})

	// A validator refusing the move must surface as its own message, because
	// "this needs an assignee" is actionable and "cannot move" is not.
	t.Run("a validator's message survives the drag", func(t *testing.T) {
		card := ws.newIssue(t, "needs an assignee to review")
		ws.move(t, h, card.Key, "Start progress")
		if _, _, err := ws.issues.Update(ws.ctx, card.Key, issue.UpdateInput{
			Assignee: ptr[*uuid.UUID](nil),
		}, ws.actor); err != nil {
			t.Fatal(err)
		}

		inReview := ws.swimlane(t, bootstrap.StatusInReview)
		_, _, err := ws.boards.Move(ws.ctx, ws.project.Key, board.MoveInput{
			IssueKey: card.Key, SwimlaneID: inReview.ID,
		}, ws.actor)

		var ruleErr *workflow.RuleError
		if !errors.As(err, &ruleErr) {
			t.Fatalf("error = %v, want the validator's own RuleError", err)
		}
		if ruleErr.Field != "assignee" {
			t.Errorf("field = %q, want assignee", ruleErr.Field)
		}
	})

	t.Run("a drag into a swimlane that does not exist is refused", func(t *testing.T) {
		card := ws.newIssue(t, "nowhere to go")
		if _, _, err := ws.boards.Move(ws.ctx, ws.project.Key, board.MoveInput{
			IssueKey: card.Key, SwimlaneID: uuid.New(),
		}, ws.actor); !errors.Is(err, board.ErrSwimlaneNotFound) {
			t.Errorf("error = %v, want ErrSwimlaneNotFound", err)
		}
	})
}

func TestBoardReordering(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "ordering")

	first := ws.newIssue(t, "first")
	second := ws.newIssue(t, "second")
	third := ws.newIssue(t, "third")
	todo := ws.swimlane(t, bootstrap.StatusToDo)

	order := func(t *testing.T) []string {
		t.Helper()
		lane := ws.swimlane(t, bootstrap.StatusToDo)
		keys := make([]string, len(lane.Cards))
		for i, c := range lane.Cards {
			keys[i] = c.Key
		}
		return keys
	}

	// Issues are created in order, so the lane starts in creation order.
	if got := strings.Join(order(t), ","); got != strings.Join([]string{first.Key, second.Key, third.Key}, ",") {
		t.Fatalf("initial order = %v", got)
	}

	t.Run("a card can be dragged to the top of its own swimlane", func(t *testing.T) {
		result, _, err := ws.boards.Move(ws.ctx, ws.project.Key, board.MoveInput{
			IssueKey:   third.Key,
			SwimlaneID: todo.ID,
			BeforeKey:  first.Key,
		}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		// Reordering inside a swimlane is not a status change, so no transition
		// should have been taken.
		if result.Transition != "" {
			t.Errorf("a reorder took transition %q", result.Transition)
		}
		if got := strings.Join(order(t), ","); got != strings.Join([]string{third.Key, first.Key, second.Key}, ",") {
			t.Errorf("order = %v, want the third card first", got)
		}
	})

	t.Run("a card can be dropped between two others", func(t *testing.T) {
		if _, _, err := ws.boards.Move(ws.ctx, ws.project.Key, board.MoveInput{
			IssueKey:   third.Key,
			SwimlaneID: todo.ID,
			AfterKey:   first.Key,
			BeforeKey:  second.Key,
		}, ws.actor); err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(order(t), ","); got != strings.Join([]string{first.Key, third.Key, second.Key}, ",") {
			t.Errorf("order = %v", got)
		}
	})

	t.Run("a card keeps its place in the new swimlane", func(t *testing.T) {
		inProgress := ws.swimlane(t, bootstrap.StatusInProgress)
		moved := ws.newIssue(t, "moving across")
		if _, _, err := ws.boards.Move(ws.ctx, ws.project.Key, board.MoveInput{
			IssueKey: moved.Key, SwimlaneID: inProgress.ID,
		}, ws.actor); err != nil {
			t.Fatal(err)
		}

		lane := ws.swimlane(t, bootstrap.StatusInProgress)
		if len(lane.Cards) != 1 || lane.Cards[0].Key != moved.Key {
			t.Errorf("In Progress holds %v, want just the moved card", lane.Cards)
		}
	})
}

func TestSwimlaneConfiguration(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "configuring")

	t.Run("a swimlane can hold several states at once", func(t *testing.T) {
		// Merge In Progress and In Review into one "Doing" lane, which is the
		// commonest reason to reconfigure a board.
		inProgress := ws.swimlane(t, bootstrap.StatusInProgress)
		inReview := ws.swimlane(t, bootstrap.StatusInReview)

		if _, err := ws.boards.DeleteSwimlane(ws.ctx, ws.openingBoard(t), inReview.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := ws.boards.UpdateSwimlane(ws.ctx, ws.openingBoard(t), inProgress.ID, board.UpdateSwimlaneInput{
			Name: strPtr("Doing"),
			StatusIDs: &[]uuid.UUID{
				h.statusID(t, ws, bootstrap.StatusInProgress),
				h.statusID(t, ws, bootstrap.StatusInReview),
			},
		}); err != nil {
			t.Fatalf("merge the two states into one lane: %v", err)
		}

		doing := ws.swimlane(t, "Doing")
		if len(doing.Statuses) != 2 {
			t.Fatalf("Doing covers %d states, want 2", len(doing.Statuses))
		}

		// A card in either state now lands in that one lane.
		card := ws.newIssue(t, "in the merged lane")
		ws.move(t, h, card.Key, "Start progress")
		ws.move(t, h, card.Key, "Ready for review")

		doing = ws.swimlane(t, "Doing")
		var found bool
		for _, c := range doing.Cards {
			if c.Key == card.Key {
				found = true
			}
		}
		if !found {
			t.Error("a card in In Review is not in the lane that claims that state")
		}
	})

	t.Run("a state cannot be in two swimlanes", func(t *testing.T) {
		fresh := h.newProject(t, "exclusive")
		todoStatus := h.statusID(t, fresh, bootstrap.StatusToDo)

		_, _, err := fresh.boards.AddSwimlane(fresh.ctx, fresh.openingBoard(t), board.AddSwimlaneInput{
			Name:      "Also To Do",
			StatusIDs: []uuid.UUID{todoStatus},
		})
		if !errors.Is(err, board.ErrStatusClaimed) {
			t.Fatalf("error = %v, want ErrStatusClaimed", err)
		}
		if !strings.Contains(err.Error(), bootstrap.StatusToDo) {
			t.Errorf("the message does not name the lane already holding it: %v", err)
		}
	})

	// Cards whose state no swimlane covers must be surfaced, not silently
	// dropped: a card that vanishes is the worst outcome of a misconfiguration.
	t.Run("cards in unmapped states are surfaced", func(t *testing.T) {
		fresh := h.newProject(t, "unmapped")
		card := fresh.newIssue(t, "about to be orphaned")
		fresh.move(t, h, card.Key, "Start progress")

		inProgress := fresh.swimlane(t, bootstrap.StatusInProgress)
		if _, err := fresh.boards.DeleteSwimlane(fresh.ctx, fresh.openingBoard(t), inProgress.ID); err != nil {
			t.Fatal(err)
		}

		b, err := fresh.boards.ForProject(fresh.ctx, fresh.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(b.Unmapped) != 1 || b.Unmapped[0].Key != card.Key {
			t.Errorf("unmapped = %v, want the orphaned card", b.Unmapped)
		}
	})

	t.Run("a new swimlane can be added and reordered", func(t *testing.T) {
		fresh := h.newProject(t, "reordering")

		lane, _, err := fresh.boards.AddSwimlane(fresh.ctx, fresh.openingBoard(t), board.AddSwimlaneInput{
			Name:     "Blocked",
			WIPLimit: 3,
		})
		if err != nil {
			t.Fatal(err)
		}

		b, err := fresh.boards.ForProject(fresh.ctx, fresh.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		if b.Swimlanes[len(b.Swimlanes)-1].Name != "Blocked" {
			t.Error("a new swimlane was not appended to the end")
		}

		// Move it to the front.
		order := []uuid.UUID{lane.ID}
		for _, l := range b.Swimlanes {
			if l.ID != lane.ID {
				order = append(order, l.ID)
			}
		}
		if _, err := fresh.boards.ReorderSwimlanes(fresh.ctx, fresh.openingBoard(t), order); err != nil {
			t.Fatal(err)
		}

		b, err = fresh.boards.ForProject(fresh.ctx, fresh.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		if b.Swimlanes[0].Name != "Blocked" {
			t.Errorf("first swimlane is %q, want the reordered one", b.Swimlanes[0].Name)
		}
	})

	t.Run("a partial reorder is refused", func(t *testing.T) {
		fresh := h.newProject(t, "partial")
		b, err := fresh.boards.ForProject(fresh.ctx, fresh.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		// Sending only some of the lanes would leave the rest at positions the
		// server would have to guess at.
		if _, err := fresh.boards.ReorderSwimlanes(fresh.ctx, fresh.openingBoard(t),
			[]uuid.UUID{b.Swimlanes[0].ID}); err == nil {
			t.Error("a reorder listing one of four swimlanes was accepted")
		}
	})

	t.Run("the last swimlane cannot be deleted", func(t *testing.T) {
		fresh := h.newProject(t, "lastlane")
		b, err := fresh.boards.ForProject(fresh.ctx, fresh.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		for i, lane := range b.Swimlanes {
			_, err := fresh.boards.DeleteSwimlane(fresh.ctx, fresh.openingBoard(t), lane.ID)
			if i < len(b.Swimlanes)-1 {
				if err != nil {
					t.Fatalf("deleting swimlane %d: %v", i, err)
				}
				continue
			}
			if !errors.Is(err, board.ErrLastSwimlane) {
				t.Errorf("error = %v, want ErrLastSwimlane", err)
			}
		}
	})

	t.Run("a work in progress limit is reported but not enforced", func(t *testing.T) {
		fresh := h.newProject(t, "wiplimit")
		todo := fresh.swimlane(t, bootstrap.StatusToDo)
		if _, err := fresh.boards.UpdateSwimlane(fresh.ctx, fresh.openingBoard(t), todo.ID,
			board.UpdateSwimlaneInput{WIPLimit: intPtr(2)}); err != nil {
			t.Fatal(err)
		}

		for i := range 3 {
			fresh.newIssue(t, "over the limit "+string(rune('a'+i)))
		}

		lane := fresh.swimlane(t, bootstrap.StatusToDo)
		if lane.WIPLimit != 2 {
			t.Errorf("limit = %d, want 2", lane.WIPLimit)
		}
		if len(lane.Cards) != 3 {
			t.Errorf("the lane holds %d cards; the limit must not have blocked the third", len(lane.Cards))
		}
		if !lane.OverWIP() {
			t.Error("the lane does not report itself as over its limit")
		}
	})

	t.Run("grouping changes how rows are labelled", func(t *testing.T) {
		fresh := h.newProject(t, "grouping")
		card := fresh.newIssue(t, "grouped card")
		fresh.move(t, h, card.Key, "Start progress")

		if _, _, err := fresh.boards.UpdateBoard(fresh.ctx, fresh.openingBoard(t), grouped(board.GroupAssignee), fresh.actor.UserID); err != nil {
			t.Fatal(err)
		}
		b, err := fresh.boards.ForProject(fresh.ctx, fresh.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		if b.GroupBy != board.GroupAssignee {
			t.Fatalf("grouping = %q", b.GroupBy)
		}
		for _, lane := range b.Swimlanes {
			for _, c := range lane.Cards {
				if c.Key == card.Key && c.Group == "" {
					t.Error("a card carries no group under assignee grouping")
				}
			}
		}

		if _, _, err := fresh.boards.UpdateBoard(fresh.ctx, fresh.openingBoard(t), grouped("nonsense"), fresh.actor.UserID); err == nil {
			t.Error("an unknown grouping was accepted")
		}
	})
}

// A board grows a lane when its workflow gains a status, so a card moved
// there is never outside every lane.
func TestABoardGainsASwimlaneWhenItsWorkflowGainsAStatus(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "lanes-follow")
	flow, review := h.name(t, "Own flow"), h.name(t, "Reviewing")
	own := h.straightThrough(t, ws, flow, bootstrap.StatusToDo, bootstrap.StatusDone)
	h.overrideWith(t, ws, h.name(t, "Own scheme"), workflow.SchemeItemInput{WorkflowID: own.ID})

	reviewing, _, err := ws.admin.CreateStatus(ws.ctx, workflow.StatusInput{Name: review, Category: workflow.CategoryInProgress}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	before := ws.boardOf(t, ws.project.Key)
	if strings.Contains(laneNames(before), review) {
		t.Fatalf("lanes = %s before the workflow knows the status", laneNames(before))
	}

	todo := h.statusID(t, ws, bootstrap.StatusToDo)
	done := h.statusID(t, ws, bootstrap.StatusDone)
	save := func() {
		t.Helper()
		if _, _, err := ws.admin.SaveWorkflow(ws.ctx, own.ID, workflow.GraphInput{
			Name:  flow,
			Steps: []workflow.StepInput{{StatusID: todo, IsInitial: true}, {StatusID: reviewing.ID}, {StatusID: done}},
			Transitions: []workflow.TransitionInput{
				{Name: "Review", FromStatusID: &todo, ToStatusID: reviewing.ID},
				{Name: "Finish", FromStatusID: &reviewing.ID, ToStatusID: done},
			},
		}, ws.actor.UserID); err != nil {
			t.Fatalf("save workflow: %v", err)
		}
	}
	save()

	after := ws.boardOf(t, ws.project.Key)
	lanes := after.Swimlanes
	if len(lanes) != len(before.Swimlanes)+1 || lanes[len(lanes)-1].Name != review {
		t.Fatalf("lanes = %s, want Reviewing added last", laneNames(after))
	}
	last := lanes[len(lanes)-1]
	if len(last.Statuses) != 1 || last.Statuses[0].ID != reviewing.ID {
		t.Errorf("the new lane holds %+v, want the coined status", last.Statuses)
	}

	t.Run("saving again adds nothing", func(t *testing.T) {
		save()
		if again := ws.boardOf(t, ws.project.Key); len(again.Swimlanes) != len(lanes) {
			t.Errorf("lanes = %s after a second save", laneNames(again))
		}
	})

	t.Run("a project on another workflow is left alone", func(t *testing.T) {
		other := h.newProject(t, "lanes-elsewhere")
		if strings.Contains(laneNames(other.boardOf(t, other.project.Key)), review) {
			t.Error("another project's board grew a lane")
		}
		spare := ws.fromTemplate(t, h, "task-tracking", "SPRE")
		if strings.Contains(laneNames(ws.boardOf(t, spare.Key)), review) {
			t.Error("a project following the default workflow grew a lane for a status it cannot reach")
		}
	})

	t.Run("and the database refuses a second mapping of the status", func(t *testing.T) {
		_, err := h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO board_swimlane_status (org_id, board_id, swimlane_id, status_id)
				VALUES (current_org_id(), $1, $2, $3)`, after.ID, lanes[0].ID, reviewing.ID)
			return err
		})
		if err == nil {
			t.Error("the unique index let the status into two lanes")
		}
	})
}
