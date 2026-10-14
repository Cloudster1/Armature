package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/installation"
)

// SignupPolicy says who may sign up. Invitations, single sign-on and
// administrators bring people in whatever it says.
type SignupPolicy string

const (
	// SignupOpen lets anybody who can reach the application sign up, as a
	// plain member of the installation once its first account set it up.
	SignupOpen SignupPolicy = "open"
	// SignupFirst lets the first account set the installation up through its
	// own sign-up page, and nobody sign up after it.
	SignupFirst SignupPolicy = "first"
	// SignupClosed lets nobody sign up, not even the first account.
	SignupClosed SignupPolicy = "closed"
)

// Valid reports whether the policy is one of the three.
func (p SignupPolicy) Valid() bool {
	return p == SignupOpen || p == SignupFirst || p == SignupClosed
}

// DefaultInstallationName is what the first account's installation is called
// when ARMATURE_NAME says nothing.
const DefaultInstallationName = "Armature"

// installationSlug is the address the installation's organization takes when
// its name makes none, such as a name in a script slugs cannot carry.
const installationSlug = "armature"

// ErrSignupClosed is returned when the policy refuses a sign-up.
var ErrSignupClosed = errors.New("sign-up is closed here")

// signupLock serializes sign-ups, so two people racing on a new installation
// cannot both set it up.
const signupLock = "armature.signup"

// Signups sets the policy. The zero value is first, as the product ships.
func (s *Service) Signups(policy SignupPolicy) *Service {
	s.signup = policy
	return s
}

// NamedInstallation sets what the first account's installation is called.
func (s *Service) NamedInstallation(name string) *Service {
	s.installationName = strings.TrimSpace(name)
	return s
}

func (s *Service) signupPolicy() SignupPolicy {
	if s.signup == "" {
		return SignupFirst
	}
	return s.signup
}

func (s *Service) nameOfInstallation() string {
	if s.installationName == "" {
		return DefaultInstallationName
	}
	return s.installationName
}

// SignupState is what the sign-up page needs to know before anybody types.
type SignupState struct {
	// Open says a sign-up would be let through right now.
	Open bool `json:"open"`
	// First says the next account sets the installation up and administers it.
	First bool `json:"first"`
}

// SignupOffer answers what a sign-up would do right now.
func (s *Service) SignupOffer(ctx context.Context) (SignupState, error) {
	var state SignupState
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		org, err := installationOrg(ctx, tx)
		if err != nil {
			return err
		}
		state = s.signupState(org)
		return nil
	})
	return state, err
}

func (s *Service) signupState(org *Org) SignupState {
	switch policy := s.signupPolicy(); {
	case policy == SignupClosed:
		return SignupState{}
	case org == nil:
		return SignupState{Open: true, First: true}
	default:
		return SignupState{Open: policy == SignupOpen}
	}
}

// installationOrg is the organization people sign up into, nil before the first
// account. The upgrade refuses a database with more than one.
func installationOrg(ctx context.Context, tx db.DBTX) (*Org, error) {
	var org Org
	err := tx.QueryRow(ctx, `
		SELECT id, slug, name FROM org WHERE archived_at IS NULL`).Scan(&org.ID, &org.Slug, &org.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("look for the installation: %w", err)
	}
	return &org, nil
}

// SignupInput is the person signing up; the installation is not theirs to name.
type SignupInput struct {
	Email     string
	Password  string
	Name      string
	UserAgent string
	IP        string
}

