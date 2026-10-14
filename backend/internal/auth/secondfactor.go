package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/installation"
	"github.com/armature/armature/backend/internal/seal"
)

// SecondFactorRule is who the installation requires a second factor of.
type SecondFactorRule string

const (
	SecondFactorOff      SecondFactorRule = "off"
	SecondFactorAdmins   SecondFactorRule = "admins"
	SecondFactorEveryone SecondFactorRule = "everyone"
)

// Valid reports whether the rule is one the database takes.
func (r SecondFactorRule) Valid() bool {
	return r == SecondFactorOff || r == SecondFactorAdmins || r == SecondFactorEveryone
}

// Covers says whether the rule asks it of somebody at this standing. A
// customer comes in by the portal's own doors and is never asked.
func (r SecondFactorRule) Covers(role OrgRole) bool {
	switch r {
	case SecondFactorAdmins:
		return role.CanAdminister()
	case SecondFactorEveryone:
		return role.IsAgent()
	}
	return false
}

const (
	// ChallengeTTL is how long the code step of a sign-in waits: time to find
	// the phone, not long enough to come back to later.
	ChallengeTTL = 5 * time.Minute
	// ChallengeAttempts is how many wrong codes one sign-in takes; the
	// database's check on login_challenge holds the same number.
	ChallengeAttempts = 5
)

var (
	// ErrSecondFactorUnavailable is an enrolment on a server with no key to seal the secret with.
	ErrSecondFactorUnavailable = errors.New("two-step sign-in is not available on this server, because ARMATURE_SECRET_KEY is not set. Ask whoever runs it to set one")
	// ErrSecondFactorEnrolled is starting over while an app is already in use.
	ErrSecondFactorEnrolled = errors.New("two-step sign-in is already on. Remove it first to move to another app")
	// ErrNoEnrolment is a confirmation with nothing started.
	ErrNoEnrolment = errors.New("there is no two-step sign-in being set up. Start again")
	// ErrNoSecondFactor is acting on a factor the account does not have.
	ErrNoSecondFactor = errors.New("two-step sign-in is not on for that account")
	// ErrSecondFactorCode is a code that is neither the app's current one nor an unused recovery code.
	ErrSecondFactorCode = errors.New("that code is not right. Type the code your app shows now, or one of your recovery codes")
	// ErrChallengeInvalid covers a code step that is unknown, expired or used up alike.
	ErrChallengeInvalid = errors.New("this sign-in has expired or had too many wrong codes. Sign in again with your password")
	// ErrEnrolFirst is an administrator requiring what they do not have, which would hold their own session.
	ErrEnrolFirst = errors.New("set up two-step sign-in for yourself first, on your profile, so the requirement does not hold you up too")
	// ErrBadSecondFactorRule is a requirement the installation cannot have.
	ErrBadSecondFactorRule = errors.New("require off, admins or everyone")
)

// ChallengeIssued is Login's answer for somebody with a second factor: no
// session yet, a challenge to answer with a code. As an error, it fails closed.
type ChallengeIssued struct {
	Secret    string
	ExpiresAt time.Time
}

func (c *ChallengeIssued) Error() string { return "a second factor is needed to finish signing in" }

// CodeRefused is a wrong code at sign-in. It names the account, so the caller
// can brake guessing at it the way it brakes guessing at the password.
type CodeRefused struct{ Email string }

func (e *CodeRefused) Error() string { return ErrSecondFactorCode.Error() }
func (e *CodeRefused) Unwrap() error { return ErrSecondFactorCode }

// SecondFactor hands the service the key authenticator secrets are sealed
// with. Without one, enrolment is refused.
func (s *Service) SecondFactor(sealer *seal.Sealer) *Service {
	s.sealer = sealer
	return s
}

