package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
)

// BootstrapInput is what a chart's bootstrap Job sets up: the installation's
// first administrator and the people let in before their first sign-in.
type BootstrapInput struct {
	AdminEmail    string
	AdminName     string
	AdminPassword string
	Members       []BootstrapMember
}

// BootstrapMember is one person let in ahead of their first sign-in, as admin or member.
type BootstrapMember struct {
	Email string
	Role  OrgRole
}

// BootstrapResult says what a run changed, so the Job's log tells a first
// install from a rerun that found everything in place.
type BootstrapResult struct {
	OwnerCreated bool
	AdminCreated bool
	MembersAdded []string
}

// Bootstrap sets an installation up without anybody signing up: on an empty
// one the administrator becomes its owner, whatever the sign-up policy, so a
// chart can keep sign-up closed from the first moment. On a set-up one it
// adds the administrator when missing and never touches an existing account,
// so a password changed since is kept. Members get an account nobody knows
// the password of, which their first single sign-on claims by email.
func (s *Service) Bootstrap(ctx context.Context, in BootstrapInput) (*BootstrapResult, error) {
	for _, m := range in.Members {
		if m.Role != RoleAdmin && m.Role != RoleMember {
			return nil, fmt.Errorf("%s is to join as %q; a bootstrap lets people in as admin or member", m.Email, m.Role)
		}
	}
	name := in.AdminName
	if name == "" {
		name, _, _ = strings.Cut(in.AdminEmail, "@")
	}
	res := &BootstrapResult{}

	first := *s
	first.signup = SignupFirst
	_, err := first.Signup(ctx, SignupInput{Email: in.AdminEmail, Password: in.AdminPassword, Name: name, UserAgent: "bootstrap"})
	switch {
	case err == nil:
		res.OwnerCreated = true
	case errors.Is(err, ErrSignupClosed), errors.Is(err, ErrEmailTaken):
	default:
		return nil, fmt.Errorf("set the installation up as %s: %w", in.AdminEmail, err)
	}

	owner, err := s.firstOwner(ctx)
	if err != nil {
		return nil, err
	}
	if !res.OwnerCreated {
		_, _, err := s.CreateLocalUser(ctx, CreateLocalUserInput{Email: in.AdminEmail, Name: name, Role: RoleAdmin, Password: in.AdminPassword}, owner, "")
		switch {
		case err == nil:
			res.AdminCreated = true
		case errors.Is(err, ErrAddressInUse):
		default:
			return nil, fmt.Errorf("add the administrator %s: %w", in.AdminEmail, err)
		}
	}

	for _, m := range in.Members {
		memberName, _, _ := strings.Cut(m.Email, "@")
		_, _, err := s.CreateLocalUser(ctx, CreateLocalUserInput{Email: m.Email, Name: memberName, Role: m.Role, Password: unknownPassword()}, owner, "")
		switch {
		case err == nil:
			res.MembersAdded = append(res.MembersAdded, m.Email)
		case errors.Is(err, ErrAddressInUse):
		default:
			return nil, fmt.Errorf("let %s in as %s: %w", m.Email, m.Role, err)
		}
	}
	return res, nil
}

// firstOwner is the account the bootstrap's changes are recorded as made by.
func (s *Service) firstOwner(ctx context.Context) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT id FROM app_user
			WHERE standing = 'owner' AND erased_at IS NULL
			ORDER BY created_at, id LIMIT 1`).Scan(&id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, errors.New("the installation has no owner, so nobody can let the administrator and members in; restore its owner first")
	}
	return id, err
}

// unknownPassword is told to nobody: a member signs in through single sign-on,
// and an account needs some password to be one.
func unknownPassword() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
