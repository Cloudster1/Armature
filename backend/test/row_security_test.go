//go:build integration

package test

import (
	"context"
	"os"
	"strings"
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

const (
	beforeTheDrop = 931
	theDrop       = 932
	// 95 tables, each with the tenant's policy and the admin role's bypass.
	policiesBeforeTheDrop = 190
)

// The drop runs as the plain owner a cluster migrates as, and its way back puts
// the wall up again: the api's role then sees nothing without the organization.
func TestRowSecurityGoesAsThePlainOwner(t *testing.T) {
	ctx := context.Background()
	provider, super := ownerDatabase(t, "armature_row_security_test", "armature_row_security_owner")
	if _, err := provider.UpTo(ctx, beforeTheDrop); err != nil {
		t.Fatalf("migrate to %d as the owner: %v", beforeTheDrop, err)
	}
	var orgID string
	if err := super.QueryRow(ctx, `INSERT INTO org (slug, name) VALUES ('acme', 'Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("insert the organization: %v", err)
	}
	if _, err := super.Exec(ctx, `INSERT INTO project (org_id, key, name) VALUES ($1, 'ROW', 'Rows')`, orgID); err != nil {
		t.Fatalf("insert a project: %v", err)
	}

	app, err := pgx.Connect(ctx, withDatabase(t, os.Getenv(envPrimary), "armature_row_security_test", "", ""))
	if err != nil {
		t.Fatalf("connect as the application role: %v", err)
	}
	t.Cleanup(func() { _ = app.Close(context.Background()) })
	projectsSeen := func(t *testing.T) int {
		t.Helper()
		var n int
		if err := app.QueryRow(ctx, `SELECT count(*) FROM project`).Scan(&n); err != nil {
			t.Fatalf("read projects as the application role: %v", err)
		}
		return n
	}
	policies := func(t *testing.T) int {
		t.Helper()
		var n int
		if err := super.QueryRow(ctx, `SELECT count(*) FROM pg_policies WHERE schemaname = 'public'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if n := policies(t); n != policiesBeforeTheDrop {
		t.Fatalf("%d policies before the drop, want %d", n, policiesBeforeTheDrop)
	}
	if _, err := provider.ApplyVersion(ctx, theDrop, true); err != nil {
		t.Fatalf("drop row security as the owner: %v", err)
	}
	if n := policies(t); n != 0 {
		t.Errorf("%d policies are left after the drop", n)
	}
	if n := projectsSeen(t); n != 1 {
		t.Errorf("the application role sees %d projects with no organization set, want 1", n)
	}

	if _, err := provider.ApplyVersion(ctx, theDrop, false); err != nil {
		t.Fatalf("take the drop back as the owner: %v", err)
	}
	if n := policies(t); n != policiesBeforeTheDrop {
		t.Errorf("%d policies after taking the drop back, want %d", n, policiesBeforeTheDrop)
	}
	if n := projectsSeen(t); n != 0 {
		t.Errorf("with the policies back, the application role sees %d projects with no organization set", n)
	}

	if _, err := provider.ApplyVersion(ctx, theDrop, true); err != nil {
		t.Fatalf("drop row security again: %v", err)
	}
	if n := policies(t); n != 0 {
		t.Errorf("%d policies are left after dropping them again", n)
	}
}

// A policy somebody added by hand is not dropped unseen with the tenant's.
func TestTheDropStopsAtAPolicyItDidNotMake(t *testing.T) {
	ctx := context.Background()
	provider, super := ownerDatabase(t, "armature_row_security_kept_test", "armature_row_security_kept_owner")
	if _, err := provider.UpTo(ctx, beforeTheDrop); err != nil {
		t.Fatalf("migrate to %d as the owner: %v", beforeTheDrop, err)
	}
	if _, err := super.Exec(ctx, `CREATE POLICY reporting_reads ON issue FOR SELECT USING (true)`); err != nil {
		t.Fatal(err)
	}
	_, err := provider.ApplyVersion(ctx, theDrop, true)
	if err == nil {
		t.Fatal("the drop went past a policy it did not make")
	}
	if want := "reporting_reads on issue"; !strings.Contains(err.Error(), want) {
		t.Errorf("the refusal does not name %q: %v", want, err)
	}
	var left int
	if err := super.QueryRow(ctx, `SELECT count(*) FROM pg_policies WHERE schemaname = 'public'`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != policiesBeforeTheDrop+1 {
		t.Errorf("%d policies after the refusal, want all %d left as they were", left, policiesBeforeTheDrop+1)
	}
}