// Signup makes an account and signs it in. The first one sets the installation
// up and owns it; a later one joins it as a member, where the policy is open.
func (s *Service) Signup(ctx context.Context, in SignupInput) (*Credentials, error) {
	// Refused before the password is hashed, which is the expensive part.
	if s.signupPolicy() == SignupClosed {
		return nil, ErrSignupClosed
	}
	email := NormalizeEmail(in.Email)
	if email == "" {
		return nil, errors.New("email is required")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	if err := ValidatePassword(in.Password); err != nil {
		return nil, err
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
		// Under the lock, so the answer cannot change between the question and the insert.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, signupLock); err != nil {
			return fmt.Errorf("wait for other sign-ups: %w", err)
		}
		org, err := installationOrg(ctx, tx)
		if err != nil {
			return err
		}
		if !s.signupState(org).Open {
			return ErrSignupClosed
		}

		var userID uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO app_user (email, name, password_hash)
			VALUES ($1, $2, $3)
			RETURNING id`,
			email, name, hash,
		).Scan(&userID)
		if isUniqueViolation(err, "app_user_email_key") {
			return ErrEmailTaken
		}
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}

		role := RoleMember
		if org == nil {
			role = RoleOwner
			if org, err = s.setUpInstallation(ctx, tx, userID, in.IP); err != nil {
				return err
			}
		} else if err := joinInstallation(ctx, tx, userID, in.IP); err != nil {
			return err
		}

		sessionID, sessionSecret, err := insertSession(ctx, tx, userID, ProofPassword, expiresAt, in.UserAgent, in.IP)
		if err != nil {
			return err
		}
		secret = sessionSecret
		principal = Principal{
			User:      User{ID: userID, Email: email, Name: name, Timezone: "UTC", Locale: "en", IsActive: true},
			Org:       org,
			Role:      role,
			SessionID: &sessionID,
			Proof:     ProofPassword,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &Credentials{Principal: &principal, SessionSecret: secret, ExpiresAt: expiresAt, LSN: lsn}, nil
}

// setUpInstallation makes the installation's organization, owned by its first
// account, with what a project needs, in the sign-up's own transaction.
func (s *Service) setUpInstallation(ctx context.Context, tx db.DBTX, ownerID uuid.UUID, ip string) (*Org, error) {
	org := Org{Name: s.nameOfInstallation(), Slug: Slugify(s.nameOfInstallation())}
	if !ValidSlug(org.Slug) {
		org.Slug = installationSlug
	}
	if err := tx.QueryRow(ctx, `INSERT INTO org (slug, name) VALUES ($1, $2) RETURNING id`, org.Slug, org.Name).Scan(&org.ID); err != nil {
		return nil, fmt.Errorf("create the installation: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO org_member (user_id, org_role) VALUES ($1, 'owner')`, ownerID); err != nil {
		return nil, fmt.Errorf("create membership: %w", err)
	}
	if err := grantJoiningRole(ctx, tx, ownerID, RoleOwner); err != nil {
		return nil, err
	}
	// Without statuses, issue types and a workflow there could be no project,
	// which is the very next thing anybody makes.
	if _, err := bootstrap.Installation(ctx, tx); err != nil {
		return nil, fmt.Errorf("prepare the installation: %w", err)
	}
	if err := writeAudit(ctx, tx, ownerID, "org.created", "org", &org.ID, ip); err != nil {
		return nil, err
	}
	return &org, nil
}

// joinInstallation makes somebody who signed up a plain member.
func joinInstallation(ctx context.Context, tx db.DBTX, userID uuid.UUID, ip string) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO org_member (user_id, org_role) VALUES ($1, 'member')`, userID); err != nil {
		return fmt.Errorf("create membership: %w", err)
	}
	if err := grantJoiningRole(ctx, tx, userID, RoleMember); err != nil {
		return err
	}
	if err := writeAudit(ctx, tx, userID, "member.joined", "user", &userID, ip); err != nil {
		return err
	}
	return memberJoined(ctx, tx, userID, RoleMember)
}

// memberJoined tells the stream somebody is in. Its orgId is the installation's
// id, deprecated with the webhook envelope's, and a later release drops it.
func memberJoined(ctx context.Context, tx db.DBTX, userID uuid.UUID, role OrgRole) error {
	id, err := installation.ID(ctx, tx)
	if err != nil {
		return err
	}
	return events.Emit(ctx, tx, events.TopicMemberJoined, map[string]any{"orgId": id, "userId": userID, "role": role})
}
