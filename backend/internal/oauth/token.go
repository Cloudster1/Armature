package oauth

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/oidc"
)

// Credentials are how a client says who it is at the token and revocation
// endpoints: HTTP Basic, or both fields in the body.
type Credentials struct {
	ID, Secret string
	// Basic is set when they came in the Authorization header.
	Basic bool
	// sentSecret tells a public client sending a secret from one sending none.
	sentSecret bool
}

// ReadCredentials takes a client's credentials from the Authorization header
// or the form, never both.
func ReadCredentials(header string, form url.Values) (Credentials, error) {
	if encoded, ok := strings.CutPrefix(header, "Basic "); ok {
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		id, secret, found := strings.Cut(string(raw), ":")
		if err != nil || !found {
			return Credentials{}, &Error{Status: http.StatusUnauthorized, Code: "invalid_client", Description: "Send the client id and secret as HTTP Basic, form-encoded first.", Basic: true}
		}
		// RFC 6749 section 2.3.1 form-encodes both before joining them.
		if id, err = url.QueryUnescape(id); err == nil {
			secret, err = url.QueryUnescape(secret)
		}
		if err != nil {
			return Credentials{}, &Error{Status: http.StatusUnauthorized, Code: "invalid_client", Description: "Send the client id and secret as HTTP Basic, form-encoded first.", Basic: true}
		}
		if form.Has("client_secret") || (form.Has("client_id") && form.Get("client_id") != id) {
			return Credentials{}, oauthError(http.StatusBadRequest, "invalid_request", "Authenticate one way: HTTP Basic or the form, not both.")
		}
		return Credentials{ID: id, Secret: secret, Basic: true, sentSecret: true}, nil
	}
	if form.Get("client_id") == "" {
		return Credentials{}, oauthError(http.StatusUnauthorized, "invalid_client", "Say which app is asking, with client_id.")
	}
	return Credentials{ID: form.Get("client_id"), Secret: form.Get("client_secret"), sentSecret: form.Has("client_secret")}, nil
}

// authenticate finds the client the credentials prove, or refuses them.
func (s *Service) authenticate(ctx context.Context, tx db.DBTX, cred Credentials) (*Client, error) {
	refuse := &Error{Status: http.StatusUnauthorized, Code: "invalid_client", Basic: cred.Basic,
		Description: "Armature does not know this app, its secret is wrong, or nobody has allowed it yet."}
	id, err := uuid.Parse(cred.ID)
	if err != nil {
		return nil, refuse
	}
	var digest []byte
	c, err := scanClientWithSecret(tx.QueryRow(ctx, selectClientsWithSecret+` WHERE c.id = $1`, id), &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, refuse
	}
	if err != nil {
		return nil, err
	}
	if c.Status != StatusActive {
		return nil, refuse
	}
	if c.Confidential {
		if cred.Secret == "" || subtle.ConstantTimeCompare(auth.HashToken(cred.Secret), digest) != 1 {
			return nil, refuse
		}
	} else if cred.sentSecret {
		return nil, refuse
	}
	return c, nil
}

const selectClientsWithSecret = `SELECT ` + clientColumns + `, c.secret_hash FROM oauth_client c LEFT JOIN webhook_endpoint w ON w.client_id = c.id`

func scanClientWithSecret(row pgx.Row, digest *[]byte) (*Client, error) {
	var (
		c       Client
		hookID  *uuid.UUID
		webhook ClientWebhook
	)
	if err := row.Scan(&c.ID, &c.Name, &c.ClientURI, &c.RedirectURIs, &c.Scopes, &c.Confidential, &c.Status,
		&c.KeycloakClientID, &c.authMethod, &c.CreatedAt, &c.UpdatedAt, &c.Grants,
		&hookID, &webhook.URL, &webhook.Topics, &webhook.Enabled, digest); err != nil {
		return nil, err
	}
	return &c, nil
}

