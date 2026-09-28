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

	"github.com/armature/armature/backend/migrations"
)

// The migrations on a cluster run as the schema's owner, a plain role that
// every FORCE ROW LEVEL SECURITY table hides its rows from. The local stack
// migrates as a superuser, which sees everything, so a migration that reads
// or fills tenant rows can pass here and quietly do nothing, or refuse, there.
// This migrates a fresh database as such an owner, with an organization from
// before the roles migration in it, and expects the organization to come out
// with its roles.
func TestMigrationsRunAsPlainOwner(t *testing.T) {
	superURL := os.Getenv(envSuperuser)
	if superURL == "" {
		t.Skipf("%s is not set; run the integration suite with `make test-integration`", envSuperuser)
	}
	ctx := context.Background()

	const (
		dbName    = "armature_owner_migrate_test"
		ownerRole = "armature_owner_test"
		ownerPass = "owner-test"
	)

	super, err := pgx.Connect(ctx, superURL)
	if err != nil {
		t.Fatalf("connect superuser: %v", err)
	}
	defer super.Close(ctx)
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
	defer mustExec(super, "DROP DATABASE IF EXISTS "+dbName+" WITH (FORCE)", "DROP ROLE IF EXISTS "+ownerRole)

	// The runtime roles exist on the cluster, so the migrations take the
	// branches that name them; the grants come from the default privileges
	// the roles Job sets up.
	superFresh, err := pgx.Connect(ctx, withDatabase(t, superURL, dbName, "", ""))
	if err != nil {
		t.Fatalf("connect superuser to %s: %v", dbName, err)
	}
	defer superFresh.Close(ctx)
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
	defer asOwner.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, asOwner, migrations.FS)
	if err != nil {
		t.Fatalf("goose provider: %v", err)
	}

	// An organization from the days before roles were rows.
	const beforeRoles = 902
	if _, err := provider.UpTo(ctx, beforeRoles); err != nil {
		t.Fatalf("migrate to %d as the owner: %v", beforeRoles, err)
	}
	var orgID uuid.UUID
	if err := superFresh.QueryRow(ctx, `INSERT INTO org (slug, name) VALUES ('acme', 'Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("insert organization: %v", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate the rest as the owner: %v", err)
	}
	current, err := provider.GetDBVersion(ctx)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	all := provider.ListSources()
	if latest := all[len(all)-1].Version; current != latest {
		t.Fatalf("database is at %d, the migrations go to %d", current, latest)
	}

	var roles int
	if err := superFresh.QueryRow(ctx, `SELECT count(*) FROM org_role WHERE org_id = $1 AND builtin`, orgID).Scan(&roles); err != nil {
		t.Fatalf("count roles: %v", err)
	}
	if roles != 5 {
		t.Fatalf("the organization from before the roles migration has %d built-in roles, want 5", roles)
	}
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
