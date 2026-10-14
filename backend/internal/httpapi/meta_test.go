package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/buildinfo"
)

// Anyone may ask which build answers and which API it speaks; the commit and
// when it was built are for somebody signed in, as at the foot of the screen.
func TestMetaKeepsTheCommitForSomebodySignedIn(t *testing.T) {
	version, commit, builtAt := buildinfo.Version, buildinfo.Commit, buildinfo.BuiltAt
	t.Cleanup(func() { buildinfo.Version, buildinfo.Commit, buildinfo.BuiltAt = version, commit, builtAt })
	buildinfo.Version, buildinfo.Commit, buildinfo.BuiltAt = "1.3.0", "abc1234", "2026-10-14T09:00:00Z"

	s := &Server{Log: slog.New(slog.NewTextHandler(os.Stderr, nil))}
	routes := s.Routes(nil)
	cases := []struct {
		name      string
		principal *auth.Principal
		told      bool
	}{
		{name: "nobody signed in", told: false},
		{name: "a session held at enrolment", principal: &auth.Principal{MustEnrol: true}, told: false},
		{name: "somebody signed in", principal: &auth.Principal{}, told: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
			if c.principal != nil {
				r = r.WithContext(context.WithValue(r.Context(), ctxPrincipal, c.principal))
			}
			rec := httptest.NewRecorder()
			routes.ServeHTTP(rec, r)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
			}
			var got struct {
				Product      string            `json:"product"`
				Build        map[string]string `json:"build"`
				APIVersion   string            `json:"apiVersion"`
				Capabilities *[]string         `json:"capabilities"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Product != "armature" || got.Build["version"] != "1.3.0" {
				t.Errorf("product and version = %q %q, want armature 1.3.0", got.Product, got.Build["version"])
			}
			if got.APIVersion != Spec().Info.Version {
				t.Errorf("apiVersion = %q, the document says %q", got.APIVersion, Spec().Info.Version)
			}
			if got.Capabilities == nil {
				t.Error("capabilities should be a list, even an empty one")
			}
			_, hasCommit := got.Build["commit"]
			_, hasBuiltAt := got.Build["builtAt"]
			if hasCommit != c.told || hasBuiltAt != c.told {
				t.Errorf("commit and builtAt shown = %v %v, want %v: %s", hasCommit, hasBuiltAt, c.told, rec.Body)
			}
		})
	}
}
