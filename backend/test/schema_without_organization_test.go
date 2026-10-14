//go:build integration

package test

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/installation"
	"github.com/armature/armature/backend/internal/perm"
)

const (
	beforeTheColumnsGo = 936
	theColumnsGo       = 937
)

// namingTheOrganization lists what in the schema still names the organization's
// column, the session's two, or the functions that answered for them.
func namingTheOrganization(t *testing.T, conn catalogReader) []string {
	t.Helper()
	rows, err := conn.Query(context.Background(), `
		SELECT format('column %s.%s', c.relname, a.attname)
		FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
		WHERE c.relnamespace = 'public'::regnamespace AND a.attnum > 0 AND NOT a.attisdropped
		  AND a.attname ~ 'org_id'
		UNION ALL
		SELECT format('function %s', p.proname) FROM pg_proc p
		WHERE p.pronamespace = 'public'::regnamespace AND (p.proname ~ 'org_id' OR p.prosrc ~ 'org_id')
		UNION ALL
		SELECT format('index %s', i.indexrelid::regclass) FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid
		WHERE c.relnamespace = 'public'::regnamespace AND pg_get_indexdef(i.indexrelid) ~ 'org_id'
		UNION ALL
		SELECT format('constraint %s on %s', k.conname, k.conrelid::regclass) FROM pg_constraint k
		WHERE k.connamespace = 'public'::regnamespace AND pg_get_constraintdef(k.oid) ~ 'org_id'
		UNION ALL
		SELECT format('default of %s.%s', c.relname, a.attname)
		FROM pg_attrdef d JOIN pg_class c ON c.oid = d.adrelid
		JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum
		WHERE c.relnamespace = 'public'::regnamespace AND pg_get_expr(d.adbin, d.adrelid) ~ 'org_id'
		UNION ALL
		SELECT format('trigger %s on %s', g.tgname, g.tgrelid::regclass) FROM pg_trigger g
		WHERE NOT g.tgisinternal AND pg_get_triggerdef(g.oid) ~ 'org_id'
		UNION ALL
		SELECT format('view %s', v.viewname) FROM pg_views v
		WHERE v.schemaname = 'public' AND v.definition ~ 'org_id'
		ORDER BY 1`)
	if err != nil {
		t.Fatalf("list what names the organization: %v", err)
	}
	named, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("read what names the organization: %v", err)
	}
	return named
}

// No table carries the organization any more: the installation is the only
// one a row can belong to, so nothing in the schema says which (#218).
func TestNothingInTheSchemaNamesTheOrganization(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	if named := namingTheOrganization(t, h.super); len(named) > 0 {
		t.Errorf("%d things in the schema still name the organization: %v", len(named), named)
	}
}