// CheckSecondFactorKey refuses to start without the key that opens the
// secrets already held, since nobody who has one could sign in.
func (s *Service) CheckSecondFactorKey(ctx context.Context) error {
	var sealed [][]byte
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT secret_sealed FROM user_second_factor WHERE confirmed_at IS NOT NULL`)
		if err != nil {
			return err
		}
		sealed, err = pgx.CollectRows(rows, pgx.RowTo[[]byte])
		return err
	})
	if err != nil {
		return fmt.Errorf("read the sealed authenticator secrets: %w", err)
	}
	if len(sealed) == 0 {
		return nil
	}
	if s.sealer == nil {
		return errors.New("people have two-step sign-in on, and ARMATURE_SECRET_KEY is not set: set the key their authenticator secrets were sealed with")
	}
	for _, value := range sealed {
		id, err := seal.KeyIDOf(value)
		if err != nil || !bytes.Equal(id, s.sealer.KeyID()) {
			return errors.New("ARMATURE_SECRET_KEY is not the key the authenticator secrets were sealed with: set the key they were sealed with")
		}
	}
	return nil
}

// SecondFactorStatus is what a person sees of their own second factor.
type SecondFactorStatus struct {
	Enrolled          bool       `json:"enrolled"`
	EnrolledAt        *time.Time `json:"enrolledAt,omitempty"`
	RecoveryCodesLeft int        `json:"recoveryCodesLeft"`
	// Available says whether the server can set one up at all.
	Available bool `json:"available"`
}

// SecondFactorStatusOf says whether the person has a second factor and how
// many recovery codes they have left. Never the secret.
func (s *Service) SecondFactorStatusOf(ctx context.Context, userID uuid.UUID) (*SecondFactorStatus, error) {
	out := SecondFactorStatus{Available: s.sealer != nil}
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			SELECT confirmed_at,
			       (SELECT count(*) FROM recovery_code WHERE user_id = $1 AND used_at IS NULL)
			FROM user_second_factor WHERE user_id = $1 AND confirmed_at IS NOT NULL`, userID,
		).Scan(&out.EnrolledAt, &out.RecoveryCodesLeft)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		out.Enrolled = err == nil
		return err
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Enrolment is what an authenticator app is given: the secret to type, and
// the same as an otpauth address for the QR code. Shown once, before confirming.
type Enrolment struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

