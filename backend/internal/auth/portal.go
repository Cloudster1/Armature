package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/project"
)

// A portal code is a sign-in for somebody with a mail address and no account:
// six digits, mailed, typed once. Whoever proves the address becomes a customer
// of the organization with an ordinary session, so the portal needs no second
// kind of principal.
const (
	portalCodeDigits   = 6
	portalCodeTTL      = 10 * time.Minute
	portalCodeAttempts = 5
	// portalCodeWindowAttempts is how many wrong codes one address may type in
	// portalCodeWindow, however many codes it asks for in between.
	portalCodeWindowAttempts = 10
	portalCodeWindow         = time.Hour
	// DefaultPortalCodeCooldown is how long an address waits between codes.
	DefaultPortalCodeCooldown = time.Minute
)

var (
	// ErrBadAddress is returned for something that is not a mail address.
	ErrBadAddress = errors.New("that is not a mail address")
	// ErrCodeTooSoon is returned when a code was mailed inside the cooldown.
	ErrCodeTooSoon = errors.New("a code was sent less than a minute ago")
	// ErrCodeInvalid covers a wrong, expired, used up or never issued code.
	ErrCodeInvalid = errors.New("that code is wrong or has expired")
	// ErrHasAccount is returned when the address belongs to an agent of the
	// organization, who signs in with their account instead.
	ErrHasAccount = errors.New("you have an account here")
	// ErrDoorAsksForCode is returned when somebody tries to walk into a desk
	// that wants the mailbox proven first.
	ErrDoorAsksForCode = errors.New("this desk asks for a code by mail first")
)

// PortalCodeCooldown sets how long an address waits between codes. Development
// shortens it so a browser test can ask twice; production keeps the minute.
func (s *Service) PortalCodeCooldown(d time.Duration) *Service {
	s.portalCooldown = d
	return s
}

func (s *Service) cooldown() time.Duration {
	if s.portalCooldown == 0 {
		return DefaultPortalCodeCooldown
	}
	return s.portalCooldown
}

// ParseAddress accepts a bare mail address and returns it normalized.
func ParseAddress(raw string) (string, error) {
	email := NormalizeEmail(raw)
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !strings.Contains(email, "@") {
		return "", ErrBadAddress
	}
	return email, nil
}

// codeDigest binds a code to its organization and address, so equal codes in
// different rows never share a digest.
func codeDigest(orgID uuid.UUID, email, code string) []byte {
	return HashToken(orgID.String() + ":" + email + ":" + code)
}

// IssuePortalCode makes a code for an address and returns it once, for the
// caller to mail. Only its digest is kept. The answer is the same whether or
// not the address is known, so asking reveals nothing about who works here.
func (s *Service) IssuePortalCode(ctx context.Context, orgID uuid.UUID, rawEmail string) (code string, expiresAt time.Time, err error) {
	email, err := ParseAddress(rawEmail)
	if err != nil {
		return "", time.Time{}, err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", time.Time{}, err
	}
	code = fmt.Sprintf("%0*d", portalCodeDigits, n.Int64())
	now := s.now()
	expiresAt = now.Add(portalCodeTTL)

	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		// The code is the organization's, so it is refused only when no desk
		// here would take the address; a refused domain burns no code row.
		if err := desksTrust(ctx, tx, orgID, email); err != nil {
			return err
		}
		var requestedAt time.Time
		err := tx.QueryRow(ctx, `SELECT requested_at FROM portal_code WHERE org_id = $1 AND email = $2 FOR UPDATE`, orgID, email).Scan(&requestedAt)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err == nil && now.Sub(requestedAt) < s.cooldown() {
			return ErrCodeTooSoon
		}
		// A fresh code gets fresh attempts, but the hour's count of wrong ones
		// carries over, so asking again does not buy more guesses.
		if _, err := tx.Exec(ctx, `
			INSERT INTO portal_code (org_id, email, code_hash, attempts, requested_at, expires_at, window_attempts, window_started)
			VALUES ($1, $2, $3, 0, $4, $5, 0, $4)
			ON CONFLICT (org_id, email) DO UPDATE
			SET code_hash = EXCLUDED.code_hash, attempts = 0, requested_at = EXCLUDED.requested_at, expires_at = EXCLUDED.expires_at,
			    window_attempts = CASE WHEN portal_code.window_started < $6 THEN 0 ELSE portal_code.window_attempts END,
			    window_started = CASE WHEN portal_code.window_started < $6 THEN EXCLUDED.window_started ELSE portal_code.window_started END`,
			orgID, email, codeDigest(orgID, email, code), now, expiresAt, now.Add(-portalCodeWindow)); err != nil {
			return fmt.Errorf("issue portal code: %w", err)
		}
		// Rows nobody redeemed are cleared as a side effect of the next request
		// anywhere, which is often enough for a table this small.
		_, err = tx.Exec(ctx, `DELETE FROM portal_code WHERE expires_at < now() - interval '1 day'`)
		return err
	})
	if err != nil {
		return "", time.Time{}, err
	}
	return code, expiresAt, nil
}

