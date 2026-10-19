// Package oauth makes Armature an OAuth 2.0 authorization server for partner
// tools: a connected app gets a person's token with their consent, by
// authorization code with PKCE, and the token never exceeds the person.
//
// docs/oauth.md is the contract partners build against; the rules here follow
// it, and the OAuth endpoints answer in OAuth's own error shape.
package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/oidc"
)

const (
	// CodeTTL is how long an authorization code waits to be redeemed.
	CodeTTL = 60 * time.Second
	// AccessTokenTTL is the most an access token lives, exchanged ones included.
	AccessTokenTTL = time.Hour
	// RefreshTokenTTL is how long a refresh token lives from its issue; each
	// refresh gives the grant another one.
	RefreshTokenTTL = 90 * 24 * time.Hour
	// PendingClientTTL is how long a self-registered client waits for an
	// administrator before it is deleted.
	PendingClientTTL = 24 * time.Hour
	// MaxRedirectURIs and MaxNameLength bound what a client registers.
	MaxRedirectURIs = 5
	MaxNameLength   = 60
	// minVerifier and maxVerifier are RFC 7636's bounds on a code verifier.
	minVerifier = 43
	maxVerifier = 128
)

// The scopes a connected app asks for. Every one but the last is a permission
// name, which the token is narrowed to where a key is narrowed.
const (
	ScopeRead            = "read"
	ScopeIssueWrite      = "issue.write"
	ScopeIssueTransition = "issue.transition"
	ScopeCommentWrite    = "comment.write"
	// ScopeWebhooks lets the app manage its own webhook endpoint, for an
	// administrator only.
	ScopeWebhooks = "webhooks.partner"
)

// Scopes are every scope, in the order the consent page lists them.
var Scopes = []string{ScopeRead, ScopeIssueWrite, ScopeIssueTransition, ScopeCommentWrite, ScopeWebhooks}

// ResourceScopes are the scopes a protected resource's metadata names.
var ResourceScopes = []string{ScopeRead, ScopeIssueWrite, ScopeIssueTransition, ScopeCommentWrite}

// words is what the consent page says each scope lets the app do.
var words = map[string]string{
	ScopeRead:            "See the projects and issues you can see",
	ScopeIssueWrite:      "File and edit issues, and link pages to them",
	ScopeIssueTransition: "Move issues through their workflow",
	ScopeCommentWrite:    "Comment on issues",
	ScopeWebhooks:        "Send Armature's events to %s, from every project (administrators only)",
}

// allows is the same, as the end of "allow it to ...".
var allows = map[string]string{
	ScopeIssueWrite:      "file and edit issues",
	ScopeIssueTransition: "move issues through their workflow",
	ScopeCommentWrite:    "comment on issues",
	ScopeWebhooks:        "manage its webhook",
}

// Words is what the consent page says a scope lets the app named do.
func Words(scope, app string) string {
	w := words[scope]
	if strings.Contains(w, "%s") {
		return fmt.Sprintf(w, app)
	}
	return w
}

// MissingScope is the sentence refusing an app a write its grant does not
// allow, naming what to allow instead.
func MissingScope(app, scope string, held []string) string {
	if len(held) == 1 && held[0] == ScopeRead {
		return app + " was allowed to read only. Connect it again and allow it to change issues."
	}
	what := allows[scope]
	if what == "" {
		what = "do that"
	}
	return fmt.Sprintf("%s was not allowed to %s. Connect it again and allow it to %s.", app, what, what)
}

// Grant types and token types, as the RFCs spell them.
const (
	GrantAuthorizationCode = "authorization_code"
	GrantRefreshToken      = "refresh_token"
	GrantTokenExchange     = "urn:ietf:params:oauth:grant-type:token-exchange"
	TokenTypeAccess        = "urn:ietf:params:oauth:token-type:access_token"
)

// Client authentication methods.
const (
	AuthBasic = "client_secret_basic"
	AuthPost  = "client_secret_post"
	AuthNone  = "none"
)

// Registration is who may register a client: anybody, pending an
// administrator, or administrators alone.
type Registration string

const (
	RegistrationApprove Registration = "approve"
	RegistrationClosed  Registration = "closed"
)

