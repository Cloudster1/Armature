//go:build integration

package test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/tenant"
)

// The database keeps its own wall while the tables carry org_id: the role the
// api connects as reads and writes nothing of the installation without it set.
func TestRowSecurityHoldsWithoutTheOrganizationSet(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	orgID, orgCtx := h.installation()
	action := unique("test.walled")
	if _, err := h.cluster.Write(orgCtx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO audit_log (org_id, action, target_type) VALUES ($1, $2, 'org')`, orgID, action)
		return err
	}); err != nil {
		t.Fatalf("write a row in the installation: %v", err)
	}

	conn, err := pgx.Connect(ctx, os.Getenv(envPrimary))
	if err != nil {
		t.Fatalf("connect as the application role: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	for _, set := range []struct{ what, org string }{
		{"nothing set", ""},
		{"an organization that is not there", uuid.NewString()},
	} {
		t.Run(set.what, func(t *testing.T) {
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if set.org != "" {
				if _, err := tx.Exec(ctx, `SELECT set_config($1, $2, true)`, tenant.PostgresVar, set.org); err != nil {
					t.Fatal(err)
				}
			}
			var seen int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = $1`, action).Scan(&seen); err != nil {
				t.Fatalf("read: %v", err)
			}
			if seen != 0 {
				t.Errorf("read %d of the installation's rows with %s", seen, set.what)
			}
			if _, err := tx.Exec(ctx, `SAVEPOINT write`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `INSERT INTO audit_log (org_id, action, target_type) VALUES ($1, $2, 'org')`, orgID, action); err == nil {
				t.Errorf("wrote into the installation with %s", set.what)
			}
		})
	}
}

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
