//go:build integration

package test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/oauth"
)

// The partner side of connecting an app, as Stator, Exciter and Rotor walk it:
// register, send a person to the consent page, trade the code, refresh.

const partnerReturn = "https://stator.example.test/oauth/callback"

// partner is a registered client and the anonymous client it calls from.
type partner struct {
	id, secret, name string
	c                *client
}

// register registers a client the way a partner does, pending an administrator.
func register(t *testing.T, api *apiServer, name, scope string) partner {
	t.Helper()
	c := api.client(t)
	resp := want(t, c.post("/api/v1/oauth/register", map[string]any{
		"client_name": name, "client_uri": "https://stator.example.test",
		"redirect_uris": []string{partnerReturn}, "scope": scope,
	}), http.StatusCreated, "registering "+name)
	return partner{id: resp.Body["client_id"].(string), secret: resp.Body["client_secret"].(string), name: name, c: c}
}

// form posts an OAuth form, authenticated with HTTP Basic when id is set.
func (c *client) form(path string, values url.Values, id, secret string) response {
	c.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, c.base+path, strings.NewReader(values.Encode()))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if id != "" {
		req.SetBasicAuth(url.QueryEscape(id), url.QueryEscape(secret))
	}
	resp, raw := c.raw(req)
	out := response{Status: resp.StatusCode, Header: resp.Header, Raw: string(raw)}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out.Body); err != nil {
			c.t.Fatalf("POST %s returned a body that is not JSON: %q", path, raw)
		}
	}
	return out
}

// token asks the token endpoint as the partner.
func (p partner) token(values url.Values) response {
	return p.c.form("/api/v1/oauth/token", values, p.id, p.secret)
}

