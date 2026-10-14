//go:build integration

package test

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/perm"
)

const (
	beforeTheTablesAreRenamed = 937
	theTablesAreRenamed       = 938
)

// oldTablePattern finds the organization's tables, and the columns that moved,
// by their old names in a definition or a function's body.
const oldTablePattern = `\m(org_member|org_role|org_invite|org_join_request|org_wide_only)\M|\m(from|join|into|update|table|references)\s+org\M`

// namingTheOldTables lists what in the schema is named for the organization's
// tables or reads them by their old names.
func namingTheOldTables(t *testing.T, conn catalogReader) []string {
	t.Helper()
	rows, err := conn.Query(context.Background(), `
		SELECT format('%s %s', CASE c.relkind WHEN 'i' THEN 'index' WHEN 'v' THEN 'view' WHEN 'S' THEN 'sequence' ELSE 'table' END, c.relname)
		FROM pg_class c
		WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p', 'i', 'v', 'm', 'S') AND c.relname ~ '^org(_|$)'
		UNION ALL
		SELECT format('column %s.%s', c.relname, a.attname)
		FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
		WHERE c.relnamespace = 'public'::regnamespace AND a.attnum > 0 AND NOT a.attisdropped AND a.attname ~ '^org(_|$)'
		UNION ALL
		SELECT format('constraint %s on %s', k.conname, k.conrelid::regclass) FROM pg_constraint k
		WHERE k.connamespace = 'public'::regnamespace AND (k.conname ~ '^org(_|$)' OR pg_get_constraintdef(k.oid) ~* $1)
		UNION ALL
		SELECT format('index definition %s', i.indexrelid::regclass) FROM pg_index i JOIN pg_class c ON c.oid = i.indrelid
		WHERE c.relnamespace = 'public'::regnamespace AND pg_get_indexdef(i.indexrelid) ~* $1
		UNION ALL
		SELECT format('trigger %s on %s', g.tgname, g.tgrelid::regclass) FROM pg_trigger g
		WHERE NOT g.tgisinternal AND (g.tgname ~ '^org(_|$)' OR pg_get_triggerdef(g.oid) ~* $1)
		UNION ALL
		SELECT format('function %s', p.proname) FROM pg_proc p
		WHERE p.pronamespace = 'public'::regnamespace AND (p.proname ~ '^org(_|$)' OR p.prosrc ~* $1)
		UNION ALL
		SELECT format('view definition %s', v.viewname) FROM pg_views v
		WHERE v.schemaname = 'public' AND v.definition ~* $1
		ORDER BY 1`, oldTablePattern)
	if err != nil {
		t.Fatalf("list what names the organization's tables: %v", err)
	}
	named, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("read what names the organization's tables: %v", err)
	}
	return named
}

// Nothing in the schema is named for the organization: its tables have names of
// their own, and a member's standing is a column of the account (#218).
func TestNothingInTheSchemaIsNamedForTheOrganization(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	if named := namingTheOldTables(t, h.super); len(named) > 0 {
		t.Errorf("%d things in the schema are still named for the organization: %v", len(named), named)
	}
}

// The installation's row holds its settings, and with nothing naming it there
// must be exactly one, so a second is refused.
func TestTheInstallationIsOneRow(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	tx, err := h.super.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO installation (slug, name) VALUES ($1, 'Second')`, strings.ToLower(unique("second")))
	if err == nil || !strings.Contains(err.Error(), "installation_one_row_idx") {
		t.Errorf("a second installation row = %v, want it refused by installation_one_row_idx", err)
	}
}

// A standing is one of four, or none for somebody who is no member; the
// database refuses any other straight through SQL.
func TestAStandingIsOneOfFour(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()
	tx, err := h.super.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO app_user (email, name, standing) VALUES ($1, 'Guest', 'guest')`, unique("guest")+"@standing.test")
	if err == nil || !strings.Contains(err.Error(), "app_user_standing_check") {
		t.Errorf("an account standing as a guest = %v, want it refused by app_user_standing_check", err)
	}
}

// renamedTables are the tables 00938 renames, by their names after it.
var renamedTables = map[string]string{"installation": "org", "role": "org_role", "invite": "org_invite", "join_request": "org_join_request"}

