package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/oauth"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/webhook"
)

// The OAuth endpoints answer OAuth's error shape and take OAuth's parameter
// names (RFC 6749 section 5.2, RFC 7591 section 3.2.2); everything else here
// answers Armature's envelope.

// oauthErrorBody is how the token, revocation and registration endpoints refuse.
type oauthErrorBody struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// respondOAuth writes an OAuth error, or Armature's envelope for anything that
// is not one, such as the database failing.
func respondOAuth(w http.ResponseWriter, r *http.Request, err error) {
	var e *oauth.Error
	if !errors.As(err, &e) {
		respondError(w, r, err)
		return
	}
	if e.Basic {
		w.Header().Set("WWW-Authenticate", `Basic realm="armature"`)
	}
	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, r, e.Status, oauthErrorBody{Error: e.Code, ErrorDescription: e.Description})
}

// tokenRequest documents the token endpoint's form; the handler reads it as a form.
type tokenRequest struct {
	GrantType          string `json:"grant_type"`
	Code               string `json:"code,omitempty"`
	RedirectURI        string `json:"redirect_uri,omitempty"`
	CodeVerifier       string `json:"code_verifier,omitempty"`
	RefreshToken       string `json:"refresh_token,omitempty"`
	Scope              string `json:"scope,omitempty"`
	Resource           string `json:"resource,omitempty"`
	SubjectToken       string `json:"subject_token,omitempty"`
	SubjectTokenType   string `json:"subject_token_type,omitempty"`
	RequestedTokenType string `json:"requested_token_type,omitempty"`
	// ArmatureExpiresIn asks an exchanged token to live fewer seconds than the hour.
	ArmatureExpiresIn string `json:"armature_expires_in,omitempty"`
	ClientID          string `json:"client_id,omitempty"`
	ClientSecret      string `json:"client_secret,omitempty"`
}

// revokeRequest documents the revocation endpoint's form.
type revokeRequest struct {
	Token         string `json:"token"`
	TokenTypeHint string `json:"token_type_hint,omitempty"`
	ClientID      string `json:"client_id,omitempty"`
	ClientSecret  string `json:"client_secret,omitempty"`
}

// readForm reads an OAuth endpoint's form body, which nothing else here takes.
func readForm(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if kind := r.Header.Get("Content-Type"); !formBody(kind) {
		return &oauth.Error{Status: http.StatusBadRequest, Code: "invalid_request", Description: "Send the parameters as application/x-www-form-urlencoded."}
	}
	if err := r.ParseForm(); err != nil {
		return &oauth.Error{Status: http.StatusBadRequest, Code: "invalid_request", Description: "The body is not a form Armature can read."}
	}
	return nil
}

// tokenMissKey is what a failed token request counts against: the client it
// named, or the caller's address when it named none.
func tokenMissKey(r *http.Request) string {
	if id := r.PostForm.Get("client_id"); id != "" {
		return "client:" + id
	}
	if user, _, ok := r.BasicAuth(); ok {
		return "client:" + user
	}
	return "address:" + clientIP(r)
}

// counted says whether a refusal is a miss the brake counts: a guessed secret,
// code or refresh token, not a malformed request.
func counted(err error) bool {
	var e *oauth.Error
	return errors.As(err, &e) && (e.Code == "invalid_client" || e.Code == "invalid_grant")
}

func (s *Server) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	if err := readForm(w, r); err != nil {
		respondOAuth(w, r, err)
		return
	}
	key := tokenMissKey(r)
	if tokenMisses.tooMany(w, r, key, "Too many refused token requests for this app. Wait a few minutes and try again.") {
		return
	}
	cred, err := oauth.ReadCredentials(r.Header.Get("Authorization"), r.PostForm)
	if err == nil {
		var out *oauth.TokenResponse
		if out, err = s.OAuth.Token(r.Context(), cred, r.PostForm); err == nil {
			w.Header().Set("Cache-Control", "no-store")
			respondJSON(w, r, http.StatusOK, out)
			return
		}
	}
	if counted(err) {
		tokenMisses.record(key)
	}
	respondOAuth(w, r, err)
}