// pkce is a fresh verifier and its S256 challenge.
func pkce(t *testing.T) (verifier, challenge string) {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// authorizeQuery is what a partner sends the browser to /oauth/authorize with.
func authorizeQuery(clientID, scope, challenge string, extra ...string) string {
	q := url.Values{
		"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {partnerReturn},
		"scope": {scope}, "state": {"state-of-the-partner"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	for i := 0; i+1 < len(extra); i += 2 {
		q.Set(extra[i], extra[i+1])
	}
	return q.Encode()
}

// answered reads the code or the error a redirect back to the partner carries.
func answered(t *testing.T, to string) url.Values {
	t.Helper()
	if !strings.HasPrefix(to, partnerReturn+"?") {
		t.Fatalf("the browser was sent to %q, not back to the partner", to)
	}
	u, err := url.Parse(to)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("state") != "state-of-the-partner" {
		t.Errorf("the answer lost the partner's state: %q", to)
	}
	return q
}

// allow has the signed-in browser allow the query on the consent page and
// returns the code it is sent back with.
func allow(t *testing.T, browser *client, query string) string {
	t.Helper()
	page := want(t, browser.get("/api/v1/oauth/consent?"+query), http.StatusOK, "the consent page")
	to, _ := page.Body["redirectTo"].(string)
	if to == "" {
		if consent := obj(t, page, "consent"); consent["mayAllow"] != true {
			t.Fatalf("the page offers no Allow: %s", page.Raw)
		}
		decided := want(t, browser.post("/api/v1/oauth/consent", map[string]any{"query": query, "decision": "allow"}), http.StatusOK, "allowing")
		to = decided.Body["redirectTo"].(string)
	}
	q := answered(t, to)
	if q.Get("iss") != "http://app.test" {
		t.Errorf("iss = %q, want the issuer (RFC 9207)", q.Get("iss"))
	}
	if q.Get("code") == "" {
		t.Fatalf("no code in %q", to)
	}
	return q.Get("code")
}

// connect walks the whole consent for scope and returns the token answer.
func connect(t *testing.T, p partner, browser *client, scope string) map[string]any {
	t.Helper()
	verifier, challenge := pkce(t)
	code := allow(t, browser, authorizeQuery(p.id, scope, challenge))
	return want(t, p.token(url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {partnerReturn}, "code_verifier": {verifier},
	}), http.StatusOK, "trading the code").Body
}

// bearer is a client calling with a connected app's access token.
func bearer(api *apiServer, t *testing.T, token any) *client {
	c := api.client(t)
	c.bearer = token.(string)
	return c
}

func TestAPartnerConnectsAPersonWithTheirConsent(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	key := h.projectKey(t, "OAC")
	adaEmail := h.person(t, "ada", auth.RoleAdmin)
	ada := api.client(t)
	ada.signIn(t, adaEmail)
	want(t, ada.post("/api/v1/projects", map[string]any{"name": "Connected", "key": key}), http.StatusCreated, "a project")
	want(t, ada.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "seen by the app"}), http.StatusCreated, "an issue")
	mia := api.client(t)
	mia.signIn(t, h.person(t, "mia", auth.RoleMember))

	stator := register(t, api, h.name(t, "Stator"), "read issue.write")

	t.Run("a member cannot let a pending app in", func(t *testing.T) {
		_, challenge := pkce(t)
		page := want(t, mia.get("/api/v1/oauth/consent?"+authorizeQuery(stator.id, "read", challenge)), http.StatusOK, "the page")
		consent := obj(t, page, "consent")
		if consent["pending"] != true || consent["mayAllow"] != false || consent["mayCancel"] != false {
			t.Errorf("a member was offered to let a pending app in: %s", page.Raw)
		}
		if !strings.Contains(consent["refusal"].(string), "An Armature administrator connects it first") {
			t.Errorf("refusal = %v", consent["refusal"])
		}
		want(t, mia.post("/api/v1/oauth/consent", map[string]any{"query": authorizeQuery(stator.id, "read", challenge), "decision": "allow"}), http.StatusForbidden, "allowing anyway")
		if resp := stator.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"armature_ort_x"}}); resp.Status != http.StatusUnauthorized || resp.Body["error"] != "invalid_client" {
			t.Errorf("a pending app was let through the token endpoint: %d %s", resp.Status, resp.Raw)
		}
	})

	var tokens map[string]any
	t.Run("an administrator allows the app and connects in one step", func(t *testing.T) {
		_, challenge := pkce(t)
		page := want(t, ada.get("/api/v1/oauth/consent?"+authorizeQuery(stator.id, "read issue.write", challenge)), http.StatusOK, "the page")
		consent := obj(t, page, "consent")
		if consent["pending"] != true || consent["mayAllow"] != true || consent["redirectHost"] != "stator.example.test" {
			t.Errorf("the administrator's page: %s", page.Raw)
		}
		words := list(t, page, "consent", "scopes")
		if len(words) != 2 || words[1].(map[string]any)["words"] != "File and edit issues, and link pages to them" {
			t.Errorf("scopes in words = %v", words)
		}
		tokens = connect(t, stator, ada, "read issue.write")
		if tokens["token_type"] != "Bearer" || tokens["expires_in"] != float64(3600) || tokens["scope"] != "read issue.write" {
			t.Errorf("token answer = %v", tokens)
		}
		if !strings.HasPrefix(tokens["access_token"].(string), auth.AccessTokenPrefix) || !strings.HasPrefix(tokens["refresh_token"].(string), auth.RefreshTokenPrefix) {
			t.Errorf("tokens are not recognisable: %v", tokens)
		}
	})

	t.Run("the token is Ada, narrowed to what she allowed", func(t *testing.T) {
		app := bearer(api, t, tokens["access_token"])
		me := want(t, app.get("/api/v1/auth/me"), http.StatusOK, "who the token is")
		if principalField(t, me, "principal", "user", "email") != adaEmail {
			t.Errorf("the token is %v", me.Raw)
		}
		token := obj(t, me, "token")
		if token["app"].(map[string]any)["clientId"] != stator.id || token["name"] != stator.name+" (connected app)" {
			t.Errorf("token = %v", token)
		}
		h.waitForPrimary(t)
		if resp := app.get("/api/v1/projects/" + key + "/issues"); resp.Status != http.StatusOK || resp.Body["total"].(float64) != 1 {
			t.Errorf("reading with the app: %d %s", resp.Status, resp.Raw)
		}
	})

	t.Run("it is in Ada's token list as the app", func(t *testing.T) {
		var found map[string]any
		for _, each := range list(t, want(t, ada.get("/api/v1/tokens"), http.StatusOK, "tokens"), "tokens") {
			if tok := each.(map[string]any); tok["app"] != nil {
				found = tok
			}
		}
		if found == nil || found["name"] != stator.name+" (connected app)" || found["expiresAt"] == nil {
			t.Fatalf("the grant in the list = %v", found)
		}
	})

	t.Run("allowed already, a new code comes without the page", func(t *testing.T) {
		_, challenge := pkce(t)
		page := want(t, ada.get("/api/v1/oauth/consent?"+authorizeQuery(stator.id, "read", challenge)), http.StatusOK, "the page")
		if to, _ := page.Body["redirectTo"].(string); answered(t, to).Get("code") == "" {
			t.Errorf("a person who allowed the app was shown the page again: %s", page.Raw)
		}
		page = want(t, ada.get("/api/v1/oauth/consent?"+authorizeQuery(stator.id, "read", challenge, "prompt", "consent")), http.StatusOK, "the page asked for")
		if page.Body["consent"] == nil {
			t.Errorf("prompt=consent did not show the page: %s", page.Raw)
		}
	})

	t.Run("a refresh rotates, and a rotated refresh token ends the grant", func(t *testing.T) {
		grant := connect(t, stator, mia, "read")
		refreshed := want(t, stator.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {grant["refresh_token"].(string)}}), http.StatusOK, "refreshing")
		if refreshed.Body["refresh_token"] == grant["refresh_token"] {
			t.Fatal("the refresh token did not rotate")
		}
		if resp := bearer(api, t, grant["access_token"]).get("/api/v1/auth/me"); resp.Status != http.StatusUnauthorized ||
			resp.Header.Get("WWW-Authenticate") != `Bearer error="invalid_token"` {
			t.Errorf("the previous access token: %d %q", resp.Status, resp.Header.Get("WWW-Authenticate"))
		}
		want(t, bearer(api, t, refreshed.Body["access_token"]).get("/api/v1/auth/me"), http.StatusOK, "the new access token")

		replayed := stator.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {grant["refresh_token"].(string)}})
		if replayed.Status != http.StatusBadRequest || replayed.Body["error"] != "invalid_grant" || replayed.Body["error_description"] != "This connection has ended. Connect Armature again." {
			t.Fatalf("a rotated refresh token: %d %s", replayed.Status, replayed.Raw)
		}
		if resp := bearer(api, t, refreshed.Body["access_token"]).get("/api/v1/auth/me"); resp.Status != http.StatusUnauthorized {
			t.Errorf("the grant outlived its replayed refresh token: %d", resp.Status)
		}
		if resp := stator.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshed.Body["refresh_token"].(string)}}); resp.Body["error"] != "invalid_grant" {
			t.Errorf("the newest refresh token outlived the grant: %d %s", resp.Status, resp.Raw)
		}
	})

	t.Run("a narrower refresh narrows the grant for good", func(t *testing.T) {
		grant := connect(t, stator, ada, "read issue.write")
		narrowed := want(t, stator.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {grant["refresh_token"].(string)}, "scope": {"read"}}), http.StatusOK, "narrowing")
		if narrowed.Body["scope"] != "read" {
			t.Errorf("scope = %v", narrowed.Body["scope"])
		}
		wider := stator.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {narrowed.Body["refresh_token"].(string)}, "scope": {"read issue.write"}})
		if wider.Status != http.StatusBadRequest || wider.Body["error"] != "invalid_scope" {
			t.Errorf("widening on refresh: %d %s", wider.Status, wider.Raw)
		}
	})
}

