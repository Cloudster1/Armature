package auth

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
)

// An organization manages only the accounts that belong nowhere else; anybody
// else is let go, not changed. app_user has no tenant, so this is the admin path.

// SignInMethod is how an account gets in.
type SignInMethod string

const (
	SignInPassword SignInMethod = "password"
	SignInProvider SignInMethod = "provider"
	SignInNone     SignInMethod = "none"
)

// ManagedUser is a member as the users page sees them.
type ManagedUser struct {
	ID          uuid.UUID    `json:"id"`
	Email       string       `json:"email"`
	Name        string       `json:"name"`
	Role        OrgRole      `json:"role"`
	IsActive    bool         `json:"isActive"`
	SignsInWith SignInMethod `json:"signsInWith"`
	// Managed: this is the one place the person belongs, and the caller may
	// touch them (only an owner touches an owner).
	Managed   bool      `json:"managed"`
	CreatedAt time.Time `json:"createdAt"`
}

// CreateLocalUserInput is what an administrator types to make an account.
type CreateLocalUserInput struct {
	Email    string
	Name     string
	Role     OrgRole
	Password string
}

// ManagedUserInput is what an administrator may change. Nil leaves a field alone.
type ManagedUserInput struct {
	Name     *string
	Role     *OrgRole
	IsActive *bool
}

var (
	// ErrAddressHasAccount is returned when making an account at an address
	// somebody already signs in with, here or elsewhere.
	ErrAddressHasAccount = errors.New("that address already has an account. Invite them instead")
	// ErrNoSuchUser is returned for a person who is not a member here.
	ErrNoSuchUser = errors.New("that person is not a member here")
	// ErrManagedElsewhere is returned for a person who also belongs to another
	// organization, whose account is therefore not this one's to change.
	ErrManagedElsewhere = errors.New("that person also belongs to another organization, so only they can change their account. You can let them go instead")
	// ErrOwnAccount is returned when an administrator turns these tools on themselves.
	ErrOwnAccount = errors.New("that is your own account. Change it from your profile")
	// ErrOwnerOnly is returned when somebody who is not an owner changes an owner.
	ErrOwnerOnly = errors.New("only an owner can change another owner")
	// ErrOwnerStanding is returned when an owner's role is changed here.
	ErrOwnerStanding = errors.New("an owner's standing is not changed here")
	// ErrLastActiveOwner is returned when switching somebody off would leave
	// an organization with nobody who owns it.
	ErrLastActiveOwner = errors.New("the last owner of an organization cannot be switched off")
	// ErrProviderAccount is returned when a password is set on an account the
	// identity provider signs in.
	ErrProviderAccount = errors.New("that account signs in through the identity provider and has no password here")
	// ErrWrongPassword is returned when the current password offered to change
	// a password is not it.
	ErrWrongPassword = errors.New("that is not your current password")
	// ErrBadUserRole is returned for a standing an account cannot be made with.
	ErrBadUserRole = errors.New("choose member or admin")
	// ErrUserName is returned for a missing or overlong name.
	ErrUserName = errors.New("give the person a name of up to 120 characters")
	// ErrUserAddress is returned for something that is not a mail address.
	ErrUserAddress = errors.New("give a mail address such as ada@example.com")
)

// managedRow is what every guard reads about a person before acting.
type managedRow struct {
	user        ManagedUser
	hasPassword bool
	onlyHere    bool
	hasProvider bool
}

// The one shape every read takes. The person is excluded who is not a member,
// who is the organization's automation account, or who was erased.
const managedSelectSQL = `
SELECT u.id, u.email::text, u.name, m.org_role, u.is_active, u.created_at,
       u.password_hash IS NOT NULL,
       NOT EXISTS (SELECT 1 FROM org_member x JOIN org xo ON xo.id = x.org_id
                   WHERE x.user_id = u.id AND x.org_id <> m.org_id AND xo.archived_at IS NULL),
       EXISTS (SELECT 1 FROM oidc_provider p WHERE p.org_id = m.org_id)
FROM org_member m
JOIN app_user u ON u.id = m.user_id
JOIN org o ON o.id = m.org_id
WHERE m.org_id = $1 AND u.erased_at IS NULL AND u.id IS DISTINCT FROM o.automation_user_id`