// countsByOldName counts every table's rows under its name before 00938, with
// a membership for every account that has a standing once the table is gone.
func countsByOldName(t *testing.T, conn *pgx.Conn) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for table, n := range rowCounts(t, conn) {
		if old, ok := renamedTables[table]; ok {
			table = old
		}
		counts[table] = n
	}
	if _, ok := counts["org_member"]; !ok {
		var n int
		if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM app_user WHERE standing IS NOT NULL`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		counts["org_member"] = n
	}
	return counts
}

// The tables are renamed as the plain owner, after refusing what would break
// unseen, and every membership survives as a standing, there and back.
func TestTheTablesAreRenamedAsThePlainOwner(t *testing.T) {
	ctx := context.Background()
	provider, super := ownerDatabase(t, "armature_tables_renamed_test", "armature_tables_renamed_owner")
	if _, err := provider.UpTo(ctx, beforeTheTablesAreRenamed); err != nil {
		t.Fatalf("migrate to %d as the owner: %v", beforeTheTablesAreRenamed, err)
	}
	exec := func(t *testing.T, sql string, args ...any) {
		t.Helper()
		if _, err := super.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(t, `INSERT INTO org (slug, name) VALUES ('acme', 'Acme')`)
	// Everybody at each standing, one of them switched off, and somebody who
	// has an account but is not a member.
	standings := map[string]string{"owner": "owner", "admin": "admin", "member": "member", "off": "member", "customer": "customer", "outside": ""}
	for name, standing := range standings {
		exec(t, `INSERT INTO app_user (email, name, is_active, updated_at) VALUES ($1 || '@tables.test', $1, $1 <> 'off', '2026-01-02T03:04:05Z')`, name)
		if standing != "" {
			exec(t, `INSERT INTO org_member (user_id, org_role) SELECT id, $2 FROM app_user WHERE email = $1 || '@tables.test'`, name, standing)
		}
	}
	exec(t, `INSERT INTO org_role (key, name, org_wide_only, permissions) VALUES ('auditor', 'Auditor', true, ARRAY['read'])`)
	exec(t, `INSERT INTO role_assignment (role, user_id) SELECT 'auditor', id FROM app_user WHERE email = 'admin@tables.test'`)
	exec(t, `INSERT INTO theme (owner_id, name, shared) SELECT id, 'House', true FROM app_user WHERE email = 'owner@tables.test'`)
	exec(t, `UPDATE org SET default_theme_id = (SELECT id FROM theme WHERE name = 'House')`)
	exec(t, `INSERT INTO org_invite (email, org_role, token_hash, expires_at) VALUES ('asked@tables.test', 'customer', '\x01', now() + interval '1 day')`)
	exec(t, `INSERT INTO org_join_request (user_id) SELECT id FROM app_user WHERE email = 'outside@tables.test'`)

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
		_, err := provider.ApplyVersion(ctx, theTablesAreRenamed, true)
		if err == nil {
			t.Fatalf("the upgrade went ahead, want it refused naming %q", want)
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
		if v := version(t); v != beforeTheTablesAreRenamed {
			t.Errorf("the refused upgrade left the database at %d, want %d", v, beforeTheTablesAreRenamed)
		}
	}
	before := namingTheOldTables(t, super)
	if len(before) == 0 {
		t.Fatalf("nothing is named for the organization at %d", beforeTheTablesAreRenamed)
	}

	t.Run("a view somebody made on the memberships stops the upgrade", func(t *testing.T) {
		exec(t, `CREATE VIEW administrators AS SELECT user_id FROM org_member WHERE org_role = 'admin'`)
		refused(t, "view administrators")
		exec(t, `DROP VIEW administrators`)
	})
	t.Run("a trigger somebody put on the memberships stops the upgrade", func(t *testing.T) {
		exec(t, `CREATE FUNCTION nothing() RETURNS trigger LANGUAGE plpgsql AS 'BEGIN RETURN NEW; END'`)
		exec(t, `CREATE TRIGGER membership_watched AFTER INSERT ON org_member FOR EACH ROW EXECUTE FUNCTION nothing()`)
		refused(t, "trigger membership_watched")
		exec(t, `DROP TRIGGER membership_watched ON org_member`)
		exec(t, `DROP FUNCTION nothing()`)
	})
	t.Run("a function somebody made that reads a renamed table stops the upgrade", func(t *testing.T) {
		exec(t, `CREATE FUNCTION house_name() RETURNS text LANGUAGE plpgsql AS 'BEGIN RETURN (SELECT name FROM org); END'`)
		refused(t, "function house_name")
		exec(t, `DROP FUNCTION house_name()`)
	})
	if after := namingTheOldTables(t, super); !slices.Equal(after, before) {
		t.Fatalf("the refused upgrades changed the schema:\n%v\nwant\n%v", after, before)
	}

	counts := countsByOldName(t, super)
	standingsNow := func(t *testing.T) map[string]string {
		t.Helper()
		var query string
		if version(t) >= theTablesAreRenamed {
			query = `SELECT u.name || ' ' || coalesce(u.standing, 'none') || ' ' || u.is_active || ' ' || u.updated_at FROM app_user u WHERE u.email LIKE '%@tables.test'`
		} else {
			query = `SELECT u.name || ' ' || coalesce(m.org_role, 'none') || ' ' || u.is_active || ' ' || u.updated_at
				FROM app_user u LEFT JOIN org_member m ON m.user_id = u.id WHERE u.email LIKE '%@tables.test'`
		}
		rows, err := super.Query(ctx, query)
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
	if len(held) != len(standings) || !strings.HasPrefix(held["off"], "member false ") || !strings.HasPrefix(held["outside"], "none true ") {
		t.Fatalf("the standings before the upgrade are %v", held)
	}
	same := func(t *testing.T, when string) {
		t.Helper()
		if got := countsByOldName(t, super); !maps.Equal(got, counts) {
			for table, n := range counts {
				if got[table] != n {
					t.Errorf("%s: %s holds %d rows, want %d", when, table, got[table], n)
				}
			}
			for table := range got {
				if _, ok := counts[table]; !ok {
					t.Errorf("%s: %s is new", when, table)
				}
			}
		}
		if got := standingsNow(t); !maps.Equal(got, held) {
			t.Errorf("%s: the standings are %v, want %v", when, got, held)
		}
	}
	renamed := func(t *testing.T, when string) {
		t.Helper()
		if named := namingTheOldTables(t, super); len(named) > 0 {
			t.Errorf("%s %d things are still named for the organization: %v", when, len(named), named)
		}
		var roles, wide, invited, asked int
		var theme *string
		if err := super.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM role WHERE builtin),
			       (SELECT count(*) FROM role WHERE key = 'auditor' AND installation_wide_only),
			       (SELECT count(*) FROM invite WHERE email = 'asked@tables.test' AND standing = 'customer'),
			       (SELECT count(*) FROM join_request j JOIN app_user u ON u.id = j.user_id WHERE u.email = 'outside@tables.test'),
			       (SELECT t.name FROM installation i JOIN theme t ON t.id = i.default_theme_id)`).Scan(&roles, &wide, &invited, &asked, &theme); err != nil {
			t.Fatalf("%s: %v", when, err)
		}
		if roles != len(perm.Builtins()) || wide != 1 || invited != 1 || asked != 1 || theme == nil || *theme != "House" {
			t.Errorf("%s: %d built-in roles, %d wide auditor, %d invitation, %d request, default theme %v; want %d, 1, 1, 1, House",
				when, roles, wide, invited, asked, theme, len(perm.Builtins()))
		}
		tx, err := super.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `INSERT INTO installation (slug, name) VALUES ('second', 'Second')`); err == nil || !strings.Contains(err.Error(), "installation_one_row_idx") {
			t.Errorf("%s: a second installation row = %v, want it refused by installation_one_row_idx", when, err)
		}
	}

	if _, err := provider.ApplyVersion(ctx, theTablesAreRenamed, true); err != nil {
		t.Fatalf("rename the tables as the owner: %v", err)
	}
	renamed(t, "after the upgrade")
	same(t, "after the upgrade")

	if _, err := provider.ApplyVersion(ctx, theTablesAreRenamed, false); err != nil {
		t.Fatalf("take the upgrade back: %v", err)
	}
	if back := namingTheOldTables(t, super); !slices.Equal(back, before) {
		t.Errorf("taking the upgrade back left\n%v\nwant\n%v", back, before)
	}
	same(t, "after taking it back")

	if _, err := provider.ApplyVersion(ctx, theTablesAreRenamed, true); err != nil {
		t.Fatalf("rename the tables again: %v", err)
	}
	renamed(t, "after the second upgrade")
	same(t, "after the second upgrade")
}
