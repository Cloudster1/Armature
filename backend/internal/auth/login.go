package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
)

// Login verifies a password and opens a session in the user's first
// organization. It returns ErrInvalidCredentials for an unknown address and a
// wrong password alike, so the endpoint cannot be used to enumerate accounts.
func (s *Service) Login(ctx context.Context, email, password, userAgent, ip string) (*Credentials, error) {
	email = NormalizeEmail(email)

	var (
		userID    uuid.UUID
		name      string
		timezone  string
		locale    string
		isActive  bool
		stored    *string
		avatarURL string
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT id, name, timezone, locale, is_active, password_hash, COALESCE(avatar_url, '')
			FROM app_user WHERE email = $1`, email,
		).Scan(&userID, &name, &timezone, &locale, &isActive, &stored, &avatarURL)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Spend comparable time on an unknown address so that response timing
		// does not reveal whether the account exists.
		_, _, _ = VerifyPassword(password, decoyHash, s.params)
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, ErrInvalidCredentials // single sign-on only account
	}

	ok, needsRehash, err := VerifyPassword(password, *stored, s.params)
	if err != nil || !ok {
		return nil, ErrInvalidCredentials
	}
	if !isActive {
		return nil, ErrUserInactive
	}

	if needsRehash {
		if upgraded, err := HashPassword(password, s.params); err == nil {
			_, _ = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
				_, err := tx.Exec(ctx, `UPDATE app_user SET password_hash = $2 WHERE id = $1`, userID, upgraded)
				return err
			})
		}
	}

	memberships, err := s.Memberships(ctx, userID)
	if err != nil {
		return nil, err
	}

	var (
		orgID     *uuid.UUID
		principal = Principal{User: User{ID: userID, Email: email, Name: name, Timezone: timezone, Locale: locale, IsActive: isActive, AvatarURL: avatarURL}}
	)
	if len(memberships) > 0 {
		m := memberships[0]
		orgID = &m.OrgID
		principal.Org = &Org{ID: m.OrgID, Slug: m.OrgSlug, Name: m.OrgName}
		principal.Role = m.Role
	}

	expiresAt := s.now().Add(s.sessionTTL)
	var secret string
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		sessionID, sessionSecret, err := insertSession(ctx, tx, userID, orgID, ProofPassword, expiresAt, userAgent, ip)
		if err != nil {
			return err
		}
		secret = sessionSecret
		principal.SessionID = &sessionID
		principal.Proof = ProofPassword
		// Recorded in the organization the session opens in, as single sign-on
		// records its own; somebody with no organization has no log to be in.
		if orgID == nil {
			return nil
		}
		return writeAudit(ctx, tx, *orgID, userID, "auth.login", "user", &userID, ip)
	})
	if err != nil {
		return nil, err
	}

	return &Credentials{Principal: &principal, SessionSecret: secret, ExpiresAt: expiresAt, LSN: lsn}, nil
}

// decoyHash is a real argon2id hash of an unguessable value, verified against
// when the email is unknown so that both branches of Login cost the same.
const decoyHash = "$argon2id$v=19$m=65536,t=3,p=4$c29tZS1zdGF0aWMtc2FsdA$Zm9yLXRpbWluZy1lcXVhbGl0eS1vbmx5LW5vdC1hLXNl"