// StartSecondFactor makes a new authenticator secret for the person. It is not
// in use until a code from the app confirms it.
func (s *Service) StartSecondFactor(ctx context.Context, userID uuid.UUID, email string) (*Enrolment, error) {
	if s.sealer == nil {
		return nil, ErrSecondFactorUnavailable
	}
	secret, err := newTOTPSecret()
	if err != nil {
		return nil, err
	}
	sealed, err := s.sealer.Seal(secret, userID[:])
	if err != nil {
		return nil, err
	}
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO user_second_factor (user_id, secret_sealed) VALUES ($1, $2)
			ON CONFLICT (user_id) DO UPDATE SET secret_sealed = EXCLUDED.secret_sealed, created_at = now()
			WHERE user_second_factor.confirmed_at IS NULL`, userID, sealed)
		if err != nil {
			return fmt.Errorf("start two-step sign-in: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrSecondFactorEnrolled
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &Enrolment{Secret: base32NoPad.EncodeToString(secret), URI: otpauthURI(secret, email)}, nil
}

// ConfirmSecondFactor puts the started secret in use once the app shows a code
// for it, and returns the recovery codes, which exist in clear only here.
func (s *Service) ConfirmSecondFactor(ctx context.Context, userID uuid.UUID, code, ip string) ([]string, db.LSN, error) {
	var codes []string
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var sealed []byte
		err := tx.QueryRow(ctx, `
			SELECT secret_sealed FROM user_second_factor
			WHERE user_id = $1 AND confirmed_at IS NULL FOR UPDATE`, userID).Scan(&sealed)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoEnrolment
		}
		if err != nil {
			return err
		}
		secret, err := s.openSecret(sealed, userID)
		if errors.Is(err, seal.ErrWrongKey) || errors.Is(err, seal.ErrMalformed) {
			// Started under a key the server no longer has: only starting again helps.
			return ErrNoEnrolment
		}
		if err != nil {
			return err
		}
		step, ok := matchTOTP(secret, code, s.now(), 0)
		if !ok {
			return ErrSecondFactorCode
		}
		if _, err := tx.Exec(ctx, `UPDATE user_second_factor SET confirmed_at = now(), last_step = $2 WHERE user_id = $1`, userID, step); err != nil {
			return fmt.Errorf("confirm two-step sign-in: %w", err)
		}
		if codes, err = replaceRecoveryCodes(ctx, tx, userID); err != nil {
			return err
		}
		return AuditAccount(ctx, tx, userID, audit.Entry{
			Action: "auth.second_factor_enrolled", TargetType: "user", TargetID: &userID, Actor: userID, IP: ip,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return codes, lsn, nil
}

// RenewRecoveryCodes replaces every recovery code with a new set, proven by a
// code, for somebody who used some or lost the list.
func (s *Service) RenewRecoveryCodes(ctx context.Context, userID uuid.UUID, code, ip string) ([]string, db.LSN, error) {
	var codes []string
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := s.spendCode(ctx, tx, userID, code); err != nil {
			return err
		}
		var err error
		if codes, err = replaceRecoveryCodes(ctx, tx, userID); err != nil {
			return err
		}
		return AuditAccount(ctx, tx, userID, audit.Entry{
			Action: "auth.recovery_codes_renewed", TargetType: "user", TargetID: &userID, Actor: userID, IP: ip,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return codes, lsn, nil
}

// RemoveSecondFactor takes the person's own second factor away, proven by a
// code, and ends every other session of theirs as a password change does.
func (s *Service) RemoveSecondFactor(ctx context.Context, userID uuid.UUID, keep *uuid.UUID, code, ip string) (db.LSN, error) {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := s.spendCode(ctx, tx, userID, code); err != nil {
			return err
		}
		if err := dropSecondFactor(ctx, tx, userID); err != nil {
			return err
		}
		if err := endEverySession(ctx, tx, userID, keep); err != nil {
			return err
		}
		return AuditAccount(ctx, tx, userID, audit.Entry{
			Action: "auth.second_factor_removed", TargetType: "user", TargetID: &userID, Actor: userID, IP: ip,
		})
	})
}

// RemoveMembersSecondFactor is an administrator's way back in for somebody who
// lost both the phone and the codes, within the reach SetManagedPassword has.
func (s *Service) RemoveMembersSecondFactor(ctx context.Context, userID uuid.UUID, actor uuid.UUID, actorRole OrgRole, ip string) (db.LSN, error) {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := guardManaged(ctx, tx, userID, actor, actorRole); err != nil {
			return err
		}
		var has bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_second_factor WHERE user_id = $1 AND confirmed_at IS NOT NULL)`, userID).Scan(&has); err != nil {
			return err
		}
		if !has {
			return ErrNoSecondFactor
		}
		if err := dropSecondFactor(ctx, tx, userID); err != nil {
			return err
		}
		return writeAudit(ctx, tx, actor, "member.second_factor_removed", "user", &userID, ip)
	})
}

// SecondFactorRequirement reads who the installation requires a second factor of.
func (s *Service) SecondFactorRequirement(ctx context.Context) (SecondFactorRule, error) {
	var rule string
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		rule, err = installation.SecondFactor(ctx, tx)
		return err
	})
	return SecondFactorRule(rule), err
}

