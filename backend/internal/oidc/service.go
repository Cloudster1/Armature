package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/db"
)

// Service runs the sign-in flow.
//
// Discovery and the key set are fetched from the provider, which is a network
// call, so verifiers are kept per issuer rather than rebuilt per login. The
// library refreshes the keys on its own when it meets a key id it has not seen.
type Service struct {
	db          *db.Cluster
	redirectURL string
	// http reaches the provider; nil is the default client.
	http *http.Client

	mu        sync.Mutex
	verifiers map[string]*coreoidc.Provider
}

func NewService(cluster *db.Cluster, redirectURL string) *Service {
	return &Service{
		db:          cluster,
		redirectURL: redirectURL,
		verifiers:   map[string]*coreoidc.Provider{},
	}
}

// ProviderFor reads one organization's configuration.
func (s *Service) ProviderFor(ctx context.Context, orgID uuid.UUID) (*Provider, error) {
	var p Provider
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT org_id, issuer, client_id, client_secret, groups_claim, scopes,
			       create_groups, enabled, updated_at
			FROM oidc_provider WHERE org_id = $1`, orgID,
		).Scan(&p.OrgID, &p.Issuer, &p.ClientID, &p.ClientSecret, &p.GroupsClaim,
			&p.Scopes, &p.CreateGroups, &p.Enabled, &p.UpdatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotConfigured
	}
	if err != nil {
		return nil, fmt.Errorf("read the identity provider: %w", err)
	}
	p.HasSecret = p.ClientSecret != ""
	return &p, nil
}

// Save writes an organization's provider configuration.
//
// An empty secret leaves the stored one alone, so that a settings form which
// never shows the secret can be submitted without erasing it.
func (s *Service) Save(ctx context.Context, orgID uuid.UUID, in Provider, actor uuid.UUID) (*Provider, db.LSN, error) {
	in.Issuer = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(in.Issuer), "/"))
	in.ClientID = strings.TrimSpace(in.ClientID)
	if in.Issuer == "" || in.ClientID == "" {
		return nil, 0, errors.New("an identity provider needs an issuer and a client id")
	}
	if in.GroupsClaim == "" {
		in.GroupsClaim = "groups"
	}
	if in.Scopes == "" {
		in.Scopes = "openid profile email"
	}

	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO oidc_provider
			    (org_id, issuer, client_id, client_secret, groups_claim, scopes, create_groups, enabled)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (org_id) DO UPDATE SET
			    issuer = EXCLUDED.issuer,
			    client_id = EXCLUDED.client_id,
			    client_secret = CASE WHEN EXCLUDED.client_secret = ''
			                         THEN oidc_provider.client_secret
			                         ELSE EXCLUDED.client_secret END,
			    groups_claim = EXCLUDED.groups_claim,
			    scopes = EXCLUDED.scopes,
			    create_groups = EXCLUDED.create_groups,
			    enabled = EXCLUDED.enabled`,
			orgID, in.Issuer, in.ClientID, in.ClientSecret, in.GroupsClaim,
			in.Scopes, in.CreateGroups, in.Enabled)
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, orgID, audit.Entry{Action: "oidc.saved", TargetType: "org", TargetID: &orgID, Actor: actor, Data: map[string]any{"issuer": in.Issuer, "enabled": in.Enabled}})
	})
	if err != nil {
		return nil, 0, fmt.Errorf("save the identity provider: %w", err)
	}

	// A changed issuer means the cached discovery is about somewhere else.
	s.mu.Lock()
	delete(s.verifiers, in.Issuer)
	s.mu.Unlock()

	saved, err := s.ProviderFor(ctx, orgID)
	return saved, lsn, err
}

// Disable turns sign-in through the provider off without forgetting how it was
// configured, so turning it back on is one switch rather than a form.
func (s *Service) Disable(ctx context.Context, orgID uuid.UUID) (db.LSN, error) {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE oidc_provider SET enabled = false WHERE org_id = $1`, orgID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotConfigured
		}
		return nil
	})
}

// discover returns the provider's endpoints and key set, fetching them once.
func (s *Service) discover(ctx context.Context, issuer string) (*coreoidc.Provider, error) {
	s.mu.Lock()
	cached, ok := s.verifiers[issuer]
	s.mu.Unlock()
	if ok {
		return cached, nil
	}

	found, err := coreoidc.NewProvider(s.providerContext(ctx), issuer)
	if err != nil {
		return nil, fmt.Errorf("ask %s who it is: %w", issuer, err)
	}

	s.mu.Lock()
	s.verifiers[issuer] = found
	s.mu.Unlock()
	return found, nil
}

// Start begins a sign-in and returns the URL to send the browser to.
//
// The state ties the callback to this attempt and the nonce ties the id token
// to it. Both are stored rather than signed into a cookie, so that a callback
// arriving at a different instance is still recognised.
func (s *Service) Start(ctx context.Context, orgID uuid.UUID, redirect string) (string, error) {
	provider, err := s.ProviderFor(ctx, orgID)
	if err != nil {
		return "", err
	}
	if !provider.Enabled {
		return "", ErrNotConfigured
	}

	discovered, err := s.discover(ctx, provider.Issuer)
	if err != nil {
		return "", err
	}

	state, err := randomToken()
	if err != nil {
		return "", err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", err
	}

	if _, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		// Anything left over from an abandoned attempt is dead weight, and the
		// table is small enough that tidying on write is the whole story.
		if _, err := tx.Exec(ctx, `DELETE FROM oidc_login WHERE expires_at < now()`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO oidc_login (state, org_id, nonce, redirect, expires_at)
			VALUES ($1, $2, $3, $4, $5)`,
			state, orgID, nonce, redirect, time.Now().Add(howLongALoginMayTake))
		return err
	}); err != nil {
		return "", fmt.Errorf("remember this sign-in: %w", err)
	}

	config := oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		Endpoint:     discovered.Endpoint(),
		RedirectURL:  s.redirectURL,
		Scopes:       provider.ScopeList(),
	}
	return config.AuthCodeURL(state, coreoidc.Nonce(nonce)), nil
}

