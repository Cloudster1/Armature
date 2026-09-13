package perm

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

var (
	// ErrNotFound is returned for a group or an assignment that does not exist
	// in the caller's organization.
	ErrNotFound = errors.New("not found")
	// ErrNotAMember is returned when granting a role to, or putting on a group,
	// somebody who is not in the organization. Neither is a way in.
	ErrNotAMember = errors.New("that person is not a member of this organization")
	// ErrScope is returned for a role granted over the wrong kind of scope.
	ErrScope = errors.New("that role cannot be granted over one project")
	// ErrNameTaken is returned when a group by that name already exists.
	ErrNameTaken = errors.New("this organization already has a group with that name")
	// ErrManagedElsewhere is returned when editing the membership of a group the
	// identity provider owns. Changing it here would last until the next login.
	ErrManagedElsewhere = errors.New("that group's members come from the identity provider")
)

// Store reads and writes roles and groups.
type Store struct {
	db *db.Cluster
}

func NewStore(cluster *db.Cluster) *Store { return &Store{db: cluster} }

// resolveFor gathers everything granted to one person: the roles given to them
// directly, and the roles given to any group they belong to.
const resolveFor = `
SELECT a.role, COALESCE(p.key, '')
FROM role_assignment a
LEFT JOIN project p ON p.id = a.project_id
LEFT JOIN group_member m ON m.group_id = a.group_id AND m.user_id = $1
WHERE a.org_id = $2
  AND (a.user_id = $1 OR m.user_id IS NOT NULL)`

// Resolve returns what one person may do in one organization.
//
// The organization's owner is always a global administrator, whatever the
// assignments say. Roles are data and data can be edited; an owner who has
// removed their own last administrator role would otherwise have locked
// themselves out of the tenant they own, with nobody able to let them back in.
func (s *Store) Resolve(ctx context.Context, tx db.DBTX, orgID, userID uuid.UUID, owner bool) (Set, error) {
	var grants []Grant
	if owner {
		grants = append(grants, Grant{Role: GlobalAdministrator})
	}

	rows, err := tx.Query(ctx, resolveFor, userID, orgID)
	if err != nil {
		return Set{}, fmt.Errorf("resolve permissions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.Role, &g.ProjectKey); err != nil {
			return Set{}, err
		}
		grants = append(grants, g)
	}
	if err := rows.Err(); err != nil {
		return Set{}, err
	}
	return NewSet(grants), nil
}

// ResolveFor is Resolve on its own connection, for callers outside a
// transaction.
func (s *Store) ResolveFor(ctx context.Context, orgID, userID uuid.UUID, owner bool) (Set, error) {
	var set Set
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		set, err = s.Resolve(ctx, tx, orgID, userID, owner)
		return err
	})
	return set, err
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}

// Group is a named set of people, and what it has been granted.
type Group struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	// Source says who owns the membership. A group from the identity provider
	// is a mirror of what it sends, not something to edit here.
	Source string `json:"source"`
	// ExternalRef is the value the provider's groups claim is matched against.
	ExternalRef string   `json:"externalRef,omitempty"`
	Members     []Member `json:"members,omitempty"`
	MemberCount int      `json:"memberCount"`
	RoleCount   int      `json:"roleCount"`
}

// FromProvider reports whether the identity provider owns this group's members.
func (g Group) FromProvider() bool { return g.Source == "oidc" }

// Member is a person in a group.
type Member struct {
	UserID uuid.UUID `json:"userId"`
	Name   string    `json:"name"`
	Email  string    `json:"email,omitempty"`
}

const selectGroup = `
SELECT g.id, g.name, g.description, g.source::text, COALESCE(g.external_ref, ''),
       (SELECT count(*) FROM group_member m WHERE m.group_id = g.id),
       (SELECT count(*) FROM role_assignment a WHERE a.group_id = g.id)
FROM user_group g`

func scanGroup(row pgx.Row) (*Group, error) {
	var g Group
	err := row.Scan(&g.ID, &g.Name, &g.Description, &g.Source, &g.ExternalRef,
		&g.MemberCount, &g.RoleCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// Groups lists the organization's groups.
func (s *Store) Groups(ctx context.Context) ([]Group, error) {
	out := []Group{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectGroup+` ORDER BY g.name`)
		if err != nil {
			return fmt.Errorf("list groups: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			g, err := scanGroup(rows)
			if err != nil {
				return err
			}
			out = append(out, *g)
		}
		return rows.Err()
	})
	return out, err
}

// GroupByID reads one group with the people in it.
func (s *Store) GroupByID(ctx context.Context, id uuid.UUID) (*Group, error) {
	var out *Group
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scanGroup(tx.QueryRow(ctx, selectGroup+` WHERE g.id = $1`, id))
		if err != nil {
			return err
		}
		out.Members, err = groupMembers(ctx, tx, id)
		return err
	})
	return out, err
}

