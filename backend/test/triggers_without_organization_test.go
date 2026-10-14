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

// Functions that belong to the organization's own table, which step 5 of #218
// turns into the installation's, and the one that reads the session's setting.
var functionsOfTheOrganizationTable = []string{"current_org_id", "org_builtin_roles", "org_default_theme_guard"}

// A trigger that reads org_id holds a row to an organization nothing can
// cross into, and one that waits on org_id would go when the column does.
func TestNoTriggerAsksForTheOrganization(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ctx := context.Background()

	rows, err := h.super.Query(ctx, `
		SELECT proname FROM pg_proc
		WHERE pronamespace = 'public'::regnamespace AND prosrc ~ 'org_id' AND NOT proname = ANY ($1)
		ORDER BY 1`, functionsOfTheOrganizationTable)
	if err != nil {
		t.Fatal(err)
	}
	asking, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(asking) > 0 {
		t.Errorf("%d functions still read org_id or current_org_id(): %v", len(asking), asking)
	}

	rows, err = h.super.Query(ctx, `
		SELECT format('%s %s', t.tgrelid::regclass, t.tgname)
		FROM pg_trigger t JOIN pg_attribute a ON a.attrelid = t.tgrelid AND a.attnum = ANY (t.tgattr)
		WHERE a.attname = 'org_id' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) > 0 {
		t.Errorf("%d triggers fire on org_id changing: %v", len(waiting), waiting)
	}
}

// Every rule the triggers keep is tried straight through SQL in an installation
// of its own, and a row's org_id, here a second organization's, decides none.
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
	made("first", `INSERT INTO org (slug, name) VALUES ('first', 'First') RETURNING id`)
	if _, err := super.Exec(ctx, `SELECT org_builtin_roles($1)`, ids["first"]); err != nil {
		t.Fatal(err)
	}
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
	// The second comes once the installation is set up: its rows name no
	// organization, and their default refuses to choose between two.
	made("second", `INSERT INTO org (slug, name) VALUES ('second', 'Second') RETURNING id`)
	ids["scheme"] = set.WorkflowSchemeID.String()
	ids["workflow"] = set.WorkflowID.String()
	ids["bug type"] = set.IssueTypeIDs[bootstrap.TypeBug].String()
	first := ids["first"]

	for _, p := range []struct{ name, role string }{
		{"admin", "owner"}, {"member", "member"}, {"customer", "customer"}, {"stranger", ""},
	} {
		made(p.name, `INSERT INTO app_user (email, name) VALUES ($1 || '@triggers.test', $1) RETURNING id`, p.name)
		if p.role != "" {
			if _, err := super.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, $3)`, first, ids[p.name], p.role); err != nil {
				t.Fatal(err)
			}
		}
	}
	const planned = `{sprints,milestones,releases,components,teams,automation}`
	made("alpha", `INSERT INTO project (org_id, key, name, features) VALUES ($1, 'ALPHA', 'Alpha', $2) RETURNING id`, first, planned)
	made("beta", `INSERT INTO project (org_id, key, name, features) VALUES ($1, 'BETA', 'Beta', $2) RETURNING id`, first, planned)
	made("desk", `INSERT INTO project (org_id, key, name, kind) VALUES ($1, 'DESK', 'Desk', 'service') RETURNING id`, first)
	issue := `INSERT INTO issue (org_id, project_id, key_num, issue_type_id, status_id, summary, rank) VALUES ($1, $2, 1, $3, $4, $5, 'a') RETURNING id`
	todo := set.StatusIDs[bootstrap.StatusToDo]
	made("alpha issue", issue, first, ids["alpha"], set.IssueTypeIDs[bootstrap.TypeTask], todo, "Alpha's task")
	made("beta epic", issue, first, ids["beta"], set.IssueTypeIDs[bootstrap.TypeEpic], todo, "Beta's epic")
	made("desk request", issue, first, ids["desk"], set.IssueTypeIDs[bootstrap.TypeTask], todo, "A request")
	for _, project := range []string{"alpha", "beta"} {
		pid := ids[project]
		made(project+" milestone", `INSERT INTO milestone (org_id, project_id, name) VALUES ($1, $2, 'M1') RETURNING id`, first, pid)
		made(project+" version", `INSERT INTO version (org_id, project_id, name) VALUES ($1, $2, '1.0') RETURNING id`, first, pid)
		made(project+" component", `INSERT INTO component (org_id, project_id, name) VALUES ($1, $2, 'Core') RETURNING id`, first, pid)
		made(project+" team", `INSERT INTO team (org_id, project_id, name) VALUES ($1, $2, 'Crew') RETURNING id`, first, pid)
		made(project+" sprint", `INSERT INTO sprint (org_id, project_id, name) VALUES ($1, $2, 'Sprint 1') RETURNING id`, first, pid)
		made(project+" field", `INSERT INTO custom_field (org_id, project_id, name, kind) VALUES ($1, $2, $3, 'text') RETURNING id`, first, pid, project+" notes")
	}
	made("closed milestone", `INSERT INTO milestone (org_id, project_id, name, closed_at) VALUES ($1, $2, 'M0', now()) RETURNING id`, first, ids["alpha"])
	made("archived version", `INSERT INTO version (org_id, project_id, name, released_at, archived_at) VALUES ($1, $2, '0.9', now(), now()) RETURNING id`, first, ids["alpha"])
	made("closed sprint", `INSERT INTO sprint (org_id, project_id, name, state) VALUES ($1, $2, 'Sprint 0', 'closed') RETURNING id`, first, ids["alpha"])
	made("team sprint", `INSERT INTO sprint (org_id, project_id, name, team_id) VALUES ($1, $2, 'Crew sprint', $3) RETURNING id`, first, ids["alpha"], ids["alpha team"])
	made("severity", `INSERT INTO custom_field (org_id, name, kind) VALUES ($1, 'Severity', 'text') RETURNING id`, first)
	made("group", `INSERT INTO user_group (org_id, name) VALUES ($1, 'Support') RETURNING id`, first)
	made("dashboard", `INSERT INTO dashboard (org_id, project_id, name) VALUES ($1, $2, 'Wall') RETURNING id`, first, ids["alpha"])
	made("calendar", `SELECT id FROM holiday_calendar WHERE org_id = $1`, first)
	made("link type", `SELECT id FROM issue_link_type WHERE org_id = $1 AND name = $2`, first, bootstrap.LinkRelates)
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
		{"a board's team is in its own project", `INSERT INTO board (org_id, project_id, name, team_id) VALUES (:first, :alpha, 'Board', :beta_team)`, "that team is not in this project"},
		{"a sprint's team is in its own project", `INSERT INTO sprint (org_id, project_id, name, team_id) VALUES (:first, :alpha, 'Sprint 2', :beta_team)`, "that team is not in this project"},
		{"an issue names a version of its own project", `INSERT INTO issue_version (org_id, issue_id, version_id, role) VALUES (:first, :alpha_issue, :beta_version, 'fix')`, "a version of its own project"},
		{"an archived version is named by no more issues", `INSERT INTO issue_version (org_id, issue_id, version_id, role) VALUES (:first, :alpha_issue, :archived_version, 'fix')`, "that version is archived"},
		{"an issue is in a component of its own project", `INSERT INTO issue_component (org_id, issue_id, component_id) VALUES (:first, :alpha_issue, :beta_component)`, "a component of its own project"},
		{"an issue's field is one of its project", `INSERT INTO issue_field_value (org_id, issue_id, field_id, value) VALUES (:first, :alpha_issue, :beta_field, '"x"')`, "is not a field of the project"},
		{"a parent is in its child's project", `UPDATE issue SET parent_id = :beta_epic WHERE id = :alpha_issue`, "same project as its child"},
		{"an article belongs to a service desk", `INSERT INTO kb_article (org_id, project_id, title) VALUES (:first, :alpha, 'How to')`, "articles belong to a service desk project"},
		{"a rating belongs to a request", `INSERT INTO csat_rating (org_id, issue_id, token_hash) VALUES (:first, :alpha_issue, '\x02')`, "a request of a service desk"},
		{"a project's field does not shadow the installation's", `INSERT INTO custom_field (org_id, project_id, name, kind) VALUES (:first, :alpha, 'severity', 'text')`, "already has a field named"},
		{"a link has two ends", `INSERT INTO issue_link (org_id, link_type_id, source_id, target_id) VALUES (:first, :link_type, :alpha_issue, gen_random_uuid())`, "issue_link_target_id_fkey"},

		// Who may do what.
		{"a customer joins no team", `INSERT INTO team_member (org_id, team_id, user_id) VALUES (:first, :alpha_team, :customer)`, "not a customer, can join a team"},
		{"a stranger joins no team", `INSERT INTO team_member (org_id, team_id, user_id) VALUES (:first, :alpha_team, :stranger)`, "not a customer, can join a team"},
		{"a customer is not away", `INSERT INTO absence (org_id, user_id, starts_on, ends_on) VALUES (:first, :customer, '2026-03-02', '2026-03-02')`, "can be away"},
		{"a customer gives no project a share", `INSERT INTO project_allocation (org_id, project_id, user_id, percent) VALUES (:first, :alpha, :customer, 50)`, "a share of their week"},
		{"a customer has no working week", `INSERT INTO member_schedule (org_id, user_id) VALUES (:first, :customer)`, "has a working week"},
		{"a stranger joins no group", `INSERT INTO group_member (org_id, group_id, user_id) VALUES (:first, :group, :stranger)`, "can join a group"},
		{"a customer shares no filter", `INSERT INTO saved_filter (org_id, owner_id, name, query, shared) VALUES (:first, :customer, 'Mine', 'status = open', true)`, "cannot share a saved filter"},
		{"a customer shares no theme", `INSERT INTO theme (org_id, owner_id, name, shared) VALUES (:first, :customer, 'Dusk', true)`, "cannot share a theme"},
		{"a stranger is granted no role", `INSERT INTO role_assignment (org_id, role, user_id) VALUES (:first, 'user', :stranger)`, "granted to a member here"},
		{"global administration is not granted over one project", `INSERT INTO role_assignment (org_id, role, project_id, user_id) VALUES (:first, 'global_administrator', :alpha, :member)`, "cannot be granted over one project"},
		{"a built-in role stays", `DELETE FROM org_role WHERE key = 'reader'`, "a built-in role cannot be deleted"},
		{"a role keeps its key", `UPDATE org_role SET key = 'viewer' WHERE key = 'reader'`, "key and standing are fixed"},
		{"the global administrator keeps administering", `UPDATE org_role SET permissions = ARRAY['read'] WHERE key = 'global_administrator'`, "keeps administering"},
		{"the last administrator stays switched on", `UPDATE app_user SET is_active = false WHERE id = :admin`, "cannot be switched off"},
		{"the installation keeps a default scheme", `UPDATE workflow_scheme SET is_default = false WHERE id = :scheme`, "one default workflow scheme"},
		{"the default scheme keeps its catch-all", `DELETE FROM workflow_scheme_item WHERE scheme_id = :scheme AND issue_type_id IS NULL`, "map the issue types it does not name"},
		{"a session keeps its proof", `UPDATE user_session SET proof = 'oidc' WHERE id = :session`, "keeps the proof it was opened with"},

		// A row that names the second organization is held to the same rules
		// as any other, and refused or let through by them alone.
		{"an issue's milestone, whatever org_id says", `UPDATE issue SET org_id = :second, milestone_id = :alpha_milestone WHERE id = :alpha_issue`, ""},
		{"an issue's sprint, whatever org_id says", `UPDATE issue SET org_id = :second, sprint_id = :alpha_sprint WHERE id = :alpha_issue`, ""},
		{"an issue's team, whatever org_id says", `UPDATE issue SET org_id = :second, team_id = :alpha_team WHERE id = :alpha_issue`, ""},
		{"an issue's version, whatever org_id says", `INSERT INTO issue_version (org_id, issue_id, version_id, role) VALUES (:second, :alpha_issue, :alpha_version, 'fix')`, ""},
		{"an issue's component, whatever org_id says", `INSERT INTO issue_component (org_id, issue_id, component_id) VALUES (:second, :alpha_issue, :alpha_component)`, ""},
		{"a field for every project, whatever org_id says", `INSERT INTO custom_field (org_id, name, kind) VALUES (:second, 'Stray', 'text'); INSERT INTO issue_field_value (org_id, issue_id, field_id, value) SELECT :first, :alpha_issue, id, '"x"' FROM custom_field WHERE name = 'Stray'`, ""},
		{"a field does not shadow, whatever org_id says", `INSERT INTO custom_field (org_id, name, kind) VALUES (:second, 'Stray', 'text'); INSERT INTO custom_field (org_id, project_id, name, kind) VALUES (:first, :alpha, 'stray', 'text')`, "already has a field named"},
		{"an article, whatever org_id says", `INSERT INTO kb_article (org_id, project_id, title) VALUES (:second, :desk, 'How to')`, ""},
		{"a rating, whatever org_id says", `INSERT INTO csat_rating (org_id, issue_id, token_hash) VALUES (:second, :desk_request, '\x03')`, ""},
		{"a team member, whatever org_id says", `INSERT INTO team_member (org_id, team_id, user_id) VALUES (:second, :alpha_team, :member)`, ""},
		{"an absence, whatever org_id says", `INSERT INTO absence (org_id, user_id, starts_on, ends_on) VALUES (:second, :member, '2026-03-02', '2026-03-02')`, ""},
		{"a share of a week, whatever org_id says", `INSERT INTO project_allocation (org_id, project_id, user_id, percent) VALUES (:second, :alpha, :member, 50)`, ""},
		{"a working week, whatever org_id says", `INSERT INTO member_schedule (org_id, user_id, calendar_id) VALUES (:second, :member, :calendar)`, ""},
		{"a group member, whatever org_id says", `INSERT INTO group_member (org_id, group_id, user_id) VALUES (:second, :group, :member)`, ""},
		{"a customer shares no filter, whatever org_id says", `INSERT INTO saved_filter (org_id, owner_id, name, query, shared) VALUES (:second, :customer, 'Mine', 'status = open', true)`, "cannot share a saved filter"},
		{"a customer shares no theme, whatever org_id says", `INSERT INTO theme (org_id, owner_id, name, shared) VALUES (:second, :customer, 'Dusk', true)`, "cannot share a theme"},
		{"a role, whatever org_id says", `INSERT INTO role_assignment (org_id, role, user_id) VALUES (:second, 'user', :member)`, ""},
		{"a role over a project, whatever org_id says", `INSERT INTO role_assignment (org_id, role, project_id, user_id) VALUES (:second, 'user', :alpha, :member)`, ""},
		{"a group's role, whatever org_id says", `INSERT INTO role_assignment (org_id, role, group_id) VALUES (:second, 'reader', :group)`, ""},
		{"another administrator, whatever org_id says", `INSERT INTO app_user (email, name) VALUES ('deputy@triggers.test', 'Deputy'); INSERT INTO org_member (org_id, user_id, org_role) SELECT :second, id, 'admin' FROM app_user WHERE email = 'deputy@triggers.test'; UPDATE app_user SET is_active = false WHERE id = :admin`, ""},
		{"another scheme, whatever org_id says", `INSERT INTO workflow_scheme (org_id, name) VALUES (:second, 'Stray scheme')`, ""},

		// These were held to the organization alone.
		{"a rule watches a project", `INSERT INTO automation_rule (org_id, project_id, name, trigger, actions) VALUES (:second, :alpha, 'Nightly', '{"kind": "manual"}', '[{"kind": "comment"}]')`, ""},
		{"a link opens a dashboard", `INSERT INTO dashboard_share (org_id, dashboard_id, name, token_hash) VALUES (:second, :dashboard, 'Wall', '\x04')`, ""},
		{"a holiday is in a calendar", `INSERT INTO holiday (org_id, calendar_id, day, name) VALUES (:second, :calendar, '2026-12-25', 'Christmas')`, ""},
		{"an issue is linked", `INSERT INTO issue_link (org_id, link_type_id, source_id, target_id) VALUES (:second, :link_type, :alpha_issue, :beta_epic)`, ""},
		{"an issue keeps an old key", `INSERT INTO issue_old_key (org_id, issue_id, key) VALUES (:second, :alpha_issue, 'OLD-1')`, ""},
		{"an issue links a page", `INSERT INTO issue_remote_link (org_id, issue_id, url, title, source) VALUES (:second, :alpha_issue, 'https://docs.example.test/page', 'Page', 'docs')`, ""},
		{"a notification is about an issue", `INSERT INTO notification (org_id, user_id, issue_id, kind, title, event_id) VALUES (:second, :member, :alpha_issue, 'assigned', 'Yours', gen_random_uuid())`, ""},
		{"a project takes a scheme", `INSERT INTO workflow_scheme (org_id, name) VALUES (:second, 'Borrowed'); UPDATE project SET workflow_scheme_id = (SELECT id FROM workflow_scheme WHERE name = 'Borrowed') WHERE id = :alpha`, ""},
		{"a scheme maps a workflow", `INSERT INTO workflow_scheme_item (org_id, scheme_id, issue_type_id, workflow_id) VALUES (:second, :scheme, :bug_type, :workflow)`, ""},
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
			INSERT INTO theme (org_id, owner_id, name, shared) VALUES (:first, :admin, 'House', true);
			UPDATE org SET default_theme_id = (SELECT id FROM theme WHERE name = 'House') WHERE id = :first;
			UPDATE theme SET shared = false WHERE name = 'House';
			DO $$ BEGIN
			    IF (SELECT default_theme_id FROM org WHERE slug = 'first') IS NOT NULL THEN
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
		if _, err := super.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'member')`, first, self); err != nil {
			t.Fatal(err)
		}
		if _, err := super.Exec(ctx, `INSERT INTO role_assignment (org_id, role, user_id, created_by) VALUES ($1, 'user', $2, $2)`, first, self); err != nil {
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
