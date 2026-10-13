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
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
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
	// signup is who may create an organization; empty means open.
	signup SignupPolicy
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

// SignupInput creates a person and their first organization together. A signup
// with neither is meaningless, and creating them in one transaction is what
// stops a failure halfway through leaving an account with nowhere to go.
type SignupInput struct {
	Email     string
	Password  string
	Name      string
	OrgName   string
	OrgSlug   string // optional; derived from OrgName when empty
	UserAgent string
	IP        string
}

// Signup creates the user, the organization and the owner membership in one
// transaction, then logs the user straight in.
func (s *Service) Signup(ctx context.Context, in SignupInput) (*Credentials, error) {
	// Refused before the password is hashed, which is the expensive part.
	if s.signupPolicy() == SignupClosed {
		return nil, ErrSignupClosed
	}
	email := NormalizeEmail(in.Email)
	if email == "" {
		return nil, errors.New("email is required")
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, errors.New("name is required")
	}
	if err := ValidatePassword(in.Password); err != nil {
		return nil, err
	}
	orgName := strings.TrimSpace(in.OrgName)
	if orgName == "" {
		return nil, errors.New("organization name is required")
	}

	slug := strings.ToLower(strings.TrimSpace(in.OrgSlug))
	if slug == "" {
		slug = Slugify(orgName)
	}
	if !ValidSlug(slug) {
		return nil, fmt.Errorf("%q is not a usable organization address: use 3 to 40 lowercase letters, digits or hyphens", slug)
	}

	hash, err := HashPassword(in.Password, s.params)
	if err != nil {
		return nil, err
	}

	var (
		principal Principal
		secret    string
		expiresAt = s.now().Add(s.sessionTTL)
	)

	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if policy := s.signupPolicy(); policy != SignupOpen {
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, signupLock); err != nil {
				return fmt.Errorf("wait for other signups: %w", err)
			}
			allowed, err := SignupAllowedIn(ctx, tx, policy)
			if err != nil {
				return err
			}
			if !allowed {
				return ErrSignupClosed
			}
		}

		var userID uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO app_user (email, name, password_hash)
			VALUES ($1, $2, $3)
			RETURNING id`,
			email, strings.TrimSpace(in.Name), hash,
		).Scan(&userID)
		if isUniqueViolation(err, "app_user_email_key") {
			return ErrEmailTaken
		}
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}

		var orgID uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO org (slug, name)
			VALUES ($1, $2)
			RETURNING id`,
			slug, orgName,
		).Scan(&orgID)
		if isUniqueViolation(err, "org_slug_key") {
			return ErrSlugTaken
		}
		if err != nil {
			return fmt.Errorf("create organization: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO org_member (org_id, user_id, org_role)
			VALUES ($1, $2, 'owner')`,
			orgID, userID,
		); err != nil {
			return fmt.Errorf("create membership: %w", err)
		}

		if err := grantJoiningRole(ctx, tx, orgID, userID, RoleOwner); err != nil {
			return err
		}

		// Statuses, issue types and a default workflow, in this same
		// transaction. Without them the new organization could not create a
		// project, which is the very next thing anybody does.
		if _, err := bootstrap.Org(ctx, tx, orgID); err != nil {
			return fmt.Errorf("prepare organization: %w", err)
		}

		sessionID, sessionSecret, err := insertSession(ctx, tx, userID, &orgID, ProofPassword, expiresAt, in.UserAgent, in.IP)
		if err != nil {
			return err
		}
		secret = sessionSecret

		principal = Principal{
			User:      User{ID: userID, Email: email, Name: strings.TrimSpace(in.Name), Timezone: "UTC", Locale: "en", IsActive: true},
			Org:       &Org{ID: orgID, Slug: slug, Name: orgName},
			Role:      RoleOwner,
			SessionID: &sessionID,
			Proof:     ProofPassword,
		}

		if err := writeAudit(ctx, tx, orgID, userID, "org.created", "org", &orgID, in.IP); err != nil {
			return err
		}
		// Emitted in the same transaction as the rows it describes, so the
		// event cannot exist for an organization that failed to be created,
		// nor go missing for one that was.
		return events.Emit(ctx, tx, orgID, events.TopicOrgCreated, map[string]any{
			"orgId":   orgID,
			"orgSlug": slug,
			"orgName": orgName,
			"ownerId": userID,
		})
	})
	if err != nil {
		return nil, err
	}

	return &Credentials{Principal: &principal, SessionSecret: secret, ExpiresAt: expiresAt, LSN: lsn}, nil
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

// sessionPrincipalSQL resolves a session to the caller's role in its
// organization; the joins are left so a lost membership can be told apart.
const sessionPrincipalSQL = `
SELECT s.id, s.last_seen_at, s.proof,
       u.id, u.email, u.name, u.timezone, u.locale, u.is_active, COALESCE(u.avatar_url, ''),
       o.id, o.slug, o.name, m.org_role, pp.id, pp.key,
       EXISTS (SELECT 1 FROM user_second_factor f WHERE f.user_id = u.id AND f.confirmed_at IS NOT NULL),
       s.proof = 'oidc' AND (s.proof_org_id = o.id OR EXISTS (
           SELECT 1 FROM oidc_provider home
           JOIN oidc_provider here ON here.issuer = home.issuer
           WHERE home.org_id = s.proof_org_id AND here.org_id = o.id AND home.enabled AND here.enabled))
FROM user_session s
JOIN app_user u ON u.id = s.user_id
LEFT JOIN org o ON o.id = s.current_org_id AND o.archived_at IS NULL
LEFT JOIN org_member m ON m.org_id = o.id AND m.user_id = u.id
LEFT JOIN project pp ON pp.id = s.portal_project_id
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
		vouched    bool
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, sessionPrincipalSQL, HashToken(secret)).Scan(
			&sessionID, &lastSeen, &p.Proof,
			&p.User.ID, &p.User.Email, &p.User.Name, &p.User.Timezone, &p.User.Locale, &p.User.IsActive, &p.User.AvatarURL,
			&orgID, &orgSlug, &orgName, &memberRole, &deskID, &deskKey,
			&enrolled, &vouched,
		)
		// Somebody with a factor, or vouched for, is held by no rule.
		if err != nil || orgID == nil || enrolled || vouched {
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
	// A provider that signed the session in and is trusted here vouched
	// for the person with whatever factor it asks for.
	p.SecondFactor = enrolled || vouched
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

// Memberships lists the organizations a user belongs to, most recently joined
// last, so that the first entry is a stable default organization.
func (s *Service) Memberships(ctx context.Context, userID uuid.UUID) ([]Membership, error) {
	var out []Membership
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT o.id, o.slug, o.name, m.org_role
			FROM org_member m
			JOIN org o ON o.id = m.org_id
			WHERE m.user_id = $1 AND o.archived_at IS NULL
			ORDER BY m.created_at`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m Membership
			if err := rows.Scan(&m.OrgID, &m.OrgSlug, &m.OrgName, &m.Role); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

// CreateOrganization makes a second organization for somebody who already has
// an account, owned by them, ready for a project, under the same policy that
// decides whether anybody may sign up at all.
func (s *Service) CreateOrganization(ctx context.Context, userID uuid.UUID, name, slug, ip string) (*Org, db.LSN, error) {
	if s.signupPolicy() == SignupClosed {
		return nil, 0, ErrSignupClosed
	}
	name = strings.TrimSpace(name)
	slug = strings.TrimSpace(strings.ToLower(slug))
	if name == "" || slug == "" {
		return nil, 0, errors.New("an organization needs a name and an address")
	}
	var org Org
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var orgID uuid.UUID
		err := tx.QueryRow(ctx, `INSERT INTO org (slug, name) VALUES ($1, $2) RETURNING id`, slug, name).Scan(&orgID)
		if isUniqueViolation(err, "org_slug_key") {
			return ErrSlugTaken
		}
		if err != nil {
			return fmt.Errorf("create organization: %w", err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'owner')`, orgID, userID); err != nil {
			return fmt.Errorf("create membership: %w", err)
		}
		if err := grantJoiningRole(ctx, tx, orgID, userID, RoleOwner); err != nil {
			return err
		}
		if _, err := bootstrap.Org(ctx, tx, orgID); err != nil {
			return fmt.Errorf("prepare organization: %w", err)
		}
		org = Org{ID: orgID, Slug: slug, Name: name}
		if err := writeAudit(ctx, tx, orgID, userID, "org.created", "org", &orgID, ip); err != nil {
			return err
		}
		return events.Emit(ctx, tx, orgID, events.TopicOrgCreated, map[string]any{
			"orgId": orgID, "orgSlug": slug, "orgName": name, "ownerId": userID,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return &org, lsn, nil
}

// SwitchOrg points a session at another organization of the person's, when its
// proof vouches for them there. The database decides that with the same
// function its trigger applies, so the two cannot drift apart.
func (s *Service) SwitchOrg(ctx context.Context, sessionID uuid.UUID, userID uuid.UUID, slug string) (*Org, db.LSN, error) {
	var org Org
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			SELECT o.id, o.slug, o.name
			FROM org o
			JOIN org_member m ON m.org_id = o.id AND m.user_id = $2
			WHERE o.slug = $1 AND o.archived_at IS NULL`,
			slug, userID,
		).Scan(&org.ID, &org.Slug, &org.Name)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotAMember
		}
		if err != nil {
			return err
		}
		var reaches bool
		if err := tx.QueryRow(ctx, `
			SELECT session_reaches(proof, proof_org_id, user_id, $2) FROM user_session WHERE id = $1`,
			sessionID, org.ID).Scan(&reaches); err != nil {
			return err
		}
		if !reaches {
			return ErrSessionStaysHome
		}
		_, err = tx.Exec(ctx, `UPDATE user_session SET current_org_id = $2 WHERE id = $1`, sessionID, org.ID)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return &org, lsn, nil
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
func insertSession(ctx context.Context, tx db.DBTX, userID uuid.UUID, orgID *uuid.UUID, proof Proof, expiresAt time.Time, userAgent, ip string) (uuid.UUID, string, error) {
	return insertSessionFor(ctx, tx, userID, orgID, nil, proof, expiresAt, userAgent, ip)
}

// insertSessionFor is insertSession with a desk: a session that came through
// an open door belongs to that desk and sees nothing else.
func insertSessionFor(ctx context.Context, tx db.DBTX, userID uuid.UUID, orgID, portalProject *uuid.UUID, proof Proof, expiresAt time.Time, userAgent, ip string) (uuid.UUID, string, error) {
	secret, digest, err := GenerateToken()
	if err != nil {
		return uuid.Nil, "", err
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO user_session (user_id, token_hash, current_org_id, proof_org_id, user_agent, ip, expires_at, portal_project_id, proof)
		VALUES ($1, $2, $3, $3, NULLIF($4, ''), NULLIF($5, '')::inet, $6, $7, $8)
		RETURNING id`,
		userID, digest, orgID, userAgent, ip, expiresAt, portalProject, string(proof),
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