// Exchange completes a sign-in: it trades the code for tokens, verifies the id
// token and reads the identity out of it.
//
// The returned redirect is where the browser wanted to end up, carried across
// the round trip so that a link to a specific issue still lands there.
func (s *Service) Exchange(ctx context.Context, state, code string) (uuid.UUID, *Identity, string, error) {
	ctx = s.providerContext(ctx)
	var (
		orgID    uuid.UUID
		nonce    string
		redirect string
	)
	// The state is consumed as it is read. A code and state pair is good for
	// exactly one sign-in; replaying it must not produce a second session.
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			DELETE FROM oidc_login WHERE state = $1 AND expires_at > now()
			RETURNING org_id, nonce, redirect`, state,
		).Scan(&orgID, &nonce, &redirect)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownLogin
		}
		return err
	})
	if err != nil {
		return uuid.Nil, nil, "", err
	}

	provider, err := s.ProviderFor(ctx, orgID)
	if err != nil {
		return uuid.Nil, nil, "", err
	}
	if !provider.Enabled {
		return uuid.Nil, nil, "", ErrNotConfigured
	}

	discovered, err := s.discover(ctx, provider.Issuer)
	if err != nil {
		return uuid.Nil, nil, "", err
	}

	config := oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		Endpoint:     discovered.Endpoint(),
		RedirectURL:  s.redirectURL,
		Scopes:       provider.ScopeList(),
	}
	tokens, err := config.Exchange(ctx, code)
	if err != nil {
		return uuid.Nil, nil, "", fmt.Errorf("exchange the code: %w", err)
	}

	raw, ok := tokens.Extra("id_token").(string)
	if !ok || raw == "" {
		return uuid.Nil, nil, "", errors.New("the identity provider returned no id token")
	}

	// Verified against the issuer's published keys, for this client, unexpired.
	// The library refuses an unsigned token and one signed with a symmetric key
	// it was not given, which are the two ways this check is usually skipped.
	verified, err := discovered.Verifier(&coreoidc.Config{ClientID: provider.ClientID}).Verify(ctx, raw)
	if err != nil {
		return uuid.Nil, nil, "", fmt.Errorf("verify the id token: %w", err)
	}
	if verified.Nonce != nonce {
		return uuid.Nil, nil, "", errors.New("the id token belongs to a different sign-in")
	}

	identity, err := identityFrom(verified, provider.GroupsClaim)
	if err != nil {
		return uuid.Nil, nil, "", err
	}
	return orgID, identity, redirect, nil
}

// identityFrom reads the claims this product cares about out of a token that
// has already been verified.
func identityFrom(token *coreoidc.IDToken, groupsClaim string) (*Identity, error) {
	var claims map[string]any
	if err := token.Claims(&claims); err != nil {
		return nil, fmt.Errorf("read the token's claims: %w", err)
	}

	identity := Identity{
		Subject: token.Subject,
		Email:   strings.TrimSpace(asString(claims["email"])),
		Name:    strings.TrimSpace(asString(claims["name"])),
	}
	if identity.Email == "" {
		return nil, ErrNoEmail
	}
	// The address is what ties a sign-in to an account here, so a provider that
	// says it never checked the address is not vouching for the person.
	switch verified := claims["email_verified"].(type) {
	case bool:
		if !verified {
			return nil, ErrEmailUnverified
		}
	case string:
		if strings.EqualFold(verified, "false") {
			return nil, ErrEmailUnverified
		}
	}
	if identity.Name == "" {
		// Better a name derived from the address than a blank one in every
		// listing the person appears in.
		identity.Name, _, _ = strings.Cut(identity.Email, "@")
	}
	identity.Groups = asStrings(claims[groupsClaim])
	return &identity, nil
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// asStrings reads a claim that is a list of strings, tolerating a provider that
// sends a single string when somebody is in exactly one group.
func asStrings(v any) []string {
	switch value := v.(type) {
	case string:
		if value == "" {
			return nil
		}
		return []string{value}
	case []any:
		out := make([]string, 0, len(value))
		for _, each := range value {
			if s := asString(each); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return value
	default:
		return nil
	}
}

// randomToken is 32 bytes of randomness, which is what both the state and the
// nonce need to be unguessable.
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
