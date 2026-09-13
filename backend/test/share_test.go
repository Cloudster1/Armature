//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// A share link is a read that names its tenant: the token finds the
// organization, the link's own query narrows every widget, and nothing
// else about the dashboard is reachable through it.
func TestADashboardIsSharedByALinkUntilItIsRevoked(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "sharer")
	if made := owner.post("/api/v1/projects", map[string]any{"name": "Shared", "key": "SHR"}); made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}
	types := owner.get("/api/v1/issue-types")
	bug := find(t, types.Body["issueTypes"], "name", "Bug")["id"].(string)
	owner.post("/api/v1/projects/SHR/issues", map[string]any{"summary": "a bug", "typeId": bug})
	owner.post("/api/v1/projects/SHR/issues", map[string]any{"summary": "a task"})
	mine := owner.post("/api/v1/projects/SHR/issues", map[string]any{"summary": "mine"})
	owner.patch("/api/v1/issues/"+obj(t, mine, "issue")["key"].(string), map[string]any{"assigneeId": obj(t, owner.get("/api/v1/auth/me"), "principal", "user")["id"]})

	board := owner.firstDashboard(t, "SHR")
	dashboardID := board["id"].(string)
	var statusWidget string
	for _, w := range board["widgets"].([]any) {
		if w.(map[string]any)["kind"] == "status_breakdown" {
			statusWidget = w.(map[string]any)["id"].(string)
		}
	}

	visitor := api.client(t)

	var url, shareID string
	t.Run("the link is made once and shown once", func(t *testing.T) {
		resp := owner.post("/api/v1/dashboards/"+dashboardID+"/shares", map[string]any{"name": "Lobby screen", "query": "type = Bug"})
		if resp.Status != http.StatusCreated {
			t.Fatalf("create share: %d %s", resp.Status, resp.Raw)
		}
		url = resp.Body["url"].(string)
		shareID = obj(t, resp, "share")["id"].(string)
		if !strings.Contains(url, "/shared/") {
			t.Errorf("url = %q, want the shared page's address", url)
		}
		listed := owner.get("/api/v1/dashboards/" + dashboardID + "/shares")
		if listed.Status != http.StatusOK || len(listed.Body["shares"].([]any)) != 1 {
			t.Fatalf("shares: %d %s", listed.Status, listed.Raw)
		}
		if _, has := listed.Body["shares"].([]any)[0].(map[string]any)["url"]; has {
			t.Error("the listing carries the address; it must be shown once")
		}
	})
	token := tokenOf(url)

	t.Run("a visitor with no session opens it and reads its widgets", func(t *testing.T) {
		view := visitor.get("/api/v1/shared/" + token)
		if view.Status != http.StatusOK {
			t.Fatalf("shared view: %d %s", view.Status, view.Raw)
		}
		if obj(t, view, "dashboard")["name"] != "Overview" || view.Body["projectName"] != "Shared" {
			t.Errorf("view = %s, want the overview of Shared", view.Raw)
		}
		if got := view.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", got)
		}
		widget := visitor.get("/api/v1/shared/" + token + "/widgets/" + statusWidget)
		if widget.Status != http.StatusOK {
			t.Fatalf("shared widget: %d %s", widget.Status, widget.Raw)
		}
		if widget.Body["total"].(float64) != 1 {
			t.Errorf("total = %v through the link, want the one bug the frozen query keeps", widget.Body["total"])
		}
		own := owner.get("/api/v1/projects/SHR/reports/status_breakdown?q=type%20%3D%20Bug")
		if own.Body["total"] != widget.Body["total"] {
			t.Errorf("the link says %v and the signed-in report says %v", widget.Body["total"], own.Body["total"])
		}
	})

	t.Run("a visitor can ask only what the dashboard asks", func(t *testing.T) {
		if resp := visitor.get("/api/v1/shared/" + token + "/widgets/" + uuid.New().String()); resp.Status != http.StatusNotFound {
			t.Errorf("a widget that is not on the dashboard: %d", resp.Status)
		}
		if resp := visitor.get("/api/v1/projects/SHR/dashboards"); resp.Status != http.StatusUnauthorized {
			t.Errorf("the ordinary route without a session: %d, want 401", resp.Status)
		}
	})

	t.Run("currentUser() in the frozen query means the sharer", func(t *testing.T) {
		resp := owner.post("/api/v1/dashboards/"+dashboardID+"/shares", map[string]any{"name": "My work", "query": "assignee = currentUser()"})
		if resp.Status != http.StatusCreated {
			t.Fatal(resp.Raw)
		}
		theirs := resp.Body["url"].(string)
		widget := visitor.get("/api/v1/shared/" + tokenOf(theirs) + "/widgets/" + statusWidget)
		if widget.Status != http.StatusOK || widget.Body["total"].(float64) != 1 {
			t.Errorf("total = %v for the sharer's own work, want 1: %s", widget.Body["total"], widget.Raw)
		}
	})

	t.Run("a link with a past expiry is refused, and one that expired is gone", func(t *testing.T) {
		past := time.Now().Add(-time.Hour)
		if resp := owner.post("/api/v1/dashboards/"+dashboardID+"/shares", map[string]any{"name": "Yesterday", "expiresAt": past}); resp.Status != http.StatusBadRequest {
			t.Errorf("a past expiry: %d %s", resp.Status, resp.Raw)
		}
		if resp := owner.post("/api/v1/dashboards/"+dashboardID+"/shares", map[string]any{"name": "  "}); resp.Status != http.StatusBadRequest {
			t.Errorf("a blank name: %d %s", resp.Status, resp.Raw)
		}
		soon := owner.post("/api/v1/dashboards/"+dashboardID+"/shares", map[string]any{"name": "Brief", "expiresAt": time.Now().Add(time.Second)})
		if soon.Status != http.StatusCreated {
			t.Fatal(soon.Raw)
		}
		brief := soon.Body["url"].(string)
		if _, err := h.super.Exec(context.Background(), `UPDATE dashboard_share SET expires_at = now() - interval '1 minute' WHERE id = $1`, obj(t, soon, "share")["id"]); err != nil {
			t.Fatal(err)
		}
		if resp := visitor.get("/api/v1/shared/" + tokenOf(brief)); resp.Status != http.StatusNotFound {
			t.Errorf("an expired link: %d, want 404", resp.Status)
		}
	})

	t.Run("revoking ends the link and keeps the row", func(t *testing.T) {
		if resp := owner.delete("/api/v1/dashboards/" + dashboardID + "/shares/" + shareID); resp.Status != http.StatusNoContent {
			t.Fatalf("revoke: %d %s", resp.Status, resp.Raw)
		}
		if resp := owner.delete("/api/v1/dashboards/" + dashboardID + "/shares/" + shareID); resp.Status != http.StatusNotFound {
			t.Errorf("revoking twice: %d, want 404", resp.Status)
		}
		if resp := visitor.get("/api/v1/shared/" + token); resp.Status != http.StatusNotFound {
			t.Errorf("a revoked link: %d, want 404", resp.Status)
		}
		if resp := visitor.get("/api/v1/shared/" + token + "/widgets/" + statusWidget); resp.Status != http.StatusNotFound {
			t.Errorf("a revoked link's widget: %d, want 404", resp.Status)
		}
		var kept bool
		if err := h.super.QueryRow(context.Background(), `SELECT revoked_at IS NOT NULL FROM dashboard_share WHERE id = $1`, shareID).Scan(&kept); err != nil || !kept {
			t.Errorf("the revoked row is gone or not marked: kept=%v err=%v", kept, err)
		}
	})

	t.Run("SQL refuses a link across organizations", func(t *testing.T) {
		other := h.newWorkspace(t, "otherorg")
		_, err := h.super.Exec(context.Background(), `
			INSERT INTO dashboard_share (org_id, dashboard_id, name, token_hash)
			VALUES ($1, $2, 'Borrowed', decode('00', 'hex'))`, other.orgID, dashboardID)
		if err == nil {
			t.Fatal("SQL let a link point at another organization's dashboard")
		}
		if !strings.Contains(err.Error(), "its own organization") {
			t.Errorf("error = %v, want the guard's own message", err)
		}
	})

	t.Run("a token nobody has is gone too", func(t *testing.T) {
		if resp := visitor.get("/api/v1/shared/not-a-token"); resp.Status != http.StatusNotFound {
			t.Errorf("an unknown token: %d, want 404", resp.Status)
		}
	})
}
