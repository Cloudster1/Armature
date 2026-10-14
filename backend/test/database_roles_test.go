//go:build integration

package test

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
)

// secretTables hold what opens an account: reset links, second-factor keys,
// recovery codes and half-finished sign-ins.
var secretTables = []string{"login_challenge", "password_reset", "recovery_code", "user_second_factor"}

// The two roles exist for the secret tables: the admin role, which serves
// sign-in and the account paths, alone reads them, and reads everything else too.
func TestOnlyTheAdminRoleReadsTheSecretTables(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	connect := func(t *testing.T, env string) *pgx.Conn {
		t.Helper()
		conn, err := pgx.Connect(ctx, os.Getenv(env))
		if err != nil {
			t.Fatalf("connect as %s: %v", env, err)
		}
		t.Cleanup(func() { _ = conn.Close(context.Background()) })
		return conn
	}
	unreadable := func(t *testing.T, conn *pgx.Conn) []string {
		t.Helper()
		rows, err := conn.Query(ctx, `
			SELECT c.relname FROM pg_class c
			WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
			  AND NOT has_table_privilege(c.oid, 'SELECT')
			ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		names, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		return names
	}
	app, admin := connect(t, envPrimary), connect(t, envAdmin)

	if got := unreadable(t, app); !slices.Equal(got, secretTables) {
		t.Errorf("the app role cannot read %v, want exactly the secret tables %v", got, secretTables)
	}
	if got := unreadable(t, admin); len(got) > 0 {
		t.Errorf("the admin role cannot read %v", got)
	}
	for _, table := range secretTables {
		var n int
		err := app.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Errorf("the app role read %s: %v", table, err)
		}
		if err := admin.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
			t.Errorf("the admin role could not read %s: %v", table, err)
		}
	}
}

// Start-up checks the app role, as a superuser would read the secret tables
// past the grants that keep them the admin role's.
func TestTheAppRoleIsRefusedAsASuperuser(t *testing.T) {
	t.Parallel()
	newHarness(t)
	ctx := context.Background()
	if err := shared.cluster.RefuseSuperuser(ctx); err != nil {
		t.Errorf("the app role was refused: %v", err)
	}
	cfg := shared.cfg.DB
	cfg.PrimaryURL, cfg.ReplicaURLs, cfg.AdminURL = os.Getenv(envSuperuser), nil, ""
	cluster, err := db.Open(ctx, cfg, shared.log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)
	if err := cluster.RefuseSuperuser(ctx); err == nil {
		t.Error("a superuser was let through as the app role")
	}
}
