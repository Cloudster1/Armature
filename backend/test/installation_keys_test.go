//go:build integration

package test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

const (
	beforeTheKeys = 932
	theKeys       = 933
	// Ten primary keys, the absence exclusion, 23 unique and 22 plain indexes,
	// and the role assignment's foreign key.
	orgKeyedBeforeTheKeys = 57
	// Somebody the rows below are about; with foreign keys off, nobody real.
	somebody = "01900000-0000-7000-8000-000000000001"
	aProject = "01900000-0000-7000-8000-000000000002"
	anIssue  = "01900000-0000-7000-8000-000000000003"
)

type catalogReader interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// orgKeyed lists every key, index and foreign key of more than one column that
// takes org_id into account. The plain foreign keys to org go with the column.
func orgKeyed(t *testing.T, db catalogReader) []string {
	t.Helper()
	rows, err := db.Query(context.Background(), `
		WITH org_column AS (
		    SELECT attrelid, attnum FROM pg_attribute WHERE attname = 'org_id' AND NOT attisdropped
		)
		SELECT format('%s %s', c.conrelid::regclass, c.conname)
		FROM pg_constraint c
		WHERE c.connamespace = 'public'::regnamespace AND c.contype = 'f' AND cardinality(c.conkey) > 1
		  AND EXISTS (SELECT 1 FROM org_column o WHERE o.attrelid = c.conrelid AND o.attnum = ANY (c.conkey))
		UNION ALL
		SELECT format('%s %s', i.indrelid::regclass, i.indexrelid::regclass)
		FROM pg_index i JOIN pg_class t ON t.oid = i.indrelid
		WHERE t.relnamespace = 'public'::regnamespace
		  AND (EXISTS (SELECT 1 FROM org_column o WHERE o.attrelid = i.indrelid AND o.attnum = ANY (i.indkey))
		       OR coalesce(pg_get_expr(i.indexprs, i.indrelid), '') ~ 'org_id'
		       OR coalesce(pg_get_expr(i.indpred, i.indrelid), '') ~ 'org_id')
		ORDER BY 1`)
	if err != nil {
		t.Fatalf("list what takes the organization into account: %v", err)
	}
	keyed, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("read what takes the organization into account: %v", err)
	}
	return keyed
}

// An installation holds one organization, so a key unique in it is unique in
// the installation, and saying so needs no org_id.
func TestNoKeyOrIndexTakesTheOrganizationIntoAccount(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	if keyed := orgKeyed(t, h.super); len(keyed) > 0 {
		t.Errorf("%d keys and indexes still take org_id into account: %v", len(keyed), keyed)
	}
}

