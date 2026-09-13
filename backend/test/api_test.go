//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"
)

func TestAPIAuthFlow(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	t.Run("signup, me, logout", func(t *testing.T) {
		c := api.client(t)

		signup := c.signup(t, h, "flow")
		if got := principalField(t, signup, "principal", "role"); got != "owner" {
			t.Errorf("role = %v, want owner", got)
		}

		// The session cookie must be locked down: unreachable from scripts and
		// not sent on cross-site requests.
		var session *http.Cookie
		for _, cookie := range signup.Cookies {
			if cookie.Name == testCookieName {
				session = cookie
			}
		}
		if session == nil {
			t.Fatal("signup did not set a session cookie")
		}
		if !session.HttpOnly {
			t.Error("the session cookie is readable by scripts")
		}
		if session.SameSite != http.SameSiteLaxMode {
			t.Error("the session cookie is not SameSite=Lax")
		}
		if session.Path != "/" {
			t.Errorf("cookie path = %q, want /", session.Path)
		}

		me := c.get("/api/v1/auth/me")
		if me.Status != http.StatusOK {
			t.Fatalf("me returned %d: %s", me.Status, me.Raw)
		}
		orgs, ok := me.Body["organizations"].([]any)
		if !ok || len(orgs) != 1 {
			t.Errorf("organizations = %v, want exactly one", me.Body["organizations"])
		}

		if out := c.post("/api/v1/auth/logout", nil); out.Status != http.StatusNoContent {
			t.Fatalf("logout returned %d: %s", out.Status, out.Raw)
		}

		if after := c.get("/api/v1/auth/me"); after.Status != http.StatusUnauthorized {
			t.Errorf("me after logout returned %d, want 401", after.Status)
		}
	})

	t.Run("login through the API", func(t *testing.T) {
		c := api.client(t)
		signup := c.signup(t, h, "relogin")
		email := principalField(t, signup, "principal", "user", "email").(string)

		fresh := api.client(t)
		login := fresh.post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword})
		if login.Status != http.StatusOK {
			t.Fatalf("login returned %d: %s", login.Status, login.Raw)
		}
		if got := fresh.get("/api/v1/auth/me"); got.Status != http.StatusOK {
			t.Errorf("me after login returned %d", got.Status)
		}
	})

	t.Run("a wrong password is a 401 with a non-committal message", func(t *testing.T) {
		c := api.client(t)
		signup := c.signup(t, h, "wrongpw")
		email := principalField(t, signup, "principal", "user", "email").(string)

		resp := api.client(t).post("/api/v1/auth/login", map[string]string{
			"email": email, "password": "not the right password",
		})
		if resp.Status != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.Status)
		}
		if resp.ErrorCode() != "invalid_credentials" {
			t.Errorf("code = %q, want invalid_credentials", resp.ErrorCode())
		}

		// An unknown address must be indistinguishable from a wrong password.
		unknown := api.client(t).post("/api/v1/auth/login", map[string]string{
			"email": "nobody-" + unique("z") + "@armature.test", "password": testPassword,
		})
		if unknown.Status != resp.Status || unknown.ErrorCode() != resp.ErrorCode() {
			t.Errorf("an unknown address answers %d/%s but a wrong password answers %d/%s; the difference identifies which accounts exist",
				unknown.Status, unknown.ErrorCode(), resp.Status, resp.ErrorCode())
		}
	})

	t.Run("a bearer token authenticates without a cookie", func(t *testing.T) {
		owner := api.client(t)
		owner.signup(t, h, "bearer")

		created := owner.post("/api/v1/tokens", map[string]any{"name": "ci"})
		if created.Status != http.StatusCreated {
			t.Fatalf("create token returned %d: %s", created.Status, created.Raw)
		}
		secret, _ := principalField(t, created, "token", "secret").(string)
		if secret == "" {
			t.Fatal("no secret was returned")
		}

		// A client with no cookie jar entry at all, only the header.
		bearer := api.client(t)
		bearer.bearer = secret
		me := bearer.get("/api/v1/auth/me")
		if me.Status != http.StatusOK {
			t.Fatalf("bearer request returned %d: %s", me.Status, me.Raw)
		}
	})

	t.Run("an API token cannot switch organization", func(t *testing.T) {
		owner := api.client(t)
		owner.signup(t, h, "bearerswitch")
		created := owner.post("/api/v1/tokens", map[string]any{"name": "ci"})
		secret := principalField(t, created, "token", "secret").(string)

		bearer := api.client(t)
		bearer.bearer = secret
		resp := bearer.post("/api/v1/auth/switch-org", map[string]string{"slug": "anything"})
		if resp.Status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400: a token is bound to one organization", resp.Status)
		}
	})
}

