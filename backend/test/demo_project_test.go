//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/demo"
)

// The demo is added once, and never over a project holding one of its keys. Its
// keys are an installation's to have once, so the test gets one of its own.
func TestTheDemoProjectIsAddedOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api, empty := emptyInstallation(t, h, "armature_demo_project_test")
	api.api.Auth = empty.authService().Signups(auth.SignupFirst)
	ctx := context.Background()

	owner := api.client(t)
	founded := want(t, owner.post("/api/v1/auth/signup", map[string]string{"email": h.email(t, "demo"), "password": testPassword, "name": "Demo"}), http.StatusCreated, "the first account")
	ownerID := principalField(t, founded, "principal", "user", "id").(string)
	member, _ := api.localMember(t, h, owner, "member")

	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := empty.cluster.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, query, args...).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	issuesIn := func(key string) int {
		t.Helper()
		return count(`SELECT count(*) FROM issue i JOIN project p ON p.id = i.project_id WHERE p.key = $1`, key)
	}
	projects := func() int {
		t.Helper()
		return count(`SELECT count(*) FROM project WHERE key IN ($1, $2)`, demo.FactoryKey, demo.MaintenanceKey)
	}
	recorded := func() int {
		t.Helper()
		return count(`SELECT count(*) FROM audit_log WHERE action = 'installation.demo_added' AND actor_user_id = $1`, ownerID)
	}
	refusedOver := func(t *testing.T, key string) {
		t.Helper()
		again := owner.post("/api/v1/installation/demo-project", nil)
		if again.Status != http.StatusConflict || !strings.Contains(again.Error()["message"].(string), key) {
			t.Fatalf("the demo over %s: %d %s", key, again.Status, again.Raw)
		}
	}

	refused := member.post("/api/v1/installation/demo-project", nil)
	if refused.Status != http.StatusForbidden {
		t.Fatalf("a member adds the demo: %d %s", refused.Status, refused.Raw)
	}

	t.Run("a project under one of the keys holds the demo off whole", func(t *testing.T) {
		held := idOf(t, want(t, owner.post("/api/v1/projects", map[string]any{"name": "Maintenance", "key": demo.MaintenanceKey, "template": "task-tracking"}), http.StatusCreated, "a project of the installation's own"), "project")
		refusedOver(t, demo.MaintenanceKey)
		if n := projects(); n != 1 {
			t.Errorf("the refused demo left %d projects under its keys, want only the installation's own", n)
		}
		if recorded() != 0 {
			t.Error("the refused demo was recorded")
		}
		if _, err := empty.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `DELETE FROM project WHERE id = $1`, held)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	})

	holidays := count(`SELECT count(*) FROM holiday`)
	added := want(t, owner.post("/api/v1/installation/demo-project", nil), http.StatusCreated, "add the demo")
	if key := obj(t, added, "project")["key"]; key != demo.FactoryKey {
		t.Fatalf("the demo answered with project %v, want %s", key, demo.FactoryKey)
	}
	line, maint := issuesIn(demo.FactoryKey), issuesIn(demo.MaintenanceKey)
	if line == 0 || maint == 0 {
		t.Fatalf("the demo filed %d issues on the line and %d in maintenance, want both filled", line, maint)
	}
	if running := count(`SELECT count(*) FROM sprint s JOIN project p ON p.id = s.project_id WHERE p.key = $1 AND s.state = 'active'`, demo.FactoryKey); running == 0 {
		t.Error("the line has no maintenance window running")
	}
	want(t, member.get("/api/v1/projects/"+demo.FactoryKey), http.StatusOK, "the member sees the line")
	if recorded() != 1 {
		t.Error("adding the demo was not recorded")
	}
	// The demo is projects: the calendar everybody keeps stays the installation's own.
	if days := count(`SELECT count(*) FROM holiday`); days != holidays {
		t.Errorf("the demo wrote %d days off into the installation's calendars", days-holidays)
	}
	if n := count(`SELECT count(*) FROM issue_status WHERE lower(name) = 'waiting for parts'`); n != 1 {
		t.Errorf("%d statuses called Waiting for parts, want one", n)
	}

	again := owner.post("/api/v1/installation/demo-project", nil)
	if again.Status != http.StatusConflict {
		t.Fatalf("the demo a second time: %d %s", again.Status, again.Raw)
	}
	if words := again.Error()["message"].(string); !strings.Contains(words, demo.FactoryKey) || !strings.Contains(words, "Production line 3") {
		t.Errorf("the second time is refused with %q, which neither names the key nor says where the demo is", words)
	}
	if issuesIn(demo.FactoryKey) != line || issuesIn(demo.MaintenanceKey) != maint {
		t.Error("the refused second demo filed issues anyway")
	}
	if recorded() != 1 {
		t.Error("the refused second demo was recorded")
	}
}

// Nobody deletes the installation through the API: the address is gone.
func TestTheInstallationIsNotDeletedThroughTheAPI(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	org := obj(t, owner.signIn(t, h.person(t, "kept", auth.RoleOwner)), "principal", "org")
	gone := owner.delete("/api/v1/organization?confirm=" + org["slug"].(string))
	if gone.Status != http.StatusNotFound {
		t.Fatalf("deleting the installation answered %d %s, want 404", gone.Status, gone.Raw)
	}
	var n int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM installation WHERE id = $1`, org["id"]).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("the installation went")
	}
	want(t, owner.get("/api/v1/projects"), http.StatusOK, "the owner still works")
}
