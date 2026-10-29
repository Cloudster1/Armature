package issue

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
)

// The database's names for the rules on a project's types, which the API maps
// to the same sentences as the service's refusals.
const (
	ProjectTypeConstraint     = "issue_project_type_fkey"
	RequestTypeTypeConstraint = "request_type_project_type_fkey"
	RetiredTypeConstraint     = "issue_type_not_retired"
	StandardTypeConstraint    = "project_keeps_a_standard_type"
)

// TypeNotAllowedError is a type the project does not use. Then says what else
// the person can do, such as "change the type first".
type TypeNotAllowedError struct {
	Project string
	Type    string
	Then    string
}

func (e *TypeNotAllowedError) Error() string {
	then := e.Then
	if then == "" {
		then = "change the type first"
	}
	return fmt.Sprintf("%s does not use %s; ask its administrator to allow it or %s", e.Project, e.Type, then)
}

// TypeRetiredError is a retired type asked for something new.
type TypeRetiredError struct {
	Type string
}

func (e *TypeRetiredError) Error() string {
	return fmt.Sprintf("%s is retired and offered for nothing new; pick another type", e.Type)
}

// ErrNoStandardType is a project left without a type at the working level,
// which only the database's backstop should ever see.
var ErrNoStandardType = errors.New("this project has no standard issue type to file the issue as; ask its administrator to allow one")

// ErrNoSuchType is a type id that names nothing.
var ErrNoSuchType = errors.New("that issue type does not exist")

// ProjectType reads a type as a project offers it, refusing one it does not
// use or one retired. With no type given it is the project's first standard one.
func ProjectType(ctx context.Context, tx db.DBTX, projectID uuid.UUID, projectKey string, typeID uuid.UUID, then string) (TypeRef, error) {
	var (
		t       TypeRef
		allowed bool
		retired bool
	)
	if typeID == uuid.Nil {
		err := tx.QueryRow(ctx, `
			SELECT t.id, t.name, t.icon, t.hierarchy_level, t.is_subtask
			FROM project_issue_type pit JOIN issue_type t ON t.id = pit.issue_type_id
			WHERE pit.project_id = $1 AND t.hierarchy_level = $2 AND t.retired_at IS NULL
			ORDER BY pit.position, t.position, t.name LIMIT 1`, projectID, LevelStandard).
			Scan(&t.ID, &t.Name, &t.Icon, &t.Level, &t.IsSubtask)
		if errors.Is(err, pgx.ErrNoRows) {
			return t, ErrNoStandardType
		}
		return t, err
	}
	err := tx.QueryRow(ctx, `
		SELECT t.id, t.name, t.icon, t.hierarchy_level, t.is_subtask, t.retired_at IS NOT NULL,
		       EXISTS (SELECT 1 FROM project_issue_type WHERE project_id = $1 AND issue_type_id = t.id)
		FROM issue_type t WHERE t.id = $2`, projectID, typeID).
		Scan(&t.ID, &t.Name, &t.Icon, &t.Level, &t.IsSubtask, &retired, &allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNoSuchType
	}
	if err != nil {
		return t, err
	}
	if !allowed {
		return t, &TypeNotAllowedError{Project: projectKey, Type: t.Name, Then: then}
	}
	if retired {
		return t, &TypeRetiredError{Type: t.Name}
	}
	return t, nil
}

// checkTypesUsedIn refuses a move when the target does not use the type of the
// issue, the first of moving, or of anything that comes along with it.
func checkTypesUsedIn(ctx context.Context, tx db.DBTX, target moveTarget, moving []*Issue) error {
	seen := map[uuid.UUID]bool{}
	for n, i := range moving {
		if seen[i.Type.ID] {
			continue
		}
		seen[i.Type.ID] = true
		var allowed bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM project_issue_type WHERE project_id = $1 AND issue_type_id = $2)`,
			target.id, i.Type.ID).Scan(&allowed); err != nil {
			return err
		}
		if !allowed && n > 0 {
			return &TypeNotAllowedError{Project: target.key, Type: i.Type.Name, Then: "change the type of " + i.Key + " first"}
		}
		if !allowed {
			return &TypeNotAllowedError{Project: target.key, Type: i.Type.Name}
		}
	}
	return nil
}

// IsTypeRule says whether err is the database refusing a type a project does
// not use, a retired type, or a project left without a standard type.
func IsTypeRule(err error) (constraint string, ok bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return "", false
	}
	switch pgErr.ConstraintName {
	case ProjectTypeConstraint, RequestTypeTypeConstraint, RetiredTypeConstraint, StandardTypeConstraint:
		return pgErr.ConstraintName, true
	}
	return "", false
}
