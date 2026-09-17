package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/auth"
)

// maxBodyBytes caps request bodies so a malformed or hostile client cannot make
// the server allocate without bound.
const maxBodyBytes = 1 << 20 // 1 MiB

// decodeJSON reads and validates a JSON request body, rejecting unknown fields
// so that a typo in a client payload is an error rather than a silent no-op.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			return ErrBadRequest("Request body is too large.")
		case errors.Is(err, io.EOF):
			return ErrBadRequest("Request body is required.")
		default:
			return ErrBadRequest("Request body is not valid JSON: " + err.Error())
		}
	}
	if dec.More() {
		return ErrBadRequest("Request body must contain a single JSON object.")
	}
	return nil
}

// setSessionCookie writes the session cookie. HttpOnly keeps it out of reach of
// scripts, SameSite=Lax blocks it on cross-site form posts, and Secure is on
// everywhere but local development.
func (s *Server) setSessionCookie(w http.ResponseWriter, secret string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.CookieName,
		Value:    secret,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

type signupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	OrgName  string `json:"orgName"`
	OrgSlug  string `json:"orgSlug,omitempty"`
}

// handleSignupOpen lets the sign-in page offer sign-up only where it would work.
func (s *Server) handleSignupOpen(w http.ResponseWriter, r *http.Request) {
	open, err := s.Auth.SignupAllowed(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"open": open})
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req signupRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	creds, err := s.Auth.Signup(r.Context(), auth.SignupInput{
		Email:     req.Email,
		Password:  req.Password,
		Name:      req.Name,
		OrgName:   req.OrgName,
		OrgSlug:   req.OrgSlug,
		UserAgent: r.UserAgent(),
		IP:        clientIP(r),
	})
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}

	s.setSessionCookie(w, creds.SessionSecret, creds.ExpiresAt)
	// A brand new account reads its own organization immediately; pin it so the
	// first page load is not served by a replica that has not caught up.
	s.Fresh.Note(r.Context(), "s:"+creds.Principal.SessionID.String(), creds.LSN)
	respondJSON(w, r, http.StatusCreated, map[string]any{"principal": creds.Principal})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	guessing := "login:" + auth.NormalizeEmail(req.Email)
	if credentialTries.tooMany(w, r, guessing, "Too many attempts for that address. Wait a few minutes and try again.") {
		return
	}

	creds, err := s.Auth.Login(r.Context(), req.Email, req.Password, r.UserAgent(), clientIP(r))
	if err != nil {
		credentialTries.record(guessing)
		respondError(w, r, err)
		return
	}

	s.setSessionCookie(w, creds.SessionSecret, creds.ExpiresAt)
	s.Fresh.Note(r.Context(), "s:"+creds.Principal.SessionID.String(), creds.LSN)
	respondJSON(w, r, http.StatusOK, map[string]any{"principal": creds.Principal})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if p != nil && p.SessionID != nil {
		if err := s.Auth.Logout(r.Context(), *p.SessionID); err != nil {
			respondError(w, r, err)
			return
		}
	}
	s.clearSessionCookie(w)
	respondNoContent(w)
}

// handleMe returns the caller plus every organization they can switch to, which
// is what the client needs to render the account menu in one request.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	memberships, err := s.Auth.Memberships(r.Context(), p.User.ID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if memberships == nil {
		memberships = []auth.Membership{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{
		"principal":     p,
		"organizations": memberships,
	})
}

type switchOrgRequest struct {
	Slug string `json:"slug"`
}

func (s *Server) handleSwitchOrg(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	if p.SessionID == nil {
		respondError(w, r, ErrBadRequest("An API token is bound to one organization and cannot switch."))
		return
	}

	var req switchOrgRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	org, lsn, err := s.Auth.SwitchOrg(r.Context(), *p.SessionID, p.User.ID, req.Slug)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"organization": org})
}

type inviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	var req inviteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	p := PrincipalFrom(r.Context())

	invite, secret, err := s.Auth.CreateInvite(r.Context(), req.Email, auth.OrgRole(req.Role), p.User.ID, 7*24*time.Hour)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	// The secret is returned once. Delivering it by email is the notify
	// package's job; returning it here also keeps the flow testable and lets an
	// admin copy a link when mail is not configured.
	respondJSON(w, r, http.StatusCreated, map[string]any{"invite": invite, "token": secret})
}

func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) {
	invites, err := s.Auth.ListInvites(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	if invites == nil {
		invites = []auth.Invite{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"invites": invites})
}

func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("inviteID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid invitation id."))
		return
	}
	if err := s.Auth.RevokeInvite(r.Context(), id); err != nil {
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}

type acceptInviteRequest struct {
	Token    string `json:"token"`
	Name     string `json:"name,omitempty"`
	Password string `json:"password,omitempty"`
}

func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	var req acceptInviteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	tried := "invite:" + clientIP(r)
	if credentialTries.tooMany(w, r, tried, "Too many invitations were tried from here. Wait a few minutes and try again.") {
		return
	}

	in := auth.AcceptInviteInput{
		Secret:    req.Token,
		Name:      req.Name,
		Password:  req.Password,
		UserAgent: r.UserAgent(),
		IP:        clientIP(r),
	}
	// An already signed-in user joins with their existing account.
	if p := PrincipalFrom(r.Context()); p != nil {
		in.UserID = &p.User.ID
		in.Proof = p.Proof
		if p.Org != nil {
			in.SessionOrg = &p.Org.ID
		}
	}

	creds, err := s.Auth.AcceptInvite(r.Context(), in)
	if err != nil {
		credentialTries.record(tried)
		respondError(w, r, asValidationError(err))
		return
	}
	s.setSessionCookie(w, creds.SessionSecret, creds.ExpiresAt)
	s.Fresh.Note(r.Context(), "s:"+creds.Principal.SessionID.String(), creds.LSN)
	respondJSON(w, r, http.StatusOK, map[string]any{"principal": creds.Principal})
}

type createTokenRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes,omitempty"`
	// Projects confines the token to these project keys. Empty lets it reach
	// wherever its owner does, which is what every token did before.
	Projects  []string   `json:"projects,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func (s *Server) handleCreateAPIToken(w http.ResponseWriter, r *http.Request) {
	var req createTokenRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	p := PrincipalFrom(r.Context())

	token, lsn, err := s.Auth.CreateAPIToken(r.Context(), p.User.ID, req.Name, req.Scopes, req.Projects, req.ExpiresAt)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"token": token})
}

func (s *Server) handleListAPITokens(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	tokens, err := s.Auth.ListAPITokens(r.Context(), p.User.ID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if tokens == nil {
		tokens = []auth.APIToken{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"tokens": tokens})
}

func (s *Server) handleRevokeAPIToken(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("tokenID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid token id."))
		return
	}
	p := PrincipalFrom(r.Context())
	lsn, err := s.Auth.RevokeAPIToken(r.Context(), p.User.ID, id)
	if err != nil {
		respondError(w, r, ErrNotFound("That token was not found."))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// asValidationError turns the service layer's plain validation errors into a
// 422 rather than letting them fall through to a 500. Domain errors that
// toAPIError already understands are passed straight through.
func asValidationError(err error) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return err
	}
	// A database or a timeout failing is not the caller's input, and its words
	// are ours rather than something written for them to read.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) || errors.Is(err, pgx.ErrNoRows) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return ErrInternal(err)
	}
	// A domain error that already has a status keeps it. Only the ones with no
	// mapping fall through, and at this point those are input problems.
	if toAPIError(err).Status != http.StatusInternalServerError {
		return err
	}
	// Their messages are written for the user, so they are safe to pass on.
	return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: capitalize(err.Error())}
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}