func TestACodeIsHeldToPKCEAndUsedOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	ada := api.client(t)
	ada.signIn(t, h.person(t, "ada", auth.RoleAdmin))
	stator := register(t, api, h.name(t, "Stator"), "read")
	want(t, ada.post("/api/v1/oauth/clients/"+stator.id+"/approve", nil), http.StatusOK, "approving")

	refusedAtTheApp := func(t *testing.T, query, code string) {
		t.Helper()
		page := want(t, ada.get("/api/v1/oauth/consent?"+query), http.StatusOK, "the page")
		to, _ := page.Body["redirectTo"].(string)
		if q := answered(t, to); q.Get("error") != code || q.Get("error_description") == "" || q.Get("code") != "" {
			t.Errorf("answered %q, want error %s", to, code)
		}
	}
	t.Run("without PKCE, or with plain, the app is told", func(t *testing.T) {
		q := url.Values{"response_type": {"code"}, "client_id": {stator.id}, "redirect_uri": {partnerReturn}, "scope": {"read"}, "state": {"state-of-the-partner"}}
		refusedAtTheApp(t, q.Encode(), "invalid_request")
		q.Set("code_challenge", "abc")
		q.Set("code_challenge_method", "plain")
		refusedAtTheApp(t, q.Encode(), "invalid_request")
		_, challenge := pkce(t)
		refusedAtTheApp(t, authorizeQuery(stator.id, "read webhooks.partner", challenge), "invalid_scope")
		refusedAtTheApp(t, authorizeQuery(stator.id, "issue.write", challenge), "invalid_scope")
		refusedAtTheApp(t, authorizeQuery(stator.id, "read", challenge, "response_type", "token"), "unsupported_response_type")
		refusedAtTheApp(t, authorizeQuery(stator.id, "read", challenge, "resource", "https://elsewhere.test"), "invalid_target")
	})

	t.Run("cancel answers access_denied", func(t *testing.T) {
		_, challenge := pkce(t)
		decided := want(t, ada.post("/api/v1/oauth/consent", map[string]any{"query": authorizeQuery(stator.id, "read", challenge, "prompt", "consent"), "decision": "deny"}), http.StatusOK, "cancelling")
		if q := answered(t, decided.Body["redirectTo"].(string)); q.Get("error") != "access_denied" {
			t.Errorf("cancel answered %v", q)
		}
	})

	t.Run("a wrong verifier is refused", func(t *testing.T) {
		_, challenge := pkce(t)
		other, _ := pkce(t)
		code := allow(t, ada, authorizeQuery(stator.id, "read", challenge, "prompt", "consent"))
		resp := stator.token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {partnerReturn}, "code_verifier": {other}})
		if resp.Status != http.StatusBadRequest || resp.Body["error"] != "invalid_grant" {
			t.Errorf("a wrong verifier: %d %s", resp.Status, resp.Raw)
		}
	})

	t.Run("a code used twice ends the tokens it issued", func(t *testing.T) {
		verifier, challenge := pkce(t)
		code := allow(t, ada, authorizeQuery(stator.id, "read", challenge, "prompt", "consent"))
		trade := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {partnerReturn}, "code_verifier": {verifier}}
		first := want(t, stator.token(trade), http.StatusOK, "the first use")
		if second := stator.token(trade); second.Body["error"] != "invalid_grant" {
			t.Fatalf("the second use: %d %s", second.Status, second.Raw)
		}
		if resp := bearer(api, t, first.Body["access_token"]).get("/api/v1/auth/me"); resp.Status != http.StatusUnauthorized {
			t.Errorf("the tokens of a replayed code still work: %d", resp.Status)
		}
	})

	t.Run("a code past its minute is refused", func(t *testing.T) {
		verifier, challenge := pkce(t)
		code := allow(t, ada, authorizeQuery(stator.id, "read", challenge, "prompt", "consent"))
		if _, err := h.super.Exec(context.Background(), `UPDATE oauth_code SET expires_at = now() - interval '1 second' WHERE code_hash = $1`, auth.HashToken(code)); err != nil {
			t.Fatal(err)
		}
		resp := stator.token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {partnerReturn}, "code_verifier": {verifier}})
		if resp.Body["error"] != "invalid_grant" {
			t.Errorf("an expired code: %d %s", resp.Status, resp.Raw)
		}
	})

	t.Run("the token endpoint refuses what it does not know", func(t *testing.T) {
		for what, c := range map[string]struct {
			values     url.Values
			id, secret string
			status     int
			code       string
		}{
			"a wrong secret":       {url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}}, stator.id, "armature_ocs_wrong", http.StatusUnauthorized, "invalid_client"},
			"no client":            {url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}}, "", "", http.StatusUnauthorized, "invalid_client"},
			"an unknown grant":     {url.Values{"grant_type": {"password"}}, stator.id, stator.secret, http.StatusBadRequest, "unsupported_grant_type"},
			"a missing code":       {url.Values{"grant_type": {"authorization_code"}}, stator.id, stator.secret, http.StatusBadRequest, "invalid_request"},
			"a resource elsewhere": {url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}, "resource": {"https://elsewhere.test"}}, stator.id, stator.secret, http.StatusBadRequest, "invalid_target"},
		} {
			resp := stator.c.form("/api/v1/oauth/token", c.values, c.id, c.secret)
			if resp.Status != c.status || resp.Body["error"] != c.code || resp.Header.Get("Cache-Control") != "no-store" {
				t.Errorf("%s: %d %s, want %d %s", what, resp.Status, resp.Raw, c.status, c.code)
			}
		}
		if resp := stator.c.form("/api/v1/oauth/token", url.Values{"grant_type": {"refresh_token"}}, stator.id, "armature_ocs_wrong"); resp.Header.Get("WWW-Authenticate") == "" {
			t.Error("a client that sent Basic was not asked for Basic again")
		}
	})
}

