package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// The operations below are tenant scoped like any other data access: they run
// through the cluster's normal Write and Read paths, so row level security
// constrains them exactly as it constrains issues or projects.

// CreateInvite records an invitation and returns it together with the secret to
// put in the invitation link. The secret is not recoverable afterwards.
func (s *Service) CreateInvite(ctx context.Context, email string, role OrgRole, invitedBy uuid.UUID, ttl time.Duration) (*Invite, string, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return nil, "", errors.New("email is required")
	}
	if !role.Valid() || role == RoleOwner {
		return nil, "", fmt.Errorf("%q is not a role that can be invited", role)
	}

	secret, digest, err := GenerateToken()
	if err != nil {
		return nil, "", err
	}
	inv := Invite{Email: email, Role: role, ExpiresAt: s.now().Add(ttl)}

	_, err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		// An invitation only ever adds somebody, or lifts a customer onto the
		// team; it never rewrites the standing of somebody who is already here.
		var standing OrgRole
		err := tx.QueryRow(ctx, `
			SELECT m.org_role FROM org_member m JOIN app_user u ON u.id = m.user_id
			WHERE m.org_id = current_org_id() AND u.email = $1`, email).Scan(&standing)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil && standing.IsAgent() {
			return ErrAlreadyMember
		}
		// Re-inviting someone who already has a pending invitation replaces it,
		// rather than failing on the partial unique index.
		return tx.QueryRow(ctx, `
			INSERT INTO org_invite (org_id, email, org_role, token_hash, invited_by, expires_at)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)
			ON CONFLICT (org_id, email) WHERE accepted_at IS NULL
			DO UPDATE SET org_role = EXCLUDED.org_role,
			              token_hash = EXCLUDED.token_hash,
			              invited_by = EXCLUDED.invited_by,
			              expires_at = EXCLUDED.expires_at
			RETURNING id, org_id, created_at`,
			email, string(role), digest, invitedBy, inv.ExpiresAt,
		).Scan(&inv.ID, &inv.OrgID, &inv.CreatedAt)
	})
	if err != nil {
		return nil, "", fmt.Errorf("create invite: %w", err)
	}
	return &inv, secret, nil
}

// ListInvites returns the organization's pending invitations.
func (s *Service) ListInvites(ctx context.Context) ([]Invite, error) {
	var out []Invite
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT id, org_id, email, org_role, expires_at, created_at
			FROM org_invite
			WHERE accepted_at IS NULL AND expires_at > now()
			ORDER BY created_at DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var inv Invite
			if err := rows.Scan(&inv.ID, &inv.OrgID, &inv.Email, &inv.Role, &inv.ExpiresAt, &inv.CreatedAt); err != nil {
				return err
			}
			out = append(out, inv)
		}
		return rows.Err()
	})
	return out, err
}

// InvitePreview is what an invitation link tells whoever opened it, before
// they accept: where it leads and for whom. Holding the link is the whole of
// the authorization, and accepting would tell them the same.
type InvitePreview struct {
	OrgName string  `json:"orgName"`
	Email   string  `json:"email"`
	Role    OrgRole `json:"role"`
}

// PreviewInvite reads a pending invitation by its secret. It runs before
// anybody is signed in, so it takes the admin path.
func (s *Service) PreviewInvite(ctx context.Context, secret string) (*InvitePreview, error) {
	var out InvitePreview
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT o.name, i.email, i.org_role
			FROM org_invite i
			JOIN org o ON o.id = i.org_id AND o.archived_at IS NULL
			WHERE i.token_hash = $1 AND i.accepted_at IS NULL AND i.expires_at > now()`,
			HashToken(secret)).Scan(&out.OrgName, &out.Email, &out.Role)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInviteInvalid
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// RevokeInvite withdraws a pending invitation.
func (s *Service) RevokeInvite(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM org_invite WHERE id = $1 AND accepted_at IS NULL`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrInviteInvalid
		}
		return nil
	})
	return err
}

// AcceptInviteInput carries either an existing user's identity or the details
// needed to create one on the spot.
type AcceptInviteInput struct {
	Secret string
	// UserID is set when an already signed-in user accepts an invitation.
	UserID *uuid.UUID
	// Proof and SessionOrg describe the signed-in acceptor's session: one
	// proven for a single organization may only take an invitation to it.
	Proof      Proof
	SessionOrg *uuid.UUID
	// Name and Password create a new account when UserID is nil.
	Name      string
	Password  string
	UserAgent string
	IP        string
}

