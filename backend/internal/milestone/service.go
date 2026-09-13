package milestone

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/project"
)

// Service owns milestones.
type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service {
	return &Service{db: cluster}
}

// selectMilestone reads a milestone with its progress counted from the issues
// assigned to it, by status category. Three correlated counts rather than a
// join: a milestone holds tens of issues, and the counts stay readable.
const selectMilestone = `
SELECT m.id, m.project_id, p.key, m.name, m.description, m.due_on, m.closed_at, m.position,
       m.created_at, m.updated_at,
       (SELECT count(*) FROM issue i JOIN issue_status s ON s.id = i.status_id
         WHERE i.milestone_id = m.id AND s.category = 'done'),
       (SELECT count(*) FROM issue i JOIN issue_status s ON s.id = i.status_id
         WHERE i.milestone_id = m.id AND s.category = 'in_progress'),
       (SELECT count(*) FROM issue i JOIN issue_status s ON s.id = i.status_id
         WHERE i.milestone_id = m.id AND s.category = 'todo')
FROM milestone m
JOIN project p ON p.id = m.project_id`

func scanMilestone(row pgx.Row) (*Milestone, error) {
	var (
		m                      Milestone
		done, inProgress, todo int
	)
	err := row.Scan(
		&m.ID, &m.ProjectID, &m.ProjectKey, &m.Name, &m.Description, &m.DueOn, &m.ClosedAt, &m.Position,
		&m.CreatedAt, &m.UpdatedAt, &done, &inProgress, &todo,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.Progress = Measure(done, inProgress, todo)
	return &m, nil
}

// order puts open milestones first by due date, undated ones after, and closed
// ones last, which is the order a project reads them in.
const order = ` ORDER BY (m.closed_at IS NOT NULL), m.due_on NULLS LAST, m.position, m.created_at`

// List returns a project's milestones. Closed ones are left out unless asked
// for: a project plans against what is still ahead of it.
func (s *Service) List(ctx context.Context, projectKey string, includeClosed bool) ([]Milestone, error) {
	query := selectMilestone + ` WHERE p.key = $1`
	if !includeClosed {
		query += ` AND m.closed_at IS NULL`
	}

	out := []Milestone{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, query+order, project.NormalizeKey(projectKey))
		if err != nil {
			return fmt.Errorf("read milestones: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanMilestone(rows)
			if err != nil {
				return err
			}
			out = append(out, *m)
		}
		return rows.Err()
	})
	return out, err
}

// ForProject returns the milestones a plan should draw: the open ones.
func (s *Service) ForProject(ctx context.Context, projectKey string) ([]Milestone, error) {
	return s.List(ctx, projectKey, false)
}

// ByID reads one milestone.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Milestone, error) {
	var out *Milestone
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scanMilestone(tx.QueryRow(ctx, selectMilestone+` WHERE m.id = $1`, id))
		return err
	})
	return out, err
}

// Input describes a milestone as an editor sends it.
type Input struct {
	Name        string
	Description string
	DueOn       *time.Time
}

// Create adds a milestone to a project.
func (s *Service) Create(ctx context.Context, projectKey string, in Input, actor uuid.UUID) (*Milestone, db.LSN, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, 0, errors.New("a milestone needs a name")
	}

	var out *Milestone
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
			INSERT INTO milestone (org_id, project_id, name, description, due_on, position)
			VALUES (current_org_id(), $1, $2, $3, $4,
			        COALESCE((SELECT max(position) + 1 FROM milestone WHERE project_id = $1), 0))
			RETURNING id`,
			projectID, in.Name, strings.TrimSpace(in.Description), in.DueOn,
		).Scan(&id)
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: %s", ErrDuplicateName, in.Name)
		}
		if err != nil {
			return fmt.Errorf("create milestone: %w", err)
		}

		out, err = scanMilestone(tx.QueryRow(ctx, selectMilestone+` WHERE m.id = $1`, id))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "milestone.created", map[string]any{
			"milestoneId": id, "projectId": projectID, "name": in.Name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// UpdateInput carries the fields an edit may change. A nil field is left
// alone; a set DueOn holding nil clears the date.
type UpdateInput struct {
	Name        *string
	Description *string
	DueOn       *(*time.Time)
}

// Update changes a milestone's details. A closed milestone is a record and is
// left as it was.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput, actor uuid.UUID) (*Milestone, db.LSN, error) {
	var out *Milestone
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanMilestone(tx.QueryRow(ctx, selectMilestone+` WHERE m.id = $1 FOR UPDATE OF m`, id))
		if err != nil {
			return err
		}
		if !before.Open() {
			return fmt.Errorf("%w: %s", ErrClosed, before.Name)
		}

		name, description, due := before.Name, before.Description, before.DueOn
		if in.Name != nil {
			if name = strings.TrimSpace(*in.Name); name == "" {
				return errors.New("a milestone needs a name")
			}
		}
		if in.Description != nil {
			description = strings.TrimSpace(*in.Description)
		}
		if in.DueOn != nil {
			due = *in.DueOn
		}

		_, err = tx.Exec(ctx, `
			UPDATE milestone SET name = $2, description = $3, due_on = $4 WHERE id = $1`,
			id, name, description, due)
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: %s", ErrDuplicateName, name)
		}
		if err != nil {
			return fmt.Errorf("update milestone: %w", err)
		}

		out, err = scanMilestone(tx.QueryRow(ctx, selectMilestone+` WHERE m.id = $1`, id))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "milestone.updated", map[string]any{
			"milestoneId": id, "name": name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// Close declares a milestone reached or dropped. What was assigned stays, for
// the record; nothing more can be assigned.
func (s *Service) Close(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*Milestone, db.LSN, error) {
	return s.setClosed(ctx, id, true, actor)
}

// Reopen takes a closed milestone back into planning.
func (s *Service) Reopen(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*Milestone, db.LSN, error) {
	return s.setClosed(ctx, id, false, actor)
}

func (s *Service) setClosed(ctx context.Context, id uuid.UUID, closed bool, actor uuid.UUID) (*Milestone, db.LSN, error) {
	var out *Milestone
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanMilestone(tx.QueryRow(ctx, selectMilestone+` WHERE m.id = $1 FOR UPDATE OF m`, id))
		if err != nil {
			return err
		}
		if closed && !before.Open() {
			return fmt.Errorf("%w: %s", ErrClosed, before.Name)
		}
		if !closed && before.Open() {
			return fmt.Errorf("%w: %s", ErrOpen, before.Name)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE milestone SET closed_at = CASE WHEN $2 THEN now() ELSE NULL END WHERE id = $1`,
			id, closed); err != nil {
			return fmt.Errorf("close milestone: %w", err)
		}
		out, err = scanMilestone(tx.QueryRow(ctx, selectMilestone+` WHERE m.id = $1`, id))
		if err != nil {
			return err
		}
		topic := "milestone.closed"
		if !closed {
			topic = "milestone.reopened"
		}
		return events.EmitInTenant(ctx, tx, topic, map[string]any{
			"milestoneId": id, "name": before.Name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// Delete removes a milestone. The issues assigned to it are left where they
// are, no longer assigned to anything: dropping a target is not dropping work.
func (s *Service) Delete(ctx context.Context, id uuid.UUID, actor uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var name string
		err := tx.QueryRow(ctx, `DELETE FROM milestone WHERE id = $1 RETURNING name`, id).Scan(&name)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("delete milestone: %w", err)
		}
		return events.EmitInTenant(ctx, tx, "milestone.deleted", map[string]any{
			"milestoneId": id, "name": name, "actorId": actor,
		})
	})
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
