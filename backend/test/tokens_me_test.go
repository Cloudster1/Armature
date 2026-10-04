//go:build integration

package test

import (
	"net/http"
	"reflect"
	"testing"
	"time"
)

// An integration asks who it is when it connects, and learns there what its
// token will be refused, rather than at the first write that fails.
func TestAuthMeTellsATokenWhatItMayDo(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "whoami")
	for _, key := range []string{"DOC", "OPS"} {
		if made := owner.post("/api/v1/projects", map[string]any{"name": "Project " + key, "key": key}); made.Status != http.StatusCreated {
			t.Fatalf("create project %s: %d %s", key, made.Status, made.Raw)
		}
	}

	t.Run("a browser session has no token", func(t *testing.T) {
		me := want(t, owner.get("/api/v1/auth/me"), http.StatusOK, "me")
		token, present := me.Body["token"]
		if !present || token != nil {
			t.Fatalf("token = %v (present %v), want null: %s", token, present, me.Raw)
		}
	})

	t.Run("a read token confined to projects says so", func(t *testing.T) {
		expires := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
		created := want(t, owner.post("/api/v1/tokens", map[string]any{
			"name": "stator", "scopes": []string{"read"}, "projects": []string{"OPS", "DOC"}, "expiresAt": expires,
		}), http.StatusCreated, "create token")
		caller := api.client(t)
		caller.bearer = principalField(t, created, "token", "secret").(string)

		token := obj(t, want(t, caller.get("/api/v1/auth/me"), http.StatusOK, "me by token"), "token")
		if token["id"] != obj(t, created, "token")["id"] || token["name"] != "stator" {
			t.Errorf("token = %v, want the one just made", token)
		}
		if got := token["scopes"]; !reflect.DeepEqual(got, []any{"read"}) {
			t.Errorf("scopes = %v, want [read]", got)
		}
		if got := token["projects"]; !reflect.DeepEqual(got, []any{"DOC", "OPS"}) {
			t.Errorf("projects = %v, want [DOC OPS]", got)
		}
		if got, err := time.Parse(time.RFC3339, token["expiresAt"].(string)); err != nil || !got.Equal(expires) {
			t.Errorf("expiresAt = %v, want %v", token["expiresAt"], expires)
		}
		if _, leaked := token["secret"]; leaked {
			t.Errorf("the secret came back: %v", token)
		}
	})

	t.Run("a full token reaches everywhere and never expires", func(t *testing.T) {
		created := want(t, owner.post("/api/v1/tokens", map[string]any{"name": "ci"}), http.StatusCreated, "create token")
		caller := api.client(t)
		caller.bearer = principalField(t, created, "token", "secret").(string)

		token := obj(t, want(t, caller.get("/api/v1/auth/me"), http.StatusOK, "me by token"), "token")
		if scopes, projects := token["scopes"].([]any), token["projects"].([]any); len(scopes) != 0 || len(projects) != 0 {
			t.Errorf("scopes = %v, projects = %v, want both empty", scopes, projects)
		}
		if expires, present := token["expiresAt"]; present && expires != nil {
			t.Errorf("expiresAt = %v, want none", expires)
		}
	})
}
