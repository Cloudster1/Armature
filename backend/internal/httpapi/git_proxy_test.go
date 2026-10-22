package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Only git's three requests below a repository are the proxy's; the webhook
// and every other git route keep the API's rules.
func TestTheGitProxyIsKnownByItsPaths(t *testing.T) {
	for path, want := range map[string]bool{
		"/api/v1/git/CP/acme/app.git/info/refs":          true,
		"/api/v1/git/CP/acme/app.git/git-upload-pack":    true,
		"/api/v1/git/CP/acme/app.git/git-receive-pack":   true,
		"/api/v1/git/CP/group%2Fsub/app.git/info/refs":   true,
		"/api/v1/git/webhooks/7b0c6a8e/git-receive-pack": false,
		"/api/v1/git/webhooks/7b0c6a8e":                  false,
		"/api/v1/issues/CP-1/git-receive-pack":           false,
		"/api/v1/git/CP/acme/app.git/HEAD":               false,
	} {
		if got := gitTransfer(path); got != want {
			t.Errorf("gitTransfer(%q) = %v, want %v", path, got, want)
		}
	}
}

// git sends a token as basic auth, and a browser's cookie never clones or
// pushes; everywhere else basic auth is no credential at all.
func TestGitSendsItsTokenAsBasicAuth(t *testing.T) {
	const cookie = "armature_session"
	request := func(path string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.AddCookie(&http.Cookie{Name: cookie, Value: "session-secret"})
		return r
	}
	clone := "/api/v1/git/CP/acme/app.git/info/refs"

	asPassword := request(clone)
	asPassword.SetBasicAuth("rotor", "armature_oat_secret")
	asName := request(clone)
	asName.SetBasicAuth("armature_oat_secret", "")
	asBearer := request(clone)
	asBearer.Header.Set("Authorization", "Bearer armature_oat_secret")
	elsewhere := request("/api/v1/projects")
	elsewhere.SetBasicAuth("rotor", "armature_oat_secret")

	for name, c := range map[string]struct {
		r    *http.Request
		want string
	}{
		"the password":         {asPassword, "armature_oat_secret"},
		"the name":             {asName, "armature_oat_secret"},
		"a bearer":             {asBearer, "armature_oat_secret"},
		"a cookie alone":       {request(clone), ""},
		"basic auth elsewhere": {elsewhere, "session-secret"},
	} {
		if got := credentialFrom(c.r, cookie); got != c.want {
			t.Errorf("%s: credential = %q, want %q", name, got, c.want)
		}
	}
}
