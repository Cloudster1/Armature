//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// An administrator fills the installation with the demo once. A second time,
// or over a project holding one of its keys, is refused before anything is made.
func TestTheDemoProjectIsAddedOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	ctx := context.Background()

	owner := api.client(t)
	orgID := principalField(t, owner.signup(t, h, "demo"), "principal", "org", "id").(string)
	invited := want(t, owner.post("/api/v1/invites", map[string]any{"email": h.email(t, "demomember"), "role": "member"}), http.StatusCreated, "invite")
	member := api.client(t)
	want(t, member.post("/api/v1/auth/invites/accept", map[string]any{"token": invited.Body["token"], "name": "Onlooker", "password": testPassword}), http.StatusOK, "accept")

	count := func(org, query string, args ...any) int {
		t.Helper()
		var n int
		if err := h.super.QueryRow(ctx, query, append([]any{org}, args...)...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	issuesIn := func(org, key string) int {
		t.Helper()
		return count(org, `SELECT count(*) FROM issue i JOIN project p ON p.id = i.project_id WHERE p.org_id = $1 AND p.key = $2`, key)
	}
	projects := func(org string) int {
		t.Helper()
		return count(org, `SELECT count(*) FROM project WHERE org_id = $1 AND key IN ('LINE', 'MAINT')`)
	}

	refused := member.post("/api/v1/installation/demo-project", nil)
	if refused.Status != http.StatusForbidden {
		t.Fatalf("a member adds the demo: %d %s", refused.Status, refused.Raw)
	}
	if projects(orgID) != 0 {
		t.Fatal("a refused demo made projects")
	}

	added := want(t, owner.post("/api/v1/installation/demo-project", nil), http.StatusCreated, "add the demo")
	if key := obj(t, added, "project")["key"]; key != "LINE" {
		t.Fatalf("the demo answered with project %v, want LINE", key)
	}
	line, maint := issuesIn(orgID, "LINE"), issuesIn(orgID, "MAINT")
	if line == 0 || maint == 0 {
		t.Fatalf("the demo filed %d issues on the line and %d in maintenance, want both filled", line, maint)
	}
	if running := count(orgID, `SELECT count(*) FROM sprint s JOIN project p ON p.id = s.project_id WHERE p.org_id = $1 AND p.key = 'LINE' AND s.state = 'active'`); running == 0 {
		t.Error("the line has no maintenance window running")
	}
	want(t, member.get("/api/v1/projects/LINE"), http.StatusOK, "the member sees the line")
	if h.auditCount(t, orgID, "installation.demo_added") != 1 {
		t.Error("adding the demo was not recorded")
	}
	// The demo is projects: the calendar everybody keeps stays the installation's own.
	if days := count(orgID, `SELECT count(*) FROM holiday WHERE org_id = $1`); days != 0 {
		t.Errorf("the demo wrote %d days off into the installation's calendar", days)
	}

	again := owner.post("/api/v1/installation/demo-project", nil)
	if again.Status != http.StatusConflict {
		t.Fatalf("the demo a second time: %d %s", again.Status, again.Raw)
	}
	if words := again.Error()["message"].(string); !strings.Contains(words, "LINE") || !strings.Contains(words, "Production line 3") {
		t.Errorf("the second time is refused with %q, which neither names the key nor says where the demo is", words)
	}
	if issuesIn(orgID, "LINE") != line || issuesIn(orgID, "MAINT") != maint {
		t.Error("the refused second demo filed issues anyway")
	}
	if h.auditCount(t, orgID, "installation.demo_added") != 1 {
		t.Error("the refused second demo was recorded")
	}

	// Until the suite stops making an organization per test, each of these is
	// an installation of its own.
	t.Run("a project of its own under one of the keys holds the demo off whole", func(t *testing.T) {
		other := api.client(t)
		otherOrg := principalField(t, other.signup(t, h, "demokeys"), "principal", "org", "id").(string)
		want(t, other.post("/api/v1/projects", map[string]any{"name": "Maintenance", "key": "MAINT", "template": "task-tracking"}), http.StatusCreated, "a project of its own")

		refused := other.post("/api/v1/installation/demo-project", nil)
		if refused.Status != http.StatusConflict || !strings.Contains(refused.Error()["message"].(string), "MAINT") {
			t.Fatalf("the demo over MAINT: %d %s", refused.Status, refused.Raw)
		}
		if n := projects(otherOrg); n != 1 {
			t.Errorf("the refused demo left %d projects under its keys, want only the installation's own", n)
		}
	})

	t.Run("a status of the demo's name is used rather than refused", func(t *testing.T) {
		other := api.client(t)
		otherOrg := principalField(t, other.signup(t, h, "demostatus"), "principal", "org", "id").(string)
		want(t, other.post("/api/v1/statuses", map[string]any{"name": "Waiting for parts", "category": "in_progress"}), http.StatusCreated, "a status of its own")

		want(t, other.post("/api/v1/installation/demo-project", nil), http.StatusCreated, "add the demo beside it")
		if n := count(otherOrg, `SELECT count(*) FROM issue_status WHERE org_id = $1 AND lower(name) = 'waiting for parts'`); n != 1 {
			t.Errorf("%d statuses called Waiting for parts, want the one there was", n)
		}
		if issuesIn(otherOrg, "MAINT") == 0 {
			t.Error("the maintenance project is empty")
		}
	})
}

// Nobody deletes the installation through the API: the address is gone.
func TestTheInstallationIsNotDeletedThroughTheAPI(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	org := obj(t, owner.signup(t, h, "kept"), "principal", "org")
	gone := owner.delete("/api/v1/organization?confirm=" + org["slug"].(string))
	if gone.Status != http.StatusNotFound {
		t.Fatalf("deleting the installation answered %d %s, want 404", gone.Status, gone.Raw)
	}
	var n int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM org WHERE id = $1`, org["id"]).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("the installation went")
	}
	want(t, owner.get("/api/v1/projects"), http.StatusOK, "the owner still works")
}
