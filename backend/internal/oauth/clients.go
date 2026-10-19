package oauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
)

// Client is a partner deployment: one Stator, one Exciter, one Rotor.
type Client struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	ClientURI    string    `json:"clientUri"`
	RedirectURIs []string  `json:"redirectUris"`
	Scopes       []string  `json:"scopes"`
	// Confidential clients authenticate at the token endpoint with a secret;
	// a public one proves itself with PKCE alone.
	Confidential bool   `json:"confidential"`
	Status       string `json:"status"`
	// KeycloakClientID is the identity provider's client whose tokens may be
	// exchanged for this client's; empty means no exchange.
	KeycloakClientID string `json:"keycloakClientId"`
	// Grants is how many people have connected it.
	Grants    int       `json:"grants"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Secret is set only on the answer that made or rotated it.
	Secret string `json:"secret,omitempty"`

	authMethod string
}

// Client statuses.
const (
	StatusActive  = "active"
	StatusPending = "pending"
)

// ClientInput is what an administrator registers a client with.
type ClientInput struct {
	Name         string   `json:"name"`
	ClientURI    string   `json:"clientUri,omitempty"`
	RedirectURIs []string `json:"redirectUris"`
	Scopes       []string `json:"scopes"`
	// Confidential defaults to true: a server-side partner keeps a secret.
	Confidential     *bool  `json:"confidential,omitempty"`
	KeycloakClientID string `json:"keycloakClientId,omitempty"`
}

// ClientChanges is what an administrator changes about a client; what is left
// out keeps its value.
type ClientChanges struct {
	Name             *string   `json:"name,omitempty"`
	ClientURI        *string   `json:"clientUri,omitempty"`
	RedirectURIs     *[]string `json:"redirectUris,omitempty"`
	Scopes           *[]string `json:"scopes,omitempty"`
	KeycloakClientID *string   `json:"keycloakClientId,omitempty"`
}

const clientColumns = `c.id, c.name, c.client_uri, c.redirect_uris, c.scopes, c.secret_hash IS NOT NULL, c.status,
	c.keycloak_client_id, c.auth_method, c.created_at, c.updated_at,
	(SELECT count(*) FROM api_token t WHERE t.client_id = c.id)`

const selectClients = `SELECT ` + clientColumns + ` FROM oauth_client c`

func scanClient(row pgx.Row) (*Client, error) {
	var c Client
	if err := row.Scan(&c.ID, &c.Name, &c.ClientURI, &c.RedirectURIs, &c.Scopes, &c.Confidential, &c.Status,
		&c.KeycloakClientID, &c.authMethod, &c.CreatedAt, &c.UpdatedAt, &c.Grants); err != nil {
		return nil, err
	}
	return &c, nil
}

// ListClients is every client, by name, pending ones nobody allowed in time left out.
func (s *Service) ListClients(ctx context.Context) ([]Client, error) {
	out := []Client{}
	err := s.db.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectClients+`
			WHERE c.status = 'active' OR c.created_at >= $1
			ORDER BY lower(c.name), c.created_at`, s.now().Add(-PendingClientTTL))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanClient(rows)
			if err != nil {
				return err
			}
			out = append(out, *c)
		}
		return rows.Err()
	})
	return out, err
}

// clientIn reads one client inside a transaction, a pending one past its time
// reading as gone.
func (s *Service) clientIn(ctx context.Context, tx db.DBTX, id uuid.UUID, lock bool) (*Client, error) {
	query := selectClients + ` WHERE c.id = $1 AND (c.status = 'active' OR c.created_at >= $2)`
	if lock {
		query += ` FOR UPDATE OF c`
	}
	c, err := scanClient(tx.QueryRow(ctx, query, id, s.now().Add(-PendingClientTTL)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// GetClient reads one client.
func (s *Service) GetClient(ctx context.Context, id uuid.UUID) (*Client, error) {
	var out *Client
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = s.clientIn(ctx, tx, id, false)
		return err
	})
	return out, err
}

// checkName is a client's name, trimmed, or the sentence refusing it.
func checkName(name string) (string, string) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", "Give the app a name, such as Stator."
	case len([]rune(name)) > MaxNameLength:
		return "", fmt.Sprintf("Keep the name to %d characters.", MaxNameLength)
	}
	return name, ""
}

// checkClientURI is the partner's own address, or the sentence refusing it.
func checkClientURI(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", "Write the app's address as an http or https URL, such as https://stator.example.com."
	}
	return raw, ""
}

// loopbackHosts may take a plain http redirect anywhere: a tool on the
// person's own machine.
var loopbackHosts = []string{"localhost", "127.0.0.1", "::1"}

// checkRedirectURIs refuses an address a code could leak through, or says why.
func (s *Service) checkRedirectURIs(uris []string) ([]string, string) {
	if len(uris) == 0 {
		return nil, "Name the address Armature sends the browser back to, such as https://stator.example.com/oauth/callback."
	}
	if len(uris) > MaxRedirectURIs {
		return nil, fmt.Sprintf("Name at most %d return addresses.", MaxRedirectURIs)
	}
	out := make([]string, 0, len(uris))
	for _, raw := range uris {
		u, err := url.Parse(raw)
		if err != nil || !u.IsAbs() || u.Host == "" || u.Opaque != "" {
			return nil, fmt.Sprintf("%s is not an address. Write a whole URL, such as https://stator.example.com/oauth/callback.", raw)
		}
		if u.Fragment != "" || strings.Contains(raw, "#") {
			return nil, fmt.Sprintf("%s has a fragment. A return address has none.", raw)
		}
		switch {
		case u.Scheme == "https":
		case u.Scheme == "http" && (s.development || slices.Contains(loopbackHosts, u.Hostname())):
		default:
			return nil, fmt.Sprintf("%s is not https. Armature sends a code back over https only, or plain http to this machine.", raw)
		}
		if !slices.Contains(out, raw) {
			out = append(out, raw)
		}
	}
	return out, ""
}

// checkScopes refuses a scope Armature does not issue, and a list without read.
func checkScopes(scopes []string) ([]string, string) {
	for _, scope := range scopes {
		if !knownScope(scope) {
			return nil, fmt.Sprintf("%s is not a scope. Choose from %s.", scope, strings.Join(Scopes, ", "))
		}
	}
	if !slices.Contains(scopes, ScopeRead) {
		return nil, "Every app reads, so read is among its scopes."
	}
	return normalScopes(scopes), ""
}

// CreateClient registers a client by hand: active at once, with its secret
// returned this once.
func (s *Service) CreateClient(ctx context.Context, in ClientInput, actor uuid.UUID) (*Client, db.LSN, error) {
	fields := map[string]string{}
	name, problem := checkName(in.Name)
	if problem != "" {
		fields["name"] = problem
	}
	clientURI, problem := checkClientURI(in.ClientURI)
	if problem != "" {
		fields["clientUri"] = problem
	}
	redirects, problem := s.checkRedirectURIs(in.RedirectURIs)
	if problem != "" {
		fields["redirectUris"] = problem
	}
	scopes, problem := checkScopes(in.Scopes)
	if problem != "" {
		fields["scopes"] = problem
	}
	if len(fields) > 0 {
		return nil, 0, &ValidationError{Fields: fields}
	}
	confidential := in.Confidential == nil || *in.Confidential
	method := AuthNone
	var (
		secret string
		digest []byte
	)
	if confidential {
		var err error
		if secret, digest, err = auth.GeneratePrefixed(auth.ClientSecretPrefix); err != nil {
			return nil, 0, err
		}
		method = AuthBasic
	}
	var out *Client
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO oauth_client (name, client_uri, redirect_uris, scopes, secret_hash, auth_method, status, keycloak_client_id, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, 'active', $7, $8)
			RETURNING id`,
			name, clientURI, redirects, scopes, digest, method, strings.TrimSpace(in.KeycloakClientID), actor).Scan(&id); err != nil {
			return err
		}
		c, err := s.clientIn(ctx, tx, id, false)
		if err != nil {
			return err
		}
		out = c
		return record(ctx, tx, "oauth.client.created", c, actor, "")
	})
	if err != nil {
		return nil, 0, err
	}
	out.Secret = secret
	return out, lsn, nil
}