func (s *Server) handleOAuthRevoke(w http.ResponseWriter, r *http.Request) {
	if err := readForm(w, r); err != nil {
		respondOAuth(w, r, err)
		return
	}
	key := tokenMissKey(r)
	if tokenMisses.tooMany(w, r, key, "Too many refused requests for this app. Wait a few minutes and try again.") {
		return
	}
	cred, err := oauth.ReadCredentials(r.Header.Get("Authorization"), r.PostForm)
	if err == nil {
		if err = s.OAuth.Revoke(r.Context(), cred, r.PostForm.Get("token")); err == nil {
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	if counted(err) {
		tokenMisses.record(key)
	}
	respondOAuth(w, r, err)
}

func (s *Server) handleOAuthRegister(w http.ResponseWriter, r *http.Request) {
	if !registrations.gate(w, r, "Too many apps registered from this address. Wait a few minutes and try again.") {
		return
	}
	var req oauth.RegistrationRequest
	// RFC 7591 lets a client send metadata Armature does not use, such as a
	// logo; it is read past rather than refused.
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := dec.Decode(&req); err != nil {
		respondOAuth(w, r, &oauth.Error{Status: http.StatusBadRequest, Code: "invalid_client_metadata", Description: "Send the client's metadata as one JSON object."})
		return
	}
	out, err := s.OAuth.Register(r.Context(), req, clientIP(r))
	if err != nil {
		respondOAuth(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	respondJSON(w, r, http.StatusCreated, out)
}

// metadataMaxAge is how long a partner may keep the metadata documents.
const metadataMaxAge = 5 * time.Minute

func (s *Server) handleServerMetadata(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age="+itoa(int(metadataMaxAge/time.Second)))
	respondJSON(w, r, http.StatusOK, s.OAuth.Server())
}

func (s *Server) handleResourceMetadata(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age="+itoa(int(metadataMaxAge/time.Second)))
	respondJSON(w, r, http.StatusOK, s.OAuth.Resource())
}

// authorizeQuery is /oauth/authorize's, which the consent page passes on whole.
var authorizeQuery = []param{
	{name: "response_type", description: "code"},
	{name: "client_id"},
	{name: "redirect_uri", description: "One the app registered, byte for byte."},
	{name: "scope", description: "Spaced scopes, read among them."},
	{name: "state", description: "The app's own, returned with the answer."},
	{name: "code_challenge", description: "The base64url SHA-256 of the app's verifier."},
	{name: "code_challenge_method", description: "S256"},
	{name: "resource", description: "The API, or its MCP endpoint (RFC 8707)."},
	{name: "prompt", description: "consent shows the page to a person who allowed the app already."},
}

// consentAnswer is either the page to show or where the browser goes at once.
type consentAnswer struct {
	Consent    *oauth.Consent `json:"consent,omitempty"`
	RedirectTo string         `json:"redirectTo,omitempty"`
}

// consentRequest is the person's decision about the query the page was opened with.
type consentRequest struct {
	Query    string `json:"query"`
	Decision string `json:"decision"`
}

// consentFor reads who is asked and the authorize query, answering the caller
// when either is not there.
func (s *Server) consentFor(w http.ResponseWriter, r *http.Request, query string) (*oauth.Authorization, oauth.Person, bool) {
	p := PrincipalFrom(r.Context())
	switch {
	case p == nil:
		respondError(w, r, ErrUnauthorized("Sign in to connect the app."))
		return nil, oauth.Person{}, false
	case p.TokenID != nil:
		respondError(w, r, ErrSessionOnly())
		return nil, oauth.Person{}, false
	case p.MustEnrol:
		respondError(w, r, &APIError{Status: http.StatusForbidden, Code: "second_factor_required",
			Message: "Two-step sign-in is required for your account. Set it up from the page you are shown, or sign out."})
		return nil, oauth.Person{}, false
	}
	a, err := s.OAuth.Authorize(r.Context(), query)
	if err != nil {
		respondError(w, r, err)
		return nil, oauth.Person{}, false
	}
	return a, oauth.Person{
		ID: p.User.ID, Name: p.User.Name,
		Agent:       p.Role.IsAgent() && p.PortalDeskID == nil,
		Administers: PermsFrom(r.Context()).CanInOrg(perm.OrgAdminister),
	}, true
}

func (s *Server) handleOAuthConsent(w http.ResponseWriter, r *http.Request) {
	a, person, ok := s.consentFor(w, r, r.URL.RawQuery)
	if !ok {
		return
	}
	consent, to, lsn, err := s.OAuth.Consent(r.Context(), a, person)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, consentAnswer{Consent: consent, RedirectTo: to})
}

func (s *Server) handleOAuthDecide(w http.ResponseWriter, r *http.Request) {
	if PrincipalFrom(r.Context()) == nil {
		respondError(w, r, ErrUnauthorized("Sign in to connect the app."))
		return
	}
	var req consentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.Decision != "allow" && req.Decision != "deny" {
		respondError(w, r, ErrValidation(map[string]string{"decision": "Decide allow or deny."}))
		return
	}
	a, person, ok := s.consentFor(w, r, req.Query)
	if !ok {
		return
	}
	to, lsn, err := s.OAuth.Decide(r.Context(), a, person, req.Decision == "allow")
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]string{"redirectTo": to})
}

func (s *Server) handleListOAuthClients(w http.ResponseWriter, r *http.Request) {
	clients, err := s.OAuth.ListClients(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"clients": clients})
}

