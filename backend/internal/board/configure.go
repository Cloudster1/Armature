package board

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/workflow"
)

// Configuring a board means saying which ticket states belong in which
// swimlane. Everything else about a swimlane is presentation; the status
// mapping is what decides where cards appear and which drags are possible.

// AddSwimlaneInput describes a new swimlane.
type AddSwimlaneInput struct {
	Name string
	// StatusIDs are the states this swimlane claims. A status already held by
	// another swimlane on the same board is refused rather than silently moved,
	// since that would empty a lane somebody else is looking at.
	StatusIDs []uuid.UUID
	WIPLimit  int
}

// AddSwimlane appends a swimlane to a board.
func (s *Service) AddSwimlane(ctx context.Context, projectKey string, in AddSwimlaneInput) (*Swimlane, db.LSN, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, 0, errors.New("a swimlane needs a name")
	}
	if in.WIPLimit < 0 {
		return nil, 0, errors.New("a work in progress limit cannot be negative")
	}

	var lane Swimlane
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		boardID, err := boardIDFor(ctx, tx, projectKey)
		if err != nil {
			return err
		}

		var nextPosition int
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(max(position) + 1, 0) FROM board_swimlane WHERE board_id = $1`,
			boardID).Scan(&nextPosition); err != nil {
			return err
		}

		if err := tx.QueryRow(ctx, `
			INSERT INTO board_swimlane (org_id, board_id, name, position, wip_limit)
			VALUES (current_org_id(), $1, $2, $3, $4)
			RETURNING id, name, position, wip_limit`,
			boardID, name, nextPosition, in.WIPLimit,
		).Scan(&lane.ID, &lane.Name, &lane.Position, &lane.WIPLimit); err != nil {
			return fmt.Errorf("create swimlane: %w", err)
		}

		if err := assignStatuses(ctx, tx, boardID, lane.ID, in.StatusIDs); err != nil {
			return err
		}
		// The answer carries the states the way the board shows them, so a
		// client can draw the new lane without reloading the board.
		lane.Statuses = []workflow.Status{}
		lane.Cards = []Card{}
		rows, err := tx.Query(ctx, `
			SELECT st.id, st.name, st.category, st.description, st.position
			FROM board_swimlane_status ls
			JOIN issue_status st ON st.id = ls.status_id
			WHERE ls.swimlane_id = $1 ORDER BY st.position`, lane.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var st workflow.Status
			if err := rows.Scan(&st.ID, &st.Name, &st.Category, &st.Description, &st.Position); err != nil {
				return err
			}
			lane.Statuses = append(lane.Statuses, st)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, 0, err
	}
	return &lane, lsn, nil
}

// UpdateSwimlaneInput carries the fields an edit may change. A nil field is
// left alone.
type UpdateSwimlaneInput struct {
	Name      *string
	WIPLimit  *int
	StatusIDs *[]uuid.UUID
}

// UpdateSwimlane renames a swimlane, changes its limit, or reassigns which
// states belong to it.
func (s *Service) UpdateSwimlane(ctx context.Context, projectKey string, swimlaneID uuid.UUID, in UpdateSwimlaneInput) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		boardID, err := boardIDFor(ctx, tx, projectKey)
		if err != nil {
			return err
		}
		if err := ensureOnBoard(ctx, tx, boardID, swimlaneID); err != nil {
			return err
		}

		if in.Name != nil {
			name := strings.TrimSpace(*in.Name)
			if name == "" {
				return errors.New("a swimlane needs a name")
			}
			if _, err := tx.Exec(ctx, `UPDATE board_swimlane SET name = $2 WHERE id = $1`, swimlaneID, name); err != nil {
				return err
			}
		}

		if in.WIPLimit != nil {
			if *in.WIPLimit < 0 {
				return errors.New("a work in progress limit cannot be negative")
			}
			if _, err := tx.Exec(ctx, `UPDATE board_swimlane SET wip_limit = $2 WHERE id = $1`, swimlaneID, *in.WIPLimit); err != nil {
				return err
			}
		}

		if in.StatusIDs != nil {
			// Replace the whole set rather than diffing it: the editor sends
			// the states it wants, and a replacement cannot leave a stale
			// mapping behind.
			if _, err := tx.Exec(ctx, `DELETE FROM board_swimlane_status WHERE swimlane_id = $1`, swimlaneID); err != nil {
				return err
			}
			if err := assignStatuses(ctx, tx, boardID, swimlaneID, *in.StatusIDs); err != nil {
				return err
			}
		}

		return nil
	})
}

// ReorderSwimlanes sets the left to right order of a board's swimlanes.
//
// The whole order is given at once. Sending one lane's new index would need the
// server to guess what happened to the others, and two people reordering at the
// same time would interleave into something neither asked for.
func (s *Service) ReorderSwimlanes(ctx context.Context, projectKey string, order []uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		boardID, err := boardIDFor(ctx, tx, projectKey)
		if err != nil {
			return err
		}

		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM board_swimlane WHERE board_id = $1`, boardID).Scan(&count); err != nil {
			return err
		}
		if count != len(order) {
			return fmt.Errorf("the board has %d swimlanes but the new order lists %d", count, len(order))
		}

		for position, id := range order {
			tag, err := tx.Exec(ctx, `
				UPDATE board_swimlane SET position = $3
				WHERE id = $1 AND board_id = $2`, id, boardID, position)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return fmt.Errorf("%w: %s", ErrSwimlaneNotFound, id)
			}
		}
		return nil
	})
}

