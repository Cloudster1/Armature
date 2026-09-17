package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/armature/armature/backend/internal/db"
)

// SignupPolicy says who may create a new organization by signing up. Joining
// an existing one is always by invitation and is not affected.
type SignupPolicy string

const (
	// SignupOpen lets anybody who can reach the application create one.
	SignupOpen SignupPolicy = "open"
	// SignupFirst lets the first organization be created and nothing after it,
	// so a new installation can be set up through its own sign-up page.
	SignupFirst SignupPolicy = "first"
	// SignupClosed creates organizations by no page at all.
	SignupClosed SignupPolicy = "closed"
)

// Valid reports whether the policy is one of the three.
func (p SignupPolicy) Valid() bool {
	return p == SignupOpen || p == SignupFirst || p == SignupClosed
}

// ErrSignupClosed is returned when the policy refuses a new organization.
var ErrSignupClosed = errors.New("new organizations cannot be created here")

// signupLock serializes signups under the first-only policy, so two people
// racing on a new installation cannot both become the first.
const signupLock = "armature.signup"

// Signups sets the policy. The zero value is open, which is what development
// and the test suite run with.
func (s *Service) Signups(policy SignupPolicy) *Service {
	s.signup = policy
	return s
}

func (s *Service) signupPolicy() SignupPolicy {
	if s.signup == "" {
		return SignupOpen
	}
	return s.signup
}

// SignupAllowed answers whether a signup would be let through right now.
func (s *Service) SignupAllowed(ctx context.Context) (bool, error) {
	var open bool
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		open, err = SignupAllowedIn(ctx, tx, s.signupPolicy())
		return err
	})
	return open, err
}

// SignupAllowedIn is the policy's answer inside a transaction the caller holds.
// Signup asks it in the transaction that creates the organization, after taking
// the lock, so the answer cannot change between the question and the insert.
func SignupAllowedIn(ctx context.Context, tx db.DBTX, policy SignupPolicy) (bool, error) {
	switch policy {
	case SignupClosed:
		return false, nil
	case SignupFirst:
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM org)`).Scan(&exists); err != nil {
			return false, fmt.Errorf("look for an existing organization: %w", err)
		}
		return !exists, nil
	default:
		return true, nil
	}
}
