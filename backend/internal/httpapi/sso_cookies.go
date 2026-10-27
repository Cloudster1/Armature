package httpapi

import (
	"crypto/subtle"
	"net/http"
	"net/url"
)

// The cookies around single sign-on: one ties a callback to the browser that
// started it, the other remembers a silent attempt that found no session.

// loginCookieMaxAge matches how long the provider's round trip may take.
const loginCookieMaxAge = 15 * 60

func (s *Server) loginCookie() string { return s.CookieName + "_sso_state" }

// quietCookie is set for the browser's session after a silent attempt fails
// or somebody signs out, so nobody is sent round the provider again.
func (s *Server) quietCookie() string { return s.CookieName + "_quiet" }

// bindLogin keeps the attempt's state in this browser, read off the URL the
// browser is sent to.
func (s *Server) bindLogin(w http.ResponseWriter, target string) {
	parsed, err := url.Parse(target)
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: s.loginCookie(), Value: parsed.Query().Get("state"), Path: oidcPath,
		MaxAge: loginCookieMaxAge, HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) unbindLogin(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: s.loginCookie(), Value: "", Path: oidcPath, MaxAge: -1,
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
}

// boundTo says whether the browser carries the state it was given.
func boundTo(r *http.Request, name, state string) bool {
	cookie, err := r.Cookie(name)
	return err == nil && state != "" && subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) == 1
}

func (s *Server) setQuiet(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: s.quietCookie(), Value: "1", Path: "/",
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearQuiet(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: s.quietCookie(), Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.Secure, SameSite: http.SameSiteLaxMode,
	})
}

func triedQuietly(r *http.Request, name string) bool {
	_, err := r.Cookie(name)
	return err == nil
}

// oidcPath is where the start and the callback live, the only paths that
// need the attempt's state.
const oidcPath = "/api/v1/auth/oidc"
