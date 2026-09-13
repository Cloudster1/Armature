//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/httpapi"
)

// The edges of the API: what a browser may send, what it is told it may do
// with an answer, how often it may guess, and what a spreadsheet reads.
func TestTheEdgesRefuseWhatABrowserWouldNotSend(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	signed := owner.signup(t, h, "edges")
	address := principalField(t, signed, "principal", "user", "email").(string)
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Edged", "key": "EDG"}), http.StatusCreated, "a project")

	t.Run("a form post carrying the session cookie is refused", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodPost, api.Server.URL+"/api/v1/projects", strings.NewReader(`{"name":"Formed","key":"FRM"}`))
		if err != nil {
			t.Fatal(err)
		}
		// A cross-site form can send this; it cannot send JSON.
		req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
		resp, body := owner.raw(req)
		if resp.StatusCode != http.StatusUnsupportedMediaType {
			t.Fatalf("a form post answered %d: %s", resp.StatusCode, body)
		}
	})

	t.Run("every answer says what a browser may do with it", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, api.Server.URL+"/api/v1/projects", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, _ := owner.raw(req)
		for header, wanted := range map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
			"Referrer-Policy":        "same-origin",
		} {
			if got := resp.Header.Get(header); got != wanted {
				t.Errorf("%s = %q, want %q", header, got, wanted)
			}
		}
		if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Errorf("policy = %q", resp.Header.Get("Content-Security-Policy"))
		}
	})

	t.Run("guessing at one address is braked", func(t *testing.T) {
		guesser := api.client(t)
		for i := 0; i < httpapi.CredentialTriesPerWindow; i++ {
			want(t, guesser.post("/api/v1/auth/login", map[string]any{"email": address, "password": "not the password"}), http.StatusUnauthorized, "a wrong password")
		}
		braked := guesser.post("/api/v1/auth/login", map[string]any{"email": address, "password": "not the password"})
		if braked.Status != http.StatusTooManyRequests {
			t.Fatalf("the eleventh guess answered %d: %s", braked.Status, braked.Raw)
		}
		// The brake is the address being guessed at, so somebody else signs in.
		other := api.client(t)
		other.signup(t, h, "unbraked")
	})

	t.Run("an export is read as text, not as formulas", func(t *testing.T) {
		want(t, owner.post("/api/v1/projects/EDG/issues", map[string]any{"summary": `=HYPERLINK("http://evil","Details")`}), http.StatusCreated, "a formula for a summary")
		h.waitForPrimary(t)
		resp, body := owner.download("/api/v1/issues/export?project=EDG")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("export: %d", resp.StatusCode)
		}
		if !strings.Contains(string(body), `'=HYPERLINK`) {
			t.Fatalf("the export carried a formula: %s", body)
		}
	})

	t.Run("readiness says whether, not why", func(t *testing.T) {
		ready := want(t, owner.get("/readyz"), http.StatusOK, "readiness")
		if ready.Body["status"] != "ok" {
			t.Fatalf("readiness = %s", ready.Raw)
		}
	})
}
