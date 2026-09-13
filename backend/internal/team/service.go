package team

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
)

// Service owns teams and who is on them.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

const selectTeam = `
SELECT t.id, t.project_id, p.key, t.name, t.description, t.position,
       (SELECT count(*) FROM team_member m WHERE m.team_id = t.id),
       (SELECT count(*) FROM issue i WHERE i.team_id = t.id),
       (SELECT count(*) FROM board b WHERE b.team_id = t.id),
       t.weekly_capacity, t.created_at, t.updated_at
FROM team t
JOIN project p ON p.id = t.project_id`

func scanTeam(row pgx.Row) (*Team, error) {
	var t Team
	err := row.Scan(
		&t.ID, &t.ProjectID, &t.ProjectKey, &t.Name, &t.Description, &t.Position,
		&t.MemberCount, &t.Issues, &t.Boards, &t.WeeklyCapacity, &t.CreatedAt, &t.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// List returns a project's teams in the order they should read.
func (s *Service) List(ctx context.Context, projectKey string) ([]Team, error) {
	out := []Team{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx,
			selectTeam+` WHERE p.key = $1 ORDER BY t.position, t.name`,
			project.NormalizeKey(projectKey))
		if err != nil {
			return fmt.Errorf("list teams: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			t, err := scanTeam(rows)
			if err != nil {
				return err
			}
			out = append(out, *t)
		}
		return rows.Err()
	})
	return out, err
}

// ByID reads one team with the people on it.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Team, error) {
	var out *Team
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scanTeam(tx.QueryRow(ctx, selectTeam+` WHERE t.id = $1`, id))
		if err != nil {
			return err
		}
		out.Members, err = readMembers(ctx, tx, id)
		return err
	})
	return out, err
}

func readMembers(ctx context.Context, tx db.DBTX, teamID uuid.UUID) ([]Member, error) {
	rows, err := tx.Query(ctx, `
		SELECT m.user_id, u.name, u.email, m.is_lead, m.created_at
		FROM team_member m
		JOIN app_user u ON u.id = m.user_id
		WHERE m.team_id = $1
		ORDER BY m.is_lead DESC, u.name`, teamID)
	if err != nil {
		return nil, fmt.Errorf("read team members: %w", err)
	}
	defer rows.Close()

	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Name, &m.Email, &m.Lead, &m.JoinedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CreateInput describes a new team.
type CreateInput struct {
	Name        string
	Description string
}

// Create adds a team to a project.
func (s *Service) Create(ctx context.Context, projectKey string, in CreateInput, actor uuid.UUID) (*Team, db.LSN, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, 0, errors.New("a team needs a name")
	}

	var out *Team
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

		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO team (org_id, project_id, name, description, position)
			VALUES (current_org_id(), $1, $2, $3,
			        COALESCE((SELECT max(position) + 1 FROM team WHERE project_id = $1), 0))
			RETURNING id`,
			projectID, in.Name, strings.TrimSpace(in.Description),
		).Scan(&id)
		if isUniqueViolation(err) {
			return ErrNameTaken
		}
		if err != nil {
			return fmt.Errorf("create team: %w", err)
		}

		out, err = scanTeam(tx.QueryRow(ctx, selectTeam+` WHERE t.id = $1`, id))
		if err != nil {
			return err
		}
		out.Members = []Member{}
		return events.EmitInTenant(ctx, tx, "team.created", map[string]any{
			"teamId": id, "projectId": projectID, "name": in.Name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// UpdateInput carries the fields an edit may change. A nil field is left alone;
// the capacity is set when SetCapacity says so, to nil for "has not said".
type UpdateInput struct {
	Name           *string
	Description    *string
	WeeklyCapacity *float64
	SetCapacity    bool
}

// Update renames a team or changes what it says about itself.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput, actor uuid.UUID) (*Team, db.LSN, error) {
	var out *Team
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanTeam(tx.QueryRow(ctx, selectTeam+` WHERE t.id = $1 FOR UPDATE OF t`, id))
		if err != nil {
			return err
		}

		name, description := before.Name, before.Description
		if in.Name != nil {
			if name = strings.TrimSpace(*in.Name); name == "" {
				return errors.New("a team needs a name")
			}
		}
		if in.Description != nil {
			description = strings.TrimSpace(*in.Description)
		}
		capacity := before.WeeklyCapacity
		if in.SetCapacity {
			if in.WeeklyCapacity != nil && *in.WeeklyCapacity < 0 {
				return ErrNegativeCapacity
			}
			capacity = in.WeeklyCapacity
		}

		_, err = tx.Exec(ctx, `UPDATE team SET name = $2, description = $3, weekly_capacity = $4 WHERE id = $1`,
			id, name, description, capacity)
		if isUniqueViolation(err) {
			return ErrNameTaken
		}
		if err != nil {
			return fmt.Errorf("update team: %w", err)
		}

		out, err = scanTeam(tx.QueryRow(ctx, selectTeam+` WHERE t.id = $1`, id))
		if err != nil {
			return err
		}
		out.Members, err = readMembers(ctx, tx, id)
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "team.updated", map[string]any{
			"teamId": id, "name": name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// Delete removes a team that is not carrying anything.
//
// The foreign keys would null the columns on their own, which is exactly the
// problem: the work would quietly become nobody's. Refusing while a team still
// has issues or boards makes somebody decide where they go.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		found, err := scanTeam(tx.QueryRow(ctx, selectTeam+` WHERE t.id = $1 FOR UPDATE OF t`, id))
		if err != nil {
			return err
		}
		switch {
		case found.Issues > 0:
			return fmt.Errorf("%w: %d issues are still assigned to %s", ErrInUse, found.Issues, found.Name)
		case found.Boards > 0:
			return fmt.Errorf("%w: %d boards still belong to %s", ErrInUse, found.Boards, found.Name)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM team WHERE id = $1`, id); err != nil {
			return fmt.Errorf("delete team: %w", err)
		}
		return nil
	})
}

