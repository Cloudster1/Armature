package oidc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
)

// SignIn turns a verified identity into a session.
//
// Authenticating is not the same as being let in: somebody the provider vouches
// for who has not been invited is refused. An organization that let anybody with
// a company account in would have no membership at all, only a login page.
func (s *Service) SignIn(ctx context.Context, orgID uuid.UUID, identity *Identity, ttl time.Duration, userAgent, ip string) (*Session, error) {
	var out Session
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		userID, err := upsertUser(ctx, tx, identity)
		if err != nil {
			return err
		}
		// The provider vouches for the person; whether the account is
		// switched on is still ours to say, as it is at every other door.
		var active bool
		if err := tx.QueryRow(ctx, `SELECT is_active FROM app_user WHERE id = $1`, userID).Scan(&active); err != nil {
			return err
		}
		if !active {
			return auth.ErrUserInactive
		}

		var role string
		err = tx.QueryRow(ctx, `
			SELECT org_role FROM org_member WHERE org_id = $1 AND user_id = $2`,
			orgID, userID).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotAMember
		}
		if err != nil {
			return err
		}

		provider, err := providerIn(ctx, tx, orgID)
		if err != nil {
			return err
		}
		if err := syncGroups(ctx, tx, orgID, userID, identity.Groups, provider.CreateGroups); err != nil {
			return err
		}

		secret, digest, err := auth.GenerateToken()
		if err != nil {
			return err
		}
		var sessionID uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO user_session (user_id, token_hash, current_org_id, user_agent, ip, expires_at, proof)
			VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, '')::inet, $6, 'oidc')
			RETURNING id`,
			userID, digest, orgID, userAgent, ip, time.Now().Add(ttl),
		).Scan(&sessionID)
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}

		out = Session{UserID: userID, OrgID: orgID, Secret: secret, ExpiresAt: time.Now().Add(ttl)}

		return audit.Write(ctx, tx, orgID, audit.Entry{Action: "auth.oidc_login", TargetType: "user", TargetID: &userID, Actor: userID, IP: ip})
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Session is what a completed sign-in hands back to the HTTP layer.
type Session struct {
	UserID    uuid.UUID
	OrgID     uuid.UUID
	Secret    string
	ExpiresAt time.Time
}

// upsertUser finds the account this identity belongs to, or makes one.
//
// The match is on email rather than on the provider's subject, because the
// invitation that let this person in was addressed to an email. A person whose
// address changes at the provider arrives as somebody new, which is the honest
// outcome: there is nothing else tying the two together.
func upsertUser(ctx context.Context, tx db.DBTX, identity *Identity) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO app_user (email, name, password_hash)
		VALUES ($1, $2, NULL)
		ON CONFLICT (email) DO UPDATE SET name = CASE
		    WHEN btrim(app_user.name) = '' THEN EXCLUDED.name ELSE app_user.name END
		RETURNING id`,
		strings.ToLower(identity.Email), identity.Name,
	).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("find or create the account: %w", err)
	}
	return id, nil
}

// syncGroups makes somebody's membership of provider-owned groups match exactly
// what the token said.
//
// Exactly, not additively: a group somebody has been removed from at the
// provider has to stop granting them anything here on their next sign-in, or
// revoking access there would not revoke it here. Groups maintained by hand are
// left alone, because the provider has no opinion about them.
func syncGroups(ctx context.Context, tx db.DBTX, orgID, userID uuid.UUID, claimed []string, create bool) error {
	if create {
		for _, ref := range claimed {
			if _, err := tx.Exec(ctx, `
				INSERT INTO user_group (org_id, name, source, external_ref)
				VALUES ($1, $2, 'oidc', $2)
				ON CONFLICT (org_id, external_ref) WHERE external_ref IS NOT NULL DO NOTHING`,
				orgID, ref); err != nil {
				return fmt.Errorf("create the group %q the provider named: %w", ref, err)
			}
		}
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM group_member m
		USING user_group g
		WHERE m.group_id = g.id AND m.user_id = $1 AND g.org_id = $2
		  AND g.source = 'oidc'
		  AND COALESCE(g.external_ref, '') <> ALL($3)`,
		userID, orgID, claimed); err != nil {
		return fmt.Errorf("leave the groups the provider no longer names: %w", err)
	}

	if len(claimed) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO group_member (org_id, group_id, user_id)
		SELECT $2, g.id, $1 FROM user_group g
		WHERE g.org_id = $2 AND g.source = 'oidc' AND g.external_ref = ANY($3)
		ON CONFLICT DO NOTHING`,
		userID, orgID, claimed); err != nil {
		return fmt.Errorf("join the groups the provider names: %w", err)
	}
	return nil
}

func providerIn(ctx context.Context, tx db.DBTX, orgID uuid.UUID) (*Provider, error) {
	var p Provider
	err := tx.QueryRow(ctx, `
		SELECT groups_claim, create_groups, enabled FROM oidc_provider WHERE org_id = $1`,
		orgID).Scan(&p.GroupsClaim, &p.CreateGroups, &p.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotConfigured
	}
	return &p, err
}
