package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/mail"
)

// Recovering a forgotten password by mail. Nobody is signed in on these routes,
// and the secret travels in the body, never in an address a log would keep.

// resetMailTimeout bounds the send that runs after the answer has gone, which
// no request is waiting on to cancel it.
const resetMailTimeout = 30 * time.Second

type passwordResetRequest struct {
	Email string `json:"email"`
}

// handleRequestPasswordReset answers every address alike and mails afterwards,
// so neither the answer nor its timing says whether an account exists.
func (s *Server) handleRequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req passwordResetRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if s.Mailer == nil {
		respondError(w, r, &APIError{Status: http.StatusServiceUnavailable, Code: "mail_unavailable",
			Message: "Mail is not set up on this server, so a password cannot be reset by mail. Ask an administrator of your organization to set a new one for you."})
		return
	}
	if !resetRequests.gate(w, r, "Too many password links were asked for from here. Wait a few minutes and try again.") {
		return
	}
	if !resetAddresses.gateKey(w, r, auth.NormalizeEmail(req.Email), "Too many password links were asked for that address. Wait a few minutes and try again.") {
		return
	}
	reset, err := s.Auth.RequestPasswordReset(r.Context(), req.Email, clientIP(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	if reset != nil {
		mailer, message := s.Mailer, s.passwordResetMail(reset)
		ctx := context.WithoutCancel(r.Context())
		go func() {
			ctx, cancel := context.WithTimeout(ctx, resetMailTimeout)
			defer cancel()
			if err := mailer.Send(ctx, message); err != nil {
				s.Log.Warn("password reset mail not sent", "error", err)
			}
		}()
	}
	respondJSON(w, r, http.StatusAccepted, map[string]any{"expiresInSeconds": int(auth.ResetLinkTTL.Seconds())})
}

// passwordResetMail words the link, or, for an account without a password, the
// way it does sign in.
func (s *Server) passwordResetMail(reset *auth.ResetRequest) mail.Mail {
	if reset.Secret == "" {
		return mail.Mail{
			To:      reset.Email,
			Subject: "How you sign in to Armature",
			Body: fmt.Sprintf("Somebody asked to set a new password for the Armature account of %s. That account has no password here: it signs in through your organization's identity provider.\n\nOn the sign-in page at %s/login, choose \"Sign in with single sign-on\" and give the organization %s.\n\nIf you did not ask, ignore this mail.\n",
				reset.Email, s.AppBaseURL, strings.Join(reset.Providers, " or ")),
		}
	}
	return mail.Mail{
		To:      reset.Email,
		Subject: "Set a new password for Armature",
		Body: fmt.Sprintf("Somebody asked to set a new password for the Armature account of %s.\n\nSet a new one at %s\n\nThe link works once and expires in an hour. Using it signs you out everywhere and revokes your API tokens. If you did not ask, ignore this mail and your password stays as it is.\n",
			reset.Email, s.AppBaseURL+"/reset-password#"+reset.Secret),
	}
}

type confirmPasswordResetRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

// handleConfirmPasswordReset sets the new password. It opens no session: the
// person signs in with what they chose, which proves they know it.
func (s *Server) handleConfirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req confirmPasswordResetRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	tried := "reset-link:" + clientIP(r)
	if credentialTries.tooMany(w, r, tried, "Too many password links were tried from here. Wait a few minutes and try again.") {
		return
	}
	if err := s.Auth.ResetPassword(r.Context(), req.Token, req.Password, clientIP(r)); err != nil {
		if errors.Is(err, auth.ErrResetLinkInvalid) {
			credentialTries.record(tried)
		}
		respondError(w, r, err)
		return
	}
	respondNoContent(w)
}
