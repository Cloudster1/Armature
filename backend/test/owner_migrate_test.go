//go:build integration

package test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/migrations"
)

// The migrations on a cluster run as the schema's owner, a plain role that
// every FORCE ROW LEVEL SECURITY table hides its rows from. The local stack
// migrates as a superuser, which sees everything, so a migration that reads
// or fills tenant rows can pass here and quietly do nothing, or refuse, there.
// This migrates a fresh database as such an owner, with organizations from
// before the backfills in it, and expects each to come out with what a new one
// is made with.
func TestMigrationsRunAsPlainOwner(t *testing.T) {
	ctx := context.Background()
	provider, superFresh := ownerDatabase(t, "armature_owner_migrate_test", "armature_owner_test")

	// One organization from before the Initiative type and the automation
	// account, one from before roles were rows and calendars existed.
	// Two organizations are what the upgrade to one installation refuses, so
	// this stops before it; TestTheUpgradeRefusesSeveralOrganizations goes on.
	const (
		beforeInitiative      = 4
		beforeRoles           = 902
		beforeResourcePlan    = 910
		beforeOneInstallation = 928
	)
	insertOrg := func(slug string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := superFresh.QueryRow(ctx, `INSERT INTO org (slug, name) VALUES ($1, $2) RETURNING id`, slug, slug).Scan(&id); err != nil {
			t.Fatalf("insert organization %s: %v", slug, err)
		}
		return id
	}
	if _, err := provider.UpTo(ctx, beforeInitiative); err != nil {
		t.Fatalf("migrate to %d as the owner: %v", beforeInitiative, err)
	}
	earliest := insertOrg("early")
	if _, err := provider.UpTo(ctx, beforeRoles); err != nil {
		t.Fatalf("migrate to %d as the owner: %v", beforeRoles, err)
	}
	orgs := map[string]uuid.UUID{"early": earliest, "acme": insertOrg("acme")}

	// One project with a scrum board and one without, from before projects
	// had a planning method.
	if _, err := provider.UpTo(ctx, beforeResourcePlan); err != nil {
		t.Fatalf("migrate to %d as the owner: %v", beforeResourcePlan, err)
	}
	insertProject := func(key string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := superFresh.QueryRow(ctx, `INSERT INTO project (org_id, key, name) VALUES ($1, $2, $2) RETURNING id`, orgs["acme"], key).Scan(&id); err != nil {
			t.Fatalf("insert project %s: %v", key, err)
		}
		return id
	}
	projects := map[string]string{"SPR": "scrum", "FLO": "kanban"}
	projectIDs := map[string]uuid.UUID{}
	for key := range projects {
		projectIDs[key] = insertProject(key)
	}
	if _, err := superFresh.Exec(ctx, `INSERT INTO board (org_id, project_id, name, type) VALUES ($1, $2, 'Sprints', 'scrum')`, orgs["acme"], projectIDs["SPR"]); err != nil {
		t.Fatalf("insert scrum board: %v", err)
	}

	if _, err := provider.UpTo(ctx, beforeOneInstallation); err != nil {
		t.Fatalf("migrate to %d as the owner: %v", beforeOneInstallation, err)
	}
	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	if current != beforeOneInstallation {
		t.Fatalf("database is at %d, want %d", current, beforeOneInstallation)
	}

	for key, want := range projects {
		var method string
		var resources bool
		if err := superFresh.QueryRow(ctx, `SELECT planning_method, 'resources' = ANY (features) FROM project WHERE id = $1`, projectIDs[key]).Scan(&method, &resources); err != nil {
			t.Fatalf("project %s: read its planning: %v", key, err)
		}
		if method != want || !resources {
			t.Errorf("project %s plans by %q with the resource page %v, want %q with it on", key, method, resources, want)
		}
	}

	automation := map[string]uuid.UUID{}
	for slug, orgID := range orgs {
		automation[slug] = expectBackfilled(ctx, t, superFresh, slug, orgID)
	}

	// Run again, the repair adds nothing to an organization that has it all.
	const backfillsRepair = 926
	if _, err := provider.ApplyVersion(ctx, backfillsRepair, false); err != nil {
		t.Fatalf("take back %d: %v", backfillsRepair, err)
	}
	if _, err := provider.ApplyVersion(ctx, backfillsRepair, true); err != nil {
		t.Fatalf("apply %d again as the owner: %v", backfillsRepair, err)
	}
	for slug, orgID := range orgs {
		if again := expectBackfilled(ctx, t, superFresh, slug, orgID); again != automation[slug] {
			t.Errorf("organization %s: running the repair again replaced its automation account %s with %s", slug, automation[slug], again)
		}
	}

	// The backfills lift row security only for themselves.
	for _, table := range []string{"org", "org_member", "issue_type", "holiday_calendar", "project", "board"} {
		var forced bool
		if err := superFresh.QueryRow(ctx, `SELECT relforcerowsecurity FROM pg_class WHERE oid = $1::regclass`, table).Scan(&forced); err != nil {
			t.Fatalf("read row security of %s: %v", table, err)
		}
		if !forced {
			t.Errorf("%s no longer forces row security on its owner after the migrations", table)
		}
	}
}

