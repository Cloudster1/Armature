package perm

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// Definition is one of an organization's roles: what it is called and what
// it grants. The built-in five are rows like any other, editable but not
// deletable; a role the organization added is wholly its own.
type Definition struct {
	Key         Role         `json:"role"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	OrgWideOnly bool         `json:"orgWideOnly"`
	Builtin     bool         `json:"builtin"`
	Permissions []Permission `json:"permissions"`
	// InUse counts the grants that name it, so a listing can say what
	// deleting it would take away.
	InUse int `json:"inUse"`
}

// Definitions is what each of an organization's roles grants, by key.
type Definitions map[Role][]Permission

// Builtins are the roles every organization starts with, as the migration
// installs them. NewSet without definitions reads from here, which is what
// a unit test wants and what a request never gets.
func Builtins() []Definition {
	names := map[Role][2]string{
		GlobalAdministrator:  {"Global administrator", "Administers the tenant: its members, its roles, its workflows and every project in it."},
		ProjectAdministrator: {"Project administrator", "Configures one project: its boards, its workflow scheme, its teams, and whether it is archived."},
		ScrumMaster:          {"Scrum master", "Runs the work in one project: sprints, teams and the planning that goes with them."},
		User:                 {"User", "Does the ordinary work: files issues, moves them, comments."},
		Reader:               {"Reader", "Sees a project and changes nothing in it."},
	}
	out := make([]Definition, 0, len(Roles))
	for _, role := range Roles {
		out = append(out, Definition{
			Key: role, Name: names[role][0], Description: names[role][1],
			OrgWideOnly: role.OrgWideOnly(), Builtin: true, Permissions: role.Permissions(),
		})
	}
	return out
}

func builtinDefinitions() Definitions {
	out := Definitions{}
	for _, d := range Builtins() {
		out[d.Key] = d.Permissions
	}
	return out
}

// AllPermissions is every permission the code checks for, in the order the
// matrix lists them.
var AllPermissions = []Permission{
	Read, IssueWrite, IssueTransition, CommentWrite,
	SprintManage, TeamManage, BoardConfigure,
	ProjectAdminister, ProjectCreate, OrgAdminister,
}

// PermissionWords says what each permission lets somebody do, for a matrix
// that people read rather than parse.
var PermissionWords = map[Permission]string{
	Read:              "See projects and everything in them",
	IssueWrite:        "Create and edit issues",
	IssueTransition:   "Move issues through the workflow",
	CommentWrite:      "Comment",
	SprintManage:      "Plan, start and complete sprints",
	TeamManage:        "Form teams and decide who is on them",
	BoardConfigure:    "Set up boards",
	ProjectAdminister: "Administer a project's own settings",
	ProjectCreate:     "Create projects",
	OrgAdminister:     "Administer the organization",
}

// RoleInput is a role as the matrix sends it; nil leaves a field alone on an edit.
type RoleInput struct {
	Key         Role
	Name        *string
	Description *string
	OrgWideOnly *bool
	Permissions []Permission
}

var (
	// ErrRoleNotFound is returned for a role the organization does not have.
	ErrRoleNotFound = errors.New("this organization has no such role")
	// ErrBuiltinRole is returned when a built-in role is deleted.
	ErrBuiltinRole = errors.New("a built-in role cannot be deleted; take its grants away instead")
	// ErrRoleNameTaken is returned when another role already has the name.
	ErrRoleNameTaken = errors.New("this organization already has a role with that name")
	// ErrRoleKey is returned for a key that is not a short lowercase word.
	ErrRoleKey = errors.New("a role's key is 2 to 40 lowercase letters, digits and underscores, starting with a letter")
	// ErrRoleName is returned for a missing name.
	ErrRoleName = errors.New("give the role a name")
	// ErrUnknownPermission is returned for a permission the code does not check for.
	ErrUnknownPermission = errors.New("that is not a permission")
	// ErrLockout is returned when the global administrator would stop
	// administering the organization.
	ErrLockout = errors.New("the global administrator keeps administering the organization, or nobody could undo this")
)

var roleKey = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

// KeyFromName makes a key out of a name: lowercase words joined by
// underscores, which is what the built-in keys look like.
func KeyFromName(name string) Role {
	var b strings.Builder
	last := '_'
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			last = r
		case last != '_':
			b.WriteRune('_')
			last = '_'
		}
	}
	key := strings.Trim(b.String(), "_")
	if key != "" && (key[0] < 'a' || key[0] > 'z') {
		key = "r_" + key
	}
	if len(key) > 40 {
		key = key[:40]
	}
	return Role(key)
}

const selectRoles = `
SELECT r.key, r.name, r.description, r.org_wide_only, r.builtin, r.permissions,
       (SELECT count(*) FROM role_assignment a WHERE a.org_id = r.org_id AND a.role = r.key)
FROM org_role r`

func scanDefinition(row pgx.Row) (*Definition, error) {
	var (
		d     Definition
		perms []string
	)
	err := row.Scan(&d.Key, &d.Name, &d.Description, &d.OrgWideOnly, &d.Builtin, &perms, &d.InUse)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrRoleNotFound
	}
	if err != nil {
		return nil, err
	}
	d.Permissions = make([]Permission, 0, len(perms))
	for _, p := range perms {
		d.Permissions = append(d.Permissions, Permission(p))
	}
	return &d, nil
}

// Roles lists the organization's roles, most powerful first as installed,
// then the organization's own by name.
func (s *Store) Roles(ctx context.Context) ([]Definition, error) {
	out := []Definition{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectRoles+` ORDER BY r.sort_order, lower(r.name)`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			d, err := scanDefinition(rows)
			if err != nil {
				return err
			}
			out = append(out, *d)
		}
		return rows.Err()
	})
	return out, err
}

// definitionsFor reads what each role grants, for resolving one person.
func definitionsFor(ctx context.Context, tx db.DBTX, orgID uuid.UUID) (Definitions, error) {
	rows, err := tx.Query(ctx, `SELECT key, permissions FROM org_role WHERE org_id = $1`, orgID)
	if err != nil {
		return nil, fmt.Errorf("read roles: %w", err)
	}
	defer rows.Close()
	out := Definitions{}
	for rows.Next() {
		var (
			key   Role
			perms []string
		)
		if err := rows.Scan(&key, &perms); err != nil {
			return nil, err
		}
		list := make([]Permission, 0, len(perms))
		for _, p := range perms {
			list = append(list, Permission(p))
		}
		out[key] = list
	}
	return out, rows.Err()
}

func cleanPermissions(in []Permission) ([]string, error) {
	seen := map[Permission]bool{}
	out := []string{}
	for _, p := range AllPermissions {
		if slices.Contains(in, p) && !seen[p] {
			seen[p] = true
			out = append(out, string(p))
		}
	}
	for _, p := range in {
		if !slices.Contains(AllPermissions, p) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownPermission, p)
		}
	}
	return out, nil
}

// CreateRole adds a role of the organization's own.
func (s *Store) CreateRole(ctx context.Context, in RoleInput, actor uuid.UUID) (*Definition, db.LSN, error) {
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" {
		return nil, 0, ErrRoleName
	}
	key := in.Key
	if key == "" {
		key = KeyFromName(name)
	}
	if !roleKey.MatchString(string(key)) {
		return nil, 0, ErrRoleKey
	}
	perms, err := cleanPermissions(in.Permissions)
	if err != nil {
		return nil, 0, err
	}
	description := ""
	if in.Description != nil {
		description = strings.TrimSpace(*in.Description)
	}
	orgWide := in.OrgWideOnly != nil && *in.OrgWideOnly

	var out *Definition
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO org_role (org_id, key, name, description, org_wide_only, permissions)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)`, string(key), name, description, orgWide, perms)
		if isUniqueViolation(err) {
			return ErrRoleNameTaken
		}
		if isCheckViolation(err) {
			return ErrRoleKey
		}
		if err != nil {
			return fmt.Errorf("create role: %w", err)
		}
		out, err = scanDefinition(tx.QueryRow(ctx, selectRoles+` WHERE r.key = $1`, string(key)))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "role.defined", map[string]any{"role": key, "actorId": actor})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// UpdateRole renames a role, describes it, or changes what it grants; every