func TestAnAppArmatureDoesNotKnowIsNeverRedirectedTo(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	ada := api.client(t)
	ada.signIn(t, h.person(t, "ada", auth.RoleAdmin))
	stator := register(t, api, h.name(t, "Stator"), "read")
	_, challenge := pkce(t)

	for what, query := range map[string]string{
		"an unknown app":            authorizeQuery("0199e1c2-7d4a-7c11-9b1e-3f0a2b4c5d6e", "read", challenge),
		"a return address it lacks": strings.Replace(authorizeQuery(stator.id, "read", challenge), url.QueryEscape(partnerReturn), url.QueryEscape("https://evil.test/cb"), 1),
		"no app at all":             "response_type=code",
	} {
		resp := ada.get("/api/v1/oauth/consent?" + query)
		if resp.Status != http.StatusBadRequest || resp.ErrorCode() != "not_connected" || resp.Body["redirectTo"] != nil {
			t.Errorf("%s: %d %s", what, resp.Status, resp.Raw)
		}
	}
	anonymous := api.client(t)
	if resp := anonymous.get("/api/v1/oauth/consent?" + authorizeQuery(stator.id, "read", challenge)); resp.Status != http.StatusUnauthorized {
		t.Errorf("somebody not signed in: %d %s", resp.Status, resp.Raw)
	}
	if resp := ada.post("/api/v1/oauth/consent", map[string]any{"query": authorizeQuery(stator.id, "read", challenge), "decision": "maybe"}); resp.Status != http.StatusUnprocessableEntity {
		t.Errorf("a decision that is neither: %d %s", resp.Status, resp.Raw)
	}
}

