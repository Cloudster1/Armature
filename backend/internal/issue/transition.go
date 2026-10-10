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
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, err
	}

	var out []workflow.Transition
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		issue, err := scanIssue(tx.QueryRow(ctx,
			selectIssue+` WHERE p.key = $1 AND i.key_num = $2`, projectKey, num))
		if err != nil {
			return err
		}
		wf, err := s.workflows.ForIssueType(ctx, tx, issue.ProjectID, issue.Type.ID)
		if err != nil {
			return err
		}
		out, err = s.engine.Available(wf, subjectOf(issue), actor.toWorkflow())
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// TransitionInput is what the user supplied along with a chosen transition.
type TransitionInput struct {
	TransitionID uuid.UUID
	Comment      string
	Assignee     *uuid.UUID
	Resolution   string
}

// Transition moves an issue through its workflow.
//
// Everything happens in one transaction: the workflow decides, the status
// changes, the post-functions' effects are applied, the changelog is written
// and the event is emitted. A failure anywhere leaves the issue exactly as it
// was.
func (s *Service) Transition(ctx context.Context, key string, in TransitionInput, actor Actor) (*Issue, db.LSN, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, 0, err
	}

	var updated *Issue
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanIssue(tx.QueryRow(ctx,
			selectIssue+` WHERE p.key = $1 AND i.key_num = $2 FOR UPDATE OF i`, projectKey, num))
		if err != nil {
			return err
		}

		wf, err := s.workflows.ForIssueType(ctx, tx, before.ProjectID, before.Type.ID)
		if err != nil {
			return err
		}

		decision, err := s.engine.Evaluate(wf, in.TransitionID, subjectOf(before), actor.toWorkflow(), workflow.Input{
			Comment:    in.Comment,
			Assignee:   in.Assignee,
			Resolution: in.Resolution,
		})
		if err != nil {
			return err
		}

		changes := []Change{{
			Field: "status",
			From:  before.Status.Name,
			To:    decision.ToStatus.Name,
		}}

		if _, err := tx.Exec(ctx, `UPDATE issue SET status_id = $2 WHERE id = $1`,
			before.ID, decision.ToStatus.ID); err != nil {
			return err
		}

		if decision.Effect.ClearAssign {
			change, err := s.applyAssignee(ctx, tx, before, nil)
			if err != nil {
				return err
			}
			if change != nil {
				changes = append(changes, *change)
			}
		} else if decision.Effect.Assignee != nil {
			change, err := s.applyAssignee(ctx, tx, before, decision.Effect.Assignee)
			if err != nil {
				return err
			}
			if change != nil {
				changes = append(changes, *change)
			}
		}

		if decision.Effect.Resolved != nil {
			if *decision.Effect.Resolved {
				if _, err := tx.Exec(ctx, `UPDATE issue SET resolved_at = now() WHERE id = $1`, before.ID); err != nil {
					return err
				}
				changes = append(changes, Change{Field: "resolution", To: resolutionOr(in.Resolution, "Done")})
			} else {
				if _, err := tx.Exec(ctx, `UPDATE issue SET resolved_at = NULL WHERE id = $1`, before.ID); err != nil {
					return err
				}
				changes = append(changes, Change{Field: "resolution", From: "Done", To: "Unresolved"})
			}
		}

		// The user's own comment first, then anything the workflow adds, so
		// the reading order matches what happened.
		if strings.TrimSpace(in.Comment) != "" {
			if err := s.insertComment(ctx, tx, before.ID, &actor.UserID, plainTextDoc(in.Comment), false); err != nil {
				return err
			}
		}
		for _, text := range decision.Effect.Comments {
			if err := s.insertComment(ctx, tx, before.ID, nil, plainTextDoc(text), false); err != nil {
				return err
			}
		}

		if err := s.recordHistory(ctx, tx, before.ID, actor.UserID, changes); err != nil {
			return err
		}

		updated, err = scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, before.ID))
		if err != nil {
			return err
		}
		return s.announceMove(ctx, tx, before, updated, decision.Transition.Name, actor)
	})
	if err != nil {
		return nil, 0, err
	}
	return updated, lsn, nil
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

func subjectOf(i *Issue) workflow.Subject {
	subject := workflow.Subject{
		IssueID:     i.ID,
		ProjectID:   i.ProjectID,
		IssueTypeID: i.Type.ID,
		StatusID:    i.Status.ID,
		HasParent:   i.ParentID != nil,
	}
	if i.Assignee != nil {
		subject.AssigneeID = &i.Assignee.ID
	}
	if i.Reporter != nil {
		subject.ReporterID = &i.Reporter.ID
	}
	return subject
}
