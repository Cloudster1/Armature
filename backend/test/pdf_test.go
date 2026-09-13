//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/render"
)

// fakeRenderer speaks the render service's protocol: a path in, a PDF out,
// and it remembers what it was asked to open.
func fakeRenderer(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Path string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || r.URL.Path != "/pdf" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		paths = append(paths, req.Path)
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write([]byte("%PDF-1.4\n%printed by a test\n"))
	}))
	t.Cleanup(srv.Close)
	return srv, &paths
}

// A PDF is the shared page printed by the browser we already ship: the api
// mints a link for the renderer alone, hands it the path, and streams the file.
func TestADashboardIsPrintedAsAPDF(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	renderer, opened := fakeRenderer(t)
	api.api.Renderer = render.HTTPRenderer{URL: renderer.URL}

	owner := api.client(t)
	owner.signup(t, h, "printer")
	if made := owner.post("/api/v1/projects", map[string]any{"name": "Printed", "key": "PRT"}); made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}
	dashboardID := owner.firstDashboard(t, "PRT")["id"].(string)

	t.Run("the signed-in export streams a file named after the dashboard", func(t *testing.T) {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, owner.base+"/api/v1/dashboards/"+dashboardID+"/pdf?q=type+%3D+Bug", nil)
		resp, body := owner.raw(req)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("export: %d %s", resp.StatusCode, body)
		}
		if got := resp.Header.Get("Content-Type"); got != "application/pdf" {
			t.Errorf("Content-Type = %q", got)
		}
		if !strings.HasPrefix(string(body), "%PDF") {
			t.Errorf("body starts %q, want a PDF", body[:min(8, len(body))])
		}
		if got := resp.Header.Get("Content-Disposition"); !strings.Contains(got, "attachment") || !strings.Contains(got, "PRT-overview-") {
			t.Errorf("Content-Disposition = %q, want a download named after the project and dashboard", got)
		}
		if len(*opened) != 1 || !strings.HasPrefix((*opened)[0], "/shared/") || !strings.Contains((*opened)[0], "print=1") {
			t.Errorf("the renderer opened %v, want the shared page in print mode", *opened)
		}
	})

	t.Run("the link the renderer used is nobody's afterwards", func(t *testing.T) {
		listed := owner.get("/api/v1/dashboards/" + dashboardID + "/shares")
		if len(listed.Body["shares"].([]any)) != 0 {
			t.Errorf("the renderer's link is listed: %s", listed.Raw)
		}
		var live int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM dashboard_share WHERE dashboard_id = $1 AND revoked_at IS NULL`, dashboardID).Scan(&live); err != nil {
			t.Fatal(err)
		}
		if live != 0 {
			t.Errorf("%d links to the dashboard are still live after the export", live)
		}
		path := (*opened)[0]
		token := tokenOf(path[:strings.Index(path, "?")])
		if resp := api.client(t).get("/api/v1/shared/" + token); resp.Status != http.StatusNotFound {
			t.Errorf("the renderer's link still opens: %d", resp.Status)
		}
	})

	t.Run("a shared link prints too, for whoever holds it", func(t *testing.T) {
		share := owner.post("/api/v1/dashboards/"+dashboardID+"/shares", map[string]any{"name": "Wall"})
		url := share.Body["url"].(string)
		token := tokenOf(url)
		visitor := api.client(t)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, visitor.base+"/api/v1/shared/"+token+"/pdf", nil)
		resp, body := visitor.raw(req)
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(string(body), "%PDF") {
			t.Errorf("shared print: %d %q", resp.StatusCode, body)
		}
		if resp := visitor.get("/api/v1/shared/not-a-token/pdf"); resp.Status != http.StatusNotFound {
			t.Errorf("printing a link nobody has: %d", resp.Status)
		}
	})

	t.Run("a dashboard that is not there cannot be printed", func(t *testing.T) {
		if resp := owner.get("/api/v1/dashboards/" + uuid.New().String() + "/pdf"); resp.Status != http.StatusNotFound {
			t.Errorf("printing a missing dashboard: %d", resp.Status)
		}
	})

	t.Run("without a render service the export says so", func(t *testing.T) {
		api.api.Renderer = render.Unavailable{}
		if resp := owner.get("/api/v1/dashboards/" + dashboardID + "/pdf"); resp.Status != http.StatusServiceUnavailable {
			t.Errorf("export without a renderer: %d %s", resp.Status, resp.Raw)
		}
	})
}