func groupMembers(ctx context.Context, tx db.DBTX, groupID uuid.UUID) ([]Member, error) {
	rows, err := tx.Query(ctx, `
		SELECT m.user_id, u.name, u.email
		FROM group_member m
		JOIN app_user u ON u.id = m.user_id
		WHERE m.group_id = $1
		ORDER BY u.name`, groupID)
	if err != nil {
		return nil, fmt.Errorf("read group members: %w", err)
	}
	defer rows.Close()

	out := []Member{}
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Name, &m.Email); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GroupInput describes a group being created or renamed.
type GroupInput struct {
	Name        string
	Description string
	// ExternalRef ties the group to a value in the provider's groups claim.
	// Setting it makes the provider the owner of the membership.
	ExternalRef string
}

// CreateGroup adds a group.
func (s *Store) CreateGroup(ctx context.Context, in GroupInput, actor uuid.UUID) (*Group, db.LSN, error) {
	if in.Name == "" {
		return nil, 0, errors.New("a group needs a name")
	}

	var out *Group
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		source, ref := "local", any(nil)
		if in.ExternalRef != "" {
			source, ref = "oidc", in.ExternalRef
		}

		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO user_group (org_id, name, description, source, external_ref)
			VALUES (current_org_id(), $1, $2, $3, $4) RETURNING id`,
			in.Name, in.Description, source, ref,
		).Scan(&id)
		if isUniqueViolation(err) {
			return ErrNameTaken
		}
		if err != nil {
			return fmt.Errorf("create group: %w", err)
		}

		out, err = scanGroup(tx.QueryRow(ctx, selectGroup+` WHERE g.id = $1`, id))
		if err != nil {
			return err
		}
		out.Members = []Member{}
		return events.EmitInTenant(ctx, tx, "group.created", map[string]any{
			"groupId": id, "name": in.Name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// DeleteGroup removes a group along with everything granted to it.
func (s *Store) DeleteGroup(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM user_group WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("delete group: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// AddToGroup puts somebody in a group.
func (s *Store) AddToGroup(ctx context.Context, groupID, userID uuid.UUID, actor uuid.UUID) (*Group, db.LSN, error) {
	var out *Group
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		found, err := scanGroup(tx.QueryRow(ctx, selectGroup+` WHERE g.id = $1`, groupID))
		if err != nil {
			return err
		}
		if found.FromProvider() {
			return fmt.Errorf("%w: %s", ErrManagedElsewhere, found.Name)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO group_member (org_id, group_id, user_id)
			VALUES (current_org_id(), $1, $2) ON CONFLICT DO NOTHING`, groupID, userID)
		if isCheckViolation(err) {
			return ErrNotAMember
		}
		if err != nil {
			return fmt.Errorf("add to group: %w", err)
		}

		out, err = s.reloadGroup(ctx, tx, groupID)
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "group.member_added", map[string]any{
			"groupId": groupID, "userId": userID, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// RemoveFromGroup takes somebody out of a group.
func (s *Store) RemoveFromGroup(ctx context.Context, groupID, userID uuid.UUID, actor uuid.UUID) (*Group, db.LSN, error) {
	var out *Group
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		found, err := scanGroup(tx.QueryRow(ctx, selectGroup+` WHERE g.id = $1`, groupID))
		if err != nil {
			return err
		}
		if found.FromProvider() {
			return fmt.Errorf("%w: %s", ErrManagedElsewhere, found.Name)
		}

		if _, err := tx.Exec(ctx, `
			DELETE FROM group_member WHERE group_id = $1 AND user_id = $2`,
			groupID, userID); err != nil {
			return fmt.Errorf("remove from group: %w", err)
		}

		out, err = s.reloadGroup(ctx, tx, groupID)
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "group.member_removed", map[string]any{
			"groupId": groupID, "userId": userID, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

func (s *Store) reloadGroup(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Group, error) {
	found, err := scanGroup(tx.QueryRow(ctx, selectGroup+` WHERE g.id = $1`, id))
	if err != nil {
		return nil, err
	}
	found.Members, err = groupMembers(ctx, tx, id)
	return found, err
}