// TokenResponse is the token endpoint's answer, as RFC 6749 section 5.1 and
// RFC 8693 section 2.2.1 name it.
type TokenResponse struct {
	AccessToken     string `json:"access_token"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int    `json:"expires_in"`
	RefreshToken    string `json:"refresh_token,omitempty"`
	Scope           string `json:"scope"`
	IssuedTokenType string `json:"issued_token_type,omitempty"`
}

// singleParams may each be sent once at most.
var singleParams = []string{"grant_type", "code", "redirect_uri", "code_verifier", "refresh_token", "scope", "resource",
	"subject_token", "subject_token_type", "requested_token_type", "armature_expires_in", "client_id", "client_secret"}

// Token answers the token endpoint.
func (s *Service) Token(ctx context.Context, cred Credentials, form url.Values) (*TokenResponse, error) {
	for _, name := range singleParams {
		if len(form[name]) > 1 {
			return nil, oauthError(http.StatusBadRequest, "invalid_request", fmt.Sprintf("Send %s once.", name))
		}
	}
	if resource := form.Get("resource"); resource != "" && !slices.Contains(s.Resources(), resource) {
		return nil, oauthError(http.StatusBadRequest, "invalid_target", fmt.Sprintf("The resource is %s.", strings.Join(s.Resources(), " or ")))
	}
	grant := form.Get("grant_type")
	if grant == "" {
		return nil, oauthError(http.StatusBadRequest, "invalid_request", "Say which grant, with grant_type.")
	}
	if !slices.Contains(grantTypes, grant) {
		return nil, oauthError(http.StatusBadRequest, "unsupported_grant_type", fmt.Sprintf("Armature offers %s.", strings.Join(grantTypes, ", ")))
	}

	var (
		out *TokenResponse
		// refusal is answered after the transaction commits, so ending a
		// grant on a replayed code or refresh token is kept.
		refusal error
	)
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		client, err := s.authenticate(ctx, tx, cred)
		if err != nil {
			return err
		}
		switch grant {
		case GrantAuthorizationCode:
			out, refusal, err = s.redeemCode(ctx, tx, client, form)
		case GrantRefreshToken:
			out, refusal, err = s.refresh(ctx, tx, client, form)
		default:
			out, refusal, err = s.exchange(ctx, tx, client, form)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if refusal != nil {
		return nil, refusal
	}
	return out, nil
}

func required(form url.Values, names ...string) error {
	for _, name := range names {
		if form.Get(name) == "" {
			return oauthError(http.StatusBadRequest, "invalid_request", fmt.Sprintf("Send %s.", name))
		}
	}
	return nil
}

// redeemCode trades a code for a grant's tokens. A code used twice ends the
// grant it made, as RFC 6749 section 10.5 asks.
func (s *Service) redeemCode(ctx context.Context, tx db.DBTX, client *Client, form url.Values) (*TokenResponse, error, error) {
	if err := required(form, "code", "redirect_uri", "code_verifier"); err != nil {
		return nil, nil, err
	}
	var (
		clientID, userID    uuid.UUID
		scopes              []string
		redirect, challenge string
		expires             time.Time
		used                *time.Time
		grantID             *uuid.UUID
	)
	digest := auth.HashToken(form.Get("code"))
	err := tx.QueryRow(ctx, `
		SELECT client_id, user_id, scopes, redirect_uri, challenge, expires_at, used_at, grant_id
		FROM oauth_code WHERE code_hash = $1 FOR UPDATE`, digest).Scan(
		&clientID, &userID, &scopes, &redirect, &challenge, &expires, &used, &grantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ended(), nil
	}
	if err != nil {
		return nil, nil, err
	}
	if clientID != client.ID {
		return nil, ended(), nil
	}
	if used != nil {
		if grantID != nil {
			if err := endGrant(ctx, tx, *grantID, "code_reused"); err != nil {
				return nil, nil, err
			}
		}
		return nil, ended(), nil
	}
	if !s.now().Before(expires) || redirect != form.Get("redirect_uri") || !verifies(form.Get("code_verifier"), challenge) {
		return nil, ended(), nil
	}
	if err := personIn(ctx, tx, userID); err != nil {
		var refused *Error
		if errors.As(err, &refused) {
			return nil, refused, nil
		}
		return nil, nil, err
	}
	out, id, held, err := s.grant(ctx, tx, client, userID, scopes)
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_code SET used_at = now(), grant_id = $2 WHERE code_hash = $1`, digest, id); err != nil {
		return nil, nil, err
	}
	return out, nil, grantCreated(ctx, tx, id, client.ID, userID, held, "consent")
}

// verifies says whether a PKCE verifier hashes to the challenge.
func verifies(verifier, challenge string) bool {
	if len(verifier) < minVerifier || len(verifier) > maxVerifier {
		return false
	}
	for _, r := range verifier {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune("-._~", r)) {
			return false
		}
	}
	sum := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) == 1
}

