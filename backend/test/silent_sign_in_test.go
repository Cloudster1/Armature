//go:build integration

package test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// silentCrew is an installation of its own with a provider, an owner who
// signs in through it, and the stub standing in for Keycloak.
type silentCrew struct {
	api      *apiServer
	h        *harness
	provider *idp
	email    string
}

func newSilentCrew(t *testing.T, h *harness, dbName string) silentCrew {
	t.Helper()
	api, empty := emptyInstallation(t, h, dbName)
	owner := api.client(t)
	email := h.email(t, "silent-owner")
	want(t, owner.post("/api/v1/auth/signup", map[string]string{"email": email, "password": testPassword, "name": "Silent Owner"}), http.StatusCreated, "set the installation up")
	provider := newIDP(t)
	provider.endSession = true
	want(t, owner.put("/api/v1/oidc-provider", map[string]any{
		"issuer": provider.URL, "clientId": "armature-test", "clientSecret": "shh", "groupsClaim": "groups",
		"scopes": "openid profile email", "createGroups": false, "enabled": true,
	}), http.StatusOK, "configure the provider")
	return silentCrew{api: api, h: empty, provider: provider, email: email}
}

// browser is a client that sees each redirect rather than following it.
func (c silentCrew) browser(t *testing.T) *client {
	b := c.api.client(t).noRedirects()
	b.host = appHost
	return b
}

// appHost is the host of the test server's ARMATURE_APP_URL, which a browser
// using the app names; a silent attempt is only tried through it.
const appHost = "app.test"

func visit(t *testing.T, c silentCrew, b *client, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.api.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = b.host
	resp, _ := b.raw(req)
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("%s answered %d, want a redirect", path, resp.StatusCode)
	}
	return resp
}

func quietly(next string) string {
	return "/api/v1/auth/oidc/start?" + url.Values{"prompt": {"none"}, "next": {next}}.Encode()
}

func hasCookie(t *testing.T, c silentCrew, b *client, name string) bool {
	t.Helper()
	base, _ := url.Parse("http://" + b.host)
	for _, cookie := range b.http.Jar.Cookies(base) {
		if cookie.Name == name {
			return true
		}
	}
	return false
}

func offersSilence(t *testing.T, c silentCrew, b *client) bool {
	t.Helper()
	return offersSilenceAt(t, c, b, appHost)
}

func offersSilenceAt(t *testing.T, c silentCrew, b *client, host string) bool {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.api.URL+"/api/v1/auth/signup", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	resp, body := b.raw(req)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("what the sign-in page may do: %d %s", resp.StatusCode, body)
	}
	return strings.Contains(string(body), `"silentSignIn":true`)
}

// A visit without a session asks the provider quietly first; with a session
// there it lands where it was going, signed in, with no click (#325).
func TestASilentSignInLandsOnTheDeepLink(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	c := newSilentCrew(t, h, "armature_silent_lands_test")
	deep := "/issues/CP-12?tab=history"
	b := c.browser(t)
	if !offersSilence(t, c, b) {
		t.Fatal("a browser that has not tried is not offered a silent sign-in")
	}
	// From another address the round trip could not come back to the page.
	if offersSilenceAt(t, c, b, "elsewhere.test") {
		t.Error("a browser on another address is offered a silent sign-in")
	}

	out := visit(t, c, b, quietly(deep))
	asked, err := url.Parse(out.Header.Get("Location"))
	if err != nil || !strings.HasPrefix(asked.String(), c.provider.URL) {
		t.Fatalf("the silent start sends the browser to %q, want the provider", out.Header.Get("Location"))
	}
	query := asked.Query()
	if query.Get("prompt") != "none" || query.Get("code_challenge") == "" || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("the silent request = %s, want prompt=none and an S256 challenge", asked.RawQuery)
	}
	state, nonce := stateFrom(t, asked.String())
	c.provider.says("armature-test", map[string]any{"nonce": nonce, "email": c.email, "sub": "subject-" + uuid.NewString()})
	back := visit(t, c, b, "/api/v1/auth/oidc/callback?"+url.Values{"state": {state}, "code": {"any-code"}}.Encode())
	if got := back.Header.Get("Location"); got != "http://app.test"+deep {
		t.Fatalf("the callback lands on %q, want the deep link with its query", got)
	}
	want(t, b.get("/api/v1/auth/me"), http.StatusOK, "signed in without a click")

	verifier := c.provider.verifiers[len(c.provider.verifiers)-1]
	sum := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != query.Get("code_challenge") {
		t.Errorf("the code was traded with verifier %q, which is not the challenge's", verifier)
	}
	var kept bool
	if err := c.h.cluster.Primary().QueryRow(context.Background(), `
		SELECT EXISTS (SELECT 1 FROM user_session s JOIN app_user u ON u.id = s.user_id
		               WHERE u.email = $1 AND s.proof = 'oidc' AND s.id_token IS NOT NULL)`, c.email).Scan(&kept); err != nil || !kept {
		t.Errorf("the session does not keep its id token for signing out (%v)", err)
	}
}