// desksTrust refuses an address at a domain none of the organization's desks
// takes, naming the domains they do. With no desk, or none that names any
// domain, everyone is taken, as before there were lists.
func desksTrust(ctx context.Context, tx db.DBTX, orgID uuid.UUID, email string) error {
	var (
		anyDesk, trusted bool
		union            []string
	)
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM project WHERE org_id = $1 AND kind = 'service' AND archived_at IS NULL),
		       EXISTS (SELECT 1 FROM project WHERE org_id = $1 AND kind = 'service' AND archived_at IS NULL
		                 AND (cardinality(trusted_domains) = 0 OR $2 = ANY(trusted_domains))),
		       (SELECT coalesce(array_agg(DISTINCT d ORDER BY d), '{}') FROM project p, unnest(p.trusted_domains) AS d
		         WHERE p.org_id = $1 AND p.kind = 'service' AND p.archived_at IS NULL)`,
		orgID, project.DomainOf(email)).Scan(&anyDesk, &trusted, &union)
	if err != nil {
		return err
	}
	if anyDesk && !trusted {
		return &project.NotTrustedError{Domains: union}
	}
	return nil
}

// PortalSessionInput is a code presented for an address.
type PortalSessionInput struct {
	OrgID     uuid.UUID
	Email     string
	Code      string
	UserAgent string
	IP        string
}

// RedeemPortalCode turns a correct code into a customer session, making the
// account and the membership when the address has none. An agent's address is
// refused here and not when the code is asked for, so the mailbox is proven
// before anything is said about it.
func (s *Service) RedeemPortalCode(ctx context.Context, in PortalSessionInput) (*Credentials, error) {
	email, err := ParseAddress(in.Email)
	if err != nil {
		return nil, err
	}
	code := strings.TrimSpace(in.Code)
	var (
		principal Principal
		secret    string
		expiresAt = s.now().Add(s.sessionTTL)
		// missed is a wrong code whose attempt was counted; the transaction
		// commits the count where returning an error would roll it back.
		missed bool
	)
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			digest         []byte
			attempts       int
			codeEnds       time.Time
			windowAttempts int
			windowStarted  time.Time
		)
		err := tx.QueryRow(ctx, `
			SELECT code_hash, attempts, expires_at, window_attempts, window_started FROM portal_code
			WHERE org_id = $1 AND email = $2 FOR UPDATE`, in.OrgID, email).Scan(&digest, &attempts, &codeEnds, &windowAttempts, &windowStarted)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCodeInvalid
		}
		if err != nil {
			return err
		}
		// A used up code stays as a row until it ages out, so the hour's count
		// of wrong guesses is not forgotten with it.
		windowFull := windowAttempts >= portalCodeWindowAttempts && windowStarted.After(s.now().Add(-portalCodeWindow))
		if !codeEnds.After(s.now()) || attempts >= portalCodeAttempts || windowFull {
			return ErrCodeInvalid
		}
		if !EqualDigest(digest, codeDigest(in.OrgID, email, code)) {
			missed = true
			_, err := tx.Exec(ctx, `
				UPDATE portal_code SET attempts = attempts + 1, window_attempts = window_attempts + 1
				WHERE org_id = $1 AND email = $2`, in.OrgID, email)
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM portal_code WHERE org_id = $1 AND email = $2`, in.OrgID, email); err != nil {
			return err
		}

		var org Org
		if err := tx.QueryRow(ctx, `SELECT id, slug, name FROM org WHERE id = $1 AND archived_at IS NULL`, in.OrgID).Scan(&org.ID, &org.Slug, &org.Name); err != nil {
			return err
		}

		// An address that already has an account joins with it, as an accepted
		// invitation does: the code has proven the mailbox.
		user, err := upsertUserByAddress(ctx, tx, email)
		if err != nil {
			return err
		}
		if !user.IsActive {
			return ErrUserInactive
		}

		var role OrgRole
		err = tx.QueryRow(ctx, `SELECT org_role FROM org_member WHERE org_id = $1 AND user_id = $2`, org.ID, user.ID).Scan(&role)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if _, err := tx.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'customer')`, org.ID, user.ID); err != nil {
				return fmt.Errorf("make the requester a customer: %w", err)
			}
			role = RoleCustomer
		case err != nil:
			return err
		case role.IsAgent():
			return ErrHasAccount
		}

		sessionID, sessionSecret, err := insertSession(ctx, tx, user.ID, &org.ID, ProofPortalCode, expiresAt, in.UserAgent, in.IP)
		if err != nil {
			return err
		}
		secret = sessionSecret
		principal = Principal{User: user, Org: &org, Role: role, SessionID: &sessionID, Proof: ProofPortalCode}
		return writeAudit(ctx, tx, org.ID, user.ID, "auth.portal_code_login", "user", &user.ID, in.IP)
	})
	if err != nil {
		return nil, err
	}
	if missed {
		return nil, ErrCodeInvalid
	}
	return &Credentials{Principal: &principal, SessionSecret: secret, ExpiresAt: expiresAt, LSN: lsn}, nil
}

// localPart is the name a requester starts with: what stands before the @.
func localPart(email string) string {
	name, _, _ := strings.Cut(email, "@")
	if name == "" {
		return email
	}
	return name
}

// doorAccount is the account an open door may let in: a new one, or one that was
// never more than a customer. Anybody with more to lose is asked for a code.
func doorAccount(ctx context.Context, tx db.DBTX, email string) (User, error) {
	user, err := upsertUserByAddress(ctx, tx, email)
	if err != nil {
		return User{}, err
	}
	var guarded bool
	err = tx.QueryRow(ctx, `
		SELECT u.password_hash IS NOT NULL
		    OR EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = u.id AND m.org_role <> 'customer')
		FROM app_user u WHERE u.id = $1`, user.ID).Scan(&guarded)
	if err != nil {
		return User{}, err
	}
	if guarded {
		return User{}, ErrDoorAsksForCode
	}
	return user, nil
}

// OpenSessionInput is who walks into a desk whose door is open.
type OpenSessionInput struct {
	OrgID      uuid.UUID
	ProjectKey string
	Email      string
	Name       string
	UserAgent  string
	IP         string
}

// OpenPortalSession lets an unproven address into one desk. The address is
// taken on trust, so the session is the desk's and sees nothing else.
func (s *Service) OpenPortalSession(ctx context.Context, in OpenSessionInput) (*Credentials, error) {
	email, err := ParseAddress(in.Email)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	var (
		principal Principal
		secret    string
		expiresAt = s.now().Add(s.sessionTTL)
	)
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var org Org
		if err := tx.QueryRow(ctx, `SELECT id, slug, name FROM org WHERE id = $1 AND archived_at IS NULL`, in.OrgID).Scan(&org.ID, &org.Slug, &org.Name); err != nil {
			return err
		}
		var (
			deskID   uuid.UUID
			deskKey  string
			verifies bool
			domains  []string
		)
		err := tx.QueryRow(ctx, `
			SELECT id, key, portal_verifies, trusted_domains FROM project
			WHERE org_id = $1 AND key = $2 AND kind = 'service' AND archived_at IS NULL`,
			org.ID, strings.ToUpper(strings.TrimSpace(in.ProjectKey))).Scan(&deskID, &deskKey, &verifies, &domains)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && verifies) {
			return ErrDoorAsksForCode
		}
		if err != nil {
			return err
		}
		if !project.Trusts(domains, email) {
			return &project.NotTrustedError{Domains: domains}
		}

		user, err := doorAccount(ctx, tx, email)
		if err != nil {
			return err
		}
		if !user.IsActive {
			return ErrUserInactive
		}
		// A name given at the door is kept while the row has nothing better:
		// no password and the name the address alone gave it.
		if name != "" && user.Name == localPart(email) {
			if _, err := tx.Exec(ctx, `UPDATE app_user SET name = $2 WHERE id = $1 AND password_hash IS NULL`, user.ID, name); err != nil {
				return err
			}
			user.Name = name
		}

		var role OrgRole
		err = tx.QueryRow(ctx, `SELECT org_role FROM org_member WHERE org_id = $1 AND user_id = $2`, org.ID, user.ID).Scan(&role)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			if _, err := tx.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'customer')`, org.ID, user.ID); err != nil {
				return fmt.Errorf("make the requester a customer: %w", err)
			}
			role = RoleCustomer
		case err != nil:
			return err
		case role.IsAgent():
			return ErrDoorAsksForCode
		}

		sessionID, sessionSecret, err := insertSessionFor(ctx, tx, user.ID, &org.ID, &deskID, ProofOpenDoor, expiresAt, in.UserAgent, in.IP)
		if err != nil {
			return err
		}
		secret = sessionSecret
		principal = Principal{User: user, Org: &org, Role: role, SessionID: &sessionID, PortalDesk: deskKey, PortalDeskID: &deskID, Proof: ProofOpenDoor}
		return writeAudit(ctx, tx, org.ID, user.ID, "auth.portal_open_login", "project", &deskID, in.IP)
	})
	if err != nil {
		return nil, err
	}
	return &Credentials{Principal: &principal, SessionSecret: secret, ExpiresAt: expiresAt, LSN: lsn}, nil
}