// grant makes or renews the person's one grant for the client. A live grant
// keeps its scopes and gains the ones asked; only a narrower refresh narrows it.
func (s *Service) grant(ctx context.Context, tx db.DBTX, client *Client, userID uuid.UUID, scopes []string) (*TokenResponse, uuid.UUID, []string, error) {
	access, accessDigest, err := auth.GeneratePrefixed(auth.AccessTokenPrefix)
	if err != nil {
		return nil, uuid.Nil, nil, err
	}
	refresh, refreshDigest, err := auth.GeneratePrefixed(auth.RefreshTokenPrefix)
	if err != nil {
		return nil, uuid.Nil, nil, err
	}
	now := s.now()
	var (
		id   uuid.UUID
		held []string
	)
	if err := tx.QueryRow(ctx, `
		INSERT INTO api_token (user_id, name, token_hash, scopes, expires_at, client_id, refresh_hash, access_expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (user_id, client_id) WHERE client_id IS NOT NULL DO UPDATE SET
		    name = EXCLUDED.name, token_hash = EXCLUDED.token_hash,
		    scopes = CASE WHEN api_token.expires_at > $9
		                  THEN ARRAY(SELECT s FROM unnest($10::text[]) s WHERE s = ANY(api_token.scopes || EXCLUDED.scopes))
		                  ELSE EXCLUDED.scopes END,
		    expires_at = EXCLUDED.expires_at, refresh_hash = EXCLUDED.refresh_hash,
		    access_expires_at = EXCLUDED.access_expires_at
		RETURNING id, scopes`,
		userID, client.Name+auth.ConnectedAppSuffix, accessDigest, scopes, now.Add(RefreshTokenTTL),
		client.ID, refreshDigest, now.Add(AccessTokenTTL), now, Scopes).Scan(&id, &held); err != nil {
		return nil, uuid.Nil, nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM oauth_exchanged_token WHERE grant_id = $1`, id); err != nil {
		return nil, uuid.Nil, nil, err
	}
	return &TokenResponse{
		AccessToken: access, TokenType: "Bearer", ExpiresIn: int(AccessTokenTTL / time.Second),
		RefreshToken: refresh, Scope: joinScope(held),
	}, id, held, nil
}

// endGrant deletes a grant whose secret was replayed, and says why in the log.
func endGrant(ctx context.Context, tx db.DBTX, id uuid.UUID, why string) error {
	var (
		userID   uuid.UUID
		clientID *uuid.UUID
	)
	err := tx.QueryRow(ctx, `DELETE FROM api_token WHERE id = $1 RETURNING user_id, client_id`, id).Scan(&userID, &clientID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return audit.Write(ctx, tx, audit.Entry{Action: "oauth.grant.ended", TargetType: "api_token", TargetID: &id, Actor: userID,
		Data: map[string]any{"clientId": clientID, "why": why}})
}

// asked is the scope parameter checked against what may be had: read among
// them, nothing beyond. Left out, it is everything that may be had.
func asked(form url.Values, allowed []string) ([]string, error) {
	scopes := splitScope(form.Get("scope"))
	if len(scopes) == 0 {
		return slices.Clone(allowed), nil
	}
	if !validScopeRequest(scopes, allowed) {
		return nil, oauthError(http.StatusBadRequest, "invalid_scope", fmt.Sprintf("Ask for read and at most %s.", joinScope(allowed)))
	}
	return normalScopes(scopes), nil
}

// refresh rotates a grant's tokens. A refresh token that was rotated already
// ends the grant: the partner refreshes one grant in one place at a time.
func (s *Service) refresh(ctx context.Context, tx db.DBTX, client *Client, form url.Values) (*TokenResponse, error, error) {
	if err := required(form, "refresh_token"); err != nil {
		return nil, nil, err
	}
	digest := auth.HashToken(form.Get("refresh_token"))
	var (
		id, userID uuid.UUID
		held       []string
		expires    time.Time
	)
	err := tx.QueryRow(ctx, `
		SELECT id, user_id, scopes, expires_at FROM api_token
		WHERE refresh_hash = $1 AND client_id = $2 FOR UPDATE`, digest, client.ID).Scan(&id, &userID, &held, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		var rotated uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT r.grant_id FROM oauth_rotated_refresh r JOIN api_token t ON t.id = r.grant_id
			WHERE r.refresh_hash = $1 AND t.client_id = $2`, digest, client.ID).Scan(&rotated)
		if err == nil {
			err = endGrant(ctx, tx, rotated, "refresh_reused")
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, err
		}
		return nil, ended(), nil
	}
	if err != nil {
		return nil, nil, err
	}
	if !s.now().Before(expires) {
		return nil, ended(), nil
	}
	if err := personIn(ctx, tx, userID); err != nil {
		var refused *Error
		if errors.As(err, &refused) {
			return nil, refused, nil
		}
		return nil, nil, err
	}
	scopes, refusal := asked(form, held)
	if refusal != nil {
		return nil, refusal, nil
	}
	access, accessDigest, err := auth.GeneratePrefixed(auth.AccessTokenPrefix)
	if err != nil {
		return nil, nil, err
	}
	refresh, refreshDigest, err := auth.GeneratePrefixed(auth.RefreshTokenPrefix)
	if err != nil {
		return nil, nil, err
	}
	now := s.now()
	if _, err := tx.Exec(ctx, `INSERT INTO oauth_rotated_refresh (refresh_hash, grant_id) VALUES ($1, $2)`, digest, id); err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM oauth_rotated_refresh WHERE rotated_at < $1`, now.Add(-RefreshTokenTTL)); err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE api_token SET token_hash = $2, refresh_hash = $3, scopes = $4, expires_at = $5, access_expires_at = $6
		WHERE id = $1`, id, accessDigest, refreshDigest, scopes, now.Add(RefreshTokenTTL), now.Add(AccessTokenTTL)); err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE oauth_exchanged_token SET scopes = ARRAY(SELECT s FROM unnest(scopes) s WHERE s = ANY($2))
		WHERE grant_id = $1 AND NOT scopes <@ $2`, id, scopes); err != nil {
		return nil, nil, err
	}
	return &TokenResponse{
		AccessToken: access, TokenType: "Bearer", ExpiresIn: int(AccessTokenTTL / time.Second),
		RefreshToken: refresh, Scope: joinScope(scopes),
	}, nil, nil
}

// exchange is RFC 8693: a narrower, shorter token from a grant's access token,
// or a grant from a token of the installation's identity provider.
func (s *Service) exchange(ctx context.Context, tx db.DBTX, client *Client, form url.Values) (*TokenResponse, error, error) {
	if err := required(form, "subject_token", "subject_token_type"); err != nil {
		return nil, nil, err
	}
	if form.Get("subject_token_type") != TokenTypeAccess {
		return nil, oauthError(http.StatusBadRequest, "invalid_request", "Armature exchanges access tokens only: subject_token_type is "+TokenTypeAccess+"."), nil
	}
	if kind := form.Get("requested_token_type"); kind != "" && kind != TokenTypeAccess {
		return nil, oauthError(http.StatusBadRequest, "invalid_request", "Armature issues access tokens only: requested_token_type is "+TokenTypeAccess+"."), nil
	}
	if auth.IsAccessToken(form.Get("subject_token")) {
		return s.narrow(ctx, tx, client, form)
	}
	return s.fromIdentityProvider(ctx, tx, client, form)
}

// narrow issues a token of the grant the subject belongs to, with at most its
// scopes and its hour, without narrowing the grant.
func (s *Service) narrow(ctx context.Context, tx db.DBTX, client *Client, form url.Values) (*TokenResponse, error, error) {
	lifetime := AccessTokenTTL
	if raw := form.Get("armature_expires_in"); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds < 1 || time.Duration(seconds)*time.Second > AccessTokenTTL {
			return nil, oauthError(http.StatusBadRequest, "invalid_request", fmt.Sprintf(
				"armature_expires_in is a number of seconds, from 1 to %d.", int(AccessTokenTTL/time.Second))), nil
		}
		lifetime = time.Duration(seconds) * time.Second
	}
	var (
		id, userID uuid.UUID
		held       []string
	)
	err := tx.QueryRow(ctx, `
		SELECT id, user_id, scopes FROM api_token
		WHERE token_hash = $1 AND client_id = $2 AND access_expires_at > now() AND expires_at > now()`,
		auth.HashToken(form.Get("subject_token")), client.ID).Scan(&id, &userID, &held)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ended(), nil
	}
	if err != nil {
		return nil, nil, err
	}
	if err := personIn(ctx, tx, userID); err != nil {
		var refused *Error
		if errors.As(err, &refused) {
			return nil, refused, nil
		}
		return nil, nil, err
	}
	scopes, refusal := asked(form, held)
	if refusal != nil {
		return nil, refusal, nil
	}
	token, digest, err := auth.GeneratePrefixed(auth.AccessTokenPrefix)
	if err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM oauth_exchanged_token WHERE expires_at < now()`); err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO oauth_exchanged_token (token_hash, grant_id, scopes, expires_at) VALUES ($1, $2, $3, $4)`,
		digest, id, scopes, s.now().Add(lifetime)); err != nil {
		return nil, nil, err
	}
	return &TokenResponse{
		AccessToken: token, TokenType: "Bearer", ExpiresIn: int(lifetime / time.Second),
		Scope: joinScope(scopes), IssuedTokenType: TokenTypeAccess,
	}, nil, nil
}

// fromIdentityProvider trades a person's token from the shared realm for a
// grant. The administrator who named the client's Keycloak client consented
// for everybody, so no page is shown, and webhooks.partner is never given.
func (s *Service) fromIdentityProvider(ctx context.Context, tx db.DBTX, client *Client, form url.Values) (*TokenResponse, error, error) {
	if client.KeycloakClientID == "" || s.provider == nil {
		return nil, oauthError(http.StatusBadRequest, "unauthorized_client", fmt.Sprintf(
			"%s may not exchange identity provider tokens. An Armature administrator names its Keycloak client under Settings, Connected apps.", client.Name)), nil
	}
	allowed := slices.DeleteFunc(slices.Clone(client.Scopes), func(scope string) bool { return scope == ScopeWebhooks })
	if slices.Contains(splitScope(form.Get("scope")), ScopeWebhooks) {
		return nil, oauthError(http.StatusBadRequest, "invalid_scope", "webhooks.partner is given on the consent page only. Connect as an administrator there."), nil
	}
	scopes, refusal := asked(form, allowed)
	if refusal != nil {
		return nil, refusal, nil
	}
	identity, err := s.provider.VerifyAccessToken(ctx, form.Get("subject_token"), client.KeycloakClientID)
	if errors.Is(err, oidc.ErrNotConfigured) {
		return nil, oauthError(http.StatusBadRequest, "unauthorized_client", "Single sign-on is not set up in this Armature, so there is no identity provider token to exchange."), nil
	}
	if err != nil {
		return nil, oauthError(http.StatusBadRequest, "invalid_grant", "The identity provider's token was refused: "+err.Error()+"."), nil
	}
	var (
		userID   uuid.UUID
		active   bool
		standing *string
	)
	err = tx.QueryRow(ctx, `SELECT id, is_active, standing FROM app_user WHERE email = $1`, auth.NormalizeEmail(identity.Email)).Scan(&userID, &active, &standing)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, err
	}
	if err != nil || !active || standing == nil || !auth.OrgRole(*standing).IsAgent() {
		return nil, oauthError(http.StatusBadRequest, "invalid_grant", fmt.Sprintf(
			"%s has no Armature account yet. Sign in to Armature once, or ask an administrator to let them in.", identity.Name)), nil
	}
	out, id, held, err := s.grant(ctx, tx, client, userID, scopes)
	if err != nil {
		return nil, nil, err
	}
	return out, nil, grantCreated(ctx, tx, id, client.ID, userID, held, "identity_provider")
}

// Revoke is RFC 7009. A grant's access or refresh token ends the whole grant;
// an exchanged token ends only itself. Every token is answered alike.
func (s *Service) Revoke(ctx context.Context, cred Credentials, token string) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		client, err := s.authenticate(ctx, tx, cred)
		if err != nil {
			return err
		}
		if token == "" {
			return nil
		}
		digest := auth.HashToken(token)
		var id, userID uuid.UUID
		err = tx.QueryRow(ctx, `
			DELETE FROM api_token WHERE client_id = $2 AND (token_hash = $1 OR refresh_hash = $1)
			RETURNING id, user_id`, digest, client.ID).Scan(&id, &userID)
		if err == nil {
			return audit.Write(ctx, tx, audit.Entry{Action: "oauth.grant.revoked", TargetType: "api_token", TargetID: &id, Actor: userID,
				Data: map[string]any{"clientId": client.ID, "by": "app"}})
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		_, err = tx.Exec(ctx, `
			DELETE FROM oauth_exchanged_token x USING api_token t
			WHERE x.token_hash = $1 AND t.id = x.grant_id AND t.client_id = $2`, digest, client.ID)
		return err
	})
	return err
}