func TestAPendingAppIsAnAdministratorsToAllowAndGoesUnallowed(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	ada := api.client(t)
	ada.signIn(t, h.person(t, "ada", auth.RoleAdmin))
	stator := register(t, api, h.name(t, "Stator"), "read")
	ctx := context.Background()

	t.Run("the database refuses a grant for it", func(t *testing.T) {
		_, err := h.super.Exec(ctx, `
			INSERT INTO api_token (user_id, name, token_hash, scopes, expires_at, client_id, refresh_hash, access_expires_at)
			SELECT id, 'forged', '\x01', '{read}', now() + interval '1 day', $2, '\x02', now() + interval '1 hour'
			FROM app_user WHERE email = $1`, h.made.emails[0], stator.id)
		if err == nil || !strings.Contains(err.Error(), "allowed by an administrator") {
			t.Errorf("a grant for a pending app went in: %v", err)
		}
	})

	t.Run("an app nobody allowed within a day is gone", func(t *testing.T) {
		if _, err := h.super.Exec(ctx, `UPDATE oauth_client SET created_at = now() - interval '25 hours' WHERE id = $1`, stator.id); err != nil {
			t.Fatal(err)
		}
		_, challenge := pkce(t)
		if resp := ada.get("/api/v1/oauth/consent?" + authorizeQuery(stator.id, "read", challenge)); resp.ErrorCode() != "not_connected" {
			t.Errorf("an expired pending app: %d %s", resp.Status, resp.Raw)
		}
		register(t, api, h.name(t, "Exciter"), "read")
		var left int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM oauth_client WHERE id = $1`, stator.id).Scan(&left); err != nil || left != 0 {
			t.Errorf("the expired app was kept: %d %v", left, err)
		}
	})
}

func TestScopesNarrowWhatAConnectedAppMayDo(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	key := h.projectKey(t, "OAS")
	hidden := h.projectKey(t, "OAH")
	ada := api.client(t)
	ada.signIn(t, h.person(t, "ada", auth.RoleAdmin))
	want(t, ada.post("/api/v1/projects", map[string]any{"name": "Scoped", "key": key}), http.StatusCreated, "a project")
	issue := want(t, ada.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "to link"}), http.StatusCreated, "an issue")
	issueKey := obj(t, issue, "issue")["key"].(string)
	miaEmail := h.outsider(t, "mia")
	mia := api.client(t)
	mia.signIn(t, miaEmail)
	want(t, ada.post("/api/v1/projects", map[string]any{"name": "Hidden", "key": hidden}), http.StatusCreated, "a project Mia cannot see")
	var miaID string
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM app_user WHERE email = $1`, miaEmail).Scan(&miaID); err != nil {
		t.Fatal(err)
	}
	grant := func(role string) string {
		return idOf(t, want(t, ada.post("/api/v1/role-assignments", map[string]any{"role": role, "projectKey": key, "userId": miaID}), http.StatusCreated, "a "+role+" in the project"), "assignment")
	}
	working := grant("user")

	stator := register(t, api, h.name(t, "Stator"), "read issue.write issue.transition comment.write")
	want(t, ada.post("/api/v1/oauth/clients/"+stator.id+"/approve", nil), http.StatusOK, "approving")
	h.waitForPrimary(t)

	t.Run("read alone refuses a write", func(t *testing.T) {
		app := bearer(api, t, connect(t, stator, mia, "read")["access_token"])
		resp := app.post("/api/v1/issues/"+issueKey+"/comments", map[string]any{"text": "from a reader"})
		if resp.Status != http.StatusForbidden || resp.ErrorCode() != "insufficient_scope" ||
			resp.Error()["message"] != stator.name+" was allowed to read only. Connect it again and allow it to change issues." {
			t.Errorf("a write with read alone: %d %s", resp.Status, resp.Raw)
		}
	})

	app := bearer(api, t, connect(t, stator, mia, "read issue.write")["access_token"])
	t.Run("issue.write links a page", func(t *testing.T) {
		want(t, app.post("/api/v1/issues/"+issueKey+"/remote-links", map[string]any{"url": "https://stator.example.test/page", "title": "Spec", "source": "Stator"}), http.StatusCreated, "a remote link")
	})
	t.Run("but moves nothing and changes no settings", func(t *testing.T) {
		resp := app.post("/api/v1/issues/"+issueKey+"/transitions", map[string]any{"transitionId": "x"})
		if resp.Status != http.StatusForbidden || resp.ErrorCode() != "insufficient_scope" ||
			resp.Header.Get("WWW-Authenticate") != `Bearer error="insufficient_scope", scope="issue.transition"` {
			t.Errorf("a transition: %d %q %s", resp.Status, resp.Header.Get("WWW-Authenticate"), resp.Raw)
		}
		resp = app.patch("/api/v1/auth/me", map[string]any{"name": "Renamed by an app"})
		if resp.Status != http.StatusForbidden || resp.ErrorCode() != "insufficient_scope" {
			t.Errorf("a profile change: %d %s", resp.Status, resp.Raw)
		}
	})
	t.Run("a project the person cannot see stays unseen", func(t *testing.T) {
		if resp := app.get("/api/v1/projects/" + hidden); resp.Status != http.StatusNotFound {
			t.Errorf("a hidden project through the app: %d", resp.Status)
		}
	})
	t.Run("the person losing the right narrows the app at once", func(t *testing.T) {
		want(t, ada.delete("/api/v1/role-assignments/"+working), http.StatusNoContent, "Mia no longer a user")
		grant("reader")
		resp := app.post("/api/v1/issues/"+issueKey+"/remote-links", map[string]any{"url": "https://stator.example.test/other", "title": "Other", "source": "Stator"})
		if resp.Status != http.StatusForbidden || resp.ErrorCode() != "forbidden" {
			t.Errorf("a write after Mia lost it: %d %s", resp.Status, resp.Raw)
		}
	})
}

func TestEndingAConnectedAppsGrant(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	ada := api.client(t)
	ada.signIn(t, h.person(t, "ada", auth.RoleAdmin))
	stator := register(t, api, h.name(t, "Stator"), "read")
	want(t, ada.post("/api/v1/oauth/clients/"+stator.id+"/approve", nil), http.StatusOK, "approving")

	gone := func(t *testing.T, grant map[string]any) {
		t.Helper()
		if resp := bearer(api, t, grant["access_token"]).get("/api/v1/auth/me"); resp.Status != http.StatusUnauthorized {
			t.Errorf("the access token outlived the grant: %d", resp.Status)
		}
		if resp := stator.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {grant["refresh_token"].(string)}}); resp.Body["error"] != "invalid_grant" {
			t.Errorf("the refresh token outlived the grant: %d %s", resp.Status, resp.Raw)
		}
	}

	t.Run("Disconnect in the token list", func(t *testing.T) {
		grant := connect(t, stator, ada, "read")
		var id string
		for _, each := range list(t, ada.get("/api/v1/tokens"), "tokens") {
			if tok := each.(map[string]any); tok["app"] != nil {
				id = tok["id"].(string)
			}
		}
		want(t, ada.delete("/api/v1/tokens/"+id), http.StatusNoContent, "disconnecting")
		gone(t, grant)
	})

	t.Run("revoking either token at the app", func(t *testing.T) {
		for _, which := range []string{"access_token", "refresh_token"} {
			grant := connect(t, stator, ada, "read")
			resp := stator.c.form("/api/v1/oauth/revoke", url.Values{"token": {grant[which].(string)}}, stator.id, stator.secret)
			if resp.Status != http.StatusOK || resp.Raw != "" {
				t.Fatalf("revoking the %s: %d %s", which, resp.Status, resp.Raw)
			}
			gone(t, grant)
		}
		want(t, stator.c.form("/api/v1/oauth/revoke", url.Values{"token": {"armature_oat_unknown"}}, stator.id, stator.secret), http.StatusOK, "an unknown token")
		want(t, stator.c.form("/api/v1/oauth/revoke", url.Values{"token": {"x"}}, stator.id, "armature_ocs_wrong"), http.StatusUnauthorized, "a wrong secret")
		want(t, stator.c.form("/api/v1/oauth/revoke", url.Values{"token": {"x"}, "client_id": {stator.id}, "client_secret": {stator.secret}}, stator.id, stator.secret), http.StatusBadRequest, "two ways at once")
	})

	t.Run("switching the person off", func(t *testing.T) {
		mia := api.client(t)
		miaEmail := h.person(t, "mia", auth.RoleMember)
		mia.signIn(t, miaEmail)
		grant := connect(t, stator, mia, "read")
		var miaID string
		if err := h.super.QueryRow(context.Background(), `SELECT id FROM app_user WHERE email = $1`, miaEmail).Scan(&miaID); err != nil {
			t.Fatal(err)
		}
		want(t, ada.delete("/api/v1/members/"+miaID), http.StatusNoContent, "letting Mia go")
		gone(t, grant)
	})
}

