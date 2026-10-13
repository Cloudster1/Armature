//go:build integration

package test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
)

// Row security kept organizations apart, and an installation holds one, so
// none is left: the walls inside it are projects, kept by the api and triggers.
func TestNoTableKeepsRowSecurity(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()

	rows, err := h.super.Query(ctx, `
		SELECT c.relname
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind IN ('r', 'p')
		  AND (c.relrowsecurity OR c.relforcerowsecurity)
		ORDER BY c.relname`)
	if err != nil {
		t.Fatalf("list tables with row security: %v", err)
	}
	guarded, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("read tables with row security: %v", err)
	}
	if len(guarded) > 0 {
		t.Errorf("%d tables still keep row security: %v", len(guarded), guarded)
	}

	var policies int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM pg_policies WHERE schemaname = 'public'`).Scan(&policies); err != nil {
		t.Fatalf("count policies: %v", err)
	}
	if policies != 0 {
		t.Errorf("%d policies are left in the schema", policies)
	}
}

// The api's own role reads and writes the installation's rows with no
// organization set, which is how the code will run once it stops setting one.
func TestTheAPIRoleNeedsNoOrganizationSet(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	orgID, orgCtx := h.installation()
	committed := unique("test.unwalled")
	if _, err := h.cluster.Write(orgCtx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO audit_log (org_id, action, target_type) VALUES ($1, $2, 'org')`, orgID, committed)
		return err
	}); err != nil {
		t.Fatalf("write a row in the installation: %v", err)
	}

	conn, err := pgx.Connect(ctx, os.Getenv(envPrimary))
	if err != nil {
		t.Fatalf("connect as the application role: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// What this writes is only to prove it can, so none of it is kept.
	defer func() { _ = tx.Rollback(ctx) }()

	var seen int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = $1`, committed).Scan(&seen); err != nil {
		t.Fatalf("read: %v", err)
	}
	if seen != 1 {
		t.Errorf("read %d of the installation's rows with no organization set, want 1", seen)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_log (org_id, action, target_type) VALUES ($1, $2, 'org')`, orgID, unique("test.unset")); err != nil {
		t.Errorf("write with no organization set: %v", err)
	}
}
