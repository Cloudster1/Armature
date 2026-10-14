package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/db"
)

// ResetLinkTTL is how long a mailed link to set a new password works: long
// enough to find the mail, short enough that an old mailbox is not a way in.
const ResetLinkTTL = time.Hour

// ErrResetLinkInvalid covers a link that is unknown, used or expired alike.
var ErrResetLinkInvalid = errors.New("this link has expired or was already used. Ask for a new one from the sign-in page")

// ResetRequest is what asking for a new password comes to: a link for an
// account with a password, or the organizations whose provider signs it in.
type ResetRequest struct {
	Email     string
	Secret    string
	ExpiresAt time.Time
	// Providers names the organizations, by slug, to sign in through instead.
	Providers []string
}

// RequestPasswordReset prepares the mail for somebody who forgot their
// password. Nil means there is nothing to send, which the caller must not show.
func (s *Service) RequestPasswordReset(ctx context.Context, email, ip string) (*ResetRequest, error) {
	email, err := ParseAddress(email)
	if err != nil {
		return nil, err
	}
	var out *ResetRequest
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			userID      uuid.UUID
			hasPassword bool
		)
		err := tx.QueryRow(ctx, `
			SELECT id, password_hash IS NOT NULL FROM app_user
			WHERE email = $1 AND is_active AND erased_at IS NULL`, email,
		).Scan(&userID, &hasPassword)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}

		req := ResetRequest{Email: email}
		answer := "link"
		if hasPassword {
			secret, digest, err := GenerateToken()
			if err != nil {
				return err
			}
			req.Secret, req.ExpiresAt = secret, s.now().Add(ResetLinkTTL)
			if _, err := tx.Exec(ctx, `
				INSERT INTO password_reset (user_id, token_hash, expires_at) VALUES ($1, $2, $3)
				ON CONFLICT (user_id) DO UPDATE
				SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at, created_at = now()`,
				userID, digest, req.ExpiresAt); err != nil {
				return fmt.Errorf("record reset link: %w", err)
			}
		} else {
			// A row without a password that no provider signs in is a portal
			// customer or a follower named in a mail: there is no account to recover.
			rows, err := tx.Query(ctx, `
				SELECT o.slug FROM app_user u, installation o, oidc_provider p
				WHERE u.id = $1 AND u.standing <> 'customer' AND p.enabled AND o.archived_at IS NULL
				ORDER BY o.slug`, userID)
			if err != nil {
				return err
			}
			req.Providers, err = pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				return err
			}
			if len(req.Providers) == 0 {
				return nil
			}
			answer = "provider"
		}

		// Nobody is proven yet, so the entry names the account and no actor.
		if err := AuditAccount(ctx, tx, userID, audit.Entry{
			Action: "auth.password_reset_requested", TargetType: "user", TargetID: &userID, IP: ip,
			Data: map[string]any{"answer": answer},
		}); err != nil {
			return err
		}
		out = &req
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ResetPassword sets the password a mailed link was for and ends every
// session and API token the person holds. It opens no session of its own.
func (s *Service) ResetPassword(ctx context.Context, secret, password, ip string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	hash, err := HashPassword(password, s.params)
	if err != nil {
		return err
	}
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		// FOR UPDATE makes the link single use when it is opened twice at once.
		var userID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT r.user_id FROM password_reset r
			JOIN app_user u ON u.id = r.user_id
			WHERE r.token_hash = $1 AND r.expires_at > $2 AND u.is_active AND u.erased_at IS NULL
			FOR UPDATE OF r`, HashToken(secret), s.now(),
		).Scan(&userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrResetLinkInvalid
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE app_user SET password_hash = $2 WHERE id = $1`, userID, hash); err != nil {
			return fmt.Errorf("reset password: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM password_reset WHERE user_id = $1`, userID); err != nil {
			return fmt.Errorf("use reset link: %w", err)
		}
		if err := endEverySession(ctx, tx, userID, nil); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM api_token WHERE user_id = $1`, userID); err != nil {
			return fmt.Errorf("revoke tokens: %w", err)
		}
		return AuditAccount(ctx, tx, userID, audit.Entry{
			Action: "auth.password_reset", TargetType: "user", TargetID: &userID, Actor: userID, IP: ip,
		})
	})
	return err
}

// AuditAccount records an act on an account once, in the installation's log;
// somebody with no membership left has no log to be in.
func AuditAccount(ctx context.Context, tx db.DBTX, userID uuid.UUID, e audit.Entry) error {
	var member bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM app_user WHERE id = $1 AND standing IS NOT NULL)`, userID).Scan(&member); err != nil {
		return err
	}
	if !member {
		return nil
	}
	return audit.Write(ctx, tx, e)
}
