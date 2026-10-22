package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/oauth"
)

// A connected app's writes are named one by one, so the list must name writes
// the build has, each with a scope Armature issues.
func TestEveryAppWriteIsAWriteTheBuildHas(t *testing.T) {
	routes := map[string]bool{}
	for _, r := range Catalog() {
		routes[r.Method+" "+r.Path] = true
	}
	for op, scope := range appWrites {
		if !routes[op] {
			t.Errorf("%s is open to connected apps but not in the operations table", op)
		}
		if strings.HasPrefix(op, "GET ") {
			t.Errorf("%s is a read; every read is open to a connected app already", op)
		}
		if !slices.Contains(oauth.Scopes, scope) || scope == oauth.ScopeRead {
			t.Errorf("%s takes %q, which is not a scope that writes", op, scope)
		}
	}
}

// The router is asked which route a request is for, so a write the list does
// not name is refused before any handler, and one it names is let through.
func TestAConnectedAppIsHeldToTheWritesItsScopesName(t *testing.T) {
	mux := (&Server{}).Routes(nil).(*chi.Mux)
	app := &auth.Principal{
		User:    auth.User{ID: uuid.New(), Name: "Ada Lovelace"},
		TokenID: new(uuid.UUID),
		Scopes:  []string{oauth.ScopeRead, oauth.ScopeIssueWrite},
		App:     &auth.ConnectedApp{ClientID: uuid.New(), Name: "Stator"},
	}
	reached := false
	guard := appScope(mux)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	for _, c := range []struct {
		method, path string
		passes       bool
		scope        string
	}{
		{http.MethodPatch, "/api/v1/auth/me", false, ""},
		{http.MethodPost, "/api/v1/filters", false, ""},
		{http.MethodPost, "/api/v1/issues/CP-1/transitions", false, oauth.ScopeIssueTransition},
		{http.MethodPost, "/api/v1/issues/CP-1/remote-links", true, ""},
		{http.MethodPost, "/api/v1/issues/CP-1/branches", true, ""},
		{http.MethodPost, "/api/v1/issues/CP-1/pull-requests", true, ""},
		{http.MethodGet, "/api/v1/projects", true, ""},
		{http.MethodPost, "/api/v1/mcp", true, ""},
		// The git proxy judges git.write itself, and answers as git reads it.
		{http.MethodPost, "/api/v1/git/CP/acme/app.git/git-receive-pack", true, ""},
	} {
		reached = false
		req := httptest.NewRequest(c.method, c.path, nil)
		req = req.WithContext(context.WithValue(req.Context(), ctxPrincipal, app))
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, req)
		if reached != c.passes {
			t.Errorf("%s %s: reached the handler = %v, want %v (%d %s)", c.method, c.path, reached, c.passes, rec.Code, rec.Body)
			continue
		}
		if c.passes {
			continue
		}
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"insufficient_scope"`) {
			t.Errorf("%s %s answered %d %s, want 403 insufficient_scope", c.method, c.path, rec.Code, rec.Body)
		}
		if header := rec.Header().Get("WWW-Authenticate"); c.scope != "" && !strings.Contains(header, `scope="`+c.scope+`"`) {
			t.Errorf("%s %s: WWW-Authenticate = %q, want it to name %s", c.method, c.path, header, c.scope)
		}
	}
}
