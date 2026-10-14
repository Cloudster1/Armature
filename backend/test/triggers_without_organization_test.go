//go:build integration

package test

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/bootstrap"
)

// Every rule the triggers keep is tried straight through SQL in an installation
// of its own.
func TestTriggersKeepTheirRulesWithoutTheOrganization(t *testing.T) {
	ctx := context.Background()
	provider, super := ownerDatabase(t, "armature_trigger_rules_test", "armature_trigger_rules_owner")
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate as the owner: %v", err)
	}

	ids := map[string]string{}
	made := func(name, sql string, args ...any) {
		t.Helper()
		var id string
		if err := super.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("make %s: %v", name, err)
		}
		ids[name] = id
	}
	made("installation", `INSERT INTO org (slug, name) VALUES ('first', 'First') RETURNING id`)
	// One transaction, as the scheme is checked once it maps a workflow.
	setUp, err := super.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	set, err := bootstrap.Installation(ctx, setUp)
	if err != nil {
		t.Fatalf("set the installation up: %v", err)
	}
	if err := setUp.Commit(ctx); err != nil {
		t.Fatalf("set the installation up: %v", err)
	}
	ids["scheme"] = set.WorkflowSchemeID.String()
	ids["workflow"] = set.WorkflowID.String()
	ids["bug type"] = set.IssueTypeIDs[bootstrap.TypeBug].String()

	for _, p := range []struct{ name, role string }{
		{"admin", "owner"}, {"member", "member"}, {"customer", "customer"}, {"stranger", ""},
	} {
		made(p.name, `INSERT INTO app_user (email, name) VALUES ($1 || '@triggers.test', $1) RETURNING id`, p.name)
		if p.role != "" {
			if _, err := super.Exec(ctx, `INSERT INTO org_member (user_id, org_role) VALUES ($1, $2)`, ids[p.name], p.role); err != nil {
				t.Fatal(err)
			}
		}
	}
	const planned = `{sprints,milestones,releases,components,teams,automation}`
	made("alpha", `INSERT INTO project (key, name, features) VALUES ('ALPHA', 'Alpha', $1) RETURNING id`, planned)
	made("beta", `INSERT INTO project (key, name, features) VALUES ('BETA', 'Beta', $1) RETURNING id`, planned)
	made("desk", `INSERT INTO project (key, name, kind) VALUES ('DESK', 'Desk', 'service') RETURNING id`)
	issue := `INSERT INTO issue (project_id, key_num, issue_type_id, status_id, summary, rank) VALUES ($1, 1, $2, $3, $4, 'a') RETURNING id`
	todo := set.StatusIDs[bootstrap.StatusToDo]
	made("alpha issue", issue, ids["alpha"], set.IssueTypeIDs[bootstrap.TypeTask], todo, "Alpha's task")
	made("beta epic", issue, ids["beta"], set.IssueTypeIDs[bootstrap.TypeEpic], todo, "Beta's epic")
	made("desk request", issue, ids["desk"], set.IssueTypeIDs[bootstrap.TypeTask], todo, "A request")
	for _, project := range []string{"alpha", "beta"} {
		pid := ids[project]
		made(project+" milestone", `INSERT INTO milestone (project_id, name) VALUES ($1, 'M1') RETURNING id`, pid)
		made(project+" version", `INSERT INTO version (project_id, name) VALUES ($1, '1.0') RETURNING id`, pid)
		made(project+" component", `INSERT INTO component (project_id, name) VALUES ($1, 'Core') RETURNING id`, pid)
		made(project+" team", `INSERT INTO team (project_id, name) VALUES ($1, 'Crew') RETURNING id`, pid)
		made(project+" sprint", `INSERT INTO sprint (project_id, name) VALUES ($1, 'Sprint 1') RETURNING id`, pid)
		made(project+" field", `INSERT INTO custom_field (project_id, name, kind) VALUES ($1, $2, 'text') RETURNING id`, pid, project+" notes")
	}
	made("closed milestone", `INSERT INTO milestone (project_id, name, closed_at) VALUES ($1, 'M0', now()) RETURNING id`, ids["alpha"])
	made("archived version", `INSERT INTO version (project_id, name, released_at, archived_at) VALUES ($1, '0.9', now(), now()) RETURNING id`, ids["alpha"])
	made("closed sprint", `INSERT INTO sprint (project_id, name, state) VALUES ($1, 'Sprint 0', 'closed') RETURNING id`, ids["alpha"])
	made("team sprint", `INSERT INTO sprint (project_id, name, team_id) VALUES ($1, 'Crew sprint', $2) RETURNING id`, ids["alpha"], ids["alpha team"])
	made("severity", `INSERT INTO custom_field (name, kind) VALUES ('Severity', 'text') RETURNING id`)
	made("group", `INSERT INTO user_group (name) VALUES ('Support') RETURNING id`)
	made("dashboard", `INSERT INTO dashboard (project_id, name) VALUES ($1, 'Wall') RETURNING id`, ids["alpha"])
	made("calendar", `SELECT id FROM holiday_calendar WHERE is_default`)
	made("link type", `SELECT id FROM issue_link_type WHERE name = $1`, bootstrap.LinkRelates)
	made("session", `INSERT INTO user_session (user_id, token_hash, expires_at) VALUES ($1, '\x01', now() + interval '1 day') RETURNING id`, ids["member"])

	// Longest name first, as the replacer tries them in order and :alpha would
	// otherwise take the start of :alpha_issue.
	names := slices.SortedFunc(maps.Keys(ids), func(a, b string) int { return len(b) - len(a) })
	placeholders := make([]string, 0, 2*len(names))
	for _, name := range names {
		placeholders = append(placeholders, ":"+strings.ReplaceAll(name, " ", "_"), "'"+ids[name]+"'")
	}
	fill := strings.NewReplacer(placeholders...)

	// Each case runs in a transaction that is rolled back, with the deferred
	// rules checked at once. An empty refusal means the database must accept it.
	try := func(t *testing.T, sql string) error {
		t.Helper()
		tx, err := super.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE; "+fill.Replace(sql))
		return err
	}
	cases := []struct{ name, sql, refusal string }{
		// What a row is held to in its project.
		{"an issue's milestone is in its own project", `UPDATE issue SET milestone_id = :beta_milestone WHERE id = :alpha_issue`, "a milestone in its own project"},
		{"a closed milestone takes no more issues", `UPDATE issue SET milestone_id = :closed_milestone WHERE id = :alpha_issue`, "that milestone is closed"},
		{"an issue's sprint is in its own project", `UPDATE issue SET sprint_id = :beta_sprint WHERE id = :alpha_issue`, "a sprint in its own project"},
		{"a sprint that is over takes no more issues", `UPDATE issue SET sprint_id = :closed_sprint WHERE id = :alpha_issue`, "that sprint is over"},
		{"an issue is in a sprint its own team plans", `UPDATE issue SET sprint_id = :team_sprint WHERE id = :alpha_issue`, "a sprint its own team plans"},
		{"an issue's team is in its own project", `UPDATE issue SET team_id = :beta_team WHERE id = :alpha_issue`, "that team is not in this project"},
		{"a board's team is in its own project", `INSERT INTO board (project_id, name, team_id) VALUES (:alpha, 'Board', :beta_team)`, "that team is not in this project"},
		{"a sprint's team is in its own project", `INSERT INTO sprint (project_id, name, team_id) VALUES (:alpha, 'Sprint 2', :beta_team)`, "that team is not in this project"},
		{"an issue names a version of its own project", `INSERT INTO issue_version (issue_id, version_id, role) VALUES (:alpha_issue, :beta_version, 'fix')`, "a version of its own project"},
		{"an archived version is named by no more issues", `INSERT INTO issue_version (issue_id, version_id, role) VALUES (:alpha_issue, :archived_version, 'fix')`, "that version is archived"},
		{"an issue is in a component of its own project", `INSERT INTO issue_component (issue_id, component_id) VALUES (:alpha_issue, :beta_component)`, "a component of its own project"},
		{"an issue's field is one of its project", `INSERT INTO issue_field_value (issue_id, field_id, value) VALUES (:alpha_issue, :beta_field, '"x"')`, "is not a field of the project"},
		{"a parent is in its child's project", `UPDATE issue SET parent_id = :beta_epic WHERE id = :alpha_issue`, "same project as its child"},
		{"an article belongs to a service desk", `INSERT INTO kb_article (project_id, title) VALUES (:alpha, 'How to')`, "articles belong to a service desk project"},
		{"a rating belongs to a request", `INSERT INTO csat_rating (issue_id, token_hash) VALUES (:alpha_issue, '\x02')`, "a request of a service desk"},
		{"a project's field does not shadow the installation's", `INSERT INTO custom_field (project_id, name, kind) VALUES (:alpha, 'severity', 'text')`, "already has a field named"},
		{"a link has two ends", `INSERT INTO issue_link (link_type_id, source_id, target_id) VALUES (:link_type, :alpha_issue, gen_random_uuid())`, "issue_link_target_id_fkey"},

		// Who may do what.
		{"a customer joins no team", `INSERT INTO team_member (team_id, user_id) VALUES (:alpha_team, :customer)`, "not a customer, can join a team"},
		{"a stranger joins no team", `INSERT INTO team_member (team_id, user_id) VALUES (:alpha_team, :stranger)`, "not a customer, can join a team"},
		{"a customer is not away", `INSERT INTO absence (user_id, starts_on, ends_on) VALUES (:customer, '2026-03-02', '2026-03-02')`, "can be away"},
		{"a customer gives no project a share", `INSERT INTO project_allocation (project_id, user_id, percent) VALUES (:alpha, :customer, 50)`, "a share of their week"},
		{"a customer has no working week", `INSERT INTO member_schedule (user_id) VALUES (:customer)`, "has a working week"},
		{"a stranger joins no group", `INSERT INTO group_member (group_id, user_id) VALUES (:group, :stranger)`, "can join a group"},
		{"a customer shares no filter", `INSERT INTO saved_filter (owner_id, name, query, shared) VALUES (:customer, 'Mine', 'status = open', true)`, "cannot share a saved filter"},
		{"a customer shares no theme", `INSERT INTO theme (owner_id, name, shared) VALUES (:customer, 'Dusk', true)`, "cannot share a theme"},
		{"a stranger is granted no role", `INSERT INTO role_assignment (role, user_id) VALUES ('user', :stranger)`, "granted to a member here"},
		{"global administration is not granted over one project", `INSERT INTO role_assignment (role, project_id, user_id) VALUES ('global_administrator', :alpha, :member)`, "cannot be granted over one project"},
		{"a built-in role stays", `DELETE FROM org_role WHERE key = 'reader'`, "a built-in role cannot be deleted"},
		{"a role keeps its key", `UPDATE org_role SET key = 'viewer' WHERE key = 'reader'`, "key and standing are fixed"},
		{"the global administrator keeps administering", `UPDATE org_role SET permissions = ARRAY['read'] WHERE key = 'global_administrator'`, "keeps administering"},
		{"the last administrator stays switched on", `UPDATE app_user SET is_active = false WHERE id = :admin`, "cannot be switched off"},
		{"the installation keeps a default scheme", `UPDATE workflow_scheme SET is_default = false WHERE id = :scheme`, "one default workflow scheme"},
		{"the default scheme keeps its catch-all", `DELETE FROM workflow_scheme_item WHERE scheme_id = :scheme AND issue_type_id IS NULL`, "map the issue types it does not name"},
		{"a session keeps its proof", `UPDATE user_session SET proof = 'oidc' WHERE id = :session`, "keeps the proof it was opened with"},
		{"the default theme is a shared one", `INSERT INTO theme (owner_id, name) VALUES (:admin, 'Private'); UPDATE org SET default_theme_id = (SELECT id FROM theme WHERE name = 'Private')`, "default theme has to be a shared theme"},

		// These were held to the organization alone, and nothing holds them now.
		{"a rule watches a project", `INSERT INTO automation_rule (project_id, name, trigger, actions) VALUES (:alpha, 'Nightly', '{"kind": "manual"}', '[{"kind": "comment"}]')`, ""},
		{"a link opens a dashboard", `INSERT INTO dashboard_share (dashboard_id, name, token_hash) VALUES (:dashboard, 'Wall', '\x04')`, ""},
		{"a holiday is in a calendar", `INSERT INTO holiday (calendar_id, day, name) VALUES (:calendar, '2026-12-25', 'Christmas')`, ""},
		{"an issue is linked", `INSERT INTO issue_link (link_type_id, source_id, target_id) VALUES (:link_type, :alpha_issue, :beta_epic)`, ""},
		{"an issue keeps an old key", `INSERT INTO issue_old_key (issue_id, key) VALUES (:alpha_issue, 'OLD-1')`, ""},
		{"an issue links a page", `INSERT INTO issue_remote_link (issue_id, url, title, source) VALUES (:alpha_issue, 'https://docs.example.test/page', 'Page', 'docs')`, ""},
		{"a notification is about an issue", `INSERT INTO notification (user_id, issue_id, kind, title, event_id) VALUES (:member, :alpha_issue, 'assigned', 'Yours', gen_random_uuid())`, ""},
		{"a project takes a scheme", `INSERT INTO workflow_scheme (name) VALUES ('Borrowed'); UPDATE project SET workflow_scheme_id = (SELECT id FROM workflow_scheme WHERE name = 'Borrowed') WHERE id = :alpha`, ""},
		{"a scheme maps a workflow", `INSERT INTO workflow_scheme_item (scheme_id, issue_type_id, workflow_id) VALUES (:scheme, :bug_type, :workflow)`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := try(t, c.sql)
			switch {
			case c.refusal == "" && err != nil:
				t.Errorf("refused: %v", err)
			case c.refusal != "" && err == nil:
				t.Errorf("accepted, want it refused with %q", c.refusal)
			case c.refusal != "" && !strings.Contains(err.Error(), c.refusal):
				t.Errorf("refused, but not with %q: %v", c.refusal, err)
			}
		})
	}

	t.Run("unsharing the default theme clears it", func(t *testing.T) {
		err := try(t, `
			INSERT INTO theme (owner_id, name, shared) VALUES (:admin, 'House', true);
			UPDATE org SET default_theme_id = (SELECT id FROM theme WHERE name = 'House');
			UPDATE theme SET shared = false WHERE name = 'House';
			DO $$ BEGIN
			    IF (SELECT default_theme_id FROM org) IS NOT NULL THEN
			        RAISE EXCEPTION 'the unshared theme is still the default';
			    END IF;
			END $$`)
		if err != nil {
			t.Error(err)
		}
	})

	// A database's history orders an account's foreign keys; made again, the
	// granter is forgotten after the membership went and before the role goes.
	t.Run("an account that granted itself a role can go", func(t *testing.T) {
		for _, fk := range []struct{ table, constraint, column, action string }{
			{"org_member", "org_member_user_id_fkey", "user_id", "CASCADE"},
			{"role_assignment", "role_assignment_created_by_fkey", "created_by", "SET NULL"},
			{"role_assignment", "role_assignment_user_id_fkey", "user_id", "CASCADE"},
		} {
			if _, err := super.Exec(ctx, `ALTER TABLE `+fk.table+` DROP CONSTRAINT `+fk.constraint+
				`, ADD CONSTRAINT `+fk.constraint+` FOREIGN KEY (`+fk.column+`) REFERENCES app_user (id) ON DELETE `+fk.action); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := super.Query(ctx, `
			SELECT c.conname FROM pg_trigger t JOIN pg_constraint c ON c.oid = t.tgconstraint
			WHERE t.tgrelid = 'app_user'::regclass AND t.tgtype & 8 = 8
			  AND c.conname IN ('org_member_user_id_fkey', 'role_assignment_created_by_fkey', 'role_assignment_user_id_fkey')
			ORDER BY t.tgname`)
		if err != nil {
			t.Fatal(err)
		}
		order, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"org_member_user_id_fkey", "role_assignment_created_by_fkey", "role_assignment_user_id_fkey"}; !slices.Equal(order, want) {
			t.Fatalf("the foreign keys act in the order %v, want %v", order, want)
		}

		var self string
		if err := super.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ('self@triggers.test', 'Self') RETURNING id`).Scan(&self); err != nil {
			t.Fatal(err)
		}
		if _, err := super.Exec(ctx, `INSERT INTO org_member (user_id, org_role) VALUES ($1, 'member')`, self); err != nil {
			t.Fatal(err)
		}
		if _, err := super.Exec(ctx, `INSERT INTO role_assignment (role, user_id, created_by) VALUES ('user', $1, $1)`, self); err != nil {
			t.Fatal(err)
		}
		if _, err := super.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, self); err != nil {
			t.Fatalf("deleting an account that granted itself a role: %v", err)
		}
		var left int
		if err := super.QueryRow(ctx, `SELECT count(*) FROM role_assignment WHERE user_id = $1`, self).Scan(&left); err != nil {
			t.Fatal(err)
		}
		if left != 0 {
			t.Errorf("%d of its roles are left", left)
		}
	})
}
