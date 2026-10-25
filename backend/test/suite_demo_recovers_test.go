//go:build integration

package test

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/demo"
)

// suiteInstallation is an installation of its own with an owner, since it has one suite demo,
// and a superuser's way into its database to set the scene.
func suiteInstallation(t *testing.T, dbName string) (*harness, *client, string, func(string, ...any)) {
	t.Helper()
	h := newHarness(t)
	api, empty := emptyInstallation(t, h, dbName)
	api.api.Auth = empty.authService().Signups(auth.SignupFirst)
	owner := api.client(t)
	founded := want(t, owner.post("/api/v1/auth/signup", map[string]string{"email": h.email(t, "suite"), "password": testPassword, "name": "Suite"}), http.StatusCreated, "the first account")
	ctx := context.Background()
	super, err := pgx.Connect(ctx, withDatabase(t, os.Getenv(envSuperuser), dbName, "", ""))
	if err != nil {
		t.Fatalf("connect superuser to %s: %v", dbName, err)
	}
	t.Cleanup(func() { _ = super.Close(ctx) })
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := super.Exec(ctx, query, args...); err != nil {
			t.Fatalf("%s: %v", query, err)
		}
	}
	return empty, owner, principalField(t, founded, "principal", "user", "id").(string), exec
}

// A failed suite demo is added again over the projects it made, and carries on in them,
// while a project somebody else made under one of its keys still holds it off.
func TestAFailedSuiteDemoIsAddedAgain(t *testing.T) {
	t.Parallel()
	h, owner, _, exec := suiteInstallation(t, "armature_suite_demo_again_test")
	ctx := context.Background()
	builder := demo.NewSuiteBuilder(h.cluster, slog.New(slog.DiscardHandler))
	readAdmin := func(query string, dest any) {
		t.Helper()
		if err := h.cluster.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, query).Scan(dest)
		}); err != nil {
			t.Fatal(err)
		}
	}

	// The database refuses Stator's project, so the build stops after Armature's and Exciter's.
	exec(`
		CREATE FUNCTION refuse_sta() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'no STA here'; END $$ LANGUAGE plpgsql`)
	exec(`CREATE TRIGGER refuse_sta BEFORE INSERT ON project FOR EACH ROW WHEN (NEW.key = 'STA') EXECUTE FUNCTION refuse_sta()`)

	want(t, owner.post("/api/v1/installation/suite-demo", nil), http.StatusAccepted, "add the suite demo")
	if built, err := builder.Once(ctx); err != nil || built != 1 {
		t.Fatalf("the worker built %d suite demos: %v", built, err)
	}
	failed := obj(t, want(t, owner.get("/api/v1/installation/suite-demo"), http.StatusOK, "the failed suite demo"), "demo")
	message, _ := failed["message"].(string)
	if failed["state"] != demo.SuiteFailed || !strings.Contains(message, "STA") || !strings.Contains(message, "Add the suite demo again") {
		t.Fatalf("the suite demo stood at %v", failed)
	}
	if strings.Contains(message, "remove") {
		t.Errorf("the message asks for what Armature cannot do: %q", message)
	}
	var made []string
	readAdmin(`SELECT made FROM suite_demo`, &made)
	if !slices.Equal(made, []string{"ARM", "EXC"}) {
		t.Errorf("the failed suite demo made %v", made)
	}

	exec(`DROP TRIGGER refuse_sta ON project`)
	held := idOf(t, want(t, owner.post("/api/v1/projects", map[string]any{"name": "Stator, ours", "key": "STA", "template": "kanban"}), http.StatusCreated, "a project of the installation's own"), "project")
	refused := owner.post("/api/v1/installation/suite-demo", nil)
	fields, _ := refused.Error()["fields"].(map[string]any)
	if refused.Status != http.StatusUnprocessableEntity || !strings.Contains(fields["key"].(string), "STA") {
		t.Fatalf("the suite demo again over somebody's STA: %d %s", refused.Status, refused.Raw)
	}

	exec(`DELETE FROM project WHERE id = $1`, held)
	queued := obj(t, want(t, owner.post("/api/v1/installation/suite-demo", nil), http.StatusAccepted, "add the failed suite demo again"), "demo")
	if queued["state"] != demo.SuiteQueued || queued["step"] != float64(0) || queued["message"] != "" {
		t.Fatalf("the suite demo again was answered %v", queued)
	}
	if again := owner.post("/api/v1/installation/suite-demo", nil); again.Status != http.StatusConflict {
		t.Fatalf("a queued suite demo was added again: %d %s", again.Status, again.Raw)
	}

	if built, err := builder.Once(ctx); err != nil || built != 1 {
		t.Fatalf("the worker built %d suite demos the second time: %v", built, err)
	}
	done := obj(t, want(t, owner.get("/api/v1/installation/suite-demo"), http.StatusOK, "the suite demo built the second time"), "demo")
	if done["state"] != demo.SuiteDone {
		t.Fatalf("the suite demo stood at %v the second time", done)
	}
	snapshots, err := demo.Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range snapshots {
		var n int
		if err := h.cluster.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM issue i JOIN project p ON p.id = i.project_id WHERE p.key = $1`, s.Key).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		if n != len(s.Issues) {
			t.Errorf("%s holds %d issues after carrying on, its snapshot %d", s.Key, n, len(s.Issues))
		}
	}
}

// A build whose worker stopped is marked failed once it has gone without a step for too long,
// and can then be added again; one still moving is left to its worker.
func TestAStaleSuiteDemoIsMarkedFailed(t *testing.T) {
	t.Parallel()
	h, owner, ownerID, exec := suiteInstallation(t, "armature_suite_demo_stale_test")
	ctx := context.Background()
	builder := demo.NewSuiteBuilder(h.cluster, slog.New(slog.DiscardHandler))
	running := func(age float64) {
		t.Helper()
		exec(`DELETE FROM suite_demo`)
		exec(`
			INSERT INTO suite_demo (state, step, steps, taken, actor_id, updated_at)
			VALUES ($1, 1, $2, '2026-10-21', $3, now() - make_interval(secs => $4))`,
			demo.SuiteRunning, demo.SuiteSteps, ownerID, age)
	}
	state := func() map[string]any {
		t.Helper()
		return obj(t, want(t, owner.get("/api/v1/installation/suite-demo"), http.StatusOK, "the suite demo"), "demo")
	}

	running(demo.SuiteStale.Seconds() / 2)
	if built, err := builder.Once(ctx); err != nil || built != 0 {
		t.Fatalf("the worker built %d suite demos over a moving one: %v", built, err)
	}
	if got := state(); got["state"] != demo.SuiteRunning {
		t.Fatalf("a suite demo still moving stood at %v", got)
	}

	running(demo.SuiteStale.Seconds() * 2)
	if built, err := builder.Once(ctx); err != nil || built != 0 {
		t.Fatalf("the worker built %d suite demos over a stale one: %v", built, err)
	}
	got := state()
	if message, _ := got["message"].(string); got["state"] != demo.SuiteFailed || !strings.Contains(message, "Add the suite demo again") {
		t.Fatalf("a stale suite demo stood at %v", got)
	}
	queued := obj(t, want(t, owner.post("/api/v1/installation/suite-demo", nil), http.StatusAccepted, "add the stale suite demo again"), "demo")
	if queued["state"] != demo.SuiteQueued {
		t.Fatalf("the stale suite demo again was answered %v", queued)
	}
}
