package sprint

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
)

// Start begins a sprint.
//
// It needs both dates, because a sprint with no end never ends, and it needs
// the project to have no other sprint running, because otherwise "the sprint"
// stops meaning anything.
func (s *Service) Start(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*Sprint, db.LSN, error) {
	// The first point of the burndown is what the sprint held when it began,
	// counted before the write so the transaction stays short.
	var totals *Totals
	if s.counter != nil {
		found, err := s.ByID(ctx, id)
		if err != nil {
			return nil, 0, err
		}
		counted, err := s.counter.SprintTotals(ctx, found.ProjectKey, id)
		if err != nil {
			return nil, 0, err
		}
		totals = &counted
	}

	var out *Sprint
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanSprint(tx.QueryRow(ctx, selectSprint+` WHERE s.id = $1 FOR UPDATE OF s`, id))
		if err != nil {
			return err
		}
		switch {
		case before.State == StateActive:
			return fmt.Errorf("%w: %s is already running", ErrNotStartable, before.Name)
		case before.State == StateClosed:
			return fmt.Errorf("%w: %s is over", ErrNotStartable, before.Name)
		case before.StartsOn == nil || before.EndsOn == nil:
			return fmt.Errorf("%w: %s needs a start and an end first", ErrNotStartable, before.Name)
		}

		// One running sprint per team, and one for the project's own work. Two
		// teams running at once is the point of giving them separate backlogs.
		var running string
		err = tx.QueryRow(ctx, `
			SELECT name FROM sprint
			WHERE project_id = $1 AND team_id IS NOT DISTINCT FROM $2 AND state = 'active'`,
			before.ProjectID, before.TeamID).Scan(&running)
		if err == nil {
			return fmt.Errorf("%w: %s is already running for %s",
				ErrNotStartable, running, whose(before))
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		if _, err := tx.Exec(ctx, `UPDATE sprint SET state = 'active' WHERE id = $1`, id); isUniqueViolation(err) {
			// Two people started two sprints at once; the index decided.
			return fmt.Errorf("%w: another sprint started first", ErrNotStartable)
		} else if err != nil {
			return fmt.Errorf("start sprint: %w", err)
		}

		out, err = scanSprint(tx.QueryRow(ctx, selectSprint+` WHERE s.id = $1`, id))
		if err != nil {
			return err
		}
		if totals != nil {
			if _, err := writeSnapshot(ctx, tx, out, time.Now(), *totals); err != nil {
				return err
			}
		}
		return events.EmitInTenant(ctx, tx, "sprint.started", map[string]any{
			"sprintId": id, "projectId": before.ProjectID, "name": before.Name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// CompleteInput says where the unfinished work should go.
type CompleteInput struct {
	// MoveTo is the sprint to carry the unfinished work into. Nil sends it back
	// to the backlog, which is the honest default: work nobody has committed to
	// yet should not look committed.
	MoveTo *uuid.UUID
}

// Report is what a sprint turned out to be.
type Report struct {
	Sprint Sprint `json:"sprint"`
	// Committed and Completed are the totals as they stood at the moment the
	// sprint closed, which is the only moment they are true of.
	Committed float64 `json:"committed"`
	Completed float64 `json:"completed"`
	// Finished and Carried are counts of issues, not points: an unestimated
	// issue is still work that was or was not done.
	Finished int `json:"finished"`
	Carried  int `json:"carried"`
	// CarriedTo names where the unfinished work went, or is empty for the backlog.
	CarriedTo string `json:"carriedTo,omitempty"`
}

// Complete ends a sprint and carries its unfinished work somewhere.
//
// The totals are taken before anything moves, because after the move they would
// describe the destination rather than the sprint being reported on.
func (s *Service) Complete(ctx context.Context, id uuid.UUID, in CompleteInput, actor uuid.UUID) (*Report, db.LSN, error) {
	running, err := s.ByID(ctx, id)
	if err != nil {
		return nil, 0, err
	}
	if !running.Running() {
		return nil, 0, fmt.Errorf("%w: %s", ErrNotRunning, running.Name)
	}

	var totals Totals
	if s.counter != nil {
		totals, err = s.counter.SprintTotals(ctx, running.ProjectKey, id)
		if err != nil {
			return nil, 0, err
		}
	}
	committed, completed := totals.Committed, totals.Completed

	report := &Report{Committed: committed, Completed: completed}
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanSprint(tx.QueryRow(ctx, selectSprint+` WHERE s.id = $1 FOR UPDATE OF s`, id))
		if err != nil {
			return err
		}
		if !before.Running() {
			return fmt.Errorf("%w: %s", ErrNotRunning, before.Name)
		}

		destination, err := s.resolveDestination(ctx, tx, before, in.MoveTo)
		if err != nil {
			return err
		}
		if destination != nil {
			report.CarriedTo = destination.Name
		}

		if err := tx.QueryRow(ctx, `
			SELECT count(*) FROM issue i
			JOIN issue_status st ON st.id = i.status_id
			WHERE i.sprint_id = $1 AND st.category = 'done'`, id).Scan(&report.Finished); err != nil {
			return fmt.Errorf("count finished work: %w", err)
		}

		// The last point of the burndown is taken before the carry-over, since
		// afterwards the unfinished work is somebody else's sprint's.
		if s.counter != nil {
			if _, err := writeSnapshot(ctx, tx, before, time.Now(), totals); err != nil {
				return err
			}
		}

		report.Carried, err = s.carryOver(ctx, tx, before, destination, actor)
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE sprint SET state = 'closed', committed = $2, completed = $3, finished = $4, carried = $5 WHERE id = $1`,
			id, committed, completed, report.Finished, report.Carried); err != nil {
			return fmt.Errorf("complete sprint: %w", err)
		}

		closed, err := scanSprint(tx.QueryRow(ctx, selectSprint+` WHERE s.id = $1`, id))
		if err != nil {
			return err
		}
		report.Sprint = *closed

		return events.EmitInTenant(ctx, tx, "sprint.completed", map[string]any{
			"sprintId": id, "projectId": before.ProjectID, "name": before.Name,
			"committed": committed, "completed": completed,
			"carried": report.Carried, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return report, lsn, nil
}

// resolveDestination checks that the sprint the work is being carried into can
// actually take it.
func (s *Service) resolveDestination(ctx context.Context, tx db.DBTX, from *Sprint, moveTo *uuid.UUID) (*Sprint, error) {
	if moveTo == nil {
		return nil, nil
	}
	if *moveTo == from.ID {
		return nil, errors.New("a sprint cannot carry its work into itself")
	}
	destination, err := scanSprint(tx.QueryRow(ctx, selectSprint+` WHERE s.id = $1`, *moveTo))
	if err != nil {
		return nil, err
	}
	if destination.ProjectID != from.ProjectID {
		return nil, fmt.Errorf("%w: %s is in another project", ErrNotFound, destination.Name)
	}
	if destination.State == StateClosed {
		return nil, fmt.Errorf("%w: %s", ErrClosed, destination.Name)
	}
	return destination, nil
}

// carryOver moves the unfinished work out, leaving the same trail in each
// issue's changelog that moving it by hand would.
func (s *Service) carryOver(ctx context.Context, tx db.DBTX, from *Sprint, to *Sprint, actor uuid.UUID) (int, error) {
	var destination *uuid.UUID
	name := ""
	if to != nil {
		destination = &to.ID
		name = to.Name
	}

	rows, err := tx.Query(ctx, `
		UPDATE issue SET sprint_id = $2
		WHERE id IN (
		    SELECT i.id FROM issue i
		    JOIN issue_status st ON st.id = i.status_id
		    WHERE i.sprint_id = $1 AND st.category <> 'done'
		)
		RETURNING id`, from.ID, destination)
	if err != nil {
		return 0, fmt.Errorf("carry unfinished work over: %w", err)
	}

	var moved []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		moved = append(moved, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	changes := []issue.Change{{Field: "sprint", From: from.Name, To: name}}
	for _, id := range moved {
		if err := issue.RecordChanges(ctx, tx, id, actor, changes); err != nil {
			return 0, err
		}
	}
	return len(moved), nil
}

// Delete removes a sprint that has not run. Its work returns to the backlog
// rather than disappearing with it.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		found, err := scanSprint(tx.QueryRow(ctx, selectSprint+` WHERE s.id = $1 FOR UPDATE OF s`, id))
		if err != nil {
			return err
		}
		if !found.Planned() {
			return fmt.Errorf("%w: %s has already run, and its report is part of the record",
				ErrClosed, found.Name)
		}
		// The foreign key would null the column on its own; doing it here means
		// the issues are released before the row goes, in one transaction.
		if _, err := tx.Exec(ctx, `UPDATE issue SET sprint_id = NULL WHERE sprint_id = $1`, id); err != nil {
			return fmt.Errorf("release the sprint's work: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sprint WHERE id = $1`, id); err != nil {
			return fmt.Errorf("delete sprint: %w", err)
		}
		return nil
	})
}

// whose names the sprint's owner the way a person would say it.
func whose(s *Sprint) string {
	if s.TeamName == "" {
		return "this project"
	}
	return s.TeamName
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}
