package sprint

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
	"github.com/armature/armature/backend/internal/project"
)

// Counter reports what a sprint holds.
//
// Counting work once, across a tree where a parent and its children can both be
// in the sprint, is a rule that belongs with the plan: the plan is the only
// thing that sees the whole hierarchy at once. The sprint service asks for the
// answer and stores it; it does not know how it was arrived at.
type Counter interface {
	SprintTotals(ctx context.Context, projectKey string, sprintID uuid.UUID) (Totals, error)
}

// Service owns sprints.
type Service struct {
	db      *db.Cluster
	counter Counter
}

func NewService(cluster *db.Cluster) *Service {
	return &Service{db: cluster}
}

// CountWith supplies the counter used when a sprint is completed.
//
// It is set after construction because the dependency genuinely runs both ways:
// the plan reads a project's sprints, and completing a sprint records what the
// plan counted. Naming that here is better than hiding it behind a handler that
// has to remember to do both halves.
func (s *Service) CountWith(counter Counter) { s.counter = counter }

const selectSprint = `
SELECT s.id, s.project_id, p.key, s.team_id, COALESCE(t.name, ''), s.name, s.goal, s.state,
       s.starts_on, s.ends_on, s.capacity, s.position,
       s.started_at, s.completed_at, s.committed, s.completed, s.finished, s.carried,
       s.created_at, s.updated_at
FROM sprint s
JOIN project p ON p.id = s.project_id
LEFT JOIN team t ON t.id = s.team_id`

func scanSprint(row pgx.Row) (*Sprint, error) {
	var s Sprint
	err := row.Scan(
		&s.ID, &s.ProjectID, &s.ProjectKey, &s.TeamID, &s.TeamName, &s.Name, &s.Goal, &s.State,
		&s.StartsOn, &s.EndsOn, &s.Capacity, &s.Position,
		&s.StartedAt, &s.CompletedAt, &s.Committed, &s.Completed, &s.Finished, &s.Carried,
		&s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func readSprints(ctx context.Context, tx db.DBTX, query string, args ...any) ([]Sprint, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read sprints: %w", err)
	}
	defer rows.Close()

	out := []Sprint{}
	for rows.Next() {
		s, err := scanSprint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// order puts the running sprint first, then what is planned, then what is over,
// which is the order a team thinks about them in.
const order = ` ORDER BY CASE s.state WHEN 'active' THEN 0 WHEN 'future' THEN 1 ELSE 2 END,
                        s.position, s.starts_on NULLS LAST, s.created_at`

// List returns a project's sprints, every team's. Closed ones are left out
// unless asked for: a team plans against what is ahead of it.
func (s *Service) List(ctx context.Context, projectKey string, includeClosed bool) ([]Sprint, error) {
	query := selectSprint + ` WHERE p.key = $1`
	if !includeClosed {
		query += ` AND s.state <> 'closed'`
	}

	var out []Sprint
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = readSprints(ctx, tx, query+order, project.NormalizeKey(projectKey))
		return err
	})
	return out, err
}

// ForProject returns the sprints a plan should draw: everything not yet over.
//
// It takes the project key rather than an id because a project with sprints and
// no issues in them yet still has sprints, and an id read off the first issue
// would leave that project's plan empty.
func (s *Service) ForProject(ctx context.Context, projectKey string) ([]Sprint, error) {
	return s.List(ctx, projectKey, false)
}

// ByID reads one sprint.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Sprint, error) {
	var out *Sprint
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scanSprint(tx.QueryRow(ctx, selectSprint+` WHERE s.id = $1`, id))
		return err
	})
	return out, err
}

// CreateInput describes a new sprint.
type CreateInput struct {
	Name     string
	Goal     string
	StartsOn *time.Time
	EndsOn   *time.Time
	Capacity *float64
	// TeamID is whose sprint this is. Nil is the project's own.
	TeamID *uuid.UUID
}