// UpdateClient changes a client. Narrowing its scopes narrows every live grant
// of it at once, and a grant left with nothing but read keeps reading.
func (s *Service) UpdateClient(ctx context.Context, id uuid.UUID, in ClientChanges, actor uuid.UUID) (*Client, db.LSN, error) {
	var out *Client
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		c, err := s.clientIn(ctx, tx, id, true)
		if err != nil {
			return err
		}
		fields := map[string]string{}
		if in.Name != nil {
			if c.Name, fields["name"] = checkName(*in.Name); fields["name"] == "" {
				delete(fields, "name")
			}
		}
		if in.ClientURI != nil {
			if c.ClientURI, fields["clientUri"] = checkClientURI(*in.ClientURI); fields["clientUri"] == "" {
				delete(fields, "clientUri")
			}
		}
		if in.RedirectURIs != nil {
			if c.RedirectURIs, fields["redirectUris"] = s.checkRedirectURIs(*in.RedirectURIs); fields["redirectUris"] == "" {
				delete(fields, "redirectUris")
			}
		}
		if in.Scopes != nil {
			if c.Scopes, fields["scopes"] = checkScopes(*in.Scopes); fields["scopes"] == "" {
				delete(fields, "scopes")
			}
		}
		if in.KeycloakClientID != nil {
			c.KeycloakClientID = strings.TrimSpace(*in.KeycloakClientID)
		}
		if len(fields) > 0 {
			return &ValidationError{Fields: fields}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE oauth_client SET name = $2, client_uri = $3, redirect_uris = $4, scopes = $5, keycloak_client_id = $6
			WHERE id = $1`, id, c.Name, c.ClientURI, c.RedirectURIs, c.Scopes, c.KeycloakClientID); err != nil {
			return err
		}
		if err := narrowGrants(ctx, tx, id, c.Scopes); err != nil {
			return err
		}
		if out, err = s.clientIn(ctx, tx, id, false); err != nil {
			return err
		}
		return record(ctx, tx, "oauth.client.updated", out, actor, "")
	})
	return out, lsn, err
}

// narrowGrants keeps every grant of a client, and every token exchanged from
// one, to the scopes the client may still ask for.
func narrowGrants(ctx context.Context, tx db.DBTX, clientID uuid.UUID, scopes []string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE api_token SET scopes = ARRAY(SELECT s FROM unnest(scopes) s WHERE s = ANY($2))
		WHERE client_id = $1 AND NOT scopes <@ $2`, clientID, scopes); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE oauth_exchanged_token x SET scopes = ARRAY(SELECT s FROM unnest(x.scopes) s WHERE s = ANY($2))
		FROM api_token t
		WHERE t.id = x.grant_id AND t.client_id = $1 AND NOT x.scopes <@ $2`, clientID, scopes)
	return err
}

// ApproveClient makes a pending client active with the scopes it registered,
// as allowing it on the consent page does.
func (s *Service) ApproveClient(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*Client, db.LSN, error) {
	var out *Client
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = s.approveIn(ctx, tx, id, actor)
		return err
	})
	return out, lsn, err
}

func (s *Service) approveIn(ctx context.Context, tx db.DBTX, id uuid.UUID, actor uuid.UUID) (*Client, error) {
	c, err := s.clientIn(ctx, tx, id, true)
	if err != nil {
		return nil, err
	}
	if c.Status == StatusActive {
		return c, nil
	}
	if _, err := tx.Exec(ctx, `UPDATE oauth_client SET status = 'active' WHERE id = $1`, id); err != nil {
		return nil, err
	}
	c.Status = StatusActive
	return c, record(ctx, tx, "oauth.client.approved", c, actor, "")
}

// RotateClientSecret issues a new secret; the old one stops at once.
func (s *Service) RotateClientSecret(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*Client, db.LSN, error) {
	secret, digest, err := auth.GeneratePrefixed(auth.ClientSecretPrefix)
	if err != nil {
		return nil, 0, err
	}
	var out *Client
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		c, err := s.clientIn(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if !c.Confidential {
			return ErrPublicClient
		}
		if _, err := tx.Exec(ctx, `UPDATE oauth_client SET secret_hash = $2 WHERE id = $1`, id, digest); err != nil {
			return err
		}
		out = c
		return record(ctx, tx, "oauth.client.secret_rotated", c, actor, "")
	})
	if err != nil {
		return nil, 0, err
	}
	out.Secret = secret
	return out, lsn, nil
}

// DeleteClient removes a client: every grant of it ends, and its partner
// webhook goes with its log.
func (s *Service) DeleteClient(ctx context.Context, id uuid.UUID, actor uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		c, err := s.clientIn(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM oauth_client WHERE id = $1`, id); err != nil {
			return err
		}
		return record(ctx, tx, "oauth.client.deleted", c, actor, "")
	})
}