// RequireSecondFactor sets who the installation requires a second factor of.
// It refuses a rule that would hold the administrator setting it.
func (s *Service) RequireSecondFactor(ctx context.Context, p *Principal, rule SecondFactorRule, ip string) (db.LSN, error) {
	if !rule.Valid() {
		return 0, ErrBadSecondFactorRule
	}
	if rule.Covers(p.Role) && !p.SecondFactor {
		return 0, ErrEnrolFirst
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := installation.SetSecondFactor(ctx, tx, string(rule)); err != nil {
			return fmt.Errorf("require two-step sign-in: %w", err)
		}
		return audit.Write(ctx, tx, audit.Entry{
			Action: "org.second_factor_required", TargetType: "org", TargetID: &p.Installation.ID, Actor: p.User.ID, IP: ip,
			Data: map[string]any{"require": string(rule)},
		})
	})
}

// issueChallenge records the code step of a password sign-in. A newer sign-in
// replaces the one before, so there is one live challenge per person.
func (s *Service) issueChallenge(ctx context.Context, userID uuid.UUID) (*ChallengeIssued, error) {
	secret, digest, err := GenerateToken()
	if err != nil {
		return nil, err
	}
	issued := &ChallengeIssued{Secret: secret, ExpiresAt: s.now().Add(ChallengeTTL)}
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO login_challenge (user_id, token_hash, expires_at) VALUES ($1, $2, $3)
			ON CONFLICT (user_id) DO UPDATE
			SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at, attempts = 0, created_at = now()`,
			userID, digest, issued.ExpiresAt)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("record the sign-in's code step: %w", err)
	}
	return issued, nil
}

// CompleteLogin finishes a password sign-in with the code its challenge asks
// for, and opens the session only then. A wrong code is counted and kept.
func (s *Service) CompleteLogin(ctx context.Context, challenge, code, userAgent, ip string) (*Credentials, error) {
	var (
		creds   *Credentials
		refused error
	)
	expiresAt := s.now().Add(s.sessionTTL)
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			userID   uuid.UUID
			attempts int
			email    string
		)
		err := tx.QueryRow(ctx, `
			SELECT c.user_id, c.attempts, u.email FROM login_challenge c
			JOIN app_user u ON u.id = c.user_id
			WHERE c.token_hash = $1 AND c.expires_at > $2 AND u.is_active AND u.erased_at IS NULL
			FOR UPDATE OF c`, HashToken(challenge), s.now(),
		).Scan(&userID, &attempts, &email)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrChallengeInvalid
		}
		if err != nil {
			return err
		}

		how, err := s.spendCode(ctx, tx, userID, code)
		switch {
		case errors.Is(err, ErrSecondFactorCode):
			// Returning nil commits the count; the refusal is returned after.
			refused = &CodeRefused{Email: email}
			if attempts+1 >= ChallengeAttempts {
				_, err = tx.Exec(ctx, `DELETE FROM login_challenge WHERE user_id = $1`, userID)
			} else {
				_, err = tx.Exec(ctx, `UPDATE login_challenge SET attempts = attempts + 1 WHERE user_id = $1`, userID)
			}
			return err
		case errors.Is(err, ErrNoSecondFactor):
			// Removed while the code step waited: the password alone does not finish it.
			refused = ErrChallengeInvalid
			_, err = tx.Exec(ctx, `DELETE FROM login_challenge WHERE user_id = $1`, userID)
			return err
		case err != nil:
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM login_challenge WHERE user_id = $1`, userID); err != nil {
			return fmt.Errorf("use the sign-in's code step: %w", err)
		}
		if how == codeRecovery {
			if err := AuditAccount(ctx, tx, userID, audit.Entry{
				Action: "auth.recovery_code_used", TargetType: "user", TargetID: &userID, Actor: userID, IP: ip,
			}); err != nil {
				return err
			}
		}
		principal, err := passwordPrincipal(ctx, tx, userID)
		if err != nil {
			return err
		}
		principal.SecondFactor = true
		secret, err := openPasswordSession(ctx, tx, principal, expiresAt, userAgent, ip)
		if err != nil {
			return err
		}
		creds = &Credentials{Principal: principal, SessionSecret: secret, ExpiresAt: expiresAt}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if refused != nil {
		return nil, refused
	}
	creds.LSN = lsn
	return creds, nil
}

