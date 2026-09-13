// Package component owns a project's parts: each with somebody who looks after
// it, and, when it says so, who new work in it goes to.
package component

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
	"github.com/armature/armature/backend/internal/project"
)

var (
	ErrNotFound      = errors.New("component not found")
	ErrDuplicateName = errors.New("a component with that name is already here")
)

// Person is who leads a component or takes its new work.
type Person struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Component is one part of a project.
type Component struct {
	ID              uuid.UUID `json:"id"`
	ProjectID       uuid.UUID `json:"projectId"`
	ProjectKey      string    `json:"projectKey"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	Lead            *Person   `json:"lead,omitempty"`
	DefaultAssignee *Person   `json:"defaultAssignee,omitempty"`
	// Issues counts what is in the component, open or not.
	Issues    int       `json:"issues"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Ref is a component as it hangs off an issue.
type Ref struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// Input is a component as an editor sends it; a set pointer holding nil
// clears the person.
type Input struct {
	Name              *string
	Description       *string
	LeadID            *(*uuid.UUID)
	DefaultAssigneeID *(*uuid.UUID)
}

type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

const selectComponent = `
SELECT c.id, c.project_id, p.key, c.name, c.description,
       l.id, COALESCE(l.name, ''), d.id, COALESCE(d.name, ''),
       (SELECT count(*) FROM issue_component ic WHERE ic.component_id = c.id),
       c.created_at, c.updated_at
FROM component c
JOIN project p ON p.id = c.project_id
LEFT JOIN app_user l ON l.id = c.lead_id
LEFT JOIN app_user d ON d.id = c.default_assignee_id`

func scan(row pgx.Row) (*Component, error) {
	var (
		c                    Component
		leadID, assigneeID   *uuid.UUID
		leadName, assignName string
	)
	err := row.Scan(&c.ID, &c.ProjectID, &c.ProjectKey, &c.Name, &c.Description, &leadID, &leadName, &assigneeID, &assignName, &c.Issues, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if leadID != nil {
		c.Lead = &Person{ID: *leadID, Name: leadName}
	}
	if assigneeID != nil {
		c.DefaultAssignee = &Person{ID: *assigneeID, Name: assignName}
	}
	return &c, nil
}

// List is a project's components, by name.
func (s *Service) List(ctx context.Context, projectKey string) ([]Component, error) {
	out := []Component{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectComponent+` WHERE p.key = $1 ORDER BY lower(c.name)`, project.NormalizeKey(projectKey))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, *c)
		}
		return rows.Err()
	})
	return out, err
}

// Create adds a component to a project.
func (s *Service) Create(ctx context.Context, projectKey string, in Input) (*Component, db.LSN, error) {
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" {
		return nil, 0, errors.New("a component needs a name")
	}
	description := ""
	if in.Description != nil {
		description = strings.TrimSpace(*in.Description)
	}
	var lead, assignee *uuid.UUID
	if in.LeadID != nil {
		lead = *in.LeadID
	}
	if in.DefaultAssigneeID != nil {
		assignee = *in.DefaultAssigneeID
	}
	var out *Component
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var projectID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1 AND archived_at IS NULL`, project.NormalizeKey(projectKey)).Scan(&projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return project.ErrNotFound
		}
		if err != nil {
			return err
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO component (org_id, project_id, name, description, lead_id, default_assignee_id)
			VALUES (current_org_id(), $1, $2, $3, $4, $5) RETURNING id`, projectID, name, description, lead, assignee).Scan(&id)
		if isUnique(err) {
			return fmt.Errorf("%w: %s", ErrDuplicateName, name)
		}
		if err != nil {
			return fmt.Errorf("create component: %w", err)
		}
		out, err = scan(tx.QueryRow(ctx, selectComponent+` WHERE c.id = $1`, id))
		return err
	})
	return out, lsn, err
}

// Update changes a component's details.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in Input) (*Component, db.LSN, error) {
	var out *Component
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := scan(tx.QueryRow(ctx, selectComponent+` WHERE c.id = $1 FOR UPDATE OF c`, id))
		if err != nil {
			return err
		}
		name, description := current.Name, current.Description
		var lead, assignee *uuid.UUID
		if current.Lead != nil {
			lead = &current.Lead.ID
		}
		if current.DefaultAssignee != nil {
			assignee = &current.DefaultAssignee.ID
		}
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
			if name == "" {
				return errors.New("a component needs a name")
			}
		}
		if in.Description != nil {
			description = strings.TrimSpace(*in.Description)
		}
		if in.LeadID != nil {
			lead = *in.LeadID
		}
		if in.DefaultAssigneeID != nil {
			assignee = *in.DefaultAssigneeID
		}
		_, err = tx.Exec(ctx, `UPDATE component SET name = $2, description = $3, lead_id = $4, default_assignee_id = $5 WHERE id = $1`, id, name, description, lead, assignee)
		if isUnique(err) {
			return fmt.Errorf("%w: %s", ErrDuplicateName, name)
		}
		if err != nil {
			return err
		}
		out, err = scan(tx.QueryRow(ctx, selectComponent+` WHERE c.id = $1`, id))
		return err
	})
	return out, lsn, err
}

// Delete removes a component; the issues in it simply leave it.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM component WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func isUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
