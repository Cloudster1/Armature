package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
)

// Authorization is an /oauth/authorize request whose client and return
// address are known. Anything else wrong with it is answered at that address.
type Authorization struct {
	client      *Client
	redirectURI string
	state       string
	scopes      []string
	challenge   string
	resource    string
	prompt      bool
	// problem is answered at the return address instead of a page.
	problem *Error
}

// Person is who is asked to connect an app.
type Person struct {
	ID   uuid.UUID
	Name string
	// Agent is false for a desk's customer, who has no account to lend.
	Agent bool
	// Administers is the person's own standing, before any narrowing.
	Administers bool
}

// Consent is what the consent page shows.
type Consent struct {
	Client       ConsentClient  `json:"client"`
	RedirectHost string         `json:"redirectHost"`
	Pending      bool           `json:"pending"`
	Person       string         `json:"person"`
	Scopes       []ScopeConsent `json:"scopes"`
	// MayAllow is false when the person may only cancel, or not even that,
	// and Refusal says why.
	MayAllow  bool   `json:"mayAllow"`
	MayCancel bool   `json:"mayCancel"`
	Refusal   string `json:"refusal,omitempty"`
}

// ConsentClient is the app as the consent page names it.
type ConsentClient struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	ClientURI string    `json:"clientUri,omitempty"`
}

// ScopeConsent is one scope in the page's words.
type ScopeConsent struct {
	Scope string `json:"scope"`
	Words string `json:"words"`
}

// RefusedError is a decision the person may not make, with the sentence why.
type RefusedError struct{ Sentence string }

func (e *RefusedError) Error() string { return e.Sentence }

// Authorize reads an /oauth/authorize query. An unknown client and a return
// address it did not register read the same, and are never redirected to.
func (s *Service) Authorize(ctx context.Context, rawQuery string) (*Authorization, error) {
	q, err := url.ParseQuery(rawQuery)
	if err != nil {
		return nil, ErrNotConnected
	}
	id, err := uuid.Parse(q.Get("client_id"))
	if err != nil {
		return nil, ErrNotConnected
	}
	client, err := s.GetClient(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return nil, ErrNotConnected
	}
	if err != nil {
		return nil, err
	}
	a := &Authorization{client: client, redirectURI: q.Get("redirect_uri"), state: q.Get("state")}
	if a.redirectURI == "" && len(client.RedirectURIs) == 1 {
		a.redirectURI = client.RedirectURIs[0]
	}
	if len(q["redirect_uri"]) > 1 || !slices.Contains(client.RedirectURIs, a.redirectURI) {
		return nil, ErrNotConnected
	}

	a.scopes = normalScopes(splitScope(q.Get("scope")))
	a.challenge = q.Get("code_challenge")
	a.resource = q.Get("resource")
	a.prompt = slices.Contains(strings.Fields(q.Get("prompt")), "consent")
	for _, name := range []string{"response_type", "client_id", "state", "scope", "code_challenge", "code_challenge_method", "resource"} {
		if len(q[name]) > 1 {
			a.problem = oauthError(http.StatusBadRequest, "invalid_request", fmt.Sprintf("Send %s once.", name))
			return a, nil
		}
	}
	switch {
	case q.Get("response_type") != "code":
		a.problem = oauthError(http.StatusBadRequest, "unsupported_response_type", "Armature answers response_type=code only.")
	case a.state == "":
		a.problem = oauthError(http.StatusBadRequest, "invalid_request", "Send a state of your own, so the answer can be tied to the request.")
	case a.challenge == "" || q.Get("code_challenge_method") != "S256":
		a.problem = oauthError(http.StatusBadRequest, "invalid_request", "Armature requires PKCE with S256.")
	case !validScopeRequest(splitScope(q.Get("scope")), client.Scopes):
		a.problem = oauthError(http.StatusBadRequest, "invalid_scope", fmt.Sprintf(
			"Ask for read and what else %s needs, from %s.", client.Name, strings.Join(client.Scopes, ", ")))
	case a.resource != "" && !slices.Contains(s.Resources(), a.resource):
		a.problem = oauthError(http.StatusBadRequest, "invalid_target", fmt.Sprintf("The resource is %s.", strings.Join(s.Resources(), " or ")))
	}
	return a, nil
}

// validScopeRequest says whether every scope asked is one the client may ask
// for, read among them.
func validScopeRequest(asked, allowed []string) bool {
	return slices.Contains(asked, ScopeRead) && subset(asked, allowed)
}

func (a *Authorization) asksWebhooks() bool { return slices.Contains(a.scopes, ScopeWebhooks) }