// How a code was proven, for the log.
const (
	codeAuthenticator = "authenticator"
	codeRecovery      = "recovery_code"
)

// spendCode accepts the app's current code or an unused recovery code, and
// uses it up, so neither works a second time.
func (s *Service) spendCode(ctx context.Context, tx db.DBTX, userID uuid.UUID, code string) (string, error) {
	if isTOTPCode(code) {
		var (
			sealed []byte
			last   int64
		)
		err := tx.QueryRow(ctx, `
			SELECT secret_sealed, last_step FROM user_second_factor
			WHERE user_id = $1 AND confirmed_at IS NOT NULL FOR UPDATE`, userID).Scan(&sealed, &last)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNoSecondFactor
		}
		if err != nil {
			return "", err
		}
		secret, err := s.openSecret(sealed, userID)
		if err != nil {
			return "", err
		}
		step, ok := matchTOTP(secret, code, s.now(), last)
		if !ok {
			return "", ErrSecondFactorCode
		}
		if _, err := tx.Exec(ctx, `UPDATE user_second_factor SET last_step = $2 WHERE user_id = $1`, userID, step); err != nil {
			return "", fmt.Errorf("use the code: %w", err)
		}
		return codeAuthenticator, nil
	}

	var has bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_second_factor WHERE user_id = $1 AND confirmed_at IS NOT NULL)`, userID).Scan(&has); err != nil {
		return "", err
	}
	if !has {
		return "", ErrNoSecondFactor
	}
	// FOR UPDATE makes a recovery code single use when it is typed twice at once.
	digest := recoveryDigest(userID, code)
	var unused bool
	err := tx.QueryRow(ctx, `
		SELECT true FROM recovery_code
		WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL FOR UPDATE`, userID, digest).Scan(&unused)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrSecondFactorCode
	}
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `UPDATE recovery_code SET used_at = now() WHERE user_id = $1 AND code_hash = $2`, userID, digest); err != nil {
		return "", fmt.Errorf("use the recovery code: %w", err)
	}
	return codeRecovery, nil
}

// openSecret unseals a person's authenticator secret, bound to their id.
func (s *Service) openSecret(sealed []byte, userID uuid.UUID) ([]byte, error) {
	if s.sealer == nil {
		return nil, ErrSecondFactorUnavailable
	}
	return s.sealer.Open(sealed, userID[:])
}

// replaceRecoveryCodes throws away a person's recovery codes and makes a new
// set. Only the digests are kept; the codes are returned to be shown once.
func replaceRecoveryCodes(ctx context.Context, tx db.DBTX, userID uuid.UUID) ([]string, error) {
	if _, err := tx.Exec(ctx, `DELETE FROM recovery_code WHERE user_id = $1`, userID); err != nil {
		return nil, fmt.Errorf("throw away the old recovery codes: %w", err)
	}
	codes := make([]string, 0, RecoveryCodeCount)
	for range RecoveryCodeCount {
		code, err := newRecoveryCode()
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO recovery_code (user_id, code_hash) VALUES ($1, $2)`, userID, recoveryDigest(userID, code)); err != nil {
			return nil, fmt.Errorf("keep a recovery code: %w", err)
		}
		codes = append(codes, code)
	}
	return codes, nil
}

// dropSecondFactor removes the secret, the recovery codes and any sign-in
// waiting on a code, so nothing of the old factor is left to use.
func dropSecondFactor(ctx context.Context, tx db.DBTX, userID uuid.UUID) error {
	for _, q := range []string{
		`DELETE FROM user_second_factor WHERE user_id = $1`,
		`DELETE FROM recovery_code WHERE user_id = $1`,
		`DELETE FROM login_challenge WHERE user_id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return fmt.Errorf("remove two-step sign-in: %w", err)
		}
	}
	return nil
}