func (s *Server) handleCreateOAuthClient(w http.ResponseWriter, r *http.Request) {
	var req oauth.ClientInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	client, lsn, err := s.OAuth.CreateClient(r.Context(), req, PrincipalFrom(r.Context()).User.ID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"client": client})
}

// clientID reads the path's client, answering the caller when it is not one.
func clientID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("clientID"))
	if err != nil {
		respondError(w, r, ErrNotFound("That connected app was not found."))
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) handleUpdateOAuthClient(w http.ResponseWriter, r *http.Request) {
	id, ok := clientID(w, r)
	if !ok {
		return
	}
	var req oauth.ClientChanges
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	client, lsn, err := s.OAuth.UpdateClient(r.Context(), id, req, PrincipalFrom(r.Context()).User.ID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"client": client})
}

func (s *Server) handleApproveOAuthClient(w http.ResponseWriter, r *http.Request) {
	id, ok := clientID(w, r)
	if !ok {
		return
	}
	client, lsn, err := s.OAuth.ApproveClient(r.Context(), id, PrincipalFrom(r.Context()).User.ID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"client": client})
}

func (s *Server) handleRotateOAuthClientSecret(w http.ResponseWriter, r *http.Request) {
	id, ok := clientID(w, r)
	if !ok {
		return
	}
	client, lsn, err := s.OAuth.RotateClientSecret(r.Context(), id, PrincipalFrom(r.Context()).User.ID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"client": client})
}

func (s *Server) handleDeleteOAuthClient(w http.ResponseWriter, r *http.Request) {
	id, ok := clientID(w, r)
	if !ok {
		return
	}
	lsn, err := s.OAuth.DeleteClient(r.Context(), id, PrincipalFrom(r.Context()).User.ID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// partnerOf is the connected app a partner webhook route acts for.
func partnerOf(r *http.Request) webhook.App {
	app := PrincipalFrom(r.Context()).App
	return webhook.App{ClientID: app.ClientID, Name: app.Name}
}

func (s *Server) handleGetPartnerWebhook(w http.ResponseWriter, r *http.Request) {
	endpoint, err := s.Webhooks.PartnerEndpoint(r.Context(), partnerOf(r).ClientID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"webhook": endpoint})
}

func (s *Server) handleCreatePartnerWebhook(w http.ResponseWriter, r *http.Request) {
	var req webhook.PartnerInput
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	app := partnerOf(r)
	made, lsn, err := s.Webhooks.CreatePartner(r.Context(), app, req, userFrom(r))
	if errors.Is(err, webhook.ErrPartnerExists) {
		respondError(w, r, &APIError{Status: http.StatusConflict, Code: "partner_webhook_exists",
			Message: app.Name + " has a webhook here already. Change it with PATCH /webhooks/partner, or remove it first."})
		return
	}
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"webhook": made})
}

func (s *Server) handleUpdatePartnerWebhook(w http.ResponseWriter, r *http.Request) {
	var req webhook.PartnerChanges
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	changed, lsn, err := s.Webhooks.UpdatePartner(r.Context(), partnerOf(r), req, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"webhook": changed})
}

func (s *Server) handleRotatePartnerWebhookSecret(w http.ResponseWriter, r *http.Request) {
	rotated, lsn, err := s.Webhooks.RotatePartnerSecret(r.Context(), partnerOf(r), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"webhook": rotated})
}

func (s *Server) handleTestPartnerWebhook(w http.ResponseWriter, r *http.Request) {
	delivery, lsn, err := s.Webhooks.TestPartner(r.Context(), partnerOf(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"delivery": delivery})
}

func (s *Server) handleDeletePartnerWebhook(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Webhooks.DeletePartner(r.Context(), partnerOf(r), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
