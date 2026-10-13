package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/installation"
	"github.com/armature/armature/backend/internal/seal"
	"github.com/armature/armature/backend/internal/tenant"
)

// Service implements the identity use cases. Operations that must span tenants
// or run before one is known (signup, login, session lookup) go through the
// cluster's admin path; everything else is tenant scoped like any other data
// access, so row level security applies to it too.
type Service struct {
	db         *db.Cluster
	params     PasswordParams
	sessionTTL time.Duration
	now        func() time.Time
	// portalCooldown is how long an address waits between portal codes; zero
	// means the default minute.
	portalCooldown time.Duration
	// signup is who may sign up; empty means first.
	signup SignupPolicy
	// installationName is what the first account's installation is called.
	installationName string
	// sealer seals authenticator secrets; nil turns enrolment off.
	sealer *seal.Sealer
}

// NewService constructs the identity service.
func NewService(cluster *db.Cluster, params PasswordParams, sessionTTL time.Duration) *Service {
	return &Service{
		db:         cluster,
		params:     params,
		sessionTTL: sessionTTL,
		now:        time.Now,
	}
}

// Credentials is the result of a successful signup or login: the authenticated
// principal, the session secret to set as a cookie, and the write ahead log
// position of the change, so the caller's next read is not served by a replica
// that has not caught up yet.
type Credentials struct {
	Principal     *Principal
	SessionSecret string
	ExpiresAt     time.Time
	LSN           db.LSN
}

// Authenticate resolves a presented secret to a principal. It accepts both a
// session cookie value and a personal access token.
func (s *Service) Authenticate(ctx context.Context, secret string) (*Principal, error) {
	if secret == "" {
		return nil, ErrInvalidToken
	}
	if IsAPIToken(secret) {
		return s.authenticateAPIToken(ctx, secret)
	}
	return s.authenticateSession(ctx, secret)
}

// firstJoined orders a person's memberships so the one a session acts in comes
// first: an installation gives each person one, the integration harness several.
const firstJoined = `ORDER BY m.created_at, m.org_id LIMIT 1`

// sessionPrincipalSQL resolves a session to the caller's role in the person's
// organization, or the desk's for an open door; left joins tell a lost membership apart.
const sessionPrincipalSQL = `
SELECT s.id, s.last_seen_at, s.proof,
       u.id, u.email, u.name, u.timezone, u.locale, u.is_active, COALESCE(u.avatar_url, ''),
       o.id, o.slug, o.name, m.org_role, pp.id, pp.key,
       EXISTS (SELECT 1 FROM user_second_factor f WHERE f.user_id = u.id AND f.confirmed_at IS NOT NULL)
FROM user_session s
JOIN app_user u ON u.id = s.user_id
LEFT JOIN project pp ON pp.id = s.portal_project_id
LEFT JOIN LATERAL (
    SELECT m.org_id, m.org_role FROM org_member m
    JOIN org mo ON mo.id = m.org_id AND mo.archived_at IS NULL
    WHERE m.user_id = u.id AND (pp.id IS NULL OR m.org_id = pp.org_id)
    ` + firstJoined + `) m ON true
LEFT JOIN org o ON o.id = m.org_id
WHERE s.token_hash = $1 AND s.expires_at > now()`

func (s *Service) authenticateSession(ctx context.Context, secret string) (*Principal, error) {
	var (
		p          Principal
		sessionID  uuid.UUID
		lastSeen   time.Time
		orgID      *uuid.UUID
		orgSlug    *string
		orgName    *string
		memberRole *string
		deskID     *uuid.UUID
		deskKey    *string
		rule       SecondFactorRule
		enrolled   bool
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, sessionPrincipalSQL, HashToken(secret)).Scan(
			&sessionID, &lastSeen, &p.Proof,
			&p.User.ID, &p.User.Email, &p.User.Name, &p.User.Timezone, &p.User.Locale, &p.User.IsActive, &p.User.AvatarURL,
			&orgID, &orgSlug, &orgName, &memberRole, &deskID, &deskKey,
			&enrolled,
		)
		// Somebody with a factor, or signed in by the provider, is held by no rule.
		if err != nil || orgID == nil || enrolled || p.Proof == ProofOIDC {
			return err
		}
		read, err := installation.SecondFactor(tenant.WithOrg(ctx, tenant.Org{ID: *orgID}), tx)
		rule = SecondFactorRule(read)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	if !p.User.IsActive {
		return nil, ErrUserInactive
	}

	// A membership row is required, not merely an org row: revoking someone's
	// membership ends an already open session at once.
	if orgID == nil || memberRole == nil {
		return nil, ErrInvalidToken
	}
	p.SessionID = &sessionID
	p.Org = &Org{ID: *orgID, Slug: derefString(orgSlug), Name: derefString(orgName)}
	p.Role = OrgRole(*memberRole)
	// The installation's one provider vouched for the person with whatever
	// factor it asks for.
	p.SecondFactor = enrolled || p.Proof == ProofOIDC
	p.MustEnrol = rule.Covers(p.Role) && !p.SecondFactor
	if deskID != nil {
		p.PortalDeskID = deskID
		p.PortalDesk = derefString(deskKey)
	}

	// Touch last_seen_at at most once a minute: on a busy instance this is one
	// write per request otherwise, for a field nothing reads in real time.
	if s.now().Sub(lastSeen) > time.Minute {
		_, _ = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE user_session SET last_seen_at = now() WHERE id = $1`, sessionID)
			return err
		})
	}
	return &p, nil
}

const apiTokenPrincipalSQL = `
SELECT t.id, t.scopes,
       u.id, u.email, u.name, u.timezone, u.locale, u.is_active, COALESCE(u.avatar_url, ''),
       o.id, o.slug, o.name, m.org_role,
       COALESCE(ARRAY(SELECT p.key FROM api_token_project tp
                      JOIN project p ON p.id = tp.project_id
                      WHERE tp.token_id = t.id ORDER BY p.key), '{}')
FROM api_token t
JOIN app_user u ON u.id = t.user_id
JOIN org o ON o.id = t.org_id AND o.archived_at IS NULL
LEFT JOIN org_member m ON m.org_id = o.id AND m.user_id = u.id
WHERE t.token_hash = $1 AND (t.expires_at IS NULL OR t.expires_at > now())`

func (s *Service) authenticateAPIToken(ctx context.Context, secret string) (*Principal, error) {
	var (
		p          Principal
		tokenID    uuid.UUID
		orgID      uuid.UUID
		orgSlug    string
		orgName    string
		memberRole *string
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, apiTokenPrincipalSQL, HashToken(secret)).Scan(
			&tokenID, &p.Scopes,
			&p.User.ID, &p.User.Email, &p.User.Name, &p.User.Timezone, &p.User.Locale, &p.User.IsActive, &p.User.AvatarURL,
			&orgID, &orgSlug, &orgName, &memberRole, &p.TokenProjects,
		)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrInvalidToken
	}
	if err != nil {
		return nil, err
	}
	if !p.User.IsActive {
		return nil, ErrUserInactive
	}
	if memberRole == nil {
		// The token outlived its owner's membership.
		return nil, ErrSwitchedOff
	}

	p.TokenID = &tokenID
	p.Org = &Org{ID: orgID, Slug: orgSlug, Name: orgName}
	p.Role = OrgRole(*memberRole)

	_, _ = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE api_token SET last_used_at = now() WHERE id = $1`, tokenID)
		return err
	})
	return &p, nil
}