func scanManaged(row pgx.Row, actorRole OrgRole) (managedRow, error) {
	var r managedRow
	err := row.Scan(&r.user.ID, &r.user.Email, &r.user.Name, &r.user.Role, &r.user.IsActive, &r.user.CreatedAt,
		&r.hasPassword, &r.onlyHere, &r.hasProvider)
	if err != nil {
		return r, err
	}
	switch {
	case r.hasPassword:
		r.user.SignsInWith = SignInPassword
	case r.hasProvider:
		r.user.SignsInWith = SignInProvider
	default:
		r.user.SignsInWith = SignInNone
	}
	r.user.Managed = r.onlyHere && (r.user.Role != RoleOwner || actorRole == RoleOwner)
	return r, nil
}

// ManagedUsers lists the organization's people, switched off ones included,
// with what an administrator may do to each.
func (s *Service) ManagedUsers(ctx context.Context, orgID uuid.UUID, actorRole OrgRole) ([]ManagedUser, error) {
	out := []ManagedUser{}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, managedSelectSQL+` ORDER BY u.name, u.email`, orgID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanManaged(rows, actorRole)
			if err != nil {
				return err
			}
			out = append(out, r.user)
		}
		return rows.Err()
	})
	return out, err
}

func readManaged(ctx context.Context, tx db.DBTX, orgID, userID uuid.UUID, actorRole OrgRole) (managedRow, error) {
	r, err := scanManaged(tx.QueryRow(ctx, managedSelectSQL+` AND u.id = $2`, orgID, userID), actorRole)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNoSuchUser
	}
	return r, err
}

// guardManaged is what every change to somebody else's account asks first.
func guardManaged(ctx context.Context, tx db.DBTX, orgID, userID, actor uuid.UUID, actorRole OrgRole) (managedRow, error) {
	if userID == actor {
		return managedRow{}, ErrOwnAccount
	}
	r, err := readManaged(ctx, tx, orgID, userID, actorRole)
	if err != nil {
		return r, err
	}
	if !r.onlyHere {
		return r, ErrManagedElsewhere
	}
	if r.user.Role == RoleOwner && actorRole != RoleOwner {
		return r, ErrOwnerOnly
	}
	return r, nil
}

func cleanUserName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > NameMaxLength {
		return "", ErrUserName
	}
	return name, nil
}

// CreateLocalUser makes a password account here. An address that signs in
// elsewhere is refused; a passwordless row of this organization is claimed.
func (s *Service) CreateLocalUser(ctx context.Context, orgID uuid.UUID, in CreateLocalUserInput, actor uuid.UUID, ip string) (*ManagedUser, db.LSN, error) {
	email, err := ParseAddress(in.Email)
	if err != nil {
		return nil, 0, ErrUserAddress
	}
	name, err := cleanUserName(in.Name)
	if err != nil {
		return nil, 0, err
	}
	if in.Role != RoleAdmin && in.Role != RoleMember {
		return nil, 0, ErrBadUserRole
	}
	if err := ValidatePassword(in.Password); err != nil {
		return nil, 0, err
	}
	hash, err := HashPassword(in.Password, s.params)
	if err != nil {
		return nil, 0, err
	}

	var made ManagedUser
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			userID uuid.UUID
			taken  bool
		)
		err := tx.QueryRow(ctx, `
			SELECT u.id,
			       u.password_hash IS NOT NULL OR u.erased_at IS NOT NULL
			           OR EXISTS (SELECT 1 FROM org_member x WHERE x.user_id = u.id AND x.org_id <> $2)
			FROM app_user u WHERE u.email = $1`, email, orgID).Scan(&userID, &taken)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			err = tx.QueryRow(ctx, `
				INSERT INTO app_user (email, name, password_hash)
				VALUES ($1, $2, $3)
				RETURNING id`, email, name, hash).Scan(&userID)
			if isUniqueViolation(err, "app_user_email_key") {
				return ErrAddressHasAccount
			}
			if err != nil {
				return fmt.Errorf("create user: %w", err)
			}
		case err != nil:
			return err
		case taken:
			return ErrAddressHasAccount
		default:
			if _, err := tx.Exec(ctx, `
				UPDATE app_user SET name = $2, password_hash = $3, is_active = true
				WHERE id = $1`, userID, name, hash); err != nil {
				return fmt.Errorf("claim the account: %w", err)
			}
		}

		// A customer row here is lifted to the standing given; a fresh
		// account is simply put in.
		if _, err := tx.Exec(ctx, `
			INSERT INTO org_member (org_id, user_id, org_role)
			VALUES ($1, $2, $3)
			ON CONFLICT (org_id, user_id) DO UPDATE SET org_role = EXCLUDED.org_role`,
			orgID, userID, string(in.Role)); err != nil {
			return fmt.Errorf("create membership: %w", err)
		}
		if err := grantJoiningRole(ctx, tx, orgID, userID, in.Role); err != nil {
			return err
		}
		if err := writeAudit(ctx, tx, orgID, actor, "member.created", "user", &userID, ip); err != nil {
			return err
		}
		if err := events.Emit(ctx, tx, orgID, events.TopicMemberJoined, map[string]any{
			"orgId":  orgID,
			"userId": userID,
			"role":   in.Role,
		}); err != nil {
			return err
		}
		r, err := readManaged(ctx, tx, orgID, userID, RoleOwner)
		if err != nil {
			return err
		}
		made = r.user
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return &made, lsn, nil
}

