package availability

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// ErrProjectNotFound is returned for a share asked of a project that is not here.
var ErrProjectNotFound = errors.New("project not found")

// FullShare is a whole week, which is what a person gives a project nobody has
// said otherwise about.
const FullShare = 100

// Allocation is the share of a person's week a project has.
type Allocation struct {
	UserID uuid.UUID `json:"userId"`
	Name   string    `json:"name"`
	// Percent is this project's share, a whole week when nobody has set one.
	Percent int `json:"percent"`
	// ElsewherePercent adds up the shares set for the person in other projects,
	// so that giving more than a whole week can be seen and warned about.
	ElsewherePercent int `json:"elsewherePercent"`
}

// selectAllocation reads a person's share in the project $1 and elsewhere.
const selectAllocation = `
SELECT u.id, u.name,
       COALESCE((SELECT a.percent FROM project_allocation a WHERE a.project_id = $1 AND a.user_id = u.id), 100),
       COALESCE((SELECT sum(a.percent) FROM project_allocation a WHERE a.user_id = u.id AND a.project_id <> $1), 0)::int
FROM app_user u
JOIN org_member o ON o.user_id = u.id AND o.org_id = current_org_id() AND o.org_role <> 'customer'`

// Allocations lists the shares of a project's people: the members of its
// teams, the assignees of its open issues, and anybody given a share in it.
func (s *Service) Allocations(ctx context.Context, projectKey string) ([]Allocation, error) {
	out := []Allocation{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		projectID, err := projectIDOf(ctx, tx, projectKey)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectAllocation+`
			WHERE u.id IN (
			    SELECT m.user_id FROM team_member m JOIN team t ON t.id = m.team_id WHERE t.project_id = $1
			    UNION SELECT i.assignee_id FROM issue i WHERE i.project_id = $1 AND i.resolved_at IS NULL
			    UNION SELECT a.user_id FROM project_allocation a WHERE a.project_id = $1)
			ORDER BY lower(u.name), u.id`, projectID)
		if err != nil {
			return fmt.Errorf("read the shares: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var a Allocation
			if err := rows.Scan(&a.UserID, &a.Name, &a.Percent, &a.ElsewherePercent); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SetAllocation gives a project a share of a person's week. A whole week is
// what no share means, so it is kept as no row rather than a row saying so.
func (s *Service) SetAllocation(ctx context.Context, projectKey string, userID uuid.UUID, percent int, actor uuid.UUID) (*Allocation, db.LSN, error) {
	if percent < 0 || percent > FullShare {
		return nil, 0, fmt.Errorf("%w: a share of the week is between 0 and 100 percent", ErrInvalid)
	}
	var out Allocation
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		projectID, err := projectIDOf(ctx, tx, projectKey)
		if err != nil {
			return err
		}
		// Read first so a stranger is told what to do, before the guard in
		// the database refuses them in its own words.
		err = tx.QueryRow(ctx, selectAllocation+` WHERE u.id = $2`, projectID, userID).
			Scan(&out.UserID, &out.Name, &out.Percent, &out.ElsewherePercent)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: only somebody who works here gives a project a share of their week; choose a member of the organization", ErrInvalid)
		}
		if err != nil {
			return err
		}
		if percent == FullShare {
			_, err = tx.Exec(ctx, `DELETE FROM project_allocation WHERE project_id = $1 AND user_id = $2`, projectID, userID)
		} else {
			_, err = tx.Exec(ctx, `
				INSERT INTO project_allocation (org_id, project_id, user_id, percent)
				VALUES (current_org_id(), $1, $2, $3)
				ON CONFLICT (project_id, user_id) DO UPDATE SET percent = EXCLUDED.percent`, projectID, userID, percent)
		}
		if err != nil {
			return fmt.Errorf("set the share: %w", err)
		}
		out.Percent = percent
		return events.EmitInTenant(ctx, tx, "project.allocation_set", map[string]any{
			"projectId": projectID, "userId": userID, "percent": percent, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return &out, lsn, nil
}

// Shares are the shares set in a project, by person; anybody missing gives it
// a whole week.
func (s *Service) Shares(ctx context.Context, projectKey string) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT a.user_id, a.percent FROM project_allocation a
			JOIN project p ON p.id = a.project_id
			WHERE p.key = $1`, projectKey)
		if err != nil {
			return fmt.Errorf("read the shares: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var (
				id      uuid.UUID
				percent int
			)
			if err := rows.Scan(&id, &percent); err != nil {
				return err
			}
			out[id] = percent
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func projectIDOf(ctx context.Context, tx db.DBTX, projectKey string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = upper($1)`, projectKey).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, ErrProjectNotFound
	}
	return id, err
}
