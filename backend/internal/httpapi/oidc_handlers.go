package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/armature/armature/backend/internal/oidc"
)

// Signing in through the installation's identity provider. The start and
// callback are outside authentication for the obvious reason; the configuration
// behind them is administration.

// handleOIDCStart sends the browser to the installation's provider. With
// prompt=none it asks quietly, and lands on the sign-in page when it cannot.
func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	next := safeRedirect(r.URL.Query().Get("next"))
	silent := r.URL.Query().Get("prompt") == "none"
	if silent && !s.maySilence(r) {
		s.landQuietly(w, r, next, "")
		return
	}
	if s.OIDC == nil {
		respondError(w, r, toAPIError(oidc.ErrNotConfigured))
		return
	}
	target, err := s.OIDC.Begin(r.Context(), next, silent)
	if err != nil {
		// A provider that cannot be reached is no reason to keep somebody from
		// the sign-in page; only a click asks to be told why.
		if silent {
			s.landQuietly(w, r, next, "")
			return
		}
		respondError(w, r, err)
		return
	}
	s.bindLogin(w, target)
	http.Redirect(w, r, target, http.StatusFound)
}

// oidcStartPath is where single sign-on starts, for the address that named an
// organization to send the browser on to.
const oidcStartPath = "/api/v1/auth/oidc/start"

// handleOIDCStartForOrganization keeps the address that named an organization
// working for a release, for links and bookmarks made before there was one.
func (s *Server) handleOIDCStartForOrganization(w http.ResponseWriter, r *http.Request) {
	target := oidcStartPath
	if next := safeRedirect(r.URL.Query().Get("next")); next != "" {
		target += "?" + url.Values{"next": {next}}.Encode()
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// handleOIDCCallback finishes a sign-in and lands the browser back in the app,
// with a redirect, since the caller is the browser following the provider.
func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	if s.OIDC == nil {
		respondError(w, r, toAPIError(oidc.ErrNotConfigured))
		return
	}
	query := r.URL.Query()
	state := query.Get("state")
	bound := boundTo(r, s.loginCookie(), state)
	s.unbindLogin(w)

	login, err := s.OIDC.Claim(r.Context(), state)
	if refused := query.Get("error"); refused != "" {
		// Quiet means quiet: login_required and its kin send the person to the
		// sign-in page as if nothing was tried. A clicked attempt says why.
		if err == nil && login.Silent {
			s.landQuietly(w, r, login.Redirect, "")
			return
		}
		s.landAfterSignIn(w, r, "", refused)
		return
	}
	if err != nil {
		s.landAfterSignIn(w, r, "", signInFailure(err))
		return
	}
	// A callback in a browser that did not start the attempt is somebody
	// else's sign-in being slipped into this one.
	if !bound {
		s.landAfter(w, r, login, signInFailure(oidc.ErrUnknownLogin))
		return
	}
	identity, err := s.OIDC.Complete(r.Context(), login, query.Get("code"))
	if err != nil {
		s.landAfter(w, r, login, signInFailure(err))
		return
	}
	session, err := s.OIDC.SignIn(r.Context(), identity, s.SessionTTL, r.UserAgent(), clientIP(r))
	if err != nil {
		s.landAfter(w, r, login, signInFailure(err))
		return
	}
	s.setSessionCookie(w, session.Secret, session.ExpiresAt)
	s.clearQuiet(w)
	s.landAfterSignIn(w, r, login.Redirect, "")
}

// landAfter is a failed attempt's landing: a silent one keeps where the person
// was going and stops trying quietly, a clicked one is as it always was.
func (s *Server) landAfter(w http.ResponseWriter, r *http.Request, login *oidc.Login, failure string) {
	if login.Silent {
		s.landQuietly(w, r, login.Redirect, failure)
		return
	}
	s.landAfterSignIn(w, r, "", failure)
}

// landQuietly ends a silent attempt on the sign-in page, remembering for the
// browser's session that it was tried, so the next page does not try again.
func (s *Server) landQuietly(w http.ResponseWriter, r *http.Request, next, failure string) {
	s.setQuiet(w)
	query := url.Values{}
	if next != "" {
		query.Set("next", next)
	}
	if failure != "" {
		query.Set("sso", failure)
	}
	target := "/login"
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	http.Redirect(w, r, s.AppBaseURL+target, http.StatusFound)
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

// handleGetOIDCProvider returns the installation's provider configuration. The
// client secret is never included; only whether one is set.
func (s *Server) handleGetOIDCProvider(w http.ResponseWriter, r *http.Request) {
	found, err := s.OIDC.ProviderFor(r.Context())
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
	// AdmitByGroup lets somebody in whose group holds a role, at sign-in and
	// at a partner's token exchange. Left out, it is off.
	AdmitByGroup bool `json:"admitByGroup"`
	Enabled      bool `json:"enabled"`
}

func (s *Server) handleSaveOIDCProvider(w http.ResponseWriter, r *http.Request) {
	var req saveOIDCProviderRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	p := PrincipalFrom(r.Context())
	saved, lsn, err := s.OIDC.Save(r.Context(), oidc.Provider{
		Issuer:       req.Issuer,
		ClientID:     req.ClientID,
		ClientSecret: req.ClientSecret,
		GroupsClaim:  req.GroupsClaim,
		Scopes:       req.Scopes,
		CreateGroups: req.CreateGroups,
		AdmitByGroup: req.AdmitByGroup,
		Enabled:      req.Enabled,
	}, p.User.ID)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"provider": saved})
}
