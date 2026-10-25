package perm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// Assignment is one role held by one subject over one scope, as a listing shows
// it: with the names filled in, because a page of identifiers explains nothing.
type Assignment struct {
	ID   uuid.UUID `json:"id"`
	Role Role      `json:"role"`

	// ProjectID is nil for a role held over the whole organization.
	ProjectID  *uuid.UUID `json:"projectId,omitempty"`
	ProjectKey string     `json:"projectKey,omitempty"`

	// Exactly one of these is set.
	UserID    *uuid.UUID `json:"userId,omitempty"`
	UserName  string     `json:"userName,omitempty"`
	GroupID   *uuid.UUID `json:"groupId,omitempty"`
	GroupName string     `json:"groupName,omitempty"`
}

// ToGroup reports whether the role was granted to a group rather than a person.
func (a Assignment) ToGroup() bool { return a.GroupID != nil }

const assignmentFrom = `
FROM role_assignment a
LEFT JOIN project p ON p.id = a.project_id
LEFT JOIN app_user u ON u.id = a.user_id
LEFT JOIN user_group g ON g.id = a.group_id`

const selectAssignment = `
SELECT a.id, a.role, a.project_id, COALESCE(p.key, ''),
       a.user_id, COALESCE(u.name, ''), a.group_id, COALESCE(g.name, '')` + assignmentFrom

// order reads the way somebody looks for a grant: the installation-wide ones
// first, then by project, and the most powerful role at the top of each.
const order = `
ORDER BY (a.project_id IS NOT NULL), p.key,
         (SELECT r.sort_order FROM role r WHERE r.key = a.role), a.role,
         COALESCE(g.name, u.name)`