// AcceptInvite consumes an invitation, creating the account if needed, adding
// the membership and opening a session. It runs before the accepting user has
// any tenant scope of their own, so it takes the admin path.
func (s *Service) AcceptInvite(ctx context.Context, in AcceptInviteInput) (*Credentials, error) {
	var (
		principal Principal
		secret    string
		expiresAt = s.now().Add(s.sessionTTL)
	)

	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			inviteID uuid.UUID
			orgID    uuid.UUID
			orgSlug  string
			orgName  string
			email    string
			role     OrgRole
		)
		// FOR UPDATE on the invite makes acceptance single use even if the link
		// is opened twice at the same moment.
		err := tx.QueryRow(ctx, `
			SELECT i.id, i.org_id, o.slug, o.name, i.email, i.org_role
			FROM org_invite i
			JOIN org o ON o.id = i.org_id AND o.archived_at IS NULL
			WHERE i.token_hash = $1 AND i.accepted_at IS NULL AND i.expires_at > now()
			FOR UPDATE OF i`,
			HashToken(in.Secret),
		).Scan(&inviteID, &orgID, &orgSlug, &orgName, &email, &role)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInviteInvalid
		}
		if err != nil {
			return err
		}

		var (
			userID uuid.UUID
			name   string
			proof  Proof
		)
		if in.UserID != nil {
			// The link is only proof for the address it was sent to, so a
			// signed-in person takes only an invitation addressed to them.
			userID = *in.UserID
			var own string
			if err := tx.QueryRow(ctx, `SELECT name, email FROM app_user WHERE id = $1`, userID).Scan(&name, &own); err != nil {
				return err
			}
			if NormalizeEmail(own) != NormalizeEmail(email) {
				return ErrInviteForSomeoneElse
			}
			if !in.Proof.ReachesEverywhere() && (in.SessionOrg == nil || *in.SessionOrg != orgID) {
				return ErrSessionStaysHome
			}
			proof = in.Proof
		} else {
			if len([]rune(in.Password)) < 12 {
				return errors.New("password must be at least 12 characters")
			}
			if strings.TrimSpace(in.Name) == "" {
				return errors.New("name is required")
			}
			// An address that already has an account is that account's to
			// accept: whoever holds the link may be the inviter, not the invitee.
			var taken bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM app_user WHERE email = $1)`, email).Scan(&taken); err != nil {
				return err
			}
			if taken {
				return ErrSignInToAccept
			}
			hash, err := HashPassword(in.Password, s.params)
			if err != nil {
				return err
			}
			name = strings.TrimSpace(in.Name)
			err = tx.QueryRow(ctx, `
				INSERT INTO app_user (email, name, password_hash)
				VALUES ($1, $2, $3)
				RETURNING id`,
				email, name, hash,
			).Scan(&userID)
			if isUniqueViolation(err, "app_user_email_key") {
				return ErrSignInToAccept
			}
			if err != nil {
				return fmt.Errorf("create user: %w", err)
			}
			proof = ProofInvite
		}

		// Only a customer is lifted by an invitation; a member already on the
		// team keeps the standing they have.
		tag, err := tx.Exec(ctx, `
			INSERT INTO org_member (org_id, user_id, org_role)
			VALUES ($1, $2, $3)
			ON CONFLICT (org_id, user_id) DO UPDATE SET org_role = EXCLUDED.org_role
			WHERE org_member.org_role = 'customer'`,
			orgID, userID, string(role),
		)
		if err != nil {
			return fmt.Errorf("create membership: %w", err)
		}
		if tag.RowsAffected() > 0 {
			if err := grantJoiningRole(ctx, tx, orgID, userID, role); err != nil {
				return err
			}
		} else if err := tx.QueryRow(ctx, `SELECT org_role FROM org_member WHERE org_id = $1 AND user_id = $2`, orgID, userID).Scan(&role); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `UPDATE org_invite SET accepted_at = now() WHERE id = $1`, inviteID); err != nil {
			return err
		}

		sessionID, sessionSecret, err := insertSession(ctx, tx, userID, &orgID, proof, expiresAt, in.UserAgent, in.IP)
		if err != nil {
			return err
		}
		secret = sessionSecret
		principal = Principal{
			User:      User{ID: userID, Email: email, Name: name, Timezone: "UTC", Locale: "en", IsActive: true},
			Org:       &Org{ID: orgID, Slug: orgSlug, Name: orgName},
			Role:      role,
			SessionID: &sessionID,
			Proof:     proof,
		}
		if err := writeAudit(ctx, tx, orgID, userID, "member.joined", "user", &userID, in.IP); err != nil {
			return err
		}
		return events.Emit(ctx, tx, orgID, events.TopicMemberJoined, map[string]any{
			"orgId":  orgID,
			"userId": userID,
			"role":   role,
		})
	})
	if err != nil {
		return nil, err
	}
	return &Credentials{Principal: &principal, SessionSecret: secret, ExpiresAt: expiresAt, LSN: lsn}, nil
}

// ErrNoSuchProject is returned when a token names a project the caller cannot
// see. Naming one would not widen the token, but it would read as if it had.
var ErrNoSuchProject = errors.New("a token can only name projects you can already reach")

// CreateAPIToken issues a personal access token for the caller in the current
// organization. The secret is returned once and never stored.
func (s *Service) CreateAPIToken(ctx context.Context, userID uuid.UUID, name string, scopes, projects []string, expiresAt *time.Time) (*APIToken, db.LSN, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, 0, errors.New("token name is required")
	}
	if err := ValidateScopes(scopes); err != nil {
		return nil, 0, err
	}
	secret, digest, err := GenerateAPIToken()
	if err != nil {
		return nil, 0, err
	}
	if scopes == nil {
		scopes = []string{}
	}
	if projects == nil {
		projects = []string{}
	}

	tok := APIToken{Name: name, Scopes: scopes, Projects: projects, ExpiresAt: expiresAt, Secret: secret}
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO api_token (org_id, user_id, name, token_hash, scopes, expires_at)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)
			RETURNING id, created_at`,
			userID, name, digest, scopes, expiresAt,
		).Scan(&tok.ID, &tok.CreatedAt); err != nil {
			return err
		}
		if len(projects) > 0 {
			// The insert selects the ids, so a key naming a project of another
			// organization inserts nothing and is refused by the count.
			tag, err := tx.Exec(ctx, `
				INSERT INTO api_token_project (org_id, token_id, project_id)
				SELECT current_org_id(), $1, p.id FROM project p
				WHERE p.org_id = current_org_id() AND p.key = ANY($2)`, tok.ID, projects)
			if err != nil {
				return err
			}
			if int(tag.RowsAffected()) != len(projects) {
				return ErrNoSuchProject
			}
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "token.created", TargetType: "api_token", TargetID: &tok.ID, Actor: userID, Data: map[string]any{"name": name, "scopes": scopes, "projects": projects}})
	})
	if err != nil {
		if errors.Is(err, ErrNoSuchProject) {
			return nil, 0, err
		}
		return nil, 0, fmt.Errorf("create api token: %w", err)
	}
	return &tok, lsn, nil
}

// ListAPITokens returns the caller's tokens in the current organization,
// without their secrets.
func (s *Service) ListAPITokens(ctx context.Context, userID uuid.UUID) ([]APIToken, error) {
	var out []APIToken
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT t.id, t.name, t.scopes, t.last_used_at, t.expires_at, t.created_at,
			       COALESCE(ARRAY(SELECT p.key FROM api_token_project tp
			                      JOIN project p ON p.id = tp.project_id
			                      WHERE tp.token_id = t.id ORDER BY p.key), '{}')
			FROM api_token t WHERE t.user_id = $1
			ORDER BY t.created_at DESC`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t APIToken
			if err := rows.Scan(&t.ID, &t.Name, &t.Scopes, &t.LastUsedAt, &t.ExpiresAt, &t.CreatedAt, &t.Projects); err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// RevokeAPIToken deletes one of the caller's tokens.
func (s *Service) RevokeAPIToken(ctx context.Context, userID, tokenID uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM api_token WHERE id = $1 AND user_id = $2`, tokenID, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrInvalidToken
		}
		return audit.Record(ctx, tx, audit.Entry{Action: "token.revoked", TargetType: "api_token", TargetID: &tokenID, Actor: userID})
	})
}