// Each key is tried with a row of a second organization, straight through SQL.
// Triggers and foreign keys are off, so a row needs nothing around it.
func TestKeysAreTheInstallations(t *testing.T) {
	ctx := context.Background()
	provider, super := ownerDatabase(t, "armature_installation_keys_test", "armature_installation_keys_owner")
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate as the owner: %v", err)
	}
	var first, second string
	if err := super.QueryRow(ctx, `INSERT INTO org (slug, name) VALUES ('first', 'First') RETURNING id`).Scan(&first); err != nil {
		t.Fatalf("insert the first organization: %v", err)
	}
	if err := super.QueryRow(ctx, `INSERT INTO org (slug, name) VALUES ('second', 'Second') RETURNING id`).Scan(&second); err != nil {
		t.Fatalf("insert the second organization: %v", err)
	}
	if _, err := super.Exec(ctx, `SET session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}

	cases := []struct{ constraint, insert string }{
		{"issue_old_key_pkey", `INSERT INTO issue_old_key (org_id, issue_id, key) VALUES ($1, gen_random_uuid(), 'OLD-1')`},
		{"member_schedule_pkey", `INSERT INTO member_schedule (org_id, user_id) VALUES ($1, '` + somebody + `')`},
		{"notification_digest_sent_pkey", `INSERT INTO notification_digest_sent (org_id, user_id, last_sent_at) VALUES ($1, '` + somebody + `', now())`},
		{"notification_preference_pkey", `INSERT INTO notification_preference (org_id, user_id) VALUES ($1, '` + somebody + `')`},
		{"oidc_provider_pkey", `INSERT INTO oidc_provider (org_id, issuer, client_id, client_secret) VALUES ($1, 'https://idp.test', 'armature', 'secret')`},
		{"org_join_request_pkey", `INSERT INTO org_join_request (org_id, user_id) VALUES ($1, '` + somebody + `')`},
		{"org_member_pkey", `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, '` + somebody + `', 'member')`},
		{"org_role_pkey", `INSERT INTO org_role (org_id, key, name) VALUES ($1, 'auditor', 'Auditor ' || gen_random_uuid())`},
		{"portal_code_pkey", `INSERT INTO portal_code (org_id, email, code_hash, expires_at) VALUES ($1, 'door@example.test', '\x01', now())`},
		{"user_theme_pkey", `INSERT INTO user_theme (org_id, user_id) VALUES ($1, '` + somebody + `')`},
		{"absence_no_overlap", `INSERT INTO absence (org_id, user_id, starts_on, ends_on) VALUES ($1, '` + somebody + `', '2026-03-02', '2026-03-06')`},
		{"automation_rule_name_idx", `INSERT INTO automation_rule (org_id, project_id, name, trigger, actions) VALUES ($1, '` + aProject + `', 'Nightly', '{"kind": "manual"}', '[{"kind": "comment"}]')`},
		{"custom_field_installation_name_idx", `INSERT INTO custom_field (org_id, name, kind) VALUES ($1, 'Severity', 'text')`},
		{"dashboard_template_name_idx", `INSERT INTO dashboard_template (org_id, name) VALUES ($1, 'Weekly')`},
		{"holiday_calendar_name_idx", `INSERT INTO holiday_calendar (org_id, name) VALUES ($1, 'Bavaria')`},
		{"holiday_calendar_one_default_idx", `INSERT INTO holiday_calendar (org_id, name, is_default) VALUES ($1, 'Default ' || gen_random_uuid(), true)`},
		{"issue_arrangement_scope_idx", `INSERT INTO issue_arrangement (org_id, project_id) VALUES ($1, '` + aProject + `')`},
		{"issue_comment_external_key_idx", `INSERT INTO issue_comment (org_id, issue_id, body, external_key) VALUES ($1, '` + anIssue + `', '{}', 'COMMENT-1')`},
		{"issue_link_type_name_idx", `INSERT INTO issue_link_type (org_id, name, outward, inward) VALUES ($1, 'Clones', 'clones', 'is cloned by')`},
		{"issue_status_name_idx", `INSERT INTO issue_status (org_id, name, category) VALUES ($1, 'Waiting', 'todo')`},
		{"issue_type_name_idx", `INSERT INTO issue_type (org_id, name) VALUES ($1, 'Chore')`},
		{"issue_worklog_external_key_idx", `INSERT INTO issue_worklog (org_id, issue_id, minutes, external_key) VALUES ($1, '` + anIssue + `', 30, 'WORKLOG-1')`},
		{"label_name_idx", `INSERT INTO label (org_id, name) VALUES ($1, 'urgent')`},
		{"org_invite_pending_idx", `INSERT INTO org_invite (org_id, email, org_role, token_hash, expires_at) VALUES ($1, 'new@example.test', 'member', sha256(gen_random_uuid()::text::bytea), now() + interval '1 day')`},
		{"org_role_name_idx", `INSERT INTO org_role (org_id, key, name) VALUES ($1, 'r' || substr(md5(gen_random_uuid()::text), 1, 12), 'Watcher')`},
		{"project_key_idx", `INSERT INTO project (org_id, key, name) VALUES ($1, 'SAME', 'Same key')`},
		{"role_assignment_unique_idx", `INSERT INTO role_assignment (org_id, role, user_id) VALUES ($1, 'reader', '` + somebody + `')`},
		{"theme_owner_name_idx", `INSERT INTO theme (org_id, owner_id, name) VALUES ($1, '` + somebody + `', 'Dusk')`},
		{"user_group_name_idx", `INSERT INTO user_group (org_id, name) VALUES ($1, 'Support')`},
		{"user_group_external_ref_idx", `INSERT INTO user_group (org_id, name, source, external_ref) VALUES ($1, 'Group ' || gen_random_uuid(), 'oidc', 'cn=support')`},
		{"webhook_endpoint_name_idx", `INSERT INTO webhook_endpoint (org_id, name, url, secret) VALUES ($1, 'Chat', 'https://hooks.example.test/chat', 'secret')`},
		{"workflow_name_idx", `INSERT INTO workflow (org_id, name) VALUES ($1, 'Review')`},
		{"workflow_scheme_one_default_idx", `INSERT INTO workflow_scheme (org_id, name, is_default) VALUES ($1, 'Default ' || gen_random_uuid(), true)`},
		{"workflow_scheme_name_idx", `INSERT INTO workflow_scheme (org_id, name) VALUES ($1, 'Scheme')`},
	}
	for _, c := range cases {
		t.Run(c.constraint, func(t *testing.T) {
			if _, err := super.Exec(ctx, c.insert, first); err != nil {
				t.Fatalf("the first organization's row: %v", err)
			}
			_, err := super.Exec(ctx, c.insert, second)
			if err == nil {
				t.Fatalf("a second organization took the same key past %s", c.constraint)
			}
			if !strings.Contains(err.Error(), c.constraint) {
				t.Errorf("refused, but not by %s: %v", c.constraint, err)
			}
		})
	}

	// With foreign keys back on, a role is the installation's whichever
	// organization's row made it; the guard is not what this is about.
	if _, err := super.Exec(ctx, `RESET session_replication_role`); err != nil {
		t.Fatal(err)
	}
	if _, err := super.Exec(ctx, `ALTER TABLE role_assignment DISABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	t.Run("an assignment names a role the installation has", func(t *testing.T) {
		var auditorID string
		if err := super.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ('auditor@example.test', 'Auditor') RETURNING id`).Scan(&auditorID); err != nil {
			t.Fatalf("insert a person: %v", err)
		}
		if _, err := super.Exec(ctx, `INSERT INTO role_assignment (org_id, role, user_id) VALUES ($1, 'auditor', $2)`, second, auditorID); err != nil {
			t.Errorf("the installation's auditor role was refused: %v", err)
		}
		_, err := super.Exec(ctx, `INSERT INTO role_assignment (org_id, role, user_id) VALUES ($1, 'no_such_role', $2)`, first, auditorID)
		if err == nil || !strings.Contains(err.Error(), "role_assignment_role_fkey") {
			t.Errorf("a role nobody made was assigned: %v", err)
		}
	})

	t.Run("a mail is never ambiguous", func(t *testing.T) {
		_, err := super.Exec(ctx, `INSERT INTO inbound_mail (message_id, from_email, outcome) VALUES ('<ambiguous@example.test>', 'customer@example.test', 'ambiguous')`)
		if err == nil || !strings.Contains(err.Error(), "inbound_mail_outcome_check") {
			t.Errorf("a mail was recorded as ambiguous: %v", err)
		}
	})
}

// Rows that share a key once it is the installation's stop the upgrade, which
// names them, and the way back puts the organization into each key again.
func TestTheUpgradeRefusesKeysThatCollide(t *testing.T) {
	ctx := context.Background()
	provider, super := ownerDatabase(t, "armature_installation_keys_upgrade_test", "armature_installation_keys_upgrade_owner")
	if _, err := provider.UpTo(ctx, beforeTheKeys); err != nil {
		t.Fatalf("migrate to %d as the owner: %v", beforeTheKeys, err)
	}
	var personID string
	if err := super.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ('both@example.test', 'Both') RETURNING id`).Scan(&personID); err != nil {
		t.Fatalf("insert a person: %v", err)
	}
	for _, slug := range []string{"first", "second"} {
		var orgID string
		if err := super.QueryRow(ctx, `INSERT INTO org (slug, name) VALUES ($1, $2) RETURNING id`, slug, slug).Scan(&orgID); err != nil {
			t.Fatalf("insert organization %s: %v", slug, err)
		}
		if _, err := super.Exec(ctx, `INSERT INTO project (org_id, key, name) VALUES ($1, 'DUP', 'Duplicate')`, orgID); err != nil {
			t.Fatalf("insert %s's project: %v", slug, err)
		}
		if _, err := super.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'member')`, orgID, personID); err != nil {
			t.Fatalf("make the person a member of %s: %v", slug, err)
		}
	}
	before := orgKeyed(t, super)
	if len(before) != orgKeyedBeforeTheKeys {
		t.Fatalf("%d keys and indexes take org_id into account at %d, want %d: %v", len(before), beforeTheKeys, orgKeyedBeforeTheKeys, before)
	}

	_, err := provider.ApplyVersion(ctx, theKeys, true)
	if err == nil {
		t.Fatal("the upgrade went past two projects keyed DUP")
	}
	// Each organization's five roles collide by key and by name, so the twelve
	// collisions are more than the ten the refusal lists.
	for _, want := range []string{"project key DUP", "member both@example.test", "role global_administrator", "and 2 more"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not say %q: %v", want, err)
		}
	}
	if v, err := provider.GetDBVersion(ctx); err != nil || v != beforeTheKeys {
		t.Errorf("the refused upgrade left the database at %d (%v), want %d", v, err, beforeTheKeys)
	}
	if after := orgKeyed(t, super); !slices.Equal(after, before) {
		t.Errorf("the refused upgrade changed the keys: %v", after)
	}

	if _, err := super.Exec(ctx, `DELETE FROM org WHERE slug = 'second'`); err != nil {
		t.Fatalf("delete the second organization: %v", err)
	}
	if _, err := provider.ApplyVersion(ctx, theKeys, true); err != nil {
		t.Fatalf("upgrade one organization: %v", err)
	}
	if keyed := orgKeyed(t, super); len(keyed) > 0 {
		t.Errorf("after the upgrade %d keys and indexes take org_id into account: %v", len(keyed), keyed)
	}

	if _, err := provider.ApplyVersion(ctx, theKeys, false); err != nil {
		t.Fatalf("take the upgrade back: %v", err)
	}
	if back := orgKeyed(t, super); !slices.Equal(back, before) {
		t.Errorf("taking the upgrade back left the keys\n%v\nwant\n%v", back, before)
	}
	if _, err := provider.ApplyVersion(ctx, theKeys, true); err != nil {
		t.Fatalf("upgrade again: %v", err)
	}
	var projects, members int
	if err := super.QueryRow(ctx, `SELECT (SELECT count(*) FROM project), (SELECT count(*) FROM org_member)`).Scan(&projects, &members); err != nil {
		t.Fatal(err)
	}
	if projects != 1 || members != 1 {
		t.Errorf("after going there and back there are %d projects and %d members, want the first organization's one of each", projects, members)
	}
}