// Logout deletes a session. It is idempotent.
func (s *Service) Logout(ctx context.Context, sessionID uuid.UUID) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM user_session WHERE id = $1`, sessionID)
		return err
	})
	return err
}

// grantJoiningRole gives somebody the role their standing implies as they join.
//
// It runs in the same transaction as the membership, so there is never a moment
// where a person is a member of an organization they can do nothing in.
func grantJoiningRole(ctx context.Context, tx db.DBTX, orgID, userID uuid.UUID, role OrgRole) error {
	appRole := role.AppRole()
	if appRole == "" {
		return nil
	}
	// An organization from before roles were rows may still lack its five;
	// this adds nothing where they are there.
	if _, err := tx.Exec(ctx, `SELECT org_builtin_roles($1)`, orgID); err != nil {
		return fmt.Errorf("make sure the organization has its roles: %w", err)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO role_assignment (org_id, role, user_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, orgID, appRole, userID)
	if err != nil {
		return fmt.Errorf("grant the role that comes with joining: %w", err)
	}
	return nil
}

// insertSession creates a session row and returns its id and secret. The secret
// exists only in this return value and the user's cookie; the database stores
// nothing but its digest.
func insertSession(ctx context.Context, tx db.DBTX, userID uuid.UUID, proof Proof, expiresAt time.Time, userAgent, ip string) (uuid.UUID, string, error) {
	return insertSessionFor(ctx, tx, userID, nil, proof, expiresAt, userAgent, ip)
}

// insertSessionFor is insertSession with a desk: a session that came through
// an open door belongs to that desk and sees nothing else.
func insertSessionFor(ctx context.Context, tx db.DBTX, userID uuid.UUID, portalProject *uuid.UUID, proof Proof, expiresAt time.Time, userAgent, ip string) (uuid.UUID, string, error) {
	secret, digest, err := GenerateToken()
	if err != nil {
		return uuid.Nil, "", err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO user_session (user_id, token_hash, user_agent, ip, expires_at, portal_project_id, proof)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, '')::inet, $5, $6, $7)
		RETURNING id`,
		userID, digest, userAgent, ip, expiresAt, portalProject, string(proof),
	).Scan(&id)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("create session: %w", err)
	}
	return id, secret, nil
}

func writeAudit(ctx context.Context, tx db.DBTX, orgID, actor uuid.UUID, action, targetType string, targetID *uuid.UUID, ip string) error {
	return audit.Write(ctx, tx, orgID, audit.Entry{Action: action, TargetType: targetType, TargetID: targetID, Actor: actor, IP: ip})
}

// NormalizeEmail lowercases and trims an address. The column is citext so the
// database is case insensitive too; this keeps what we store tidy.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ContextForOrg returns a context scoped to the principal's organization. It is
// the seam between authentication and data access: handlers get this from the
// middleware, and command line tools build it directly.
func ContextForOrg(ctx context.Context, p *Principal) context.Context {
	if p == nil {
		return ctx
	}
	return tenant.WithOrg(ctx, tenant.Org{ID: p.Org.ID, Slug: p.Org.Slug})
}

// OrgBySlug finds an organization by the slug in a URL.
//
// It runs on the admin path because the caller is not signed in yet: a sign-in
// has to know which tenant it is for before there is anybody to ask.
func (s *Service) OrgBySlug(ctx context.Context, slug string) (*Org, error) {
	var org Org
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT id, slug, name FROM org WHERE slug = $1 AND archived_at IS NULL`,
			strings.ToLower(strings.TrimSpace(slug)),
		).Scan(&org.ID, &org.Slug, &org.Name)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotAMember
	}
	if err != nil {
		return nil, fmt.Errorf("find organization: %w", err)
	}
	return &org, nil
}
