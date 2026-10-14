package desk

import (
	"context"
	"strings"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/workflow"
)

// CustomerResponded is the transition a customer's reply takes when the desk
// was waiting on them, when the workflow has one by that name.
const CustomerResponded = "Customer responded"

// Reopen is the transition a customer's reply takes out of a done status, when
// the workflow has one by that name.
const Reopen = "Reopen"

// Reply adds the customer's comment to their own request and hands the request
// back to the agents when the desk was waiting on them or had finished it.
func (s *Service) Reply(ctx context.Context, key, text string, customer issue.Actor) (*issue.Comment, db.LSN, error) {
	if _, err := s.Request(ctx, key, customer.UserID); err != nil {
		return nil, 0, err
	}
	comment, lsn, err := s.issues.AddComment(ctx, key, issue.TextDocument(text), customer)
	if err != nil {
		return nil, 0, err
	}

	found, err := s.issues.ByKey(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	switch {
	case strings.EqualFold(found.Status.Name, WaitingOnCustomer):
		if moved, ok := s.takeWayBack(ctx, key, customer, CustomerResponded, nil); ok {
			lsn = moved
		}
	case found.Status.Category == workflow.CategoryDone:
		// The resolution mail promises that a reply brings the request back.
		if moved, ok := s.takeWayBack(ctx, key, customer, Reopen, s.leavesDone(ctx)); ok {
			return comment, moved, nil
		}
		told, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
			return events.Emit(ctx, tx, events.TopicRequestStaysDone, map[string]any{
				"key": found.Key, "actorId": customer.UserID, "commentId": comment.ID, "status": found.Status.Name,
			})
		})
		if err != nil {
			return nil, 0, err
		}
		lsn = told
	}
	return comment, lsn, nil
}

// takeWayBack moves the request as the customer, and says whether it moved. A
// nil lands means any status will do.
func (s *Service) takeWayBack(ctx context.Context, key string, customer issue.Actor, named string, lands func(workflow.Transition) bool) (db.LSN, bool) {
	available, err := s.issues.Transitions(ctx, key, customer)
	if err != nil {
		return 0, false
	}
	if lands == nil {
		lands = func(workflow.Transition) bool { return true }
	}
	chosen := pickWay(available, named, lands)
	if chosen == nil {
		return 0, false
	}
	_, moved, err := s.issues.Transition(ctx, key, issue.TransitionInput{TransitionID: chosen.ID}, customer)
	return moved, err == nil
}

// pickWay picks the transition a reply takes: the one named for it, else the
// first that is not global, since a global one is a "resolve from anywhere".
func pickWay(available []workflow.Transition, named string, lands func(workflow.Transition) bool) *workflow.Transition {
	for i := range available {
		if strings.EqualFold(available[i].Name, named) && lands(available[i]) {
			return &available[i]
		}
	}
	for i := range available {
		if !available[i].IsGlobal() && lands(available[i]) {
			return &available[i]
		}
	}
	return nil
}

// leavesDone says whether a transition ends outside the done category, so a
// move from one finished status to another never counts as reopening.
func (s *Service) leavesDone(ctx context.Context) func(workflow.Transition) bool {
	return func(t workflow.Transition) bool {
		var done bool
		err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `
				SELECT st.category = 'done' FROM workflow_step ws JOIN issue_status st ON st.id = ws.status_id
				WHERE ws.id = $1`, t.ToStepID).Scan(&done)
		})
		return err == nil && !done
	}
}