// record writes what was done to a client, never a secret.
func record(ctx context.Context, tx db.DBTX, action string, c *Client, actor uuid.UUID, ip string) error {
	return audit.Write(ctx, tx, audit.Entry{Action: action, TargetType: "oauth_client", TargetID: &c.ID, Actor: actor, IP: ip, Data: map[string]any{
		"name": c.Name, "clientUri": c.ClientURI, "redirectUris": c.RedirectURIs, "scopes": c.Scopes, "status": c.Status,
	}})
}

// RegistrationRequest is RFC 7591's client metadata, in its names.
type RegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	Scope                   string   `json:"scope,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
}

// RegistrationResponse is what a registered client is told, its secret once.
type RegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	Scope                   string   `json:"scope"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
}

// grantTypes are the grant types a client may register for.
var grantTypes = []string{GrantAuthorizationCode, GrantRefreshToken, GrantTokenExchange}

// Register is RFC 7591 dynamic registration. The client is pending: nothing it
// asks for works until an administrator allows it, and it goes after a day.
func (s *Service) Register(ctx context.Context, in RegistrationRequest, ip string) (*RegistrationResponse, error) {
	if !s.RegistrationOpen() {
		name := strings.TrimSpace(in.ClientName)
		if name == "" {
			name = "the app"
		}
		return nil, oauthError(http.StatusForbidden, "access_denied", fmt.Sprintf(
			"This Armature registers connected apps only by hand. Ask an Armature administrator to add %s under Settings, Connected apps.", name))
	}
	invalid := func(description string) error {
		return oauthError(http.StatusBadRequest, "invalid_client_metadata", description)
	}
	name, problem := checkName(in.ClientName)
	if problem != "" {
		return nil, invalid(problem)
	}
	clientURI, problem := checkClientURI(in.ClientURI)
	if problem != "" {
		return nil, invalid(problem)
	}
	redirects, problem := s.checkRedirectURIs(in.RedirectURIs)
	if problem != "" {
		return nil, oauthError(http.StatusBadRequest, "invalid_redirect_uri", problem)
	}
	requested := splitScope(in.Scope)
	if len(requested) == 0 {
		requested = []string{ScopeRead}
	}
	scopes, problem := checkScopes(requested)
	if problem != "" {
		return nil, invalid(problem)
	}
	method := in.TokenEndpointAuthMethod
	if method == "" {
		method = AuthBasic
	}
	if !slices.Contains([]string{AuthBasic, AuthPost, AuthNone}, method) {
		return nil, invalid("Choose client_secret_basic, client_secret_post or none as token_endpoint_auth_method.")
	}
	grants := in.GrantTypes
	if len(grants) == 0 {
		grants = []string{GrantAuthorizationCode, GrantRefreshToken}
	}
	for _, g := range grants {
		if !slices.Contains(grantTypes, g) {
			return nil, invalid(fmt.Sprintf("Armature does not offer the %s grant. Choose from %s.", g, strings.Join(grantTypes, ", ")))
		}
	}
	responses := in.ResponseTypes
	if len(responses) == 0 {
		responses = []string{"code"}
	}
	for _, r := range responses {
		if r != "code" {
			return nil, invalid("Armature answers the code response type only.")
		}
	}

	var (
		secret string
		digest []byte
	)
	if method != AuthNone {
		var err error
		if secret, digest, err = auth.GeneratePrefixed(auth.ClientSecretPrefix); err != nil {
			return nil, err
		}
	}
	out := &RegistrationResponse{
		ClientSecret: secret, ClientName: name, ClientURI: clientURI, RedirectURIs: redirects,
		Scope: joinScope(scopes), TokenEndpointAuthMethod: method, GrantTypes: grants, ResponseTypes: responses,
	}
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := deletePending(ctx, tx, s.now()); err != nil {
			return err
		}
		var (
			id      uuid.UUID
			created time.Time
		)
		if err := tx.QueryRow(ctx, `
			INSERT INTO oauth_client (name, client_uri, redirect_uris, scopes, secret_hash, auth_method, status)
			VALUES ($1, $2, $3, $4, $5, $6, 'pending')
			RETURNING id, created_at`,
			name, clientURI, redirects, scopes, digest, method).Scan(&id, &created); err != nil {
			return err
		}
		out.ClientID, out.ClientIDIssuedAt = id.String(), created.Unix()
		c := &Client{ID: id, Name: name, ClientURI: clientURI, RedirectURIs: redirects, Scopes: scopes, Status: StatusPending}
		return record(ctx, tx, "oauth.client.registered", c, uuid.Nil, ip)
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// redirectHost is the host a consent page names as where the browser goes.
func redirectHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		return "[" + host + "]"
	}
	return host
}
