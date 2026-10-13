package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
)

// A second factor for password sign-in: an authenticator app, recovery codes
// for when the phone is gone, and the installation's rule about who needs one.

// secondFactorRoutes registers a person's own second factor. Session only: a
// token cannot add or take away what guards the password.
func (s *Server) secondFactorRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(requireSession)
		r.Get("/auth/me/second-factor", s.handleSecondFactorStatus)
		r.Post("/auth/me/second-factor", s.handleStartSecondFactor)
		r.Post("/auth/me/second-factor/confirm", s.handleConfirmSecondFactor)
		r.Post("/auth/me/second-factor/recovery-codes", s.handleRenewRecoveryCodes)
		// A POST, not a DELETE: it carries the code that proves it.
		r.Post("/auth/me/second-factor/remove", s.handleRemoveSecondFactor)
	})
}

// requireSecondFactor holds a session the installation requires a second
// factor of, for somebody without one: it may enrol, look or leave.
func requireSecondFactor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := PrincipalFrom(r.Context())
		if p == nil || !p.MustEnrol || reachableWhileHeld(r) {
			next.ServeHTTP(w, r)
			return
		}
		respondError(w, r, &APIError{Status: http.StatusForbidden, Code: "second_factor_required",
			Message: "Two-step sign-in is required for your account. Set it up from the page you are shown, or sign out."})
	})
}

// reachableWhileHeld names what a held session may still do.
func reachableWhileHeld(r *http.Request) bool {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1")
	switch {
	case path == "/auth/me" && r.Method == http.MethodGet,
		path == "/auth/logout", path == "/build",
		path == "/auth/me/second-factor", strings.HasPrefix(path, "/auth/me/second-factor/"),
		// Somebody held may still end a session that is not theirs to keep.
		path == "/auth/sessions", strings.HasPrefix(path, "/auth/sessions/"):
		return true
	}
	return false
}

type secondFactorLoginRequest struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

// handleLoginSecondFactor is the second half of a password sign-in, for
// somebody with a second factor. The session opens only here.
func (s *Server) handleLoginSecondFactor(w http.ResponseWriter, r *http.Request) {
	var req secondFactorLoginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	tried := "sign-in:" + clientIP(r)
	if secondFactorTries.tooMany(w, r, tried, "Too many codes were tried from here. Wait a few minutes and sign in again.") {
		return
	}
	creds, err := s.Auth.CompleteLogin(r.Context(), req.Challenge, req.Code, r.UserAgent(), clientIP(r))
	if err != nil {
		var refused *auth.CodeRefused
		switch {
		case errors.As(err, &refused):
			// A wrong code counts against the account as a wrong password does,
			// so new challenges do not buy new guesses.
			secondFactorTries.record(tried)
			credentialTries.record("login:" + refused.Email)
		case errors.Is(err, auth.ErrChallengeInvalid):
			secondFactorTries.record(tried)
		}
		respondError(w, r, err)
		return
	}
	s.setSessionCookie(w, creds.SessionSecret, creds.ExpiresAt)
	s.Fresh.Note(r.Context(), "s:"+creds.Principal.SessionID.String(), creds.LSN)
	respondJSON(w, r, http.StatusOK, map[string]any{"principal": creds.Principal})
}

func (s *Server) handleSecondFactorStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.Auth.SecondFactorStatusOf(r.Context(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"secondFactor": status})
}

// handleStartSecondFactor makes the secret for the app. It is the one answer
// that carries it; after confirming, nothing does.
func (s *Server) handleStartSecondFactor(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	enrolment, err := s.Auth.StartSecondFactor(r.Context(), p.User.ID, p.User.Email)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusCreated, map[string]any{"enrolment": enrolment})
}

type secondFactorCodeRequest struct {
	Code string `json:"code"`
}

// codeFrom reads the code a signed-in person typed, braked per account so a
// session left open is not a way to guess the factor away.
func codeFrom(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	var req secondFactorCodeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return "", "", false
	}
	tried := "account:" + userFrom(r).String()
	if secondFactorTries.tooMany(w, r, tried, "Too many wrong codes. Wait a few minutes and try again.") {
		return "", "", false
	}
	return req.Code, tried, true
}

// refuseCode answers a refused code, counting it when it was a wrong one.
func refuseCode(w http.ResponseWriter, r *http.Request, tried string, err error) {
	if errors.Is(err, auth.ErrSecondFactorCode) {
		secondFactorTries.record(tried)
	}
	respondError(w, r, err)
}

func (s *Server) handleConfirmSecondFactor(w http.ResponseWriter, r *http.Request) {
	code, tried, ok := codeFrom(w, r)
	if !ok {
		return
	}
	codes, lsn, err := s.Auth.ConfirmSecondFactor(r.Context(), userFrom(r), code, clientIP(r))
	if err != nil {
		refuseCode(w, r, tried, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"recoveryCodes": codes})
}

func (s *Server) handleRenewRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	code, tried, ok := codeFrom(w, r)
	if !ok {
		return
	}
	codes, lsn, err := s.Auth.RenewRecoveryCodes(r.Context(), userFrom(r), code, clientIP(r))
	if err != nil {
		refuseCode(w, r, tried, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"recoveryCodes": codes})
}

// handleRemoveSecondFactor takes the person's own factor away. Every other
// session of theirs ends; this one stays.
func (s *Server) handleRemoveSecondFactor(w http.ResponseWriter, r *http.Request) {
	code, tried, ok := codeFrom(w, r)
	if !ok {
		return
	}
	p := PrincipalFrom(r.Context())
	lsn, err := s.Auth.RemoveSecondFactor(r.Context(), p.User.ID, p.SessionID, code, clientIP(r))
	if err != nil {
		refuseCode(w, r, tried, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// handleRemoveMembersSecondFactor lets an administrator take away the factor
// of somebody who lost both the phone and the codes.
func (s *Server) handleRemoveMembersSecondFactor(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(r.PathValue("userID"))
	if err != nil {
		respondError(w, r, ErrNotFound("That person was not found."))
		return
	}
	p := PrincipalFrom(r.Context())
	lsn, err := s.Auth.RemoveMembersSecondFactor(r.Context(), p.Org.ID, userID, p.User.ID, p.Role, clientIP(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

type secondFactorRuleRequest struct {
	// Require is off, admins or everyone.
	Require auth.SecondFactorRule `json:"require"`
}

func (s *Server) handleSecondFactorRule(w http.ResponseWriter, r *http.Request) {
	rule, err := s.Auth.SecondFactorRequirement(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"require": rule})
}

func (s *Server) handleRequireSecondFactor(w http.ResponseWriter, r *http.Request) {
	var req secondFactorRuleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	lsn, err := s.Auth.RequireSecondFactor(r.Context(), PrincipalFrom(r.Context()), req.Require, clientIP(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"require": req.Require})
}
