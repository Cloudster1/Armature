package issue

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

var (
	// ErrMilestoneNotFound is returned for a milestone that is not in this project.
	ErrMilestoneNotFound = errors.New("that milestone is not in this project")
	// ErrMilestoneClosed is returned when assigning work to a closed milestone.
	ErrMilestoneClosed = errors.New("that milestone is closed")
)

// SetMilestone makes an issue count towards a milestone, or towards none.
//
// Its own endpoint, like the sprint: which milestone a piece of work counts
// towards is a planning decision, and it is refused for reasons an ordinary
// field edit never has.
func (s *Service) SetMilestone(ctx context.Context, key string, milestoneID *uuid.UUID, actor Actor) (*Issue, db.LSN, error) {
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
		if equalUUID(before.MilestoneID, milestoneID) {
			updated = before
			return nil
		}

		to := ""
		if milestoneID != nil {
			var closed bool
			err := tx.QueryRow(ctx, `
				SELECT name, closed_at IS NOT NULL FROM milestone WHERE id = $1 AND project_id = $2`,
				*milestoneID, before.ProjectID).Scan(&to, &closed)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrMilestoneNotFound
			}
			if err != nil {
				return err
			}
			if closed {
				return fmt.Errorf("%w: %s", ErrMilestoneClosed, to)
			}
		}

		from := ""
		if before.Milestone != nil {
			from = before.Milestone.Name
		}

		if _, err := tx.Exec(ctx, `UPDATE issue SET milestone_id = $2 WHERE id = $1`,
			before.ID, milestoneID); isCheckViolation(err) {
			return ErrMilestoneNotFound
		} else if err != nil {
			return fmt.Errorf("assign the issue to a milestone: %w", err)
		}

		changes := []Change{{Field: "milestone", From: from, To: to}}
		if err := s.recordHistory(ctx, tx, before.ID, actor.UserID, changes); err != nil {
			return err
		}

		updated, err = scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, before.ID))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, events.TopicIssueUpdated, map[string]any{
			"issueId": updated.ID, "key": updated.Key, "changes": changes, "actorId": actor.UserID,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return updated, lsn, nil
}
