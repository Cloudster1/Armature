//go:build integration

package test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An address somebody typed is followed by the server itself, so what it must
// not reach is whatever else answers inside this network.
func TestAnAddressSomebodyTypedIsCheckedBeforeItIsCalled(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "outbound")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Reaching", "key": "RCH"}), http.StatusCreated, "a project")

	t.Run("a repository is reached over http or not at all", func(t *testing.T) {
		for _, address := range []string{"file:///etc/passwd", "gopher://inside:70/", "not a url at all"} {
			refused := owner.post("/api/v1/projects/RCH/repositories", map[string]any{
				"host": "gitea", "name": "acme/app", "url": address,
			})
			if refused.Status != http.StatusBadRequest {
				t.Errorf("%q was accepted as a repository address: %d %s", address, refused.Status, refused.Raw)
			}
		}
		refused := owner.post("/api/v1/projects/RCH/repositories", map[string]any{
			"host": "gitea", "name": "acme/app", "apiBaseUrl": "file:///etc/passwd",
		})
		if refused.Status != http.StatusBadRequest {
			t.Errorf("a file address was accepted as an API address: %d %s", refused.Status, refused.Raw)
		}
	})

	t.Run("a host's answer is not read back to the caller", func(t *testing.T) {
		// The host says something only its operator should see; the refusal
		// carries the status and nothing of the body.
		talkative := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("root:x:0:0:the body of whatever answered"))
		}))
		defer talkative.Close()
		made := want(t, owner.post("/api/v1/projects/RCH/repositories", map[string]any{
			"host": "gitea", "name": "acme/app", "apiBaseUrl": talkative.URL, "accessToken": "t",
		}), http.StatusCreated, "a repository at a host under our control")
		repository := idOf(t, made, "repository")
		issue := obj(t, want(t, owner.post("/api/v1/projects/RCH/issues", map[string]any{"summary": "make a branch"}), http.StatusCreated, "an issue"), "issue")
		answered := owner.post("/api/v1/issues/"+issue["key"].(string)+"/branches", map[string]any{"name": "feature/x", "repositoryId": repository})
		if strings.Contains(answered.Raw, "root:x:0:0") {
			t.Fatalf("the host's body came back to the caller: %s", answered.Raw)
		}
		if answered.Status != http.StatusBadGateway {
			t.Fatalf("a refusing host answered %d: %s", answered.Status, answered.Raw)
		}
	})
}
