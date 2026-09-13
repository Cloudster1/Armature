package issue

import (
	"context"
	"fmt"
	"time"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// ScheduleInput is the stretch of time an issue occupies. A nil field is left
// alone; a set field holding nil clears that end.
type ScheduleInput struct {
	Start *(*time.Time)
	Due   *(*time.Time)
}

// Schedule moves an issue in time. Both ends are written in one statement, so
// the range is never briefly backwards.
func (s *Service) Schedule(ctx context.Context, key string, in ScheduleInput, actor Actor) (*Issue, db.LSN, error) {
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

		start, due := before.StartDate, before.DueDate
		if in.Start != nil {
			start = *in.Start
		}
		if in.Due != nil {
			due = *in.Due
		}
		if start != nil && due != nil && due.Before(*start) {
			return fmt.Errorf("%w: %s ends before %s",
				ErrBackwardsRange, formatDate(due), formatDate(start))
		}

		var changes []Change
		if !sameDay(before.StartDate, start) {
			changes = append(changes, Change{Field: "startDate", From: formatDate(before.StartDate), To: formatDate(start)})
		}
		if !sameDay(before.DueDate, due) {
			changes = append(changes, Change{Field: "dueDate", From: formatDate(before.DueDate), To: formatDate(due)})
		}
		if len(changes) == 0 {
			updated, err = scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, before.ID))
			return err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE issue SET start_date = $2, due_date = $3 WHERE id = $1`,
			before.ID, start, due); err != nil {
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

// sameDay compares two optional dates, treating absence as a value.
func sameDay(a, b *time.Time) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return a.Year() == b.Year() && a.YearDay() == b.YearDay()
	}
}