func scanAssignment(row pgx.Row) (*Assignment, error) {
	var a Assignment
	err := row.Scan(&a.ID, &a.Role, &a.ProjectID, &a.ProjectKey,
		&a.UserID, &a.UserName, &a.GroupID, &a.GroupName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// DefaultAssignmentPage is a page of grants unless asked otherwise;
// MaxAssignmentPage is the most one read returns.
const (
	DefaultAssignmentPage = 50
	MaxAssignmentPage     = 200
)

// AssignmentFilter narrows and pages a listing of grants.
type AssignmentFilter struct {
	// ProjectKey keeps the grants that answer in that project, the
	// installation-wide ones included because they answer everywhere.
	ProjectKey string
	// Text matches the person, the group, or the project's key or name.
	Text   string
	Role   Role
	Limit  int
	Offset int
}

// AssignmentPage is one page of grants plus how many matched in all.
type AssignmentPage struct {
	Assignments []Assignment `json:"assignments"`
	Total       int          `json:"total"`
	Limit       int          `json:"limit"`
	Offset      int          `json:"offset"`
}

func (f AssignmentFilter) where() (string, []any) {
	var (
		clauses []string
		args    []any
	)
	if f.ProjectKey != "" {
		args = append(args, f.ProjectKey)
		clauses = append(clauses, fmt.Sprintf("(a.project_id IS NULL OR p.key = $%d)", len(args)))
	}
	if f.Role != "" {
		args = append(args, string(f.Role))
		clauses = append(clauses, fmt.Sprintf("a.role = $%d", len(args)))
	}
	if text := strings.TrimSpace(f.Text); text != "" {
		args = append(args, text)
		n := len(args)
		clauses = append(clauses, fmt.Sprintf("(strpos(lower(COALESCE(u.name, '')), lower($%[1]d)) > 0 OR strpos(lower(COALESCE(g.name, '')), lower($%[1]d)) > 0"+
			" OR strpos(lower(COALESCE(p.key, '')), lower($%[1]d)) > 0 OR strpos(lower(COALESCE(p.name, '')), lower($%[1]d)) > 0)", n))
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// Assignments lists every grant that answers in a project, or every grant
// at all for an empty key, for a caller that must see each one.
func (s *Store) Assignments(ctx context.Context, projectKey string) ([]Assignment, error) {
	where, args := AssignmentFilter{ProjectKey: projectKey}.where()
	out := []Assignment{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = readAssignments(ctx, tx, selectAssignment+where+order, args...)
		return err
	})
	return out, err
}

// ListAssignments returns one page of the grants the filter allows, in the
// order a person looks for one, with how many matched in all.
func (s *Store) ListAssignments(ctx context.Context, f AssignmentFilter) (AssignmentPage, error) {
	if f.Limit <= 0 || f.Limit > MaxAssignmentPage {
		f.Limit = DefaultAssignmentPage
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	page := AssignmentPage{Limit: f.Limit, Offset: f.Offset}
	where, args := f.where()
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT count(*)`+assignmentFrom+where, args...).Scan(&page.Total); err != nil {
			return fmt.Errorf("count role assignments: %w", err)
		}
		n := len(args)
		var err error
		page.Assignments, err = readAssignments(ctx, tx,
			selectAssignment+where+order+fmt.Sprintf(", a.id LIMIT $%d OFFSET $%d", n+1, n+2), append(args, f.Limit, f.Offset)...)
		return err
	})
	if err != nil {
		return AssignmentPage{}, err
	}
	return page, nil
}

func readAssignments(ctx context.Context, tx db.DBTX, query string, args ...any) ([]Assignment, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list role assignments: %w", err)
	}
	defer rows.Close()
	out := []Assignment{}
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// GrantInput names a role, a scope and a subject.
type GrantInput struct {
	Role Role
	// ProjectKey scopes the grant to one project. Empty is the whole
	// organization.
	ProjectKey string
	// Exactly one of these is set.
	UserID  *uuid.UUID
	GroupID *uuid.UUID
}

// Grant gives a role to somebody, or to a group.
//
// Granting the same thing twice is not an error: it is the state the caller
// asked for, and refusing would make an administration page that has to read
// before it writes.
func (s *Store) Grant(ctx context.Context, in GrantInput, actor uuid.UUID) (*Assignment, db.LSN, error) {
	if in.Role == "" {
		return nil, 0, ErrRoleNotFound
	}
	if (in.UserID == nil) == (in.GroupID == nil) {
		return nil, 0, errors.New("a role is granted to a person or to a group, not both")
	}

	var out *Assignment
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		// The role is the installation's own: what it is called and whether
		// it answers for the whole installation are its row's to say.
		var wholeOrg bool
		err := tx.QueryRow(ctx, `SELECT installation_wide_only FROM role WHERE key = $1`, string(in.Role)).Scan(&wholeOrg)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrRoleNotFound, in.Role)
		}
		if err != nil {
			return err
		}
		if wholeOrg && in.ProjectKey != "" {
			return fmt.Errorf("%w: %s answers for all projects at once", ErrScope, in.Role)
		}
		var projectID *uuid.UUID
		if in.ProjectKey != "" {
			var found uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, in.ProjectKey).Scan(&found)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: no project %s", ErrNotFound, in.ProjectKey)
			}
			if err != nil {
				return err
			}
			projectID = &found
		}

		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO role_assignment (role, project_id, user_id, group_id, created_by)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (role, project_id, user_id, group_id)
			DO UPDATE SET created_by = EXCLUDED.created_by
			RETURNING id`,
			string(in.Role), projectID, in.UserID, in.GroupID, actor,
		).Scan(&id)
		if isCheckViolation(err) {
			return ErrNotAMember
		}
		if err != nil {
			return fmt.Errorf("grant role: %w", err)
		}

		out, err = scanAssignment(tx.QueryRow(ctx, selectAssignment+` WHERE a.id = $1`, id))
		if err != nil {
			return err
		}
		return events.Emit(ctx, tx, "role.granted", map[string]any{
			"assignmentId": id, "role": in.Role, "projectId": projectID,
			"userId": in.UserID, "groupId": in.GroupID, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// globalAdministratorsLock serializes taking global administration away. The
// database's guard on role_assignment (00951) takes the same one.
const globalAdministratorsLock = "armature.global_administrators"

// KeepsAnAdministrator names the trigger's refusal, so a removal it stops, such as erasing the last
// administrator's account, reads as ErrLastGlobalAdministrator.
const KeepsAnAdministrator = "role_assignment_keeps_an_administrator"

// ErrLastGlobalAdministrator refuses taking away the last global administrator assignment.
var ErrLastGlobalAdministrator = errors.New("this is the last global administrator, so granting another one has to come first")

// Revoke takes a role away.
//
// The last global administrator cannot be revoked. An organization with nobody
// able to administer it is one where the fix is a database console, and the
// owner's standing role is not enough on its own because an owner can leave.
func (s *Store) Revoke(ctx context.Context, id uuid.UUID, actor uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		found, err := scanAssignment(tx.QueryRow(ctx,
			selectAssignment+` WHERE a.id = $1 FOR UPDATE OF a`, id))
		if err != nil {
			return err
		}

		if found.Role == GlobalAdministrator {
			// Counted under the lock, so two revokes at once cannot each count
			// the other as the one that stays.
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, globalAdministratorsLock); err != nil {
				return fmt.Errorf("wait for other revokes: %w", err)
			}
			var remaining int
			if err := tx.QueryRow(ctx, `
				SELECT count(*) FROM role_assignment
				WHERE role = 'global_administrator' AND id <> $1`, id).Scan(&remaining); err != nil {
				return err
			}
			if remaining == 0 {
				return ErrLastGlobalAdministrator
			}
		}

		if _, err := tx.Exec(ctx, `DELETE FROM role_assignment WHERE id = $1`, id); err != nil {
			return fmt.Errorf("revoke role: %w", err)
		}
		return events.Emit(ctx, tx, "role.revoked", map[string]any{
			"assignmentId": id, "role": found.Role, "actorId": actor,
		})
	})
}
