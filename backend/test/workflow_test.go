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
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/workflow"
)

// newIssue creates an issue and fails the test if it cannot.
func (ws *workspace) newIssue(t *testing.T, summary string) *issue.Issue {
	t.Helper()
	created, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
		ProjectKey: ws.project.Key, Summary: summary,
	}, ws.actor)
	if err != nil {
		t.Fatalf("create issue %q: %v", summary, err)
	}
	return created
}

// move takes a named transition, failing the test if it cannot be taken.
func (ws *workspace) move(t *testing.T, h *harness, key, transition string, in ...issue.TransitionInput) *issue.Issue {
	t.Helper()
	input := issue.TransitionInput{}
	if len(in) > 0 {
		input = in[0]
	}
	input.TransitionID = h.transitionID(t, ws, key, transition)

	updated, _, err := ws.issues.Transition(ws.ctx, key, input, ws.actor)
	if err != nil {
		t.Fatalf("transition %q on %s: %v", transition, key, err)
	}
	return updated
}

func TestTransitions(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "flowing")

	t.Run("only the transitions leaving the current status are offered", func(t *testing.T) {
		created := ws.newIssue(t, "walk the graph")

		available, err := ws.issues.Transitions(ws.ctx, created.Key, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		got := transitionNames(available)
		// From To Do: the one step forward, plus the global close.
		if got != "Close,Start progress" {
			t.Errorf("from To Do: %v, want Start progress and Close", got)
		}

		inProgress := ws.move(t, h, created.Key, "Start progress")
		if inProgress.Status.Name != bootstrap.StatusInProgress {
			t.Fatalf("status = %q, want In Progress", inProgress.Status.Name)
		}

		available, err = ws.issues.Transitions(ws.ctx, created.Key, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		if got := transitionNames(available); got != "Close,Ready for review,Stop progress" {
			t.Errorf("from In Progress: %v", got)
		}
	})

	t.Run("a transition that does not leave the current status is refused", func(t *testing.T) {
		created := ws.newIssue(t, "no shortcuts")

		// Approve leaves In Review, so it must not be usable from To Do even
		// when its id is known.
		var approveID uuid.UUID
		err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT id FROM workflow_transition WHERE name = 'Approve'`).Scan(&approveID)
		})
		if err != nil {
			t.Fatal(err)
		}

		_, _, err = ws.issues.Transition(ws.ctx, created.Key, issue.TransitionInput{TransitionID: approveID}, ws.actor)
		if !errors.Is(err, workflow.ErrTransitionNotAvailable) {
			t.Fatalf("error = %v, want ErrTransitionNotAvailable", err)
		}

		// And nothing moved.
		after, err := ws.issues.ByKey(ws.ctx, created.Key)
		if err != nil {
			t.Fatal(err)
		}
		if after.Status.Name != bootstrap.StatusToDo {
			t.Errorf("status = %q after a refused transition, want it untouched", after.Status.Name)
		}
	})

	t.Run("an unknown transition id is refused", func(t *testing.T) {
		created := ws.newIssue(t, "invented transition")
		_, _, err := ws.issues.Transition(ws.ctx, created.Key,
			issue.TransitionInput{TransitionID: uuid.New()}, ws.actor)
		if !errors.Is(err, workflow.ErrTransitionNotFound) {
			t.Errorf("error = %v, want ErrTransitionNotFound", err)
		}
	})

	t.Run("a post-function assigns the issue to whoever started it", func(t *testing.T) {
		created := ws.newIssue(t, "picked up")
		if created.Assignee != nil {
			t.Fatal("a new issue should start unassigned")
		}

		started := ws.move(t, h, created.Key, "Start progress")
		if started.Assignee == nil || started.Assignee.ID != ws.actor.UserID {
			t.Errorf("assignee = %v, want the person who started it", started.Assignee)
		}
	})

	t.Run("a validator blocks a transition until its input is supplied", func(t *testing.T) {
		created := ws.newIssue(t, "needs an assignee")

		// Start progress assigns it, so unassign first to reach the validator.
		ws.move(t, h, created.Key, "Start progress")
		if _, _, err := ws.issues.Update(ws.ctx, created.Key, issue.UpdateInput{
			Assignee: ptr[*uuid.UUID](nil),
		}, ws.actor); err != nil {
			t.Fatal(err)
		}

		id := h.transitionID(t, ws, created.Key, "Ready for review")
		_, _, err := ws.issues.Transition(ws.ctx, created.Key, issue.TransitionInput{TransitionID: id}, ws.actor)
		var ruleErr *workflow.RuleError
		if !errors.As(err, &ruleErr) {
			t.Fatalf("error = %v, want a RuleError about the assignee", err)
		}
		if ruleErr.Field != "assignee" {
			t.Errorf("field = %q, want assignee", ruleErr.Field)
		}

		// Assigning as part of the transition satisfies it.
		if _, _, err := ws.issues.Transition(ws.ctx, created.Key, issue.TransitionInput{
			TransitionID: id, Assignee: &ws.actor.UserID,
		}, ws.actor); err != nil {
			t.Errorf("assigning during the transition was still refused: %v", err)
		}
	})

	t.Run("reaching a done status records the resolution, and reopening clears it", func(t *testing.T) {
		created := ws.newIssue(t, "the full loop")

		ws.move(t, h, created.Key, "Start progress")
		ws.move(t, h, created.Key, "Ready for review")
		done := ws.move(t, h, created.Key, "Approve", issue.TransitionInput{Resolution: "Fixed"})

		if done.Status.Category != workflow.CategoryDone {
			t.Errorf("category = %q, want done", done.Status.Category)
		}
		if done.ResolvedAt == nil {
			t.Error("resolvedAt was not set on reaching a done status")
		}

		reopened := ws.move(t, h, created.Key, "Reopen")
		if reopened.ResolvedAt != nil {
			t.Error("resolvedAt survived a reopen")
		}
		if reopened.Status.Name != bootstrap.StatusToDo {
			t.Errorf("status = %q after reopening, want To Do", reopened.Status.Name)
		}
	})

	t.Run("the global transition is available from everywhere", func(t *testing.T) {
		created := ws.newIssue(t, "closeable anywhere")

		closed := ws.move(t, h, created.Key, "Close")
		if closed.Status.Name != bootstrap.StatusDone {
			t.Errorf("status = %q, want Done", closed.Status.Name)
		}

		// And not offered once it leads nowhere new.
		available, err := ws.issues.Transitions(ws.ctx, created.Key, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(transitionNames(available), "Close") {
			t.Error("Close is still offered on an issue that is already closed")
		}
	})

	t.Run("a comment supplied with a transition is recorded", func(t *testing.T) {
		created := ws.newIssue(t, "with commentary")
		ws.move(t, h, created.Key, "Start progress", issue.TransitionInput{Comment: "taking this on"})

		comments, err := ws.issues.Comments(ws.ctx, created.Key, true)
		if err != nil {
			t.Fatal(err)
		}
		if len(comments) != 1 {
			t.Fatalf("got %d comments, want 1", len(comments))
		}
		if text := issue.PlainText(comments[0].Body); text != "taking this on" {
			t.Errorf("comment = %q, want the text supplied with the transition", text)
		}
	})

	t.Run("every transition is written to the changelog", func(t *testing.T) {
		created := ws.newIssue(t, "fully logged")
		ws.move(t, h, created.Key, "Start progress")
		ws.move(t, h, created.Key, "Ready for review")

		history, err := ws.issues.History(ws.ctx, created.Key)
		if err != nil {
			t.Fatal(err)
		}

		var statusChanges []string
		for _, entry := range history {
			for _, c := range entry.Changes {
				if c.Field == "status" {
					statusChanges = append(statusChanges, c.From+"->"+c.To)
				}
			}
		}
		// Newest first.
		want := []string{"In Progress->In Review", "To Do->In Progress"}
		if strings.Join(statusChanges, ",") != strings.Join(want, ",") {
			t.Errorf("status changes = %v, want %v", statusChanges, want)
		}
	})
}

// TestTransitionAtomicity checks that a transition which fails partway leaves
// nothing behind: no status change, no comment, no changelog entry.
func TestTransitionAtomicity(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "atomic")
	created := ws.newIssue(t, "all or nothing")

	ws.move(t, h, created.Key, "Start progress")
	if _, _, err := ws.issues.Update(ws.ctx, created.Key, issue.UpdateInput{
		Assignee: ptr[*uuid.UUID](nil),
	}, ws.actor); err != nil {
		t.Fatal(err)
	}

	before, err := ws.issues.ByKey(ws.ctx, created.Key)
	if err != nil {
		t.Fatal(err)
	}
	commentsBefore, err := ws.issues.Comments(ws.ctx, created.Key, true)
	if err != nil {
		t.Fatal(err)
	}
	historyBefore, err := ws.issues.History(ws.ctx, created.Key)
	if err != nil {
		t.Fatal(err)
	}

	// This fails on the assignee validator, but only after the comment would
	// otherwise have been written.
	id := h.transitionID(t, ws, created.Key, "Ready for review")
	if _, _, err := ws.issues.Transition(ws.ctx, created.Key, issue.TransitionInput{
		TransitionID: id, Comment: "this comment must not survive",
	}, ws.actor); err == nil {
		t.Fatal("the transition succeeded despite the validator")
	}

	after, err := ws.issues.ByKey(ws.ctx, created.Key)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status.ID != before.Status.ID {
		t.Error("the status changed despite the transition failing")
	}

	commentsAfter, err := ws.issues.Comments(ws.ctx, created.Key, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(commentsAfter) != len(commentsBefore) {
		t.Errorf("the failed transition left %d extra comments", len(commentsAfter)-len(commentsBefore))
	}

	historyAfter, err := ws.issues.History(ws.ctx, created.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(historyAfter) != len(historyBefore) {
		t.Errorf("the failed transition left %d extra changelog entries", len(historyAfter)-len(historyBefore))
	}
}

// TestConditionHidesTransition attaches a role condition to a live workflow and
// checks that it takes effect for a real user.
func TestConditionHidesTransition(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "gated")
	created := ws.newIssue(t, "restricted")

	// Restrict "Start progress" to owners and admins.
	_, err := h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		var transitionID uuid.UUID
		if err := tx.QueryRow(ctx,
			`SELECT id FROM workflow_transition WHERE name = 'Start progress'`).Scan(&transitionID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO transition_rule (org_id, transition_id, kind, rule_type, config, position)
			VALUES (current_org_id(), $1, 'condition', $2, $3, 0)`,
			transitionID, workflow.ConditionOrgRole, `{"roles":["owner","admin"]}`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// The owner still sees it.
	available, err := ws.issues.Transitions(ws.ctx, created.Key, ws.actor)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(transitionNames(available), "Start progress") {
		t.Errorf("the owner cannot see the transition: %v", transitionNames(available))
	}

	// A plain member does not.
	member := issue.Actor{UserID: uuid.New(), OrgRole: "member"}
	available, err = ws.issues.Transitions(ws.ctx, created.Key, member)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(transitionNames(available), "Start progress") {
		t.Errorf("a member was offered a restricted transition: %v", transitionNames(available))
	}

	// And cannot take it even knowing the id.
	id := h.transitionID(t, ws, created.Key, "Start progress")
	if _, _, err := ws.issues.Transition(ws.ctx, created.Key,
		issue.TransitionInput{TransitionID: id}, member); !errors.Is(err, workflow.ErrTransitionNotAvailable) {
		t.Errorf("error = %v, want ErrTransitionNotAvailable", err)
	}
}

// transitionNames returns a stable, comparable list of names.
func transitionNames(ts []workflow.Transition) string {
	names := make([]string, len(ts))
	for i, t := range ts {
		names[i] = t.Name
	}
	sortStrings(names)
	return strings.Join(names, ",")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// ptr returns a pointer to a value, for the double-pointer patch fields.
func ptr[T any](v T) *T { return &v }