// DeleteSwimlane removes a swimlane. Its states become unmapped, so the cards
// in them move to the board's unmapped area rather than disappearing.
func (s *Service) DeleteSwimlane(ctx context.Context, projectKey string, swimlaneID uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		boardID, err := boardIDFor(ctx, tx, projectKey)
		if err != nil {
			return err
		}

		var remaining int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM board_swimlane WHERE board_id = $1`, boardID).Scan(&remaining); err != nil {
			return err
		}
		if remaining <= 1 {
			return ErrLastSwimlane
		}

		tag, err := tx.Exec(ctx, `DELETE FROM board_swimlane WHERE id = $1 AND board_id = $2`, swimlaneID, boardID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrSwimlaneNotFound
		}
		return nil
	})
}

// assignStatuses maps a set of states onto a swimlane.
//
// The conflict is looked for before inserting rather than after. A constraint
// violation aborts the surrounding transaction, so the query that would name
// the swimlane already holding the status could not run afterwards, and the
// error would have to say "another swimlane" instead of which one. The unique
// index is still the real guarantee against a race; this is for the message.
func assignStatuses(ctx context.Context, tx db.DBTX, boardID, swimlaneID uuid.UUID, statusIDs []uuid.UUID) error {
	for _, statusID := range statusIDs {
		var owner string
		err := tx.QueryRow(ctx, `
			SELECT l.name
			FROM board_swimlane_status ls
			JOIN board_swimlane l ON l.id = ls.swimlane_id
			WHERE ls.board_id = $1 AND ls.status_id = $2 AND ls.swimlane_id <> $3`,
			boardID, statusID, swimlaneID).Scan(&owner)
		switch {
		case err == nil:
			return fmt.Errorf("%w: it is in %q", ErrStatusClaimed, owner)
		case errors.Is(err, pgx.ErrNoRows):
			// Nobody has it, which is what we want.
		default:
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO board_swimlane_status (org_id, board_id, swimlane_id, status_id)
			VALUES (current_org_id(), $1, $2, $3)
			ON CONFLICT (swimlane_id, status_id) DO NOTHING`, boardID, swimlaneID, statusID)

		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// Lost a race with a concurrent edit; the index did its job.
			return fmt.Errorf("%w: another swimlane claimed it first", ErrStatusClaimed)
		}
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return fmt.Errorf("no such status: %s", statusID)
		}
		if err != nil {
			return fmt.Errorf("map status into swimlane: %w", err)
		}
	}
	return nil
}

func boardIDFor(ctx context.Context, tx db.DBTX, projectKey string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT b.id FROM board b
		JOIN project p ON p.id = b.project_id
		WHERE p.key = $1
		ORDER BY b.created_at LIMIT 1`, project.NormalizeKey(projectKey)).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrNotFound
	}
	return id, err
}

func ensureOnBoard(ctx context.Context, tx db.DBTX, boardID, swimlaneID uuid.UUID) error {
	var exists bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM board_swimlane WHERE id = $1 AND board_id = $2)`,
		swimlaneID, boardID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return ErrSwimlaneNotFound
	}
	return nil
}

// CreateInput describes a new board.
type CreateInput struct {
	Name        string
	Description string
	// Type is scrum or kanban. Empty takes after the project's first board, so
	// a team's board in a scrum project is a scrum board without anyone saying.
	Type Type
	// TeamID scopes the board to one team. Nil is a board over the whole
	// project, which is what the project's first board is.
	TeamID *uuid.UUID
}