// UpdateManagedUser renames somebody, changes their standing, or switches
// them off or on. Switching off ends every session and token they hold.
func (s *Service) UpdateManagedUser(ctx context.Context, orgID, userID uuid.UUID, in ManagedUserInput, actor uuid.UUID, actorRole OrgRole, ip string) (*ManagedUser, db.LSN, error) {
	if in.Name != nil {
		name, err := cleanUserName(*in.Name)
		if err != nil {
			return nil, 0, err
		}
		in.Name = &name
	}
	if in.Role != nil && *in.Role != RoleAdmin && *in.Role != RoleMember {
		return nil, 0, ErrBadUserRole
	}

	var updated ManagedUser
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := guardManaged(ctx, tx, orgID, userID, actor, actorRole)
		if err != nil {
			return err
		}
		changes := map[string]any{}

		if in.Name != nil && *in.Name != r.user.Name {
			if _, err := tx.Exec(ctx, `UPDATE app_user SET name = $2 WHERE id = $1`, userID, *in.Name); err != nil {
				return fmt.Errorf("rename: %w", err)
			}
			changes["name"] = *in.Name
		}

		if in.Role != nil && *in.Role != r.user.Role {
			if r.user.Role == RoleOwner {
				return ErrOwnerStanding
			}
			if _, err := tx.Exec(ctx, `UPDATE org_member SET org_role = $3 WHERE org_id = $1 AND user_id = $2`,
				orgID, userID, string(*in.Role)); err != nil {
				return fmt.Errorf("change standing: %w", err)
			}
			// The old standing's joining role goes with it, or a demoted
			// administrator would keep the keys.
			if old := r.user.Role.AppRole(); old != "" {
				if _, err := tx.Exec(ctx, `
					DELETE FROM role_assignment
					WHERE org_id = $1 AND user_id = $2 AND project_id IS NULL AND role = $3::app_role`,
					orgID, userID, old); err != nil {
					return fmt.Errorf("take back the joining role: %w", err)
				}
			}
			if err := grantJoiningRole(ctx, tx, orgID, userID, *in.Role); err != nil {
				return err
			}
			changes["role"] = *in.Role
		}

		if in.IsActive != nil && *in.IsActive != r.user.IsActive {
			if !*in.IsActive {
				if orphan, err := soleActiveOwnerOf(ctx, tx, userID); err != nil {
					return err
				} else if orphan != "" {
					return ErrLastActiveOwner
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE app_user SET is_active = $2 WHERE id = $1`, userID, *in.IsActive); err != nil {
				return fmt.Errorf("switch the account: %w", err)
			}
			action := "member.reactivated"
			if !*in.IsActive {
				action = "member.deactivated"
				if err := endEverySession(ctx, tx, userID, nil); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `DELETE FROM api_token WHERE user_id = $1`, userID); err != nil {
					return fmt.Errorf("revoke tokens: %w", err)
				}
			}
			if err := writeAudit(ctx, tx, orgID, actor, action, "user", &userID, ip); err != nil {
				return err
			}
		}

		if len(changes) > 0 {
			if err := writeAudit(ctx, tx, orgID, actor, "member.updated", "user", &userID, ip); err != nil {
				return err
			}
		}
		r, err = readManaged(ctx, tx, orgID, userID, actorRole)
		if err != nil {
			return err
		}
		updated = r.user
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return &updated, lsn, nil
}

// SetManagedPassword gives somebody a new password and ends every session
// they had, so whoever held the old one is out.
func (s *Service) SetManagedPassword(ctx context.Context, orgID, userID uuid.UUID, password string, actor uuid.UUID, actorRole OrgRole, ip string) (db.LSN, error) {
	if err := ValidatePassword(password); err != nil {
		return 0, err
	}
	hash, err := HashPassword(password, s.params)
	if err != nil {
		return 0, err
	}
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		r, err := guardManaged(ctx, tx, orgID, userID, actor, actorRole)
		if err != nil {
			return err
		}
		if !r.hasPassword && r.hasProvider {
			return ErrProviderAccount
		}
		if _, err := tx.Exec(ctx, `UPDATE app_user SET password_hash = $2 WHERE id = $1`, userID, hash); err != nil {
			return fmt.Errorf("set password: %w", err)
		}
		if err := endEverySession(ctx, tx, userID, nil); err != nil {
			return err
		}
		return writeAudit(ctx, tx, orgID, actor, "member.password_set", "user", &userID, ip)
	})
}

// ChangeOwnPassword is the person's own change, proven by the password they
// have. Every other session of theirs ends; the one asking stays.
func (s *Service) ChangeOwnPassword(ctx context.Context, userID uuid.UUID, keep *uuid.UUID, current, next string) (db.LSN, error) {
	var stored *string
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT password_hash FROM app_user WHERE id = $1`, userID).Scan(&stored)
	})
	if err != nil {
		return 0, err
	}
	if stored == nil {
		return 0, ErrProviderAccount
	}
	ok, _, err := VerifyPassword(current, *stored, s.params)
	if err != nil || !ok {
		return 0, ErrWrongPassword
	}
	if err := ValidatePassword(next); err != nil {
		return 0, err
	}
	hash, err := HashPassword(next, s.params)
	if err != nil {
		return 0, err
	}
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `UPDATE app_user SET password_hash = $2 WHERE id = $1`, userID, hash); err != nil {
			return fmt.Errorf("change password: %w", err)
		}
		return endEverySession(ctx, tx, userID, keep)
	})
}

// endEverySession signs the person out everywhere, except the session named.
func endEverySession(ctx context.Context, tx db.DBTX, userID uuid.UUID, keep *uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM user_session WHERE user_id = $1 AND ($2::uuid IS NULL OR id <> $2)`, userID, keep); err != nil {
		return fmt.Errorf("end sessions: %w", err)
	}
	return nil
}

// soleActiveOwnerOf names an organization with no other active owner, or "".
// A trigger asks the same, so a write that forgets to ask is refused there.
func soleActiveOwnerOf(ctx context.Context, tx db.DBTX, userID uuid.UUID) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `
		SELECT o.name FROM org_member m JOIN org o ON o.id = m.org_id
		WHERE m.user_id = $1 AND m.org_role = 'owner' AND o.archived_at IS NULL
		  AND NOT EXISTS (SELECT 1 FROM org_member x JOIN app_user xu ON xu.id = x.user_id
		                  WHERE x.org_id = m.org_id AND x.org_role = 'owner' AND x.user_id <> $1 AND xu.is_active)
		ORDER BY o.name LIMIT 1`, userID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return name, err
}
