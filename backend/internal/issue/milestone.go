package issue

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
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
	return s.writeAndAnnounce(ctx, key, actor, func(ctx context.Context, tx db.DBTX) (*Issue, []Change, error) {
		return s.SetMilestoneIn(ctx, tx, key, milestoneID)
	})
}

// SetMilestoneIn is SetMilestone inside the caller's transaction, leaving the
// change for the caller to record and announce.
func (s *Service) SetMilestoneIn(ctx context.Context, tx db.DBTX, key string, milestoneID *uuid.UUID) (*Issue, []Change, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, nil, err
	}
	before, err := scanIssue(tx.QueryRow(ctx,
		selectIssue+` WHERE p.key = $1 AND i.key_num = $2 FOR UPDATE OF i`, projectKey, num))
	if err != nil {
		return nil, nil, err
	}
	if equalUUID(before.MilestoneID, milestoneID) {
		return before, nil, nil
	}

	to := ""
	if milestoneID != nil {
		var closed bool
		err := tx.QueryRow(ctx, `
			SELECT name, closed_at IS NOT NULL FROM milestone WHERE id = $1 AND project_id = $2`,
			*milestoneID, before.ProjectID).Scan(&to, &closed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrMilestoneNotFound
		}
		if err != nil {
			return nil, nil, err
		}
		if closed {
			return nil, nil, fmt.Errorf("%w: %s", ErrMilestoneClosed, to)
		}
	}

	from := ""
	if before.Milestone != nil {
		from = before.Milestone.Name
	}

	if _, err := tx.Exec(ctx, `UPDATE issue SET milestone_id = $2 WHERE id = $1`,
		before.ID, milestoneID); isCheckViolation(err) {
		return nil, nil, ErrMilestoneNotFound
	} else if err != nil {
		return nil, nil, fmt.Errorf("assign the issue to a milestone: %w", err)
	}

	updated, err := scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, before.ID))
	if err != nil {
		return nil, nil, err
	}
	return updated, []Change{{Field: "milestone", From: from, To: to}}, nil
}
