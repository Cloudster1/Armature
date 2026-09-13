//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/armature/armature/backend/internal/db"
)

// A migration naming a role the bootstrap did not create is skipped, not
// refused, so the bypass signup and the relay need vanishes without an error.
func TestEveryGuardedTableHasAnAdminBypass(t *testing.T) {
	// The role's name comes from the admin pool, never a literal, so the
	// assertion cannot drift from the configuration.
	h := newHarness(t)
	ctx := context.Background()

	var adminRole string
	if err := h.cluster.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT current_user`).Scan(&adminRole)
	}); err != nil {
		t.Fatalf("ask the admin pool who it is: %v", err)
	}

	rows, err := h.super.Query(ctx, `
		SELECT c.relname
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relrowsecurity
		  AND NOT EXISTS (
			SELECT 1 FROM pg_policies p
			WHERE p.schemaname = 'public' AND p.tablename = c.relname
			  AND $1 = ANY (p.roles))
		ORDER BY c.relname`, adminRole)
	if err != nil {
		t.Fatalf("list tables without a bypass: %v", err)
	}
	defer rows.Close()

	var missing []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		missing = append(missing, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read rows: %v", err)
	}
	if len(missing) > 0 {
		t.Fatalf("these tables force row level security but no policy lets %s through, so a migration naming another role was skipped: %v", adminRole, missing)
	}

	// A run that found no tables at all would pass for the wrong reason.
	var guarded int
	if err := h.super.QueryRow(ctx, `
		SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r' AND c.relrowsecurity`).Scan(&guarded); err != nil {
		t.Fatalf("count guarded tables: %v", err)
	}
	if guarded < 70 {
		t.Fatalf("only %d tables force row level security; the schema is not fully applied", guarded)
	}
}
