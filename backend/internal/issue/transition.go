package issue

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/workflow"
)

// Transitions lists the moves available to this actor from the issue's current
// status.
func (s *Service) Transitions(ctx context.Context, key string, actor Actor) ([]workflow.Transition, error) {
	var out []workflow.Transition
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		_, out, err = s.TransitionsIn(ctx, tx, key, actor)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// TransitionsIn is Transitions inside the caller's transaction, so it sees
// what that transaction has already changed; the issue comes back with them.
func (s *Service) TransitionsIn(ctx context.Context, tx db.DBTX, key string, actor Actor) (*Issue, []workflow.Transition, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, nil, err
	}
	issue, err := scanIssue(tx.QueryRow(ctx,
		selectIssue+` WHERE p.key = $1 AND i.key_num = $2`, projectKey, num))
	if err != nil {
		return nil, nil, err
	}
	wf, err := s.workflows.ForIssueType(ctx, tx, issue.ProjectID, issue.Type.ID)
	if err != nil {
		return nil, nil, err
	}
	offered, err := s.engine.Available(wf, subjectOf(issue), actor.toWorkflow())
	if err != nil {
		return nil, nil, err
	}
	return issue, offered, nil
}

// TransitionInput is what the user supplied along with a chosen transition.
type TransitionInput struct {
	TransitionID uuid.UUID
	Comment      string
	Assignee     *uuid.UUID
	Resolution   string
}

// Transition moves an issue through its workflow in one transaction, so a
// failure in a post-function or observer leaves the issue exactly as it was.
func (s *Service) Transition(ctx context.Context, key string, in TransitionInput, actor Actor) (*Issue, db.LSN, error) {
	if _, _, err := ParseKey(key); err != nil {
		return nil, 0, err
	}
	var updated *Issue
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		updated, err = s.TransitionIn(ctx, tx, key, in, actor)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return updated, lsn, nil
}

// TransitionIn is Transition inside the caller's transaction, observers and
// event included, so a later refusal in that transaction undoes the move too.
func (s *Service) TransitionIn(ctx context.Context, tx db.DBTX, key string, in TransitionInput, actor Actor) (*Issue, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, err
	}
	before, err := scanIssue(tx.QueryRow(ctx,
		selectIssue+` WHERE p.key = $1 AND i.key_num = $2 FOR UPDATE OF i`, projectKey, num))
	if err != nil {
		return nil, err
	}

	wf, err := s.workflows.ForIssueType(ctx, tx, before.ProjectID, before.Type.ID)
	if err != nil {
		return nil, err
	}

	decision, err := s.engine.Evaluate(wf, in.TransitionID, subjectOf(before), actor.toWorkflow(), workflow.Input{
		Comment:    in.Comment,
		Assignee:   in.Assignee,
		Resolution: in.Resolution,
	})
	if err != nil {
		return nil, err
	}

	changes := []Change{{
		Field: "status",
		From:  before.Status.Name,
		To:    decision.ToStatus.Name,
	}}

	if _, err := tx.Exec(ctx, `UPDATE issue SET status_id = $2 WHERE id = $1`,
		before.ID, decision.ToStatus.ID); err != nil {
		return nil, err
	}

	if decision.Effect.ClearAssign {
		change, err := s.applyAssignee(ctx, tx, before, nil)
		if err != nil {
			return nil, err
		}
		if change != nil {
			changes = append(changes, *change)
		}
	} else if decision.Effect.Assignee != nil {
		change, err := s.applyAssignee(ctx, tx, before, decision.Effect.Assignee)
		if err != nil {
			return nil, err
		}
		if change != nil {
			changes = append(changes, *change)
		}
	}

	if decision.Effect.Resolved != nil {
		if *decision.Effect.Resolved {
			if _, err := tx.Exec(ctx, `UPDATE issue SET resolved_at = now() WHERE id = $1`, before.ID); err != nil {
				return nil, err
			}
			changes = append(changes, Change{Field: "resolution", To: resolutionOr(in.Resolution, "Done")})
		} else {
			if _, err := tx.Exec(ctx, `UPDATE issue SET resolved_at = NULL WHERE id = $1`, before.ID); err != nil {
				return nil, err
			}
			changes = append(changes, Change{Field: "resolution", From: "Done", To: "Unresolved"})
		}
	}

	// The user's own comment first, then anything the workflow adds, so
	// the reading order matches what happened.
	if strings.TrimSpace(in.Comment) != "" {
		if err := s.insertComment(ctx, tx, before.ID, &actor.UserID, plainTextDoc(in.Comment), false); err != nil {
			return nil, err
		}
	}
	for _, text := range decision.Effect.Comments {
		if err := s.insertComment(ctx, tx, before.ID, nil, plainTextDoc(text), false); err != nil {
			return nil, err
		}
	}

	if err := s.recordHistory(ctx, tx, before.ID, actor.UserID, changes); err != nil {
		return nil, err
	}

	updated, err := scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, before.ID))
	if err != nil {
		return nil, err
	}
	if err := s.announceMove(ctx, tx, before, updated, decision.Transition.Name, actor); err != nil {
		return nil, err
	}
	return updated, nil
}

// announceMove tells the observers and the outbox that the status changed, so
// clocks, automation and mail hear of every move, not only a transition's.
func (s *Service) announceMove(ctx context.Context, tx db.DBTX, before, after *Issue, transition string, actor Actor) error {
	for _, o := range s.observers {
		if err := o.IssueTransitioned(ctx, tx, before, after, actor); err != nil {
			return err
		}
	}
	return events.EmitInTenant(ctx, tx, events.TopicIssueTransitioned, map[string]any{
		"issueId":    after.ID,
		"key":        after.Key,
		"transition": transition,
		"fromStatus": before.Status.Name,
		"toStatus":   after.Status.Name,
		"actorId":    actor.UserID,
	})
}
