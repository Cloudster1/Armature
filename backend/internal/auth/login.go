package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
)

// Login opens a session for a password, or a *ChallengeIssued for somebody with
// a second factor; an unknown address and a wrong password fail alike.
func (s *Service) Login(ctx context.Context, email, password, userAgent, ip string) (*Credentials, error) {
	email = NormalizeEmail(email)

	var (
		userID   uuid.UUID
		isActive bool
		stored   *string
		enrolled bool
	)
	// Whether there is a second factor is read with the password, so a wrong
	// password costs the same whether or not the account has one.
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT id, is_active, password_hash,
			       EXISTS (SELECT 1 FROM user_second_factor f WHERE f.user_id = app_user.id AND f.confirmed_at IS NOT NULL)
			FROM app_user WHERE email = $1`, email,
		).Scan(&userID, &isActive, &stored, &enrolled)
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

	if enrolled {
		challenge, err := s.issueChallenge(ctx, userID)
		if err != nil {
			return nil, err
		}
		return nil, challenge
	}

	expiresAt := s.now().Add(s.sessionTTL)
	var (
		principal *Principal
		secret    string
	)
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		if principal, err = passwordPrincipal(ctx, tx, userID); err != nil {
			return err
		}
		secret, err = openPasswordSession(ctx, tx, principal, expiresAt, userAgent, ip)
		return err
	})
	if err != nil {
		return nil, err
	}

	return &Credentials{Principal: principal, SessionSecret: secret, ExpiresAt: expiresAt, LSN: lsn}, nil
}

// passwordPrincipal is who a password sign-in opens a session for, in the
// person's organization; with none left there is nobody to sign in.
func passwordPrincipal(ctx context.Context, tx db.DBTX, userID uuid.UUID) (*Principal, error) {
	var p Principal
	err := tx.QueryRow(ctx, `
		SELECT id, email, name, timezone, locale, is_active, COALESCE(avatar_url, '')
		FROM app_user WHERE id = $1`, userID,
	).Scan(&p.User.ID, &p.User.Email, &p.User.Name, &p.User.Timezone, &p.User.Locale, &p.User.IsActive, &p.User.AvatarURL)
	if err != nil {
		return nil, fmt.Errorf("read the account: %w", err)
	}
	var org Org
	err = tx.QueryRow(ctx, `
		SELECT o.id, o.slug, o.name, u.standing
		FROM app_user u, installation o
		WHERE u.id = $1 AND u.standing IS NOT NULL AND o.archived_at IS NULL`, userID,
	).Scan(&org.ID, &org.Slug, &org.Name, &p.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSwitchedOff
	}
	if err != nil {
		return nil, fmt.Errorf("read the person's organization: %w", err)
	}
	p.placeIn(org)
	return &p, nil
}

// openPasswordSession opens the session a proven password sign-in gets and
// records the sign-in.
func openPasswordSession(ctx context.Context, tx db.DBTX, p *Principal, expiresAt time.Time, userAgent, ip string) (string, error) {
	sessionID, secret, err := insertSession(ctx, tx, p.User.ID, ProofPassword, expiresAt, userAgent, ip)
	if err != nil {
		return "", err
	}
	p.SessionID = &sessionID
	p.Proof = ProofPassword
	return secret, writeAudit(ctx, tx, p.User.ID, "auth.login", "user", &p.User.ID, ip)
}

// decoyHash is a real argon2id hash of an unguessable value, verified against
// when the email is unknown so that both branches of Login cost the same.
const decoyHash = "$argon2id$v=19$m=65536,t=3,p=4$c29tZS1zdGF0aWMtc2FsdA$Zm9yLXRpbWluZy1lcXVhbGl0eS1vbmx5LW5vdC1hLXNl"
