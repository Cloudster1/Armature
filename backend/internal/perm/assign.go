package perm

import (
	"context"
	"errors"
	"fmt"

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

const selectAssignment = `
SELECT a.id, a.role, a.project_id, COALESCE(p.key, ''),
       a.user_id, COALESCE(u.name, ''), a.group_id, COALESCE(g.name, '')
FROM role_assignment a
LEFT JOIN project p ON p.id = a.project_id
LEFT JOIN app_user u ON u.id = a.user_id
LEFT JOIN user_group g ON g.id = a.group_id`

// order reads the way somebody looks for a grant: the organization-wide ones
// first, then by project, and the most powerful role at the top of each.
const order = `
ORDER BY (a.project_id IS NOT NULL), p.key,
         array_position(ARRAY['global_administrator', 'project_administrator',
                              'scrum_master', 'user', 'reader']::app_role[], a.role),
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

// Assignments lists who holds what. A project key narrows it to the roles that
// answer in that project, which includes the organization-wide ones because
// they answer everywhere.
func (s *Store) Assignments(ctx context.Context, projectKey string) ([]Assignment, error) {
	query := selectAssignment
	args := []any{}
	if projectKey != "" {
		query += ` WHERE a.project_id IS NULL OR p.key = $1`
		args = append(args, projectKey)
	}

	out := []Assignment{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, query+order, args...)
		if err != nil {
			return fmt.Errorf("list role assignments: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			a, err := scanAssignment(rows)
			if err != nil {
				return err
			}
			out = append(out, *a)
		}
		return rows.Err()
	})
	return out, err
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
	if !in.Role.Valid() {
		return nil, 0, fmt.Errorf("%q is not a role", in.Role)
	}
	if (in.UserID == nil) == (in.GroupID == nil) {
		return nil, 0, errors.New("a role is granted to a person or to a group, not both")
	}
	if in.Role.OrgWideOnly() && in.ProjectKey != "" {
		return nil, 0, fmt.Errorf("%w: %s answers for the whole organization", ErrScope, in.Role)
	}

	var out *Assignment
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
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
		err := tx.QueryRow(ctx, `
			INSERT INTO role_assignment (org_id, role, project_id, user_id, group_id, created_by)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)
			ON CONFLICT (org_id, role, project_id, user_id, group_id)
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
		return events.EmitInTenant(ctx, tx, "role.granted", map[string]any{
			"assignmentId": id, "role": in.Role, "projectId": projectID,
			"userId": in.UserID, "groupId": in.GroupID, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

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
			var remaining int
			if err := tx.QueryRow(ctx, `
				SELECT count(*) FROM role_assignment
				WHERE role = 'global_administrator' AND id <> $1`, id).Scan(&remaining); err != nil {
				return err
			}
			if remaining == 0 {
				return errors.New("this is the last global administrator, so granting another one has to come first")
			}
		}

		if _, err := tx.Exec(ctx, `DELETE FROM role_assignment WHERE id = $1`, id); err != nil {
			return fmt.Errorf("revoke role: %w", err)
		}
		return events.EmitInTenant(ctx, tx, "role.revoked", map[string]any{
			"assignmentId": id, "role": found.Role, "actorId": actor,
		})
	})
}
