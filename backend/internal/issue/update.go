package issue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/workflow"
)

// UpdateInput carries the fields an edit may change. A nil field is left alone,
// which is what separates "not mentioned" from "cleared".
type UpdateInput struct {
	Summary     *string
	Description *json.RawMessage
	Priority    *Priority
	// Assignee is a pointer to a pointer so that clearing the assignee can be
	// expressed distinctly from not touching it.
	Assignee *(*uuid.UUID)
	DueDate  *(*time.Time)
	TypeID   *uuid.UUID
	// ReporterID hands the issue's authorship to somebody else, which a desk
	// does when it files on a customer's behalf.
	ReporterID *uuid.UUID
	// TimeEstimate and TimeRemaining are minutes; the inner nil clears.
	TimeEstimate  *(*int)
	TimeRemaining *(*int)
}

// Update edits an issue's fields and records every change in the changelog.
//
// The status is deliberately not editable here. Moving between statuses goes
// through Transition, so the workflow's conditions, validators and
// post-functions cannot be bypassed by editing the field directly.
func (s *Service) Update(ctx context.Context, key string, in UpdateInput, actor Actor) (*Issue, db.LSN, error) {
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

		var changes []Change

		if in.Summary != nil {
			summary := strings.TrimSpace(*in.Summary)
			if summary == "" {
				return errors.New("an issue needs a summary")
			}
			if len([]rune(summary)) > MaxSummary {
				return fmt.Errorf("the summary must be %d characters or fewer", MaxSummary)
			}
			if summary != before.Summary {
				if _, err := tx.Exec(ctx, `UPDATE issue SET summary = $2 WHERE id = $1`, before.ID, summary); err != nil {
					return err
				}
				changes = append(changes, Change{Field: "summary", From: before.Summary, To: summary})
			}
		}

		if in.Description != nil {
			body := *in.Description
			if IsEmptyDocument(body) {
				body = nil
			} else if err := ValidateDocument(body, "description"); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE issue SET description = $2 WHERE id = $1`,
				before.ID, nullableJSON(body)); err != nil {
				return err
			}
			// The body of a rich text document is not worth reproducing in the
			// changelog; that it changed, and when, is the useful part.
			to := "updated"
			if nullableJSON(body) == nil {
				to = "cleared"
			}
			changes = append(changes, Change{Field: "description", To: to})
			if body != nil {
				if _, err := s.mentionWatchers(ctx, tx, before.ID, before.Key, body, actor); err != nil {
					return err
				}
			}
		}

		if in.Priority != nil {
			if !in.Priority.Valid() {
				return fmt.Errorf("%q is not a priority", *in.Priority)
			}
			if *in.Priority != before.Priority {
				if _, err := tx.Exec(ctx, `UPDATE issue SET priority = $2 WHERE id = $1`,
					before.ID, string(*in.Priority)); err != nil {
					return err
				}
				changes = append(changes, Change{Field: "priority", From: string(before.Priority), To: string(*in.Priority)})
			}
		}

		if in.Assignee != nil {
			change, err := s.applyAssignee(ctx, tx, before, *in.Assignee)
			if err != nil {
				return err
			}
			if change != nil {
				changes = append(changes, *change)
			}
		}

		if in.DueDate != nil {
			if _, err := tx.Exec(ctx, `UPDATE issue SET due_date = $2 WHERE id = $1`, before.ID, *in.DueDate); err != nil {
				return err
			}
			changes = append(changes, Change{
				Field: "dueDate",
				From:  formatDate(before.DueDate),
				To:    formatDate(*in.DueDate),
			})
		}

		if in.ReporterID != nil {
			change, err := s.applyReporter(ctx, tx, before, *in.ReporterID)
			if err != nil {
				return err
			}
			if change != nil {
				changes = append(changes, *change)
			}
		}

		if in.TimeEstimate != nil {
			change, err := applyMinutes(ctx, tx, before.ID, "time_estimate_minutes", "timeEstimate", before.TimeEstimateMinutes, *in.TimeEstimate)
			if err != nil {
				return err
			}
			if change != nil {
				changes = append(changes, *change)
			}
		}
		if in.TimeRemaining != nil {
			change, err := applyMinutes(ctx, tx, before.ID, "time_remaining_minutes", "timeRemaining", before.TimeRemainingMinutes, *in.TimeRemaining)
			if err != nil {
				return err
			}
			if change != nil {
				changes = append(changes, *change)
			}
		}

		if in.TypeID != nil && *in.TypeID != before.Type.ID {
			change, err := s.applyType(ctx, tx, before, *in.TypeID)
			if err != nil {
				return err
			}
			changes = append(changes, *change)
		}

		if len(changes) == 0 {
			// Nothing actually moved. Recording an empty changelog entry would
			// fill the history with noise from clients that resend unchanged
			// forms.
			updated, err = scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, before.ID))
			return err
		}

		if err := s.recordHistory(ctx, tx, before.ID, actor.UserID, changes); err != nil {
			return err
		}

		updated, err = scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, before.ID))
		if err != nil {
			return err
		}

		return events.EmitInTenant(ctx, tx, events.TopicIssueUpdated, map[string]any{
			"issueId": updated.ID,
			"key":     updated.Key,
			"changes": changes,
			"actorId": actor.UserID,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return updated, lsn, nil
}

// applyAssignee writes a new assignee and describes the change.
func (s *Service) applyAssignee(ctx context.Context, tx db.DBTX, before *Issue, assignee *uuid.UUID) (*Change, error) {
	var currentID *uuid.UUID
	if before.Assignee != nil {
		currentID = &before.Assignee.ID
	}
	if equalUUID(currentID, assignee) {
		return nil, nil
	}

	toName := "Unassigned"
	if assignee != nil {
		if err := tx.QueryRow(ctx, `
			SELECT u.name FROM app_user u
			JOIN org_member m ON m.user_id = u.id AND m.org_id = current_org_id()
			WHERE u.id = $1`, *assignee).Scan(&toName); errors.Is(err, pgx.ErrNoRows) {
			// Assigning to somebody outside the organization would leak the
			// issue to them the moment they were added.
			return nil, errors.New("that person is not a member of this organization")
		} else if err != nil {
			return nil, err
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE issue SET assignee_id = $2 WHERE id = $1`, before.ID, assignee); err != nil {
		return nil, err
	}

	fromName := "Unassigned"
	if before.Assignee != nil {
		fromName = before.Assignee.Name
	}
	return &Change{Field: "assignee", From: fromName, To: toName}, nil
}