// CreateBoard adds a board to a project, with one swimlane per status of the
// workflow the project's issue types follow.
//
// A project can have as many boards as it has ways of looking at its work. A
// board scoped to a team draws only that team's issues, which is what gives
// each team a board and a backlog of its own.
func (s *Service) CreateBoard(ctx context.Context, projectKey string, in CreateInput, actor uuid.UUID) (*Board, db.LSN, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, 0, errors.New("a board needs a name")
	}
	if in.Type != "" && !in.Type.Valid() {
		return nil, 0, fmt.Errorf("%w: %q", ErrBadType, in.Type)
	}
	if in.Description == "" {
		in.Description = describe(in.Type)
	}

	var out *Board
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var projectID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1 AND archived_at IS NULL`,
			project.NormalizeKey(projectKey)).Scan(&projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return project.ErrNotFound
		}
		if err != nil {
			return err
		}

		if in.Type == "" {
			if in.Type, err = firstBoardType(ctx, tx, projectID); err != nil {
				return err
			}
			in.Description = describe(in.Type)
		}

		statuses, err := s.projectStatuses(ctx, tx, projectID)
		if err != nil {
			return err
		}

		boardID, err := createBoard(ctx, tx, projectID, in, statuses)
		if err != nil {
			return err
		}

		out, err = s.load(ctx, tx, selectBoard+` WHERE b.id = $1`, boardID)
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "board.created", map[string]any{
			"boardId": boardID, "projectId": projectID, "name": in.Name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// UpdateBoardInput carries the fields an edit may change. A nil field is left
// alone; a set field holding nil takes the board off its team.
type UpdateBoardInput struct {
	Name        *string
	Description *string
	// Type may change: it decides which cards the board draws and nothing is
	// stored against it, so switching is as safe as changing a filter.
	Type    *Type
	GroupBy *Grouping
	TeamID  *(*uuid.UUID)
}

// UpdateBoard changes a board's details, including which team it draws from.
func (s *Service) UpdateBoard(ctx context.Context, id uuid.UUID, in UpdateBoardInput, actor uuid.UUID) (*Board, db.LSN, error) {
	var out *Board
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := s.load(ctx, tx, selectBoard+` WHERE b.id = $1`, id)
		if err != nil {
			return err
		}

		name, description, kind, groupBy, teamID := before.Name, before.Description, before.Type, before.GroupBy, before.TeamID
		if in.Name != nil {
			if name = strings.TrimSpace(*in.Name); name == "" {
				return errors.New("a board needs a name")
			}
		}
		if in.Description != nil {
			description = strings.TrimSpace(*in.Description)
		}
		if in.Type != nil {
			if !in.Type.Valid() {
				return fmt.Errorf("%w: %q", ErrBadType, *in.Type)
			}
			kind = *in.Type
		}
		if in.GroupBy != nil {
			if !in.GroupBy.Valid() {
				return fmt.Errorf("%q is not a grouping", *in.GroupBy)
			}
			groupBy = *in.GroupBy
		}
		if in.TeamID != nil {
			teamID = *in.TeamID
		}

		_, err = tx.Exec(ctx, `
			UPDATE board SET name = $2, description = $3, type = $4, group_by = $5, team_id = $6
			WHERE id = $1`, id, name, description, string(kind), string(groupBy), teamID)
		switch {
		case isUniqueViolation(err):
			return ErrNameTaken
		case isCheckViolation(err):
			return ErrTeamNotFound
		case err != nil:
			return fmt.Errorf("update board: %w", err)
		}

		out, err = s.load(ctx, tx, selectBoard+` WHERE b.id = $1`, id)
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "board.updated", map[string]any{
			"boardId": id, "name": name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// DeleteBoard removes a board. A project keeps at least one: a project with no
// board at all is one nobody can work in.
func (s *Service) DeleteBoard(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			projectID uuid.UUID
			remaining int
		)
		err := tx.QueryRow(ctx, `
			SELECT b.project_id, (SELECT count(*) FROM board o WHERE o.project_id = b.project_id)
			FROM board b WHERE b.id = $1`, id).Scan(&projectID, &remaining)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if remaining <= 1 {
			return ErrLastBoard
		}

		if _, err := tx.Exec(ctx, `DELETE FROM board WHERE id = $1`, id); err != nil {
			return fmt.Errorf("delete board: %w", err)
		}
		return nil
	})
}

// firstBoardType is the type of the board a project was made with, which is
// what a board added without a stated type takes after. A project somehow
// without one gets kanban, the type that needs no sprint to show anything.
func firstBoardType(ctx context.Context, tx db.DBTX, projectID uuid.UUID) (Type, error) {
	var kind Type
	err := tx.QueryRow(ctx, `
		SELECT type FROM board WHERE project_id = $1 ORDER BY created_at LIMIT 1`, projectID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return TypeKanban, nil
	}
	if err != nil {
		return "", fmt.Errorf("read the project's first board: %w", err)
	}
	return kind, nil
}

// projectStatuses reads the statuses a new board should have a swimlane for:
// every state the project's issues can be in, across all the workflows it
// resolves to.
func (s *Service) projectStatuses(ctx context.Context, tx db.DBTX, projectID uuid.UUID) ([]workflow.Status, error) {
	return workflow.NewStore().ProjectStatuses(ctx, tx, projectID)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}