func TestAnExchangedTokenIsNarrowerAndEndsWithItsGrant(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	key := h.projectKey(t, "OAX")
	ada := api.client(t)
	ada.signIn(t, h.person(t, "ada", auth.RoleAdmin))
	want(t, ada.post("/api/v1/projects", map[string]any{"name": "Exchanged", "key": key}), http.StatusCreated, "a project")
	issueKey := obj(t, want(t, ada.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "for a workflow"}), http.StatusCreated, "an issue"), "issue")["key"].(string)
	rotor := register(t, api, h.name(t, "Rotor"), "read comment.write")
	want(t, ada.post("/api/v1/oauth/clients/"+rotor.id+"/approve", nil), http.StatusOK, "approving")
	grant := connect(t, rotor, ada, "read comment.write")
	h.waitForPrimary(t)

	exchange := url.Values{
		"grant_type": {oauth.GrantTokenExchange}, "subject_token": {grant["access_token"].(string)},
		"subject_token_type": {oauth.TokenTypeAccess}, "scope": {"read"}, "armature_expires_in": {"600"},
	}
	narrow := want(t, rotor.token(exchange), http.StatusOK, "exchanging")
	if narrow.Body["expires_in"] != float64(600) || narrow.Body["issued_token_type"] != oauth.TokenTypeAccess || narrow.Body["refresh_token"] != nil {
		t.Errorf("the exchanged token = %s", narrow.Raw)
	}
	reader := bearer(api, t, narrow.Body["access_token"])
	if resp := reader.post("/api/v1/issues/"+issueKey+"/comments", map[string]any{"text": "from a run"}); resp.ErrorCode() != "insufficient_scope" {
		t.Errorf("the narrower token wrote: %d %s", resp.Status, resp.Raw)
	}
	want(t, bearer(api, t, grant["access_token"]).post("/api/v1/issues/"+issueKey+"/comments", map[string]any{"text": "from the grant"}), http.StatusCreated, "the grant still writes")

	exchange.Set("scope", "read issue.write")
	if resp := rotor.token(exchange); resp.Body["error"] != "invalid_scope" {
		t.Errorf("an exchange wider than the grant: %d %s", resp.Status, resp.Raw)
	}
	exchange.Set("scope", "read")
	exchange.Set("armature_expires_in", "7200")
	if resp := rotor.token(exchange); resp.Body["error"] != "invalid_request" {
		t.Errorf("an exchange longer than the hour: %d %s", resp.Status, resp.Raw)
	}

	want(t, rotor.c.form("/api/v1/oauth/revoke", url.Values{"token": {narrow.Body["access_token"].(string)}}, rotor.id, rotor.secret), http.StatusOK, "revoking the narrow one")
	if resp := reader.get("/api/v1/auth/me"); resp.Status != http.StatusUnauthorized {
		t.Errorf("a revoked exchanged token: %d", resp.Status)
	}
	want(t, bearer(api, t, grant["access_token"]).get("/api/v1/auth/me"), http.StatusOK, "the grant it came from")

	exchange.Del("armature_expires_in")
	again := bearer(api, t, want(t, rotor.token(exchange), http.StatusOK, "exchanging again").Body["access_token"])
	want(t, rotor.c.form("/api/v1/oauth/revoke", url.Values{"token": {grant["refresh_token"].(string)}}, rotor.id, rotor.secret), http.StatusOK, "disconnecting")
	if resp := again.get("/api/v1/auth/me"); resp.Status != http.StatusUnauthorized {
		t.Errorf("an exchanged token outlived its grant: %d", resp.Status)
	}
}