// applyReporter records who raised the issue, which has to be a member.
func (s *Service) applyReporter(ctx context.Context, tx db.DBTX, before *Issue, reporter uuid.UUID) (*Change, error) {
	if before.Reporter != nil && before.Reporter.ID == reporter {
		return nil, nil
	}
	var name string
	if err := tx.QueryRow(ctx, `
		SELECT u.name FROM app_user u
		JOIN org_member m ON m.user_id = u.id AND m.org_id = current_org_id()
		WHERE u.id = $1`, reporter).Scan(&name); errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("that person is not a member of this organization")
	} else if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE issue SET reporter_id = $2 WHERE id = $1`, before.ID, reporter); err != nil {
		return nil, err
	}
	from := "Unknown"
	if before.Reporter != nil {
		from = before.Reporter.Name
	}
	return &Change{Field: "reporter", From: from, To: name}, nil
}

// applyMinutes writes one of the time columns and describes the change in the
// words people use for time. Nil clears; a nil to nil edit is nothing.
func applyMinutes(ctx context.Context, tx db.DBTX, issueID uuid.UUID, column, field string, before *int, after *int) (*Change, error) {
	if after != nil && (*after < 0 || *after > 365*8*60) {
		return nil, fmt.Errorf("%w: minutes must be between 0 and a year of working days", ErrBadDuration)
	}
	if before == nil && after == nil || before != nil && after != nil && *before == *after {
		return nil, nil
	}
	// The column is one of two names this package chose, never the caller's.
	if _, err := tx.Exec(ctx, `UPDATE issue SET `+column+` = $2 WHERE id = $1`, issueID, after); err != nil {
		return nil, err
	}
	return &Change{Field: field, From: formatMinutesOrNone(before), To: formatMinutesOrNone(after)}, nil
}

func formatMinutesOrNone(minutes *int) string {
	if minutes == nil {
		return ""
	}
	return FormatMinutes(*minutes)
}

// applyType changes an issue's type, which moves it in the hierarchy and can
// also change which workflow it follows.
func (s *Service) applyType(ctx context.Context, tx db.DBTX, before *Issue, typeID uuid.UUID) (*Change, error) {
	var (
		name  string
		level int
	)
	err := tx.QueryRow(ctx, `SELECT name, hierarchy_level FROM issue_type WHERE id = $1`, typeID).Scan(&name, &level)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("that issue type does not exist")
	}
	if err != nil {
		return nil, err
	}
	if err := s.checkTypeFitsTheTree(ctx, tx, before, name, level); err != nil {
		return nil, err
	}

	// The new type may follow a different workflow, one that does not contain
	// the status the issue is currently in. Move it to that workflow's starting
	// status rather than leaving it somewhere the workflow cannot describe.
	wf, err := s.workflows.ForIssueType(ctx, tx, before.ProjectID, typeID)
	if err != nil {
		return nil, err
	}
	if _, inWorkflow := wf.StepByStatus(before.Status.ID); !inWorkflow {
		initial, err := wf.InitialStep()
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE issue SET status_id = $2 WHERE id = $1`, before.ID, initial.Status.ID); err != nil {
			return nil, err
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE issue SET issue_type_id = $2 WHERE id = $1`, before.ID, typeID); err != nil {
		return nil, err
	}
	return &Change{Field: "type", From: before.Type.Name, To: name}, nil
}

// checkTypeFitsTheTree refuses a type change that would strand the issue's
// parent or its children, and says which so the user knows what to fix first.
func (s *Service) checkTypeFitsTheTree(ctx context.Context, tx db.DBTX, before *Issue, name string, level int) error {
	if before.Parent == nil {
		if NeedsParent(level) {
			return levelError{ErrNeedsParent, article(name) + " needs a parent issue"}
		}
	} else if !CanParent(before.Parent.Type.Level, level) {
		return levelError{ErrParentLevel, fmt.Sprintf("%s is %s, which cannot be the parent of %s",
			before.Parent.Key, article(before.Parent.Type.Name), article(name))}
	}

	var stranded int
	err := tx.QueryRow(ctx, `
		SELECT count(*) FROM issue c
		JOIN issue_type ct ON ct.id = c.issue_type_id
		WHERE c.parent_id = $1 AND ct.hierarchy_level <> $2`, before.ID, level-1).Scan(&stranded)
	if err != nil {
		return err
	}
	if stranded > 0 {
		return levelError{ErrChildrenInTheWay,
			fmt.Sprintf("%d of this issue's children cannot sit under %s", stranded, article(name))}
	}
	return nil
}

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
		for _, o := range s.observers {
			if err := o.IssueTransitioned(ctx, tx, before, updated, actor); err != nil {
				return err
			}
		}

		return events.EmitInTenant(ctx, tx, events.TopicIssueTransitioned, map[string]any{
			"issueId":    updated.ID,
			"key":        updated.Key,
			"transition": decision.Transition.Name,
			"fromStatus": before.Status.Name,
			"toStatus":   decision.ToStatus.Name,
			"actorId":    actor.UserID,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return updated, lsn, nil
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

// recordHistory appends to the changelog. It is the only writer of that table.
func (s *Service) recordHistory(ctx context.Context, tx db.DBTX, issueID, actorID uuid.UUID, changes []Change) error {
	return RecordChanges(ctx, tx, issueID, actorID, changes)
}

// RecordChanges writes a changelog entry inside the caller's transaction.
//
// It is exported so that a service which moves issues in bulk, such as one
// completing a sprint, leaves the same trail behind as an ordinary edit. A
// change nobody can see in the changelog is a change nobody can explain.
func RecordChanges(ctx context.Context, tx db.DBTX, issueID, actorID uuid.UUID, changes []Change) error {
	if len(changes) == 0 {
		return nil
	}
	encoded, err := json.Marshal(changes)
	if err != nil {
		return fmt.Errorf("encode changelog entry: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO issue_history (org_id, issue_id, actor_id, changes)
		VALUES (current_org_id(), $1, $2, $3)`, issueID, actorID, encoded)
	if err != nil {
		return fmt.Errorf("record changelog entry: %w", err)
	}
	return nil
}

func equalUUID(a, b *uuid.UUID) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}

func formatDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

func resolutionOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
