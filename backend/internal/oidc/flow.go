package oidc

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/armature/armature/backend/internal/db"
)

// Login is one sign-in in flight, as its callback claims it.
type Login struct {
	nonce    string
	verifier string
	// Redirect is where the browser wanted to end up.
	Redirect string
	// Silent says the attempt asked the provider not to show anything.
	Silent bool
}

// Start begins a sign-in and returns the URL to send the browser to.
func (s *Service) Start(ctx context.Context, redirect string) (string, error) {
	return s.Begin(ctx, redirect, false)
}

// Begin starts a sign-in, silently with prompt=none when asked. The state,
// nonce and PKCE verifier are stored, so any instance can take the callback.
func (s *Service) Begin(ctx context.Context, redirect string, silent bool) (string, error) {
	provider, err := s.ProviderFor(ctx)
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
	verifier := oauth2.GenerateVerifier()

	if _, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		// Anything left over from an abandoned attempt is dead weight, and the
		// table is small enough that tidying on write is the whole story.
		if _, err := tx.Exec(ctx, `DELETE FROM oidc_login WHERE expires_at < now()`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO oidc_login (state, nonce, redirect, expires_at, code_verifier, silent)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			state, nonce, redirect, time.Now().Add(howLongALoginMayTake), verifier, silent)
		return err
	}); err != nil {
		return "", fmt.Errorf("remember this sign-in: %w", err)
	}

	options := []oauth2.AuthCodeOption{coreoidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)}
	if silent {
		options = append(options, oauth2.SetAuthURLParam("prompt", "none"))
	}
	config := s.config(provider, discovered)
	return config.AuthCodeURL(state, options...), nil
}

func (s *Service) config(provider *Provider, discovered *coreoidc.Provider) oauth2.Config {
	return oauth2.Config{
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		Endpoint:     discovered.Endpoint(),
		RedirectURL:  s.redirectURL,
		Scopes:       provider.ScopeList(),
	}
}

// Claim consumes the state a callback carries. A state is good for exactly one
// callback, whether it brings a code or the provider's refusal.
func (s *Service) Claim(ctx context.Context, state string) (*Login, error) {
	var login Login
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			DELETE FROM oidc_login WHERE state = $1 AND expires_at > now()
			RETURNING nonce, redirect, code_verifier, silent`, state,
		).Scan(&login.nonce, &login.Redirect, &login.verifier, &login.Silent)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownLogin
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return &login, nil
}

// Exchange completes a sign-in from its state and code, for a caller that has
// not claimed the state itself.
func (s *Service) Exchange(ctx context.Context, state, code string) (*Identity, string, error) {
	login, err := s.Claim(ctx, state)
	if err != nil {
		return nil, "", err
	}
	identity, err := s.Complete(ctx, login, code)
	if err != nil {
		return nil, "", err
	}
	return identity, login.Redirect, nil
}

// Complete trades a claimed sign-in's code for tokens, verifies the id token
// and reads the identity out of it.
func (s *Service) Complete(ctx context.Context, login *Login, code string) (*Identity, error) {
	ctx = s.providerContext(ctx)
	provider, err := s.ProviderFor(ctx)
	if err != nil {
		return nil, err
	}
	if !provider.Enabled {
		return nil, ErrNotConfigured
	}
	discovered, err := s.discover(ctx, provider.Issuer)
	if err != nil {
		return nil, err
	}
	options := []oauth2.AuthCodeOption{}
	if login.verifier != "" {
		options = append(options, oauth2.VerifierOption(login.verifier))
	}
	config := s.config(provider, discovered)
	tokens, err := config.Exchange(ctx, code, options...)
	if err != nil {
		return nil, fmt.Errorf("exchange the code: %w", err)
	}
	raw, ok := tokens.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, errors.New("the identity provider returned no id token")
	}

	// The library refuses an unsigned token and one signed with a symmetric key
	// it was not given, the two ways this check is usually skipped.
	verified, err := discovered.Verifier(&coreoidc.Config{ClientID: provider.ClientID}).Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("verify the id token: %w", err)
	}
	if verified.Nonce != login.nonce {
		return nil, errors.New("the id token belongs to a different sign-in")
	}
	identity, err := identityFrom(verified, provider.GroupsClaim)
	if err != nil {
		return nil, err
	}
	identity.IDToken = raw
	return identity, nil
}

// SignOutURL is where the browser goes to end the provider's session too, for
// a session that came from it, returning to afterwards; empty when there is none.
func (s *Service) SignOutURL(ctx context.Context, sessionID uuid.UUID, afterwards string) (string, error) {
	var idToken *string
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT id_token FROM user_session WHERE id = $1 AND proof = 'oidc'`, sessionID).Scan(&idToken)
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (idToken == nil || *idToken == "")) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	provider, err := s.ProviderFor(ctx)
	if errors.Is(err, ErrNotConfigured) || (err == nil && !provider.Enabled) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	discovered, err := s.discover(ctx, provider.Issuer)
	if err != nil {
		return "", err
	}
	var endpoints struct {
		EndSession string `json:"end_session_endpoint"`
	}
	if err := discovered.Claims(&endpoints); err != nil || endpoints.EndSession == "" {
		return "", err
	}
	target, err := url.Parse(endpoints.EndSession)
	if err != nil {
		return "", fmt.Errorf("read the provider's end session endpoint: %w", err)
	}
	query := target.Query()
	query.Set("id_token_hint", *idToken)
	query.Set("client_id", provider.ClientID)
	query.Set("post_logout_redirect_uri", afterwards)
	target.RawQuery = query.Encode()
	return target.String(), nil
}