// Without a session at the provider the person lands on the sign-in page,
// and the browser does not go round again on the next page.
func TestASilentSignInWithoutASessionTriesOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	c := newSilentCrew(t, h, "armature_silent_once_test")
	b := c.browser(t)

	out := visit(t, c, b, quietly("/projects"))
	state, _ := stateFrom(t, out.Header.Get("Location"))
	back := visit(t, c, b, "/api/v1/auth/oidc/callback?"+url.Values{"state": {state}, "error": {"login_required"}}.Encode())
	if got := back.Header.Get("Location"); got != "http://app.test/login?next=%2Fprojects" {
		t.Fatalf("a silent attempt the provider refused lands on %q, want the sign-in page with the page asked for", got)
	}
	if !hasCookie(t, c, b, testCookieName+"_quiet") {
		t.Fatal("the browser is not marked as having tried")
	}
	again := visit(t, c, b, quietly("/board"))
	if got := again.Header.Get("Location"); got != "http://app.test/login?next=%2Fboard" {
		t.Errorf("the next silent start goes to %q, want the sign-in page without the provider", got)
	}
	if offersSilence(t, c, b) {
		t.Error("a browser that tried is still offered a silent sign-in")
	}
	if !offersSilence(t, c, c.browser(t)) {
		t.Error("another browser is not offered one")
	}

	// A sign-in the person clicks still goes to the provider, and says why it failed.
	clicked := visit(t, c, b, "/api/v1/auth/oidc/start?next=%2Fprojects")
	if !strings.HasPrefix(clicked.Header.Get("Location"), c.provider.URL) {
		t.Fatalf("a clicked sign-in goes to %q, want the provider", clicked.Header.Get("Location"))
	}
	state, _ = stateFrom(t, clicked.Header.Get("Location"))
	refused := visit(t, c, b, "/api/v1/auth/oidc/callback?"+url.Values{"state": {state}, "error": {"access_denied"}}.Encode())
	if got := refused.Header.Get("Location"); got != "http://app.test/login?sso=access_denied" {
		t.Errorf("a clicked attempt the provider refused lands on %q", got)
	}
}

// Signing out ends the provider's session too, and the next visit shows the
// sign-in page rather than signing the person straight back in.
func TestSigningOutEndsTheProvidersSessionToo(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	c := newSilentCrew(t, h, "armature_silent_sign_out_test")
	b := c.browser(t)
	out := visit(t, c, b, "/api/v1/auth/oidc/start")
	state, nonce := stateFrom(t, out.Header.Get("Location"))
	c.provider.says("armature-test", map[string]any{"nonce": nonce, "email": c.email, "sub": "subject-" + uuid.NewString()})
	visit(t, c, b, "/api/v1/auth/oidc/callback?"+url.Values{"state": {state}, "code": {"any-code"}}.Encode())

	left := want(t, b.post("/api/v1/auth/logout", nil), http.StatusOK, "signing out")
	ending, err := url.Parse(left.Body["endSession"].(string))
	if err != nil || !strings.HasPrefix(ending.String(), c.provider.URL+"/logout") {
		t.Fatalf("signing out sends the browser to %v, want the provider's end session endpoint", left.Body["endSession"])
	}
	if q := ending.Query(); q.Get("id_token_hint") == "" || q.Get("client_id") != "armature-test" || q.Get("post_logout_redirect_uri") != "http://app.test/login" {
		t.Errorf("the end session address = %s", ending.RawQuery)
	}
	want(t, b.get("/api/v1/auth/me"), http.StatusUnauthorized, "signed out here")
	if got := visit(t, c, b, quietly("/projects")).Header.Get("Location"); got != "http://app.test/login?next=%2Fprojects" {
		t.Errorf("a silent start after signing out goes to %q, want the sign-in page", got)
	}

	// A session with a password has nothing at the provider to end.
	local := c.api.client(t)
	want(t, local.post("/api/v1/auth/login", map[string]string{"email": c.email, "password": testPassword}), http.StatusOK, "a password sign-in")
	want(t, local.post("/api/v1/auth/logout", nil), http.StatusNoContent, "signing a password session out")
}