func TestAPIAuthorization(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	// Every endpoint that needs a signed-in caller must say so, rather than
	// falling through to a handler that then queries with no tenant scope.
	t.Run("protected endpoints reject anonymous callers", func(t *testing.T) {
		anonymous := api.client(t)
		for _, ep := range []struct {
			method, path string
		}{
			{http.MethodGet, "/api/v1/auth/me"},
			{http.MethodPost, "/api/v1/auth/logout"},
			{http.MethodPost, "/api/v1/auth/switch-org"},
			{http.MethodGet, "/api/v1/tokens"},
			{http.MethodPost, "/api/v1/tokens"},
			{http.MethodDelete, "/api/v1/tokens/00000000-0000-0000-0000-000000000000"},
			{http.MethodGet, "/api/v1/invites"},
			{http.MethodPost, "/api/v1/invites"},
		} {
			resp := anonymous.do(ep.method, ep.path, nil)
			if resp.Status != http.StatusUnauthorized {
				t.Errorf("%s %s returned %d, want 401", ep.method, ep.path, resp.Status)
			}
			if resp.ErrorCode() != "unauthorized" {
				t.Errorf("%s %s error code = %q, want unauthorized", ep.method, ep.path, resp.ErrorCode())
			}
		}
	})

	t.Run("a plain member cannot administer the organization", func(t *testing.T) {
		owner := api.client(t)
		owner.signup(t, h, "adminonly")

		// Invite a member and accept, which signs the new client in.
		invite := owner.post("/api/v1/invites", map[string]string{
			"email": h.email(t, "plainmember"), "role": "member",
		})
		if invite.Status != http.StatusCreated {
			t.Fatalf("invite returned %d: %s", invite.Status, invite.Raw)
		}
		token, _ := invite.Body["token"].(string)

		member := api.client(t)
		accepted := member.post("/api/v1/auth/invites/accept", map[string]string{
			"token": token, "name": "Plain Member", "password": testPassword,
		})
		if accepted.Status != http.StatusOK {
			t.Fatalf("accept returned %d: %s", accepted.Status, accepted.Raw)
		}
		if got := principalField(t, accepted, "principal", "role"); got != "member" {
			t.Fatalf("role = %v, want member", got)
		}

		// They can use ordinary endpoints...
		if resp := member.get("/api/v1/tokens"); resp.Status != http.StatusOK {
			t.Errorf("a member cannot list their own tokens: %d", resp.Status)
		}
		// ...but not administrative ones.
		for _, ep := range []struct{ method, path string }{
			{http.MethodGet, "/api/v1/invites"},
			{http.MethodPost, "/api/v1/invites"},
		} {
			resp := member.do(ep.method, ep.path, map[string]string{"email": "x@armature.test", "role": "member"})
			if resp.Status != http.StatusForbidden {
				t.Errorf("%s %s as a member returned %d, want 403", ep.method, ep.path, resp.Status)
			}
			if resp.ErrorCode() != "forbidden" {
				t.Errorf("%s %s code = %q, want forbidden", ep.method, ep.path, resp.ErrorCode())
			}
		}
	})

	t.Run("an admin can administer the organization", func(t *testing.T) {
		owner := api.client(t)
		owner.signup(t, h, "adminrole")
		invite := owner.post("/api/v1/invites", map[string]string{
			"email": h.email(t, "theadmin"), "role": "admin",
		})
		token, _ := invite.Body["token"].(string)

		admin := api.client(t)
		admin.post("/api/v1/auth/invites/accept", map[string]string{
			"token": token, "name": "The Admin", "password": testPassword,
		})

		if resp := admin.get("/api/v1/invites"); resp.Status != http.StatusOK {
			t.Errorf("an admin cannot list invitations: %d %s", resp.Status, resp.Raw)
		}
	})
}