// Consent decides what the page shows the person, or where the browser goes
// at once: an error answered at the app, or a new code for an app the person
// already allowed.
func (s *Service) Consent(ctx context.Context, a *Authorization, person Person) (*Consent, string, db.LSN, error) {
	c, to := s.rules(a, person)
	if to != "" || !c.MayAllow || c.Pending || a.prompt || a.asksWebhooks() {
		return c, to, 0, nil
	}
	var covered bool
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM api_token
			               WHERE user_id = $1 AND client_id = $2 AND expires_at > now() AND scopes @> $3)`,
			person.ID, a.client.ID, a.scopes).Scan(&covered)
	})
	if err != nil || !covered {
		return c, "", 0, err
	}
	to, lsn, err := s.issueCode(ctx, a, person)
	return nil, to, lsn, err
}

// rules are the consent page's, in order: who may not allow at all, what is
// answered at the app, and what only an administrator may allow.
func (s *Service) rules(a *Authorization, person Person) (*Consent, string) {
	c := &Consent{
		Client:       ConsentClient{ID: a.client.ID, Name: a.client.Name, ClientURI: a.client.ClientURI},
		RedirectHost: redirectHost(a.redirectURI),
		Pending:      a.client.Status == StatusPending,
		Person:       person.Name,
		Scopes:       []ScopeConsent{},
	}
	for _, scope := range a.scopes {
		c.Scopes = append(c.Scopes, ScopeConsent{Scope: scope, Words: Words(scope, a.client.Name)})
	}
	name := a.client.Name
	switch {
	case !person.Agent:
		c.Refusal, c.MayCancel = "A connected app acts for an Armature account. Sign in with yours.", true
	case a.problem != nil:
		return nil, a.errorRedirect(s.issuer, a.problem)
	case c.Pending && !person.Administers:
		c.Refusal = fmt.Sprintf("%s is not connected to Armature yet. An Armature administrator connects it first.", name)
	case a.asksWebhooks() && !person.Administers:
		c.Refusal, c.MayCancel = fmt.Sprintf(
			"Sending Armature's events to %s needs an Armature administrator. Sign in as one, or ask one to connect %s.", name, name), true
	default:
		c.MayAllow, c.MayCancel = true, true
	}
	return c, ""
}

// Decide carries out the person's Allow or Cancel and says where the browser
// goes. Allowing a pending client, which only an administrator may, makes it
// active with the scopes it registered.
func (s *Service) Decide(ctx context.Context, a *Authorization, person Person, allow bool) (string, db.LSN, error) {
	c, to := s.rules(a, person)
	switch {
	case to != "":
		return to, 0, nil
	case !allow && !c.MayCancel, allow && !c.MayAllow:
		return "", 0, &RefusedError{Sentence: c.Refusal}
	case !allow:
		return a.errorRedirect(s.issuer, oauthError(http.StatusBadRequest, "access_denied", person.Name+" did not connect "+a.client.Name+".")), 0, nil
	}
	return s.issueCode(ctx, a, person)
}

// issueCode makes a code for the request and the address that carries it.
func (s *Service) issueCode(ctx context.Context, a *Authorization, person Person) (string, db.LSN, error) {
	code, digest, err := auth.GeneratePrefixed(auth.CodePrefix)
	if err != nil {
		return "", 0, err
	}
	lsn, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		if a.client.Status == StatusPending {
			if _, err := s.approveIn(ctx, tx, a.client.ID, person.ID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM oauth_code WHERE expires_at < now() - interval '1 day'`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO oauth_code (code_hash, client_id, user_id, scopes, redirect_uri, challenge, resource, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			digest, a.client.ID, person.ID, a.scopes, a.redirectURI, a.challenge, a.resource, s.now().Add(CodeTTL))
		return err
	})
	if err != nil {
		return "", 0, err
	}
	return a.redirect(url.Values{"code": {code}, "iss": {s.issuer}}), lsn, nil
}

// errorRedirect answers a problem at the app's return address.
func (a *Authorization) errorRedirect(issuer string, e *Error) string {
	return a.redirect(url.Values{"error": {e.Code}, "error_description": {e.Description}, "iss": {issuer}})
}

// redirect is the return address with the answer and the state added to the
// query it already has.
func (a *Authorization) redirect(add url.Values) string {
	u, err := url.Parse(a.redirectURI)
	if err != nil {
		return a.redirectURI
	}
	q := u.Query()
	for k, v := range add {
		q[k] = v
	}
	if a.state != "" {
		q.Set("state", a.state)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// grantCreated records a person connecting an app.
func grantCreated(ctx context.Context, tx db.DBTX, grantID uuid.UUID, clientID uuid.UUID, person uuid.UUID, scopes []string, via string) error {
	return audit.Write(ctx, tx, audit.Entry{Action: "oauth.grant.created", TargetType: "api_token", TargetID: &grantID, Actor: person,
		Data: map[string]any{"clientId": clientID, "scopes": scopes, "via": via}})
}

// personIn reads who a code or a token is for, refusing somebody switched off.
func personIn(ctx context.Context, tx db.DBTX, id uuid.UUID) error {
	var (
		active   bool
		standing *string
	)
	err := tx.QueryRow(ctx, `SELECT is_active, standing FROM app_user WHERE id = $1`, id).Scan(&active, &standing)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (!active || standing == nil || !auth.OrgRole(*standing).IsAgent())) {
		return ended()
	}
	return err
}
