//go:build integration

package test

import (
	"context"
	"testing"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/tenant"
)

// The belt as well as the braces: a query with no organization at all is refused
// before it reaches the database, rather than run with no tenant filter.
func TestUnscopedAccessIsRefused(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
