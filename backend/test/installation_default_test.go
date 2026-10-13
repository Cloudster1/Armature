//go:build integration

package test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/installation"
)

// installationDefault is what every org_id column falls back to, until step 5
// of #218 drops the columns.
const installationDefault = "installation_org_id()"

// A row written without its org_id lands in the installation, which is what
// lets the code stop naming the organization at all.
func TestEveryOrganizationColumnDefaultsToTheInstallation(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()

	rows, err := h.super.Query(ctx, `
		SELECT c.relname || ' ' || coalesce(pg_get_expr(d.adbin, d.adrelid), 'no default')
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
		WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
		  AND a.attname = 'org_id' AND NOT a.attisdropped
		  AND pg_get_expr(d.adbin, d.adrelid) IS DISTINCT FROM $1
		ORDER BY 1`, installationDefault)
	if err != nil {
		t.Fatal(err)
	}
	without, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(without) > 0 {
		t.Errorf("%d org_id columns do not default to %s: %v", len(without), installationDefault, without)
	}

	// The api's own role, with nothing set on its session.
	conn, err := pgx.Connect(ctx, os.Getenv(envPrimary))
	if err != nil {
		t.Fatalf("connect as the application role: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var landed uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO audit_log (action, target_type) VALUES ($1, 'org') RETURNING org_id`, unique("test.defaulted")).Scan(&landed); err != nil {
		t.Fatalf("write without org_id: %v", err)
	}
	if landed != shared.installation.ID {
		t.Errorf("a row written without org_id landed in %s, want the installation %s", landed, shared.installation.ID)
	}
	var answered *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT current_org_id()`).Scan(&answered); err != nil {
		t.Fatal(err)
	}
	if answered == nil || *answered != shared.installation.ID {
		t.Errorf("current_org_id() = %v with nothing set, want the installation %s", answered, shared.installation.ID)
	}
	// What the api once set on every transaction is no longer read.
	if _, err := tx.Exec(ctx, `SELECT set_config('app.org_id', $1, true)`, uuid.NewString()); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT current_org_id()`).Scan(&answered); err != nil {
		t.Fatal(err)
	}
	if answered == nil || *answered != shared.installation.ID {
		t.Errorf("current_org_id() = %v with another id set, want the installation %s", answered, shared.installation.ID)
	}
}

// A fresh database migrates before anybody signs up, so the default finds the
// installation when a row is written, not when the migration ran.
func TestTheInstallationIsFoundWhenARowIsWritten(t *testing.T) {
	ctx := context.Background()
	provider, super := ownerDatabase(t, "armature_installation_default_test", "armature_installation_default_owner")
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate as the owner: %v", err)
	}

	var before *uuid.UUID
	if err := super.QueryRow(ctx, `SELECT current_org_id()`).Scan(&before); err != nil {
		t.Fatalf("ask before the installation is set up: %v", err)
	}
	if before != nil {
		t.Errorf("current_org_id() = %s before the installation is set up, want none", *before)
	}

	var orgID, landed uuid.UUID
	if err := super.QueryRow(ctx, `INSERT INTO org (slug, name) VALUES ('first', 'First') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("set the installation up: %v", err)
	}
	if err := super.QueryRow(ctx, `INSERT INTO audit_log (action, target_type) VALUES ('test.defaulted', 'org') RETURNING org_id`).Scan(&landed); err != nil {
		t.Fatalf("write without org_id: %v", err)
	}
	if landed != orgID {
		t.Errorf("the row landed in %s, want the installation %s", landed, orgID)
	}

	// A second one would make the default a guess, so it is refused instead.
	if _, err := super.Exec(ctx, `INSERT INTO org (slug, name) VALUES ('second', 'Second')`); err != nil {
		t.Fatal(err)
	}
	_, err := super.Exec(ctx, `INSERT INTO audit_log (action, target_type) VALUES ('test.guessed', 'org')`)
	if err == nil || !strings.Contains(err.Error(), "holds 2 organizations") {
		t.Errorf("writing without org_id beside two organizations = %v, want it refused", err)
	}
}

// Nothing in a context names the organization any more: a transaction and a
// service work with none.
func TestAServiceNeedsNoOrganizationInItsContext(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := db.PinPrimary(context.Background())

	action := unique("test.contextless")
	if _, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO audit_log (action, target_type) VALUES ($1, 'org')`, action)
		return err
	}); err != nil {
		t.Fatalf("write with no organization in the context: %v", err)
	}
	var landed uuid.UUID
	if err := h.cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT org_id FROM audit_log WHERE action = $1`, action).Scan(&landed)
	}); err != nil {
		t.Fatalf("read with no organization in the context: %v", err)
	}
	if landed != shared.installation.ID {
		t.Errorf("the row landed in %s, want the installation %s", landed, shared.installation.ID)
	}

	got, err := installation.NewService(h.cluster).Get(ctx)
	if err != nil {
		t.Fatalf("read the installation with no organization in the context: %v", err)
	}
	if got.Name == "" {
		t.Error("the installation has no name")
	}
}
