package issue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// WorklogInput is one stretch of time as it was reported.
type WorklogInput struct {
	Minutes int
	// StartedOn is the day the work was done; nil is today.
	StartedOn *time.Time
	Note      string
}

// MaxWorklogNote keeps a note a note.
const MaxWorklogNote = 2000

func checkWorklog(in WorklogInput) (WorklogInput, error) {
	if in.Minutes <= 0 {
		return in, fmt.Errorf("%w: log at least a minute", ErrBadDuration)
	}
	if in.Minutes > MaxWorklogMinutes {
		return in, fmt.Errorf("%w: one entry covers at most %s; log a longer stretch as several", ErrBadDuration, FormatMinutes(MaxWorklogMinutes))
	}
	if len([]rune(in.Note)) > MaxWorklogNote {
		return in, fmt.Errorf("a worklog note must be %d characters or fewer", MaxWorklogNote)
	}
	if in.StartedOn == nil {
		now := time.Now().UTC()
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		in.StartedOn = &day
	}
	in.Note = strings.TrimSpace(in.Note)
	return in, nil
}

const selectWorklog = `
SELECT w.id, w.issue_id, w.minutes, w.started_on, w.note, w.created_at, w.updated_at,
       u.id, u.name, u.email, u.avatar_url
FROM issue_worklog w
LEFT JOIN app_user u ON u.id = w.author_id`

func scanWorklog(row pgx.Row) (*Worklog, error) {
	var (
		w                  Worklog
		uid                *uuid.UUID
		name, mail, avatar *string
	)
	err := row.Scan(&w.ID, &w.IssueID, &w.Minutes, &w.StartedOn, &w.Note, &w.CreatedAt, &w.UpdatedAt, &uid, &name, &mail, &avatar)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWorklogNotFound
	}
	if err != nil {
		return nil, err
	}
	if uid != nil {
		w.Author = userRef(*uid, name, mail, avatar)
	}
	return &w, nil
}

// Worklogs lists the time logged on an issue, most recent day first.
func (s *Service) Worklogs(ctx context.Context, key string) ([]Worklog, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, err
	}
	out := []Worklog{}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var exists bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM issue i JOIN project p ON p.id = i.project_id WHERE p.key = $1 AND i.key_num = $2)`,
			projectKey, num).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		rows, err := tx.Query(ctx, selectWorklog+`
			JOIN issue i ON i.id = w.issue_id
			JOIN project p ON p.id = i.project_id
			WHERE p.key = $1 AND i.key_num = $2
			ORDER BY w.started_on DESC, w.created_at DESC`, projectKey, num)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			w, err := scanWorklog(rows)
			if err != nil {
				return err
			}
			out = append(out, *w)
		}
		return rows.Err()
	})
	return out, err
}

// LogWork records time on an issue and takes it off what remains: an explicit
// remaining estimate comes down by the minutes logged, and an issue with only
// an original estimate gets a remaining one from what is left of it.
func (s *Service) LogWork(ctx context.Context, key string, in WorklogInput, actor Actor) (*Worklog, db.LSN, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, 0, err
	}
	in, err = checkWorklog(in)
	if err != nil {
		return nil, 0, err
	}
	var logged *Worklog
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE p.key = $1 AND i.key_num = $2 FOR UPDATE OF i`, projectKey, num))
		if err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO issue_worklog (org_id, issue_id, author_id, minutes, started_on, note)
			VALUES (current_org_id(), $1, $2, $3, $4, $5) RETURNING id`,
			before.ID, actor.UserID, in.Minutes, *in.StartedOn, in.Note).Scan(&id); err != nil {
			return fmt.Errorf("log work: %w", err)
		}

		changes := []Change{{Field: "timeSpent", From: FormatMinutes(before.TimeSpentMinutes), To: FormatMinutes(before.TimeSpentMinutes + in.Minutes)}}
		var remaining *int
		switch {
		case before.TimeRemainingMinutes != nil:
			left := max(0, *before.TimeRemainingMinutes-in.Minutes)
			remaining = &left
		case before.TimeEstimateMinutes != nil:
			left := max(0, *before.TimeEstimateMinutes-before.TimeSpentMinutes-in.Minutes)
			remaining = &left
		}
		if remaining != nil {
			change, err := applyMinutes(ctx, tx, before.ID, "time_remaining_minutes", "timeRemaining", before.TimeRemainingMinutes, remaining)
			if err != nil {
				return err
			}
			if change != nil {
				changes = append(changes, *change)
			}
		}
		if err := s.recordHistory(ctx, tx, before.ID, actor.UserID, changes); err != nil {
			return err
		}
		logged, err = scanWorklog(tx.QueryRow(ctx, selectWorklog+` WHERE w.id = $1`, id))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, events.TopicWorkLogged, map[string]any{
			"issueId": before.ID, "key": before.Key, "worklogId": id, "minutes": in.Minutes, "actorId": actor.UserID,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return logged, lsn, nil
}

// UpdateWorklog corrects an entry. Only its author, or an administrator, may.
func (s *Service) UpdateWorklog(ctx context.Context, id uuid.UUID, in WorklogInput, actor Actor) (*Worklog, db.LSN, error) {
	in, err := checkWorklog(in)
	if err != nil {
		return nil, 0, err
	}
	var updated *Worklog
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanWorklog(tx.QueryRow(ctx, selectWorklog+` WHERE w.id = $1 FOR UPDATE OF w`, id))
		if err != nil {
			return err
		}
		if err := mayTouch(before, actor); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE issue_worklog SET minutes = $2, started_on = $3, note = $4 WHERE id = $1`,
			id, in.Minutes, *in.StartedOn, in.Note); err != nil {
			return err
		}
		if before.Minutes != in.Minutes {
			if err := s.recordHistory(ctx, tx, before.IssueID, actor.UserID, []Change{{
				Field: "worklog", From: FormatMinutes(before.Minutes), To: FormatMinutes(in.Minutes),
			}}); err != nil {
				return err
			}
		}
		updated, err = scanWorklog(tx.QueryRow(ctx, selectWorklog+` WHERE w.id = $1`, id))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return updated, lsn, nil
}

// DeleteWorklog removes an entry. What remains is left alone: the estimate of
// what is left was a judgement, and it does not become wrong because a
// mistaken entry went.
func (s *Service) DeleteWorklog(ctx context.Context, id uuid.UUID, actor Actor) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanWorklog(tx.QueryRow(ctx, selectWorklog+` WHERE w.id = $1 FOR UPDATE OF w`, id))
		if err != nil {
			return err
		}
		if err := mayTouch(before, actor); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM issue_worklog WHERE id = $1`, id); err != nil {
			return err
		}
		return s.recordHistory(ctx, tx, before.IssueID, actor.UserID, []Change{{Field: "worklog", From: FormatMinutes(before.Minutes)}})
	})
}

func mayTouch(w *Worklog, actor Actor) error {
	mine := w.Author != nil && w.Author.ID == actor.UserID
	if !mine && !actor.OrgRole.CanAdminister() {
		return ErrNotYourWorklog
	}
	return nil
}