// grant of it follows from the next request.
func (s *Store) UpdateRole(ctx context.Context, in RoleInput, actor uuid.UUID) (*Definition, db.LSN, error) {
	var perms []string
	if in.Permissions != nil {
		cleaned, err := cleanPermissions(in.Permissions)
		if err != nil {
			return nil, 0, err
		}
		perms = cleaned
	}
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		return nil, 0, ErrRoleName
	}
	if in.Key == GlobalAdministrator && perms != nil && !slices.Contains(perms, string(OrgAdminister)) {
		return nil, 0, ErrLockout
	}
	var out *Definition
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := scanDefinition(tx.QueryRow(ctx, selectRoles+` WHERE r.key = $1 FOR UPDATE OF r`, string(in.Key)))
		if err != nil {
			return err
		}
		name, description, orgWide := current.Name, current.Description, current.OrgWideOnly
		if in.Name != nil {
			name = strings.TrimSpace(*in.Name)
		}
		if in.Description != nil {
			description = strings.TrimSpace(*in.Description)
		}
		if in.OrgWideOnly != nil {
			orgWide = *in.OrgWideOnly
		}
		if perms == nil {
			perms = make([]string, 0, len(current.Permissions))
			for _, p := range current.Permissions {
				perms = append(perms, string(p))
			}
		}
		_, err = tx.Exec(ctx, `
			UPDATE org_role SET name = $2, description = $3, org_wide_only = $4, permissions = $5
			WHERE key = $1`, string(in.Key), name, description, orgWide, perms)
		if isUniqueViolation(err) {
			return ErrRoleNameTaken
		}
		if isCheckViolation(err) {
			return ErrLockout
		}
		if err != nil {
			return fmt.Errorf("update role: %w", err)
		}
		out, err = scanDefinition(tx.QueryRow(ctx, selectRoles+` WHERE r.key = $1`, string(in.Key)))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "role.redefined", map[string]any{"role": in.Key, "actorId": actor})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// DeleteRole removes a role of the organization's own and every grant of it.
func (s *Store) DeleteRole(ctx context.Context, key Role, actor uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := scanDefinition(tx.QueryRow(ctx, selectRoles+` WHERE r.key = $1 FOR UPDATE OF r`, string(key)))
		if err != nil {
			return err
		}
		if current.Builtin {
			return ErrBuiltinRole
		}
		if _, err := tx.Exec(ctx, `DELETE FROM org_role WHERE key = $1`, string(key)); err != nil {
			if isCheckViolation(err) {
				return ErrBuiltinRole
			}
			return fmt.Errorf("delete role: %w", err)
		}
		return events.EmitInTenant(ctx, tx, "role.removed", map[string]any{"role": key, "grants": current.InUse, "actorId": actor})
	})
}