func TestAPIRequestHandling(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	t.Run("every response carries a request id", func(t *testing.T) {
		resp := api.client(t).get("/healthz")
		if resp.Header.Get("X-Request-Id") == "" {
			t.Error("no X-Request-Id header on a successful response")
		}

		failed := api.client(t).get("/api/v1/auth/me")
		if failed.Header.Get("X-Request-Id") == "" {
			t.Error("no X-Request-Id header on an error response")
		}
		// The id in the body is what a user would quote in a support request,
		// so it has to match the header.
		if id, _ := failed.Error()["requestId"].(string); id != failed.Header.Get("X-Request-Id") {
			t.Errorf("body request id %q does not match header %q", id, failed.Header.Get("X-Request-Id"))
		}
	})

	t.Run("a supplied request id is echoed back", func(t *testing.T) {
		c := api.client(t)
		req, _ := http.NewRequest(http.MethodGet, c.base+"/healthz", nil)
		req.Header.Set("X-Request-Id", "caller-supplied-id")
		resp, err := c.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if got := resp.Header.Get("X-Request-Id"); got != "caller-supplied-id" {
			t.Errorf("request id = %q, want the one the caller sent", got)
		}
	})

	t.Run("malformed and hostile bodies are rejected cleanly", func(t *testing.T) {
		c := api.client(t)
		cases := []struct {
			name, body string
			wantCode   string
		}{
			{"not json", "this is not json", "bad_request"},
			{"empty", "", "bad_request"},
			{"trailing content", `{"email":"a@b.test"} {"extra":1}`, "bad_request"},
			// An unknown field usually means a client typo; silently ignoring it
			// turns a bug into mysterious missing data.
			{"unknown field", `{"email":"a@b.test","password":"x","surprise":true}`, "bad_request"},
		}
		for _, tc := range cases {
			resp := c.post("/api/v1/auth/login", tc.body)
			if resp.ErrorCode() != tc.wantCode {
				t.Errorf("%s: code = %q (status %d), want %q", tc.name, resp.ErrorCode(), resp.Status, tc.wantCode)
			}
		}
	})

	t.Run("an oversized body is refused rather than buffered", func(t *testing.T) {
		huge := `{"email":"` + strings.Repeat("a", 2<<20) + `@armature.test","password":"x"}`
		resp := api.client(t).post("/api/v1/auth/login", huge)
		if resp.Status != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 for a body over the limit", resp.Status)
		}
	})

	t.Run("unknown routes and methods answer in the same error shape", func(t *testing.T) {
		missing := api.client(t).get("/api/v1/no-such-thing")
		if missing.Status != http.StatusNotFound || missing.ErrorCode() != "not_found" {
			t.Errorf("unknown route = %d/%q, want 404/not_found", missing.Status, missing.ErrorCode())
		}

		wrongMethod := api.client(t).delete("/api/v1/auth/login")
		if wrongMethod.Status != http.StatusMethodNotAllowed || wrongMethod.ErrorCode() != "method_not_allowed" {
			t.Errorf("wrong method = %d/%q, want 405/method_not_allowed", wrongMethod.Status, wrongMethod.ErrorCode())
		}
	})

	t.Run("validation failures name the problem", func(t *testing.T) {
		resp := api.client(t).post("/api/v1/auth/signup", map[string]string{
			"email": "someone@armature.test", "password": "short", "name": "A", "orgName": "B",
		})
		if resp.Status != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422: %s", resp.Status, resp.Raw)
		}
		message, _ := resp.Error()["message"].(string)
		if !strings.Contains(strings.ToLower(message), "12 characters") {
			t.Errorf("message = %q, want it to say what is wrong with the password", message)
		}
	})

	t.Run("a duplicate signup is a conflict, not a server error", func(t *testing.T) {
		c := api.client(t)
		signup := c.signup(t, h, "conflict")
		email := principalField(t, signup, "principal", "user", "email").(string)

		again := api.client(t).post("/api/v1/auth/signup", map[string]string{
			"email": email, "password": testPassword, "name": "Twin", "orgName": "Twin Co",
			"orgSlug": h.orgSlug(t, "twin"),
		})
		if again.Status != http.StatusConflict {
			t.Errorf("status = %d, want 409", again.Status)
		}
		if again.ErrorCode() != "email_taken" {
			t.Errorf("code = %q, want email_taken", again.ErrorCode())
		}
	})

	t.Run("health endpoints need no credentials", func(t *testing.T) {
		c := api.client(t)
		if resp := c.get("/healthz"); resp.Status != http.StatusOK {
			t.Errorf("healthz = %d, want 200", resp.Status)
		}
		ready := c.get("/readyz")
		if ready.Status != http.StatusOK {
			t.Errorf("readyz = %d, want 200", ready.Status)
		}
		if _, ok := ready.Body["routing"]; !ok {
			t.Error("readyz does not report the read routing state")
		}
	})

	t.Run("cross-origin requests are allowed only from configured origins", func(t *testing.T) {
		c := api.client(t)

		allowed, _ := http.NewRequest(http.MethodGet, c.base+"/healthz", nil)
		allowed.Header.Set("Origin", "http://localhost:5173")
		resp, err := c.http.Do(allowed)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
			t.Errorf("allowed origin header = %q, want the configured origin", got)
		}
		if resp.Header.Get("Access-Control-Allow-Credentials") != "true" {
			t.Error("credentials are not allowed, so the session cookie would not be sent")
		}

		denied, _ := http.NewRequest(http.MethodGet, c.base+"/healthz", nil)
		denied.Header.Set("Origin", "https://evil.example")
		resp2, err := c.http.Do(denied)
		if err != nil {
			t.Fatal(err)
		}
		resp2.Body.Close()
		if got := resp2.Header.Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("an unconfigured origin was allowed: %q", got)
		}
	})
}