// expectBackfilled checks an organization made before the migrations ran has
// what a new one is made with, exactly once, and returns its automation account.
func expectBackfilled(ctx context.Context, t *testing.T, conn *pgx.Conn, slug string, orgID uuid.UUID) uuid.UUID {
	t.Helper()
	var roles, calendars, initiatives, members int
	var calendarName string
	var automation *uuid.UUID
	var automationActive bool
	err := conn.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM org_role WHERE org_id = o.id AND builtin),
		       (SELECT count(*) FROM holiday_calendar WHERE org_id = o.id AND is_default),
		       (SELECT coalesce(min(name), '') FROM holiday_calendar WHERE org_id = o.id AND is_default),
		       (SELECT count(*) FROM issue_type WHERE org_id = o.id AND lower(name) = 'initiative' AND hierarchy_level = 2),
		       o.automation_user_id,
		       coalesce((SELECT is_active FROM app_user WHERE id = o.automation_user_id), false),
		       (SELECT count(*) FROM org_member WHERE org_id = o.id AND user_id = o.automation_user_id)
		FROM org o WHERE o.id = $1`, orgID,
	).Scan(&roles, &calendars, &calendarName, &initiatives, &automation, &automationActive, &members)
	if err != nil {
		t.Fatalf("organization %s: read what it has: %v", slug, err)
	}
	if roles != 5 {
		t.Errorf("organization %s has %d built-in roles, want 5", slug, roles)
	}
	if calendars != 1 || calendarName != bootstrap.DefaultHolidayCalendarName {
		t.Errorf("organization %s has %d default holiday calendars named %q, want one named %q",
			slug, calendars, calendarName, bootstrap.DefaultHolidayCalendarName)
	}
	if initiatives != 1 {
		t.Errorf("organization %s has %d Initiative types at level 2, want 1", slug, initiatives)
	}
	if automation == nil {
		t.Errorf("organization %s has no automation account", slug)
		return uuid.Nil
	}
	if automationActive || members != 1 {
		t.Errorf("organization %s: automation account active=%v and a member %d times, want an inactive member once", slug, automationActive, members)
	}
	return *automation
}

// ownerDatabase makes a fresh database owned by a plain role, the way a
// cluster has it, and hands back goose as that owner and a superuser on it.
func ownerDatabase(t *testing.T, dbName, ownerRole string) (*goose.Provider, *pgx.Conn) {
	t.Helper()
	superURL := os.Getenv(envSuperuser)
	if superURL == "" {
		t.Skipf("%s is not set; run the integration suite with `make test-integration`", envSuperuser)
	}
	ctx := context.Background()
	const ownerPass = "owner-test"

	super, err := pgx.Connect(ctx, superURL)
	if err != nil {
		t.Fatalf("connect superuser: %v", err)
	}
	t.Cleanup(func() { _ = super.Close(ctx) })
	mustExec := func(conn *pgx.Conn, stmts ...string) {
		t.Helper()
		for _, s := range stmts {
			if _, err := conn.Exec(ctx, s); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
		}
	}
	// A previous run that died leaves its database behind; nothing else
	// connects to it, so dropping it is safe.
	mustExec(super,
		"DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)",
		"DROP ROLE IF EXISTS "+ownerRole,
		fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOBYPASSRLS", ownerRole, ownerPass),
		"CREATE DATABASE "+dbName+" OWNER "+ownerRole,
	)
	t.Cleanup(func() {
		mustExec(super, "DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)", "DROP ROLE IF EXISTS "+ownerRole)
	})

	// The runtime roles exist on the cluster, so the migrations take the
	// branches that name them; the grants come from the default privileges
	// the roles Job sets up.
	superFresh, err := pgx.Connect(ctx, withDatabase(t, superURL, dbName, "", ""))
	if err != nil {
		t.Fatalf("connect superuser to %s: %v", dbName, err)
	}
	t.Cleanup(func() { _ = superFresh.Close(ctx) })
	mustExec(superFresh,
		"GRANT CONNECT ON DATABASE "+dbName+" TO armature_app, armature_admin",
		"GRANT USAGE ON SCHEMA public TO armature_app, armature_admin",
		"ALTER DEFAULT PRIVILEGES FOR ROLE "+ownerRole+" IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO armature_app, armature_admin",
		"ALTER DEFAULT PRIVILEGES FOR ROLE "+ownerRole+" IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO armature_app, armature_admin",
		"ALTER DEFAULT PRIVILEGES FOR ROLE "+ownerRole+" IN SCHEMA public GRANT EXECUTE ON FUNCTIONS TO armature_app, armature_admin",
	)

	asOwner, err := sql.Open("pgx", withDatabase(t, superURL, dbName, ownerRole, ownerPass))
	if err != nil {
		t.Fatalf("open as owner: %v", err)
	}
	t.Cleanup(func() { _ = asOwner.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, asOwner, migrations.FS)
	if err != nil {
		t.Fatalf("goose provider: %v", err)
	}
	return provider, superFresh
}

// withDatabase points a connection URL at another database, and at another
// role when one is given.
func withDatabase(t *testing.T, base, dbName, user, pass string) string {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse %s: %v", base, err)
	}
	u.Path = "/" + dbName
	if user != "" {
		u.User = url.UserPassword(user, pass)
	}
	return u.String()
}
