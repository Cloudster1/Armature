package issue

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

var (
	// ErrSprintNotFound is returned for a sprint that is not in this project.
	ErrSprintNotFound = errors.New("that sprint is not in this project")
	// ErrSprintClosed is returned when committing work to a sprint that is over.
	ErrSprintClosed = errors.New("that sprint is over")
	// ErrNegativeEstimate is returned for an estimate below zero.
	ErrNegativeEstimate = errors.New("an estimate cannot be negative")
	// ErrTeamNotFound is returned for a team that is not in this project.
	ErrTeamNotFound = errors.New("that team is not in this project")
	// ErrSprintOtherTeam is returned when committing work to a sprint another
	// team plans, where neither team's Sprints page would show it.
	ErrSprintOtherTeam = errors.New("that sprint is planned for another team")
)

// sprintRow is what deciding whether a sprint can take an issue needs.
type sprintRow struct {
	name, state string
	team        *uuid.UUID
}

func sprintOf(ctx context.Context, tx db.DBTX, projectID, sprintID uuid.UUID) (*sprintRow, error) {
	var row sprintRow
	err := tx.QueryRow(ctx, `SELECT name, state::text, team_id FROM sprint WHERE id = $1 AND project_id = $2`,
		sprintID, projectID).Scan(&row.name, &row.state, &row.team)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSprintNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// commitTo checks that a sprint can take work of the given team and answers
// with its name; the same sentences apply wherever work is committed.
func commitTo(ctx context.Context, tx db.DBTX, projectID, sprintID uuid.UUID, teamID *uuid.UUID) (string, error) {
	row, err := sprintOf(ctx, tx, projectID, sprintID)
	if err != nil {
		return "", err
	}
	if row.state == "closed" {
		return "", fmt.Errorf("%w: %s", ErrSprintClosed, row.name)
	}
	if !equalUUID(row.team, teamID) {
		return "", fmt.Errorf("%w: %s", ErrSprintOtherTeam, row.name)
	}
	return row.name, nil
}

// SetTeam hands an issue to a team, or takes it back to the project at large.
//
// Its own endpoint, like the sprint and the schedule: which team carries a
// piece of work decides which board it appears on and which backlog it sits in,
// and it is refused for reasons an ordinary field edit never has.
func (s *Service) SetTeam(ctx context.Context, key string, teamID *uuid.UUID, actor Actor) (*Issue, db.LSN, error) {
	return s.writeAndAnnounce(ctx, key, actor, func(ctx context.Context, tx db.DBTX) (*Issue, []Change, error) {
		return s.SetTeamIn(ctx, tx, key, teamID)
	})
}

// SetTeamIn is SetTeam inside the caller's transaction, leaving the change for
// the caller to record and announce.
func (s *Service) SetTeamIn(ctx context.Context, tx db.DBTX, key string, teamID *uuid.UUID) (*Issue, []Change, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, nil, err
	}
	before, err := scanIssue(tx.QueryRow(ctx,
		selectIssue+` WHERE p.key = $1 AND i.key_num = $2 FOR UPDATE OF i`, projectKey, num))
	if err != nil {
		return nil, nil, err
	}
	if equalUUID(before.TeamID, teamID) {
		return before, nil, nil
	}

	to := ""
	if teamID != nil {
		err := tx.QueryRow(ctx, `SELECT name FROM team WHERE id = $1 AND project_id = $2`,
			*teamID, before.ProjectID).Scan(&to)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrTeamNotFound
		}
		if err != nil {
			return nil, nil, err
		}
	}

	from := ""
	if before.Team != nil {
		from = before.Team.Name
	}

	changes := []Change{{Field: "team", From: from, To: to}}

	// Another stream's open sprint is not the new team's plan, so the issue
	// goes to its backlog. A closed sprint is history and keeps it.
	sprintID := before.SprintID
	if sprintID != nil {
		row, err := sprintOf(ctx, tx, before.ProjectID, *sprintID)
		if err != nil {
			return nil, nil, err
		}
		if row.state != "closed" && !equalUUID(row.team, teamID) {
			sprintID = nil
			changes = append(changes, Change{Field: "sprint", From: row.name})
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE issue SET team_id = $2, sprint_id = $3 WHERE id = $1`,
		before.ID, teamID, sprintID); isCheckViolation(err) {
		return nil, nil, ErrTeamNotFound
	} else if err != nil {
		return nil, nil, fmt.Errorf("hand the issue to a team: %w", err)
	}

	updated, err := scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, before.ID))
	if err != nil {
		return nil, nil, err
	}
	return updated, changes, nil
}

// SetSprint commits an issue to a sprint, or sends it back to the backlog.
//
// It is its own endpoint rather than a field on the ordinary edit for the same
// reason scheduling is: moving work between sprints is a decision people make
// on its own, and it is refused for reasons an ordinary field edit never has.
func (s *Service) SetSprint(ctx context.Context, key string, sprintID *uuid.UUID, actor Actor) (*Issue, db.LSN, error) {
	return s.writeAndAnnounce(ctx, key, actor, func(ctx context.Context, tx db.DBTX) (*Issue, []Change, error) {
		return s.SetSprintIn(ctx, tx, key, sprintID)
	})
}

// SetSprintIn is SetSprint inside the caller's transaction, leaving the change
// for the caller to record and announce.
func (s *Service) SetSprintIn(ctx context.Context, tx db.DBTX, key string, sprintID *uuid.UUID) (*Issue, []Change, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, nil, err
	}
	before, err := scanIssue(tx.QueryRow(ctx,
		selectIssue+` WHERE p.key = $1 AND i.key_num = $2 FOR UPDATE OF i`, projectKey, num))
	if err != nil {
		return nil, nil, err
	}
	if equalUUID(before.SprintID, sprintID) {
		return before, nil, nil
	}

	to := ""
	if sprintID != nil {
		if to, err = commitTo(ctx, tx, before.ProjectID, *sprintID, before.TeamID); err != nil {
			return nil, nil, err
		}
	}

	from := ""
	if before.Sprint != nil {
		from = before.Sprint.Name
	}

	if _, err := tx.Exec(ctx, `UPDATE issue SET sprint_id = $2 WHERE id = $1`,
		before.ID, sprintID); isCheckViolation(err) {
		return nil, nil, ErrSprintNotFound
	} else if err != nil {
		return nil, nil, fmt.Errorf("move the issue between sprints: %w", err)
	}

	updated, err := scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, before.ID))
	if err != nil {
		return nil, nil, err
	}
	return updated, []Change{{Field: "sprint", From: from, To: to}}, nil
}

// SetEstimate sizes an issue, or clears the estimate. Clearing is not the same
// as estimating zero: one says nobody has decided, the other says no work.
func (s *Service) SetEstimate(ctx context.Context, key string, estimate *float64, actor Actor) (*Issue, db.LSN, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, 0, err
	}
	if estimate != nil && *estimate < 0 {
		return nil, 0, ErrNegativeEstimate
	}

	var updated *Issue
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanIssue(tx.QueryRow(ctx,
			selectIssue+` WHERE p.key = $1 AND i.key_num = $2 FOR UPDATE OF i`, projectKey, num))
		if err != nil {
			return err
		}
		if sameEstimate(before.Estimate, estimate) {
			updated = before
			return nil
		}

		if _, err := tx.Exec(ctx, `UPDATE issue SET estimate = $2 WHERE id = $1`,
			before.ID, estimate); err != nil {
			return fmt.Errorf("set the estimate: %w", err)
		}

		changes := []Change{{
			Field: "estimate",
			From:  formatEstimate(before.Estimate),
			To:    formatEstimate(estimate),
		}}
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

func sameEstimate(a, b *float64) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}

// formatEstimate writes an estimate the way a person does: 5 rather than 5.00.
func formatEstimate(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}