// Create adds a sprint to a project's plan. It starts in the future: adding one
// is not the same as beginning it.
func (s *Service) Create(ctx context.Context, projectKey string, in CreateInput, actor uuid.UUID) (*Sprint, db.LSN, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return nil, 0, errors.New("a sprint needs a name")
	}
	if err := checkRange(in.StartsOn, in.EndsOn); err != nil {
		return nil, 0, err
	}
	if in.Capacity != nil && *in.Capacity < 0 {
		return nil, 0, errors.New("a capacity cannot be negative")
	}

	var out *Sprint
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

		// New sprints go to the end of the queue, which is where a team adds one.
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO sprint (org_id, project_id, team_id, name, goal, starts_on, ends_on, capacity, position)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7,
			        COALESCE((SELECT max(position) + 1 FROM sprint WHERE project_id = $1), 0))
			RETURNING id`,
			projectID, in.TeamID, in.Name, strings.TrimSpace(in.Goal),
			in.StartsOn, in.EndsOn, in.Capacity,
		).Scan(&id)
		if isCheckViolation(err) {
			return ErrTeamNotFound
		}
		if err != nil {
			return fmt.Errorf("create sprint: %w", err)
		}

		out, err = scanSprint(tx.QueryRow(ctx, selectSprint+` WHERE s.id = $1`, id))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "sprint.created", map[string]any{
			"sprintId": id, "projectId": projectID, "name": in.Name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// UpdateInput carries the fields an edit may change. A nil field is left alone;
// a set field holding nil clears that value.
type UpdateInput struct {
	Name     *string
	Goal     *string
	StartsOn *(*time.Time)
	EndsOn   *(*time.Time)
	Capacity *(*float64)
}

// Update changes a sprint's details.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput, actor uuid.UUID) (*Sprint, db.LSN, error) {
	var out *Sprint
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := scanSprint(tx.QueryRow(ctx, selectSprint+` WHERE s.id = $1 FOR UPDATE OF s`, id))
		if err != nil {
			return err
		}
		if before.State == StateClosed {
			return ErrClosed
		}

		name, goal := before.Name, before.Goal
		starts, ends, capacity := before.StartsOn, before.EndsOn, before.Capacity
		if in.Name != nil {
			if name = strings.TrimSpace(*in.Name); name == "" {
				return errors.New("a sprint needs a name")
			}
		}
		if in.Goal != nil {
			goal = strings.TrimSpace(*in.Goal)
		}
		if in.StartsOn != nil {
			starts = *in.StartsOn
		}
		if in.EndsOn != nil {
			ends = *in.EndsOn
		}
		if in.Capacity != nil {
			capacity = *in.Capacity
		}
		if err := checkRange(starts, ends); err != nil {
			return err
		}
		if capacity != nil && *capacity < 0 {
			return errors.New("a capacity cannot be negative")
		}
		// The database refuses this too; saying it here names the sprint.
		if before.Running() && (starts == nil || ends == nil) {
			return fmt.Errorf("%w: %s is running, so it needs both dates", ErrNotStartable, before.Name)
		}

		if _, err := tx.Exec(ctx, `
			UPDATE sprint SET name = $2, goal = $3, starts_on = $4, ends_on = $5, capacity = $6
			WHERE id = $1`, id, name, goal, starts, ends, capacity); err != nil {
			return fmt.Errorf("update sprint: %w", err)
		}

		out, err = scanSprint(tx.QueryRow(ctx, selectSprint+` WHERE s.id = $1`, id))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "sprint.updated", map[string]any{
			"sprintId": id, "name": name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

func checkRange(starts, ends *time.Time) error {
	if starts != nil && ends != nil && ends.Before(*starts) {
		return fmt.Errorf("a sprint cannot end on %s, before it starts on %s",
			ends.Format("2 Jan 2006"), starts.Format("2 Jan 2006"))
	}
	return nil
}
