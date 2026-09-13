//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/tenant"
)

// TestTenantIsolation is the load bearing security test for the whole product.
// Every tenant scoped table must be invisible across organizations even when
// the query deliberately omits a tenant filter, because that is precisely the
// mistake row level security exists to survive.
func TestTenantIsolation(t *testing.T) {
	h := newHarness(t)

	orgA, ctxA := h.makeOrg(t, "alpha")
	orgB, ctxB := h.makeOrg(t, "beta")

	// Give each organization one row in every tenant scoped table.
	seed := func(t *testing.T, ctx context.Context, org uuid.UUID) {
		t.Helper()
		_, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
			if _, err := tx.Exec(ctx, `
				INSERT INTO audit_log (org_id, action, target_type) VALUES ($1, 'test.seeded', 'org')`, org); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO outbox_event (org_id, topic, payload) VALUES ($1, 'test.seeded', '{}'::jsonb)`, org); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			t.Fatalf("seed org %s: %v", org, err)
		}
	}
	seed(t, ctxA, orgA)
	seed(t, ctxB, orgB)

	// Counting with no WHERE clause: whatever comes back is exactly what this
	// tenant is permitted to see.
	countAll := func(t *testing.T, ctx context.Context, table string) int {
		t.Helper()
		var n int
		err := h.cluster.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n)
		})
		if err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}

	for _, table := range []string{"audit_log", "outbox_event"} {
		t.Run(table+" is scoped to its tenant", func(t *testing.T) {
			if got := countAll(t, ctxA, table); got != 1 {
				t.Errorf("org A sees %d rows in %s, want only its own 1", got, table)
			}
			if got := countAll(t, ctxB, table); got != 1 {
				t.Errorf("org B sees %d rows in %s, want only its own 1", got, table)
			}
		})
	}

	t.Run("org row itself is scoped", func(t *testing.T) {
		var slug string
		err := h.cluster.ReadPrimary(ctxA, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT slug FROM org`).Scan(&slug)
		})
		if err != nil {
			t.Fatalf("org A reading org: %v", err)
		}

		// Selecting org B by its primary key from inside org A must find nothing.
		var n int
		err = h.cluster.ReadPrimary(ctxA, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM org WHERE id = $1`, orgB).Scan(&n)
		})
		if err != nil {
			t.Fatalf("org A probing for org B: %v", err)
		}
		if n != 0 {
			t.Errorf("org A can see org B by id: count = %d, want 0", n)
		}
	})

	t.Run("writing into another tenant is refused", func(t *testing.T) {
		_, err := h.cluster.Write(ctxA, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO audit_log (org_id, action, target_type)
				VALUES ($1, 'test.cross_tenant', 'org')`, orgB)
			return err
		})
		if err == nil {
			t.Fatal("org A successfully wrote a row into org B")
		}

		// And nothing was left behind.
		if got := countAll(t, ctxB, "audit_log"); got != 1 {
			t.Errorf("org B has %d audit rows after the rejected write, want 1", got)
		}
	})

	t.Run("updating another tenant's row affects nothing", func(t *testing.T) {
		var affected int64
		_, err := h.cluster.Write(ctxA, func(ctx context.Context, tx db.DBTX) error {
			tag, err := tx.Exec(ctx, `UPDATE audit_log SET action = 'tampered' WHERE org_id = $1`, orgB)
			affected = tag.RowsAffected()
			return err
		})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if affected != 0 {
			t.Errorf("org A updated %d of org B's rows, want 0", affected)
		}
	})

	t.Run("deleting another tenant's row affects nothing", func(t *testing.T) {
		var affected int64
		_, err := h.cluster.Write(ctxA, func(ctx context.Context, tx db.DBTX) error {
			tag, err := tx.Exec(ctx, `DELETE FROM audit_log WHERE org_id = $1`, orgB)
			affected = tag.RowsAffected()
			return err
		})
		if err != nil {
			t.Fatalf("delete: %v", err)
		}
		if affected != 0 {
			t.Errorf("org A deleted %d of org B's rows, want 0", affected)
		}
		if got := countAll(t, ctxB, "audit_log"); got != 1 {
			t.Errorf("org B has %d audit rows after the attempted delete, want 1", got)
		}
	})
}

// TestUnscopedAccessIsRefused checks the belt as well as the braces: a query
// issued with no organization at all must be rejected before it reaches the
// database, rather than quietly running with no tenant filter.
func TestUnscopedAccessIsRefused(t *testing.T) {
	h := newHarness(t)

	err := h.cluster.Read(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		var n int
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&n)
	})
	if err == nil {
		t.Fatal("a read with no organization in context was allowed")
	}
	if !errorIs(err, tenant.ErrNoTenant) {
		t.Fatalf("error = %v, want tenant.ErrNoTenant", err)
	}

	_, err = h.cluster.Write(context.Background(), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO audit_log (org_id, action, target_type) VALUES (gen_random_uuid(), 'x', 'y')`)
		return err
	})
	if err == nil {
		t.Fatal("a write with no organization in context was allowed")
	}
}

// TestSystemContextStillNeedsAnExplicitOptIn confirms the escape hatch works
// but has to be asked for.
func TestSystemContextStillNeedsAnExplicitOptIn(t *testing.T) {
	h := newHarness(t)

	err := h.cluster.Read(db.WithSystem(context.Background()), func(ctx context.Context, tx db.DBTX) error {
		var n int
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&n)
	})
	if err != nil {
		t.Fatalf("an explicitly system-level read was refused: %v", err)
	}
}

func errorIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
