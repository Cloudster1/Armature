package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/armature/armature/backend/internal/oidc"
)

// Signing in through an identity provider. The start and callback are outside
// authentication for the obvious reason; the configuration behind them is
// organization administration.

// handleOIDCStart sends the browser to the provider.
//
// The organization is named in the path because a sign-in has to know which
// tenant it is for before anybody is signed in to ask.
func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		respondError(w, r, toAPIError(oidc.ErrNotConfigured))
		return
	}

	org, err := s.Auth.OrgBySlug(r.Context(), r.PathValue("orgSlug"))
	if err != nil {
		// An organization that does not exist and one with no provider are the
		// same answer, so that this endpoint cannot be used to enumerate slugs.
		respondError(w, r, toAPIError(oidc.ErrNotConfigured))
		return
	}

	target, err := s.OIDC.Start(r.Context(), org.ID, safeRedirect(r.URL.Query().Get("next")))
	if err != nil {
		respondError(w, r, err)
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// handleOIDCCallback finishes a sign-in and lands the browser back in the app.
//
// It answers with a redirect rather than JSON because the caller is the
// browser following the provider's own redirect, not the client's fetch.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		respondError(w, r, toAPIError(oidc.ErrNotConfigured))
		return
	}

	query := r.URL.Query()
	if refused := query.Get("error"); refused != "" {
		// The provider refused. Its own description is the useful part, and it
		// is shown to the person rather than swallowed.
		s.landAfterSignIn(w, r, "", refused)
		return
	}

	orgID, identity, redirect, err := s.OIDC.Exchange(r.Context(), query.Get("state"), query.Get("code"))
	if err != nil {
		s.landAfterSignIn(w, r, "", signInFailure(err))
		return
	}

	session, err := s.OIDC.SignIn(r.Context(), orgID, identity, s.SessionTTL,
		r.UserAgent(), clientIP(r))
	if err != nil {
		s.landAfterSignIn(w, r, "", signInFailure(err))
		return
	}

	s.setSessionCookie(w, session.Secret, session.ExpiresAt)
	s.landAfterSignIn(w, r, redirect, "")
}

// signInFailure turns a refusal into something worth showing somebody, without
// handing back anything about the tenant they were trying to reach.
func signInFailure(err error) string {
	switch {
	case errors.Is(err, oidc.ErrNotAMember):
		return "not_a_member"
	case errors.Is(err, oidc.ErrNoEmail):
		return "no_email"
	case errors.Is(err, oidc.ErrEmailUnverified):
		return "unverified_email"
	case errors.Is(err, oidc.ErrUnknownLogin):
		return "expired"
	case errors.Is(err, oidc.ErrNotConfigured):
		return "not_configured"
	default:
		return "failed"
	}
}

// landAfterSignIn sends the browser back into the app, either where it was
// heading or to the sign-in page with a reason it can show.
func (s *Server) landAfterSignIn(w http.ResponseWriter, r *http.Request, redirect, failure string) {
	target := "/"
	if redirect != "" {
		target = redirect
	}
	if failure != "" {
		target = "/login?sso=" + url.QueryEscape(failure)
	}
	http.Redirect(w, r, s.AppBaseURL+target, http.StatusFound)
}

// safeRedirect keeps a redirect inside this application. An open redirect on a
// sign-in endpoint is how a convincing phishing link gets made.
func safeRedirect(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return ""
	}
	return next
}

// handleGetOIDCProvider returns the organization's provider configuration. The
// client secret is never included; only whether one is set.
func (s *Server) handleGetOIDCProvider(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	found, err := s.OIDC.ProviderFor(r.Context(), p.Org.ID)
	if errors.Is(err, oidc.ErrNotConfigured) {
		respondJSON(w, r, http.StatusOK, map[string]any{"provider": nil})
		return
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"provider": found})
}

type saveOIDCProviderRequest struct {
	Issuer       string `json:"issuer"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	GroupsClaim  string `json:"groupsClaim"`
	Scopes       string `json:"scopes"`
	CreateGroups bool   `json:"createGroups"`
	Enabled      bool   `json:"enabled"`
}

func (s *Server) handleSaveOIDCProvider(w http.ResponseWriter, r *http.Request) {
	var req saveOIDCProviderRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	p := PrincipalFrom(r.Context())
	saved, lsn, err := s.OIDC.Save(r.Context(), p.Org.ID, oidc.Provider{
		Issuer:       req.Issuer,
		ClientID:     req.ClientID,
		ClientSecret: req.ClientSecret,
		GroupsClaim:  req.GroupsClaim,
		Scopes:       req.Scopes,
		CreateGroups: req.CreateGroups,
		Enabled:      req.Enabled,
	}, p.User.ID)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"provider": saved})
}
