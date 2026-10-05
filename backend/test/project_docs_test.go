//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// A project can point at its documentation elsewhere, so the docs are one click
// from the board; the address has to be a web page, here and in the database.
func TestAProjectLinksToItsDocumentation(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	orgID := obj(t, owner.signup(t, h, "documented"), "principal", "org")["id"].(string)
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Documented", "key": "DOCS"}), http.StatusCreated, "create project")

	t.Run("a new project has no link", func(t *testing.T) {
		p := obj(t, want(t, owner.get("/api/v1/projects/DOCS"), http.StatusOK, "read project"), "project")
		if _, set := p["docsUrl"]; set {
			t.Errorf("a new project links to %v", p["docsUrl"])
		}
	})

	t.Run("the link is set, named and read back", func(t *testing.T) {
		updated := obj(t, want(t, owner.patch("/api/v1/projects/DOCS", map[string]any{
			"docsUrl": " https://wiki.example.com/spaces/DOCS ", "docsLabel": " Team wiki ",
		}), http.StatusOK, "set the link"), "project")
		if updated["docsUrl"] != "https://wiki.example.com/spaces/DOCS" || updated["docsLabel"] != "Team wiki" {
			t.Fatalf("project = %v", updated)
		}
		read := obj(t, want(t, owner.get("/api/v1/projects/DOCS"), http.StatusOK, "read project"), "project")
		if read["docsUrl"] != updated["docsUrl"] || read["docsLabel"] != "Team wiki" {
			t.Errorf("read back = %v", read)
		}
		// Changing something else leaves the link alone.
		renamed := obj(t, want(t, owner.patch("/api/v1/projects/DOCS", map[string]any{"name": "Documented well"}), http.StatusOK, "rename"), "project")
		if renamed["docsUrl"] != updated["docsUrl"] {
			t.Errorf("a rename lost the link: %v", renamed)
		}
	})

	t.Run("an address that is not a web page is refused with a sentence on the field", func(t *testing.T) {
		for _, bad := range []string{"javascript:alert(1)", "wiki.example.com", "ftp://files.example.com"} {
			refused := want(t, owner.patch("/api/v1/projects/DOCS", map[string]any{"docsUrl": bad}), http.StatusUnprocessableEntity, bad)
			fields, _ := refused.Error()["fields"].(map[string]any)
			message, _ := fields["docsUrl"].(string)
			if !strings.Contains(message, "http:// or https://") || !strings.HasSuffix(message, ".") {
				t.Errorf("%q refused with %v", bad, refused.Error())
			}
		}
		refused := want(t, owner.patch("/api/v1/projects/DOCS", map[string]any{"docsLabel": strings.Repeat("x", 61)}), http.StatusUnprocessableEntity, "a long label")
		if fields, _ := refused.Error()["fields"].(map[string]any); fields["docsLabel"] == nil {
			t.Errorf("a long label refused with %v", refused.Error())
		}
		read := obj(t, owner.get("/api/v1/projects/DOCS"), "project")
		if read["docsUrl"] != "https://wiki.example.com/spaces/DOCS" {
			t.Errorf("a refused change touched the link: %v", read["docsUrl"])
		}
	})

	t.Run("a new address drops the old name, and an empty one clears the link", func(t *testing.T) {
		moved := obj(t, want(t, owner.patch("/api/v1/projects/DOCS", map[string]any{"docsUrl": "https://docs.example.com/docs"}), http.StatusOK, "move the docs"), "project")
		if moved["docsUrl"] != "https://docs.example.com/docs" || moved["docsLabel"] != nil {
			t.Errorf("moved = %v", moved)
		}
		want(t, owner.patch("/api/v1/projects/DOCS", map[string]any{"docsLabel": "Docs site"}), http.StatusOK, "name it")
		cleared := obj(t, want(t, owner.patch("/api/v1/projects/DOCS", map[string]any{"docsUrl": ""}), http.StatusOK, "clear the link"), "project")
		if cleared["docsUrl"] != nil || cleared["docsLabel"] != nil {
			t.Errorf("cleared = %v", cleared)
		}
		want(t, owner.patch("/api/v1/projects/DOCS", map[string]any{"docsLabel": "Orphan"}), http.StatusUnprocessableEntity, "a label with no address")
	})

	t.Run("the database refuses what the service refuses", func(t *testing.T) {
		for name, query := range map[string]string{
			"a script address":        `UPDATE project SET docs_url = 'javascript:alert(1)' WHERE key = 'DOCS' AND org_id = $1`,
			"an address with no host": `UPDATE project SET docs_url = 'https://' WHERE key = 'DOCS' AND org_id = $1`,
			"a label with no address": `UPDATE project SET docs_url = NULL, docs_label = 'Docs' WHERE key = 'DOCS' AND org_id = $1`,
			"a blank label":           `UPDATE project SET docs_url = 'https://docs.test', docs_label = '  ' WHERE key = 'DOCS' AND org_id = $1`,
		} {
			_, err := h.super.Exec(context.Background(), query, orgID)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Errorf("%s: got %v, want a check violation", name, err)
			}
		}
	})
}
