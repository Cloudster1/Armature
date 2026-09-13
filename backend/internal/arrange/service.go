package arrange

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/project"
)

type Service struct {
	db *db.Cluster
}

func NewService(cluster *db.Cluster) *Service { return &Service{db: cluster} }

// resolveOne is the whole precedence rule: a project's own arrangement beats
// the organization's, and one that names the issue type beats the fallback.
const resolveOne = `
SELECT id, project_id IS NOT NULL, issue_type_id IS NOT NULL
FROM issue_arrangement
WHERE (project_id = $1 OR project_id IS NULL) AND (issue_type_id = $2 OR issue_type_id IS NULL)
ORDER BY (project_id IS NULL), (issue_type_id IS NULL)
LIMIT 1`

// InProject answers for every issue type as this project draws it.
func (s *Service) InProject(ctx context.Context, projectKey string) ([]Arrangement, error) {
	var out []Arrangement
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var projectID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, project.NormalizeKey(projectKey)).Scan(&projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return project.ErrNotFound
		}
		if err != nil {
			return err
		}
		out, err = arrangements(ctx, tx, &projectID)
		return err
	})
	return out, err
}

// InOrg answers for every issue type as the organization draws it, which is
// what every project follows until it says otherwise.
func (s *Service) InOrg(ctx context.Context) ([]Arrangement, error) {
	var out []Arrangement
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = arrangements(ctx, tx, nil)
		return err
	})
	return out, err
}

// arrangements resolves every issue type at once. An organization has a handful
// of types, so this is a handful of small reads rather than one clever one.
func arrangements(ctx context.Context, tx db.DBTX, projectID *uuid.UUID) ([]Arrangement, error) {
	types, err := issueTypes(ctx, tx)
	if err != nil {
		return nil, err
	}
	out := make([]Arrangement, 0, len(types))
	for _, each := range types {
		answer := Arrangement{IssueTypeID: each.id, IssueTypeName: each.name, Origin: Origin{Scope: ScopeBuiltin}, Places: Default()}
		var (
			id      uuid.UUID
			ofOne   bool
			byName  bool
			scanErr = tx.QueryRow(ctx, resolveOne, projectID, each.id).Scan(&id, &ofOne, &byName)
		)
		switch {
		case errors.Is(scanErr, pgx.ErrNoRows):
			out = append(out, answer)
			continue
		case scanErr != nil:
			return nil, fmt.Errorf("resolve the arrangement: %w", scanErr)
		}
		answer.Origin = Origin{Scope: ScopeOrganization, Named: byName}
		if ofOne {
			answer.Origin.Scope = ScopeProject
		}
		if answer.Places, err = places(ctx, tx, id); err != nil {
			return nil, err
		}
		out = append(out, answer)
	}
	return out, nil
}

type issueType struct {
	id   uuid.UUID
	name string
}

func issueTypes(ctx context.Context, tx db.DBTX) ([]issueType, error) {
	rows, err := tx.Query(ctx, `SELECT id, name FROM issue_type ORDER BY hierarchy_level DESC, position, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []issueType
	for rows.Next() {
		var each issueType
		if err := rows.Scan(&each.id, &each.name); err != nil {
			return nil, err
		}
		out = append(out, each)
	}
	return out, rows.Err()
}

func places(ctx context.Context, tx db.DBTX, arrangementID uuid.UUID) ([]Placement, error) {
	rows, err := tx.Query(ctx, `
		SELECT area, builtin, custom_field_id FROM issue_arrangement_slot
		WHERE arrangement_id = $1 ORDER BY area, position`, arrangementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Placement{}
	for rows.Next() {
		var (
			area    string
			builtin *string
			fieldID *uuid.UUID
		)
		if err := rows.Scan(&area, &builtin, &fieldID); err != nil {
			return nil, err
		}
		place := Placement{Area: Area(area), FieldID: fieldID}
		if builtin != nil {
			place.Slot = Slot(*builtin)
		}
		out = append(out, place)
	}
	return out, rows.Err()
}