// A callback in a browser that did not start the attempt is refused, so a
// sign-in cannot be slipped into somebody else's browser.
func TestASignInIsBoundToItsBrowser(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	c := newSilentCrew(t, h, "armature_silent_bound_test")
	started, other := c.browser(t), c.browser(t)
	out := visit(t, c, started, "/api/v1/auth/oidc/start")
	state, nonce := stateFrom(t, out.Header.Get("Location"))
	c.provider.says("armature-test", map[string]any{"nonce": nonce, "email": c.email, "sub": "subject-" + uuid.NewString()})
	back := visit(t, c, other, "/api/v1/auth/oidc/callback?"+url.Values{"state": {state}, "code": {"any-code"}}.Encode())
	if got := back.Header.Get("Location"); got != "http://app.test/login?sso=expired" {
		t.Errorf("a callback in another browser lands on %q, want the sign-in page", got)
	}
	want(t, other.get("/api/v1/auth/me"), http.StatusUnauthorized, "the other browser is not signed in")
}

// Somebody with a session at the provider who is no member lands on the
// sign-in page told so, and is asked about as a clicked sign-in is.
func TestASilentSignInByANonMemberFilesTheJoinRequest(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	c := newSilentCrew(t, h, "armature_silent_stranger_test")
	b := c.browser(t)
	stranger := h.email(t, "silent-stranger")
	out := visit(t, c, b, quietly("/projects"))
	state, nonce := stateFrom(t, out.Header.Get("Location"))
	c.provider.says("armature-test", map[string]any{"nonce": nonce, "email": stranger, "sub": "subject-" + uuid.NewString()})
	back := visit(t, c, b, "/api/v1/auth/oidc/callback?"+url.Values{"state": {state}, "code": {"any-code"}}.Encode())
	if got := back.Header.Get("Location"); got != "http://app.test/login?next=%2Fprojects&sso=not_a_member" {
		t.Errorf("a stranger's silent attempt lands on %q", got)
	}
	var asked bool
	if err := c.h.cluster.Primary().QueryRow(context.Background(), `
		SELECT EXISTS (SELECT 1 FROM join_request r JOIN app_user u ON u.id = r.user_id WHERE u.email = $1)`, stranger).Scan(&asked); err != nil || !asked {
		t.Errorf("no join request was filed (%v)", err)
	}
}

// Turned off, or with no provider, a silent start goes straight to the
// sign-in page and nobody is offered one.
func TestASilentSignInCanBeTurnedOff(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	c := newSilentCrew(t, h, "armature_silent_off_test")
	c.api.api.SilentSignIn = false
	b := c.browser(t)
	if offersSilence(t, c, b) {
		t.Error("a silent sign-in is offered while it is turned off")
	}
	if got := visit(t, c, b, quietly("/projects")).Header.Get("Location"); got != "http://app.test/login?next=%2Fprojects" {
		t.Errorf("a silent start while turned off goes to %q", got)
	}

	none, _ := emptyInstallation(t, h, "armature_silent_none_test")
	bare := silentCrew{api: none}
	if offersSilence(t, bare, bare.browser(t)) {
		t.Error("a silent sign-in is offered with no provider")
	}
	if got := visit(t, bare, bare.browser(t), quietly("/projects")).Header.Get("Location"); got != "http://app.test/login?next=%2Fprojects" {
		t.Errorf("a silent start with no provider goes to %q", got)
	}
}