// Valid says whether the setting is one Armature knows.
func (r Registration) Valid() bool { return r == RegistrationApprove || r == RegistrationClosed }

// Error is an OAuth error, answered as RFC 6749 section 5.2 says: a code from
// the RFCs and a sentence for whoever reads the partner's log.
type Error struct {
	Status      int
	Code        string
	Description string
	// Basic asks for WWW-Authenticate: Basic, since the client sent Basic.
	Basic bool
}

func (e *Error) Error() string { return e.Code + ": " + e.Description }

func oauthError(status int, code, description string) *Error {
	return &Error{Status: status, Code: code, Description: description}
}

// ended is every refusal of a code or a refresh token: the partner can do
// nothing with the difference but connect again.
func ended() *Error {
	return oauthError(http.StatusBadRequest, "invalid_grant", "This connection has ended. Connect Armature again.")
}

var (
	// ErrNotFound is a client that is not here.
	ErrNotFound = errors.New("there is no such connected app")
	// ErrNotConnected is a client or a redirect address Armature does not
	// know, which the consent page never redirects for.
	ErrNotConnected = errors.New("this app is not connected to Armature. Ask an Armature administrator to connect it")
	// ErrPublicClient is a secret asked of a client that has none.
	ErrPublicClient = errors.New("a public client has no secret to rotate")
)

// ValidationError is a client's field refused, with a sentence for that field.
type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return "connected app refused: " + strings.Join(keys, ", ")
}

// Service keeps clients, codes and grants.
type Service struct {
	db *db.Cluster
	// issuer is ARMATURE_APP_URL, the base of every address in the metadata.
	issuer       string
	registration Registration
	// development lets a redirect address be any http one.
	development bool
	// provider verifies a Keycloak token for an exchange; nil turns it off.
	provider *oidc.Service
	now      func() time.Time
}

// NewService makes the service for the installation at issuer.
func NewService(cluster *db.Cluster, issuer string) *Service {
	return &Service{
		db: cluster, issuer: strings.TrimRight(issuer, "/"),
		registration: RegistrationApprove, now: time.Now,
	}
}

// WithRegistration sets who may register a client.
func (s *Service) WithRegistration(r Registration) *Service {
	if r.Valid() {
		s.registration = r
	}
	return s
}

// WithDevelopment lets a redirect address be plain http anywhere.
func (s *Service) WithDevelopment(on bool) *Service {
	s.development = on
	return s
}

// WithIdentityProvider lets a client trade a token of the installation's
// identity provider for a grant.
func (s *Service) WithIdentityProvider(p *oidc.Service) *Service {
	s.provider = p
	return s
}

// WithClock sets the time, for a test that waits out a code.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Issuer is the authorization server's identifier.
func (s *Service) Issuer() string { return s.issuer }

// RegistrationOpen says whether a partner may register itself.
func (s *Service) RegistrationOpen() bool { return s.registration == RegistrationApprove }

// Resources are the resource indicators a token may be asked for.
func (s *Service) Resources() []string {
	return []string{s.issuer + "/api/v1", s.issuer + "/api/v1/mcp"}
}

// knownScope says whether a scope is one Armature issues.
func knownScope(scope string) bool { return slices.Contains(Scopes, scope) }

// normalScopes sorts and removes repeats, in the order Scopes lists them.
func normalScopes(in []string) []string {
	out := []string{}
	for _, scope := range Scopes {
		if slices.Contains(in, scope) {
			out = append(out, scope)
		}
	}
	return out
}

// subset says whether every scope of a is in b.
func subset(a, b []string) bool {
	for _, scope := range a {
		if !slices.Contains(b, scope) {
			return false
		}
	}
	return true
}

// splitScope reads a space separated scope parameter.
func splitScope(raw string) []string { return strings.Fields(raw) }

// joinScope writes one.
func joinScope(scopes []string) string { return strings.Join(scopes, " ") }

// deletePending tidies away the clients nobody allowed in time; the table is
// small enough that doing it on write is the whole story.
func deletePending(ctx context.Context, tx db.DBTX, now time.Time) error {
	_, err := tx.Exec(ctx, `DELETE FROM oauth_client WHERE status = 'pending' AND created_at < $1`, now.Add(-PendingClientTTL))
	return err
}