// rowCounts counts the rows of every table, so an upgrade can be held to
// losing none.
func rowCounts(t *testing.T, conn *pgx.Conn) map[string]int {
	t.Helper()
	ctx := context.Background()
	rows, err := conn.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename <> 'goose_db_version' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, table := range tables {
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM `+pgx.Identifier{table}.Sanitize()).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		counts[table] = n
	}
	return counts
}

// The columns go as the plain owner, losing no row and no standing, after
// refusing what would take an object somebody made; the way back fills them.
func TestTheOrganizationColumnsGoAsThePlainOwner(t *testing.T) {
	ctx := context.Background()
	provider, super := ownerDatabase(t, "armature_columns_go_test", "armature_columns_go_owner")
	if _, err := provider.UpTo(ctx, beforeTheColumnsGo); err != nil {
		t.Fatalf("migrate to %d as the owner: %v", beforeTheColumnsGo, err)
	}
	exec := func(t *testing.T, sql string, args ...any) {
		t.Helper()
		if _, err := super.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	var orgID string
	if err := super.QueryRow(ctx, `INSERT INTO org (slug, name) VALUES ('acme', 'Acme') RETURNING id`).Scan(&orgID); err != nil {
		t.Fatalf("set the installation up: %v", err)
	}
	// The bootstrap in Go writes today's schema, so an issue's type and status
	// are made by hand at this version.
	var taskType, toDo string
	if err := super.QueryRow(ctx, `INSERT INTO issue_type (name) VALUES ('Task') RETURNING id`).Scan(&taskType); err != nil {
		t.Fatal(err)
	}
	if err := super.QueryRow(ctx, `INSERT INTO issue_status (name, category) VALUES ('To Do', 'todo') RETURNING id`).Scan(&toDo); err != nil {
		t.Fatal(err)
	}
	// Everybody at each standing, one of them switched off, and somebody who
	// has an account but is not a member.
	standings := map[string]string{"owner": "owner", "admin": "admin", "member": "member", "off": "member", "customer": "customer", "outside": ""}
	for name, standing := range standings {
		exec(t, `INSERT INTO app_user (email, name, is_active) VALUES ($1 || '@columns.test', $1, $1 <> 'off')`, name)
		if standing != "" {
			exec(t, `INSERT INTO org_member (user_id, org_role) SELECT id, $2 FROM app_user WHERE email = $1 || '@columns.test'`, name, standing)
		}
	}
	exec(t, `INSERT INTO role_assignment (role, user_id) SELECT 'global_administrator', id FROM app_user WHERE email = 'owner@columns.test'`)
	exec(t, `INSERT INTO project (key, name) VALUES ('COL', 'Columns')`)
	exec(t, `INSERT INTO issue (project_id, key_num, issue_type_id, status_id, summary, rank)
		SELECT id, 1, $1, $2, 'Kept', 'a' FROM project WHERE key = 'COL'`, taskType, toDo)
	exec(t, `INSERT INTO user_session (user_id, token_hash, expires_at) SELECT id, '\x01', now() + interval '1 day' FROM app_user WHERE email = 'member@columns.test'`)
	exec(t, `INSERT INTO inbound_mail (message_id, from_email, outcome) VALUES ('<kept@columns.test>', 'someone@columns.test', 'unmatched')`)
	exec(t, `INSERT INTO portal_code (email, code_hash, expires_at) VALUES ('door@columns.test', '\x01', now() + interval '1 hour')`)

	version := func(t *testing.T) int64 {
		t.Helper()
		v, err := provider.GetDBVersion(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	refused := func(t *testing.T, want string) {
		t.Helper()
		_, err := provider.ApplyVersion(ctx, theColumnsGo, true)
		if err == nil {
			t.Fatalf("the upgrade went ahead, want it refused naming %q", want)
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
		if v := version(t); v != beforeTheColumnsGo {
			t.Errorf("the refused upgrade left the database at %d, want %d", v, beforeTheColumnsGo)
		}
	}
	before := namingTheOrganization(t, super)
	if len(before) == 0 {
		t.Fatalf("nothing names the organization at %d", beforeTheColumnsGo)
	}

	t.Run("a view somebody made on the column stops the upgrade", func(t *testing.T) {
		exec(t, `CREATE VIEW projects_by_org AS SELECT org_id, key FROM project`)
		refused(t, "view projects_by_org")
		exec(t, `DROP VIEW projects_by_org`)
	})
	t.Run("a function somebody made that reads the column stops the upgrade", func(t *testing.T) {
		exec(t, `CREATE FUNCTION first_org() RETURNS uuid LANGUAGE plpgsql AS 'BEGIN RETURN (SELECT org_id FROM project LIMIT 1); END'`)
		refused(t, "function first_org")
		exec(t, `DROP FUNCTION first_org()`)
	})
	t.Run("a second organization stops the upgrade", func(t *testing.T) {
		exec(t, `INSERT INTO org (slug, name) VALUES ('second', 'Second')`)
		refused(t, "holds 2 organizations")
		exec(t, `DELETE FROM org WHERE slug = 'second'`)
	})
	if after := namingTheOrganization(t, super); !slices.Equal(after, before) {
		t.Fatalf("the refused upgrades changed the schema:\n%v\nwant\n%v", after, before)
	}

	counts := rowCounts(t, super)
	standingsNow := func(t *testing.T) map[string]string {
		t.Helper()
		rows, err := super.Query(ctx, `
			SELECT u.name || ' ' || coalesce(m.org_role, 'none') || ' ' || u.is_active
			FROM app_user u LEFT JOIN org_member m ON m.user_id = u.id
			WHERE u.email LIKE '%@columns.test'`)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, line := range got {
			name, rest, _ := strings.Cut(line, " ")
			out[name] = rest
		}
		return out
	}
	held := standingsNow(t)
	if len(held) != len(standings) || held["off"] != "member false" || held["outside"] != "none true" {
		t.Fatalf("the standings before the upgrade are %v", held)
	}
	same := func(t *testing.T, when string) {
		t.Helper()
		if got := rowCounts(t, super); !maps.Equal(got, counts) {
			for table, n := range counts {
				if got[table] != n {
					t.Errorf("%s: %s holds %d rows, want %d", when, table, got[table], n)
				}
			}
		}
		if got := standingsNow(t); !maps.Equal(got, held) {
			t.Errorf("%s: the standings are %v, want %v", when, got, held)
		}
	}

	if _, err := provider.ApplyVersion(ctx, theColumnsGo, true); err != nil {
		t.Fatalf("drop the columns as the owner: %v", err)
	}
	if named := namingTheOrganization(t, super); len(named) > 0 {
		t.Errorf("after the upgrade %d things still name the organization: %v", len(named), named)
	}
	same(t, "after the upgrade")
	var roles int
	if err := super.QueryRow(ctx, `SELECT count(*) FROM org_role WHERE builtin`).Scan(&roles); err != nil {
		t.Fatal(err)
	}
	if roles != len(perm.Builtins()) {
		t.Errorf("the installation has %d built-in roles after the upgrade, want %d", roles, len(perm.Builtins()))
	}

	if _, err := provider.ApplyVersion(ctx, theColumnsGo, false); err != nil {
		t.Fatalf("take the upgrade back: %v", err)
	}
	if back := namingTheOrganization(t, super); !slices.Equal(back, before) {
		t.Errorf("taking the upgrade back left\n%v\nwant\n%v", back, before)
	}
	same(t, "after taking it back")
	var unplaced int
	if err := super.QueryRow(ctx, `
		SELECT count(*) FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
		WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'r' AND a.attname = 'org_id'
		  AND a.attnotnull AND NOT a.attisdropped`).Scan(&unplaced); err != nil {
		t.Fatal(err)
	}
	if unplaced != 92 {
		t.Errorf("taking the upgrade back made %d org_id columns that must name the organization, want 92", unplaced)
	}
	var elsewhere int
	if err := super.QueryRow(ctx, `SELECT count(*) FROM project WHERE org_id IS DISTINCT FROM $1`, orgID).Scan(&elsewhere); err != nil {
		t.Fatal(err)
	}
	if elsewhere != 0 {
		t.Errorf("taking the upgrade back left %d projects outside the installation", elsewhere)
	}

	if _, err := provider.ApplyVersion(ctx, theColumnsGo, true); err != nil {
		t.Fatalf("drop the columns again: %v", err)
	}
	if named := namingTheOrganization(t, super); len(named) > 0 {
		t.Errorf("after the second upgrade %d things still name the organization: %v", len(named), named)
	}
	same(t, "after the second upgrade")
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
	var written int
	if err := h.cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = $1`, action).Scan(&written)
	}); err != nil {
		t.Fatalf("read with no organization in the context: %v", err)
	}
	if written != 1 {
		t.Errorf("read %d of the rows just written, want 1", written)
	}

	got, err := installation.NewService(h.cluster).Get(ctx)
	if err != nil {
		t.Fatalf("read the installation with no organization in the context: %v", err)
	}
	if got.Name == "" {
		t.Error("the installation has no name")
	}
}