func TestAnIdentityProviderTokenIsExchangedForAGrant(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	provider := newIDP(t)
	ws := h.newProject(t, "kcexchange")
	_, armatureClient := configured(t, h, ws, provider, false)
	adaEmail := h.person(t, "ada", auth.RoleAdmin)
	ada := api.client(t)
	ada.signIn(t, adaEmail)

	made := want(t, ada.post("/api/v1/oauth/clients", map[string]any{
		"name": h.name(t, "Stator"), "redirectUris": []string{partnerReturn}, "scopes": []string{"read", "issue.write", "webhooks.partner"},
	}), http.StatusCreated, "a client")
	stator := partner{id: obj(t, made, "client")["id"].(string), secret: obj(t, made, "client")["secret"].(string), c: api.client(t)}
	signedIn := func(claims map[string]any) string {
		full := map[string]any{"iss": provider.URL, "aud": []string{armatureClient, "account"}, "azp": "stator-kc",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "sub": "kc-ada",
			"email": adaEmail, "email_verified": true, "name": "Ada Lovelace"}
		for k, v := range claims {
			full[k] = v
		}
		token, err := provider.sign(provider.key, full)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	exchange := func(token string, scope string) response {
		return stator.token(url.Values{"grant_type": {oauth.GrantTokenExchange}, "subject_token": {token},
			"subject_token_type": {oauth.TokenTypeAccess}, "scope": {scope}})
	}

	if resp := exchange(signedIn(nil), "read"); resp.Body["error"] != "unauthorized_client" {
		t.Errorf("a client with no Keycloak client named: %d %s", resp.Status, resp.Raw)
	}
	want(t, ada.patch("/api/v1/oauth/clients/"+stator.id, map[string]any{"keycloakClientId": "stator-kc"}), http.StatusOK, "naming its Keycloak client")

	granted := want(t, exchange(signedIn(nil), "read issue.write"), http.StatusOK, "the exchange")
	if granted.Body["refresh_token"] == nil || granted.Body["scope"] != "read issue.write" {
		t.Errorf("the grant = %s", granted.Raw)
	}
	me := want(t, bearer(api, t, granted.Body["access_token"]).get("/api/v1/auth/me"), http.StatusOK, "who it is")
	if principalField(t, me, "principal", "user", "email") != adaEmail {
		t.Errorf("the exchange connected %s", me.Raw)
	}

	for what, c := range map[string]struct {
		claims map[string]any
		scope  string
		code   string
	}{
		"another client's token":   {map[string]any{"azp": "someone-else"}, "read", "invalid_grant"},
		"a token not for Armature": {map[string]any{"aud": "account"}, "read", "invalid_grant"},
		"an expired token":         {map[string]any{"exp": time.Now().Add(-time.Minute).Unix()}, "read", "invalid_grant"},
		"somebody with no account": {map[string]any{"email": "nobody-" + h.name(t, "x") + "@armature.test"}, "read", "invalid_grant"},
		"the webhook scope":        {nil, "read webhooks.partner", "invalid_scope"},
	} {
		if resp := exchange(signedIn(c.claims), c.scope); resp.Status != http.StatusBadRequest || resp.Body["error"] != c.code {
			t.Errorf("%s: %d %s, want %s", what, resp.Status, resp.Raw, c.code)
		}
	}
}

func TestRegistrationClosedLeavesConnectingToAdministrators(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	api.api.OAuth = oauth.NewService(h.cluster, api.api.AppBaseURL).WithRegistration(oauth.RegistrationClosed)
	c := api.client(t)
	resp := c.post("/api/v1/oauth/register", map[string]any{"client_name": "Stator", "redirect_uris": []string{partnerReturn}})
	if resp.Status != http.StatusForbidden || resp.Body["error"] != "access_denied" ||
		!strings.Contains(resp.Body["error_description"].(string), "add Stator under Settings, Connected apps") {
		t.Errorf("registering where it is closed: %d %s", resp.Status, resp.Raw)
	}
	if meta := want(t, c.get(oauth.ServerMetadataPath), http.StatusOK, "the metadata"); meta.Body["registration_endpoint"] != nil {
		t.Errorf("a closed Armature names a registration endpoint: %s", meta.Raw)
	}
}

func TestRegistrationRefusesMetadataItCannotKeep(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	for what, c := range map[string]struct {
		body map[string]any
		code string
	}{
		"plain http":       {map[string]any{"client_name": "Stator", "redirect_uris": []string{"http://stator.example.test/cb"}}, "invalid_redirect_uri"},
		"a fragment":       {map[string]any{"client_name": "Stator", "redirect_uris": []string{"https://stator.example.test/cb#x"}}, "invalid_redirect_uri"},
		"no name":          {map[string]any{"redirect_uris": []string{partnerReturn}}, "invalid_client_metadata"},
		"an unknown grant": {map[string]any{"client_name": "Stator", "redirect_uris": []string{partnerReturn}, "grant_types": []string{"password"}}, "invalid_client_metadata"},
	} {
		if resp := api.client(t).post("/api/v1/oauth/register", c.body); resp.Status != http.StatusBadRequest || resp.Body["error"] != c.code {
			t.Errorf("%s: %d %s", what, resp.Status, resp.Raw)
		}
	}
	public := want(t, c.post("/api/v1/oauth/register", map[string]any{
		"client_name": h.name(t, "Desktop"), "redirect_uris": []string{"http://127.0.0.1:7777/cb"}, "token_endpoint_auth_method": "none", "logo_uri": "https://x.test/logo.png",
	}), http.StatusCreated, "a public client on this machine")
	if public.Body["client_secret"] != nil || public.Body["scope"] != "read" {
		t.Errorf("a public client = %s", public.Raw)
	}
}

func TestTheMetadataSaysWhereEverythingIs(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	server := want(t, c.get(oauth.ServerMetadataPath), http.StatusOK, "the server's metadata")
	for field, value := range map[string]string{
		"issuer": "http://app.test", "authorization_endpoint": "http://app.test/oauth/authorize",
		"token_endpoint": "http://app.test/api/v1/oauth/token", "revocation_endpoint": "http://app.test/api/v1/oauth/revoke",
		"registration_endpoint": "http://app.test/api/v1/oauth/register",
	} {
		if server.Body[field] != value {
			t.Errorf("%s = %v, want %s", field, server.Body[field], value)
		}
	}
	if methods := server.Body["code_challenge_methods_supported"].([]any); len(methods) != 1 || methods[0] != "S256" {
		t.Errorf("PKCE methods = %v", methods)
	}
	resource := want(t, c.get(oauth.ResourceMetadataPath), http.StatusOK, "the resource's metadata")
	if resource.Body["resource"] != "http://app.test/api/v1" {
		t.Errorf("resource = %s", resource.Raw)
	}
	resp := c.post("/api/v1/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if resp.Status != http.StatusUnauthorized || resp.Header.Get("WWW-Authenticate") != `Bearer resource_metadata="http://app.test/.well-known/oauth-protected-resource"` {
		t.Errorf("an MCP call without a token: %d %q", resp.Status, resp.Header.Get("WWW-Authenticate"))
	}
}

func TestAdministratorsKeepTheConnectedApps(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	ada := api.client(t)
	ada.signIn(t, h.person(t, "ada", auth.RoleAdmin))
	mia := api.client(t)
	mia.signIn(t, h.person(t, "mia", auth.RoleMember))
	name := h.name(t, "Exciter")

	want(t, mia.get("/api/v1/oauth/clients"), http.StatusForbidden, "a member listing apps")
	want(t, ada.post("/api/v1/oauth/clients", map[string]any{"name": name, "redirectUris": []string{"ftp://x"}, "scopes": []string{"read"}}), http.StatusUnprocessableEntity, "a bad return address")
	made := want(t, ada.post("/api/v1/oauth/clients", map[string]any{
		"name": name, "redirectUris": []string{partnerReturn}, "scopes": []string{"read", "comment.write"},
	}), http.StatusCreated, "an app by hand")
	client := obj(t, made, "client")
	exciter := partner{id: client["id"].(string), secret: client["secret"].(string), c: api.client(t)}
	if client["status"] != "active" || client["confidential"] != true || !strings.HasPrefix(exciter.secret, auth.ClientSecretPrefix) {
		t.Errorf("the app = %v", client)
	}
	grant := connect(t, exciter, mia, "read comment.write")

	t.Run("it is listed with its grant and no secret", func(t *testing.T) {
		var found map[string]any
		for _, each := range list(t, want(t, ada.get("/api/v1/oauth/clients"), http.StatusOK, "the apps"), "clients") {
			if each.(map[string]any)["id"] == exciter.id {
				found = each.(map[string]any)
			}
		}
		if found == nil || found["grants"] != float64(1) || found["secret"] != nil || found["webhook"] != nil {
			t.Errorf("the listed app = %v", found)
		}
	})

	t.Run("narrowing its scopes narrows the grant at once", func(t *testing.T) {
		want(t, ada.patch("/api/v1/oauth/clients/"+exciter.id, map[string]any{"scopes": []string{"read"}}), http.StatusOK, "narrowing")
		h.waitForPrimary(t)
		if resp := bearer(api, t, grant["access_token"]).get("/api/v1/auth/me"); obj(t, resp, "token")["scopes"].([]any)[0] != "read" || len(obj(t, resp, "token")["scopes"].([]any)) != 1 {
			t.Errorf("the grant after narrowing: %s", resp.Raw)
		}
		want(t, ada.patch("/api/v1/oauth/clients/"+exciter.id, map[string]any{"scopes": []string{"issue.write"}}), http.StatusUnprocessableEntity, "scopes without read")
	})

	t.Run("a new secret stops the old one", func(t *testing.T) {
		rotated := want(t, ada.post("/api/v1/oauth/clients/"+exciter.id+"/rotate-secret", nil), http.StatusOK, "rotating")
		refresh := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {grant["refresh_token"].(string)}}
		if resp := exciter.token(refresh); resp.Body["error"] != "invalid_client" {
			t.Errorf("the old secret: %d %s", resp.Status, resp.Raw)
		}
		exciter.secret = obj(t, rotated, "client")["secret"].(string)
		grant = want(t, exciter.token(refresh), http.StatusOK, "the new secret").Body
	})

	t.Run("removing it ends every grant", func(t *testing.T) {
		want(t, ada.delete("/api/v1/oauth/clients/"+exciter.id), http.StatusNoContent, "removing")
		want(t, ada.delete("/api/v1/oauth/clients/"+exciter.id), http.StatusNotFound, "removing it again")
		want(t, ada.post("/api/v1/oauth/clients/"+exciter.id+"/approve", nil), http.StatusNotFound, "approving what is gone")
		want(t, ada.post("/api/v1/oauth/clients/"+exciter.id+"/rotate-secret", nil), http.StatusNotFound, "rotating what is gone")
		want(t, ada.patch("/api/v1/oauth/clients/"+exciter.id, map[string]any{"name": "Gone"}), http.StatusNotFound, "changing what is gone")
		if resp := bearer(api, t, grant["access_token"]).get("/api/v1/auth/me"); resp.Status != http.StatusUnauthorized {
			t.Errorf("a grant outlived its app: %d", resp.Status)
		}
	})

	t.Run("a public client has no secret to rotate", func(t *testing.T) {
		public := want(t, ada.post("/api/v1/oauth/clients", map[string]any{
			"name": h.name(t, "Desktop"), "redirectUris": []string{"http://localhost:7777/cb"}, "scopes": []string{"read"}, "confidential": false,
		}), http.StatusCreated, "a public app")
		id := obj(t, public, "client")["id"].(string)
		want(t, ada.post("/api/v1/oauth/clients/"+id+"/rotate-secret", nil), http.StatusConflict, "rotating a public app's secret")
	})
}