// AddMember puts somebody on a team. Adding a person who is already on it sets
// whether they lead it, so the call is safe to repeat.
func (s *Service) AddMember(ctx context.Context, teamID, userID uuid.UUID, lead bool, actor uuid.UUID) (*Team, db.LSN, error) {
	var out *Team
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := scanTeam(tx.QueryRow(ctx, selectTeam+` WHERE t.id = $1`, teamID)); err != nil {
			return err
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO team_member (org_id, team_id, user_id, is_lead)
			VALUES (current_org_id(), $1, $2, $3)
			ON CONFLICT (team_id, user_id) DO UPDATE SET is_lead = EXCLUDED.is_lead`,
			teamID, userID, lead)
		if isCheckViolation(err) {
			return ErrNotAMember
		}
		if err != nil {
			return fmt.Errorf("add team member: %w", err)
		}

		out, err = s.reload(ctx, tx, teamID)
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "team.member_added", map[string]any{
			"teamId": teamID, "userId": userID, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// RemoveMember takes somebody off a team. Their issues stay with the team: work
// does not follow a person off it.
func (s *Service) RemoveMember(ctx context.Context, teamID, userID uuid.UUID, actor uuid.UUID) (*Team, db.LSN, error) {
	var out *Team
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM team_member WHERE team_id = $1 AND user_id = $2`,
			teamID, userID)
		if err != nil {
			return fmt.Errorf("remove team member: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}

		out, err = s.reload(ctx, tx, teamID)
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "team.member_removed", map[string]any{
			"teamId": teamID, "userId": userID, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

func (s *Service) reload(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Team, error) {
	found, err := scanTeam(tx.QueryRow(ctx, selectTeam+` WHERE t.id = $1`, id))
	if err != nil {
		return nil, err
	}
	found.Members, err = readMembers(ctx, tx, id)
	return found, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}
