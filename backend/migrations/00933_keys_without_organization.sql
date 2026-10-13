-- +goose Up
-- An installation holds one organization, so a name or key unique in it is
-- unique in the installation, and saying so needs no org_id (#218). Each key
-- and index is built again without it, the new one first and the old dropped
-- after. Project keys, and so issue keys, are the installation's.
--
-- Rows that would share a key once it is the installation's stop the upgrade
-- by name. With one organization there are none: (org_id, x) unique is then
-- x unique.
-- +goose StatementBegin
DO $$
DECLARE
    listed CONSTANT int := 10;
    check_sql text;
    found text[];
    collisions text[] := '{}';
    total int;
    names text;
BEGIN
    FOREACH check_sql IN ARRAY ARRAY[
        $q$SELECT format('project key %s', key) FROM project GROUP BY key HAVING count(*) > 1$q$,
        $q$SELECT format('old issue key %s', key) FROM issue_old_key GROUP BY key HAVING count(*) > 1$q$,
        $q$SELECT format('member %s', u.email) FROM org_member m JOIN app_user u ON u.id = m.user_id GROUP BY u.id HAVING count(*) > 1$q$,
        $q$SELECT format('request to join from %s', u.email) FROM org_join_request r JOIN app_user u ON u.id = r.user_id GROUP BY u.id HAVING count(*) > 1$q$,
        $q$SELECT format('working schedule of %s', u.email) FROM member_schedule s JOIN app_user u ON u.id = s.user_id GROUP BY u.id HAVING count(*) > 1$q$,
        $q$SELECT format('last digest of %s', u.email) FROM notification_digest_sent d JOIN app_user u ON u.id = d.user_id GROUP BY u.id HAVING count(*) > 1$q$,
        $q$SELECT format('notification settings of %s', u.email) FROM notification_preference p JOIN app_user u ON u.id = p.user_id GROUP BY u.id HAVING count(*) > 1$q$,
        $q$SELECT format('theme choice of %s', u.email) FROM user_theme c JOIN app_user u ON u.id = c.user_id GROUP BY u.id HAVING count(*) > 1$q$,
        $q$SELECT format('portal code for %s', email) FROM portal_code GROUP BY email HAVING count(*) > 1$q$,
        $q$SELECT format('role %s', key) FROM org_role GROUP BY key HAVING count(*) > 1$q$,
        $q$SELECT format('role named %s', min(name)) FROM org_role GROUP BY lower(name) HAVING count(*) > 1$q$,
        $q$SELECT format('%s identity providers', count(*)) FROM oidc_provider HAVING count(*) > 1$q$,
        $q$SELECT DISTINCT format('overlapping absences of %s', u.email) FROM absence a
             JOIN absence b ON b.user_id = a.user_id AND b.id > a.id
              AND daterange(b.starts_on, b.ends_on, '[]') && daterange(a.starts_on, a.ends_on, '[]')
             JOIN app_user u ON u.id = a.user_id$q$,
        $q$SELECT format('automation rule %s', min(name)) FROM automation_rule GROUP BY project_id, lower(name) HAVING count(*) > 1$q$,
        $q$SELECT format('custom field %s', min(name)) FROM custom_field WHERE project_id IS NULL GROUP BY lower(name) HAVING count(*) > 1$q$,
        $q$SELECT format('dashboard template %s', min(name)) FROM dashboard_template GROUP BY lower(name) HAVING count(*) > 1$q$,
        $q$SELECT format('holiday calendar %s', min(name)) FROM holiday_calendar GROUP BY lower(name) HAVING count(*) > 1$q$,
        $q$SELECT format('%s default holiday calendars', count(*)) FROM holiday_calendar WHERE is_default HAVING count(*) > 1$q$,
        $q$SELECT format('issue arrangement for %s, %s',
                     coalesce((SELECT key FROM project WHERE id = project_id), 'every project'),
                     coalesce((SELECT name FROM issue_type WHERE id = issue_type_id), 'every type'))
           FROM issue_arrangement GROUP BY project_id, issue_type_id HAVING count(*) > 1$q$,
        $q$SELECT format('imported comment %s', external_key) FROM issue_comment WHERE external_key IS NOT NULL GROUP BY external_key HAVING count(*) > 1$q$,
        $q$SELECT format('link type %s', min(name)) FROM issue_link_type GROUP BY lower(name) HAVING count(*) > 1$q$,
        $q$SELECT format('status %s', min(name)) FROM issue_status GROUP BY lower(name) HAVING count(*) > 1$q$,
        $q$SELECT format('issue type %s', min(name)) FROM issue_type GROUP BY lower(name) HAVING count(*) > 1$q$,
        $q$SELECT format('imported work log %s', external_key) FROM issue_worklog WHERE external_key IS NOT NULL GROUP BY external_key HAVING count(*) > 1$q$,
        $q$SELECT format('label %s', min(name)) FROM label GROUP BY lower(name) HAVING count(*) > 1$q$,
        $q$SELECT format('open invitation for %s', email) FROM org_invite WHERE accepted_at IS NULL GROUP BY email HAVING count(*) > 1$q$,
        $q$SELECT format('role %s given to %s', role, coalesce(user_id, group_id)) FROM role_assignment
           GROUP BY role, project_id, user_id, group_id HAVING count(*) > 1$q$,
        $q$SELECT format('theme %s of %s', min(t.name), u.email) FROM theme t JOIN app_user u ON u.id = t.owner_id
           GROUP BY u.id, lower(t.name) HAVING count(*) > 1$q$,
        $q$SELECT format('group %s', min(name)) FROM user_group GROUP BY lower(name) HAVING count(*) > 1$q$,
        $q$SELECT format('group reference %s', external_ref) FROM user_group WHERE external_ref IS NOT NULL GROUP BY external_ref HAVING count(*) > 1$q$,
        $q$SELECT format('webhook %s', min(name)) FROM webhook_endpoint GROUP BY lower(name) HAVING count(*) > 1$q$,
        $q$SELECT format('workflow %s', min(name)) FROM workflow GROUP BY lower(name) HAVING count(*) > 1$q$,
        $q$SELECT format('%s default workflow schemes', count(*)) FROM workflow_scheme WHERE is_default HAVING count(*) > 1$q$,
        $q$SELECT format('workflow scheme %s', min(name)) FROM workflow_scheme GROUP BY lower(name) HAVING count(*) > 1$q$
    ]
    LOOP
        EXECUTE format('SELECT array_agg(item ORDER BY item) FROM (%s) found(item)', check_sql) INTO found;
        collisions := collisions || coalesce(found, '{}');
    END LOOP;

    total := cardinality(collisions);
    IF total = 0 THEN
        RETURN;
    END IF;
    names := array_to_string(collisions[1:listed], ', ');
    IF total > listed THEN
        names := names || format(' and %s more', total - listed);
    END IF;
    RAISE EXCEPTION 'Names and keys that were unique in each organization are unique in the whole installation from this release, and this database has % that are not: %. Rename or delete all but one of each, then upgrade again.', total, names
        USING ERRCODE = 'unique_violation';
END;
$$;
-- +goose StatementEnd

-- The primary keys. oidc_provider had nothing but the organization to key it
-- by, so a column that can only be true keeps it to one row.
ALTER TABLE oidc_provider ADD COLUMN one_row boolean NOT NULL DEFAULT true CONSTRAINT oidc_provider_one_row CHECK (one_row);

CREATE UNIQUE INDEX issue_old_key_pkey_new ON issue_old_key (key);
CREATE UNIQUE INDEX member_schedule_pkey_new ON member_schedule (user_id);
CREATE UNIQUE INDEX notification_digest_sent_pkey_new ON notification_digest_sent (user_id);
CREATE UNIQUE INDEX notification_preference_pkey_new ON notification_preference (user_id);
CREATE UNIQUE INDEX oidc_provider_pkey_new ON oidc_provider (one_row);
CREATE UNIQUE INDEX org_join_request_pkey_new ON org_join_request (user_id);
CREATE UNIQUE INDEX org_member_pkey_new ON org_member (user_id);
CREATE UNIQUE INDEX org_role_pkey_new ON org_role (key);
CREATE UNIQUE INDEX portal_code_pkey_new ON portal_code (email);
CREATE UNIQUE INDEX user_theme_pkey_new ON user_theme (user_id);

ALTER TABLE role_assignment DROP CONSTRAINT role_assignment_role_fkey;
ALTER TABLE issue_old_key DROP CONSTRAINT issue_old_key_pkey, ADD CONSTRAINT issue_old_key_pkey PRIMARY KEY USING INDEX issue_old_key_pkey_new;
ALTER TABLE member_schedule DROP CONSTRAINT member_schedule_pkey, ADD CONSTRAINT member_schedule_pkey PRIMARY KEY USING INDEX member_schedule_pkey_new;
ALTER TABLE notification_digest_sent DROP CONSTRAINT notification_digest_sent_pkey, ADD CONSTRAINT notification_digest_sent_pkey PRIMARY KEY USING INDEX notification_digest_sent_pkey_new;
ALTER TABLE notification_preference DROP CONSTRAINT notification_preference_pkey, ADD CONSTRAINT notification_preference_pkey PRIMARY KEY USING INDEX notification_preference_pkey_new;
ALTER TABLE oidc_provider DROP CONSTRAINT oidc_provider_pkey, ADD CONSTRAINT oidc_provider_pkey PRIMARY KEY USING INDEX oidc_provider_pkey_new;
ALTER TABLE org_join_request DROP CONSTRAINT org_join_request_pkey, ADD CONSTRAINT org_join_request_pkey PRIMARY KEY USING INDEX org_join_request_pkey_new;
ALTER TABLE org_member DROP CONSTRAINT org_member_pkey, ADD CONSTRAINT org_member_pkey PRIMARY KEY USING INDEX org_member_pkey_new;
ALTER TABLE org_role DROP CONSTRAINT org_role_pkey, ADD CONSTRAINT org_role_pkey PRIMARY KEY USING INDEX org_role_pkey_new;
ALTER TABLE portal_code DROP CONSTRAINT portal_code_pkey, ADD CONSTRAINT portal_code_pkey PRIMARY KEY USING INDEX portal_code_pkey_new;
ALTER TABLE user_theme DROP CONSTRAINT user_theme_pkey, ADD CONSTRAINT user_theme_pkey PRIMARY KEY USING INDEX user_theme_pkey_new;
ALTER TABLE role_assignment ADD CONSTRAINT role_assignment_role_fkey FOREIGN KEY (role) REFERENCES org_role (key) ON DELETE CASCADE;

ALTER TABLE absence ADD CONSTRAINT absence_no_overlap_new
    EXCLUDE USING gist (user_id WITH =, daterange(starts_on, ends_on, '[]') WITH &&);
ALTER TABLE absence DROP CONSTRAINT absence_no_overlap;
ALTER TABLE absence RENAME CONSTRAINT absence_no_overlap_new TO absence_no_overlap;

-- The unique indexes. Those named for the organization lose the word; the
-- rest keep their names, which the code reads in a refusal.
CREATE UNIQUE INDEX automation_rule_name_idx_new ON automation_rule (project_id, lower(name)) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX custom_field_installation_name_idx ON custom_field (lower(name)) WHERE project_id IS NULL;
CREATE UNIQUE INDEX dashboard_template_name_idx ON dashboard_template (lower(name));
CREATE UNIQUE INDEX holiday_calendar_name_idx_new ON holiday_calendar (lower(name));
CREATE UNIQUE INDEX holiday_calendar_one_default_idx_new ON holiday_calendar (is_default) WHERE is_default;
CREATE UNIQUE INDEX issue_arrangement_scope_idx_new ON issue_arrangement (project_id, issue_type_id) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX issue_comment_external_key_idx_new ON issue_comment (external_key);
CREATE UNIQUE INDEX issue_link_type_name_idx ON issue_link_type (lower(name));
CREATE UNIQUE INDEX issue_status_name_idx ON issue_status (lower(name));
CREATE UNIQUE INDEX issue_type_name_idx ON issue_type (lower(name));
CREATE UNIQUE INDEX issue_worklog_external_key_idx_new ON issue_worklog (external_key);
CREATE UNIQUE INDEX label_name_idx ON label (lower(name));
CREATE UNIQUE INDEX org_invite_pending_idx_new ON org_invite (email) WHERE accepted_at IS NULL;
CREATE UNIQUE INDEX org_role_name_idx_new ON org_role (lower(name));
CREATE UNIQUE INDEX project_key_idx ON project (key);
CREATE UNIQUE INDEX role_assignment_unique_idx_new ON role_assignment (role, project_id, user_id, group_id) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX theme_owner_name_idx_new ON theme (owner_id, lower(name));
CREATE UNIQUE INDEX user_group_name_idx ON user_group (lower(name));
CREATE UNIQUE INDEX user_group_external_ref_idx ON user_group (external_ref) WHERE external_ref IS NOT NULL;
CREATE UNIQUE INDEX webhook_endpoint_name_idx_new ON webhook_endpoint (lower(name));
CREATE UNIQUE INDEX workflow_name_idx ON workflow (lower(name));
CREATE UNIQUE INDEX workflow_scheme_one_default_idx_new ON workflow_scheme (is_default) WHERE is_default;
CREATE UNIQUE INDEX workflow_scheme_name_idx ON workflow_scheme (lower(name));

-- The plain indexes. Two are not built again: issue_project_key_idx already
-- leads with project_id, and issue_link_unique_idx with link type and source.
CREATE INDEX absence_days_idx_new ON absence (ends_on, starts_on);
CREATE INDEX api_token_user_idx ON api_token (user_id);
CREATE INDEX audit_log_action_idx ON audit_log (action, created_at DESC);
CREATE INDEX audit_log_actor_idx ON audit_log (actor_user_id, created_at DESC);
CREATE INDEX audit_log_created_idx ON audit_log (created_at DESC);
CREATE INDEX audit_log_target_idx_new ON audit_log (target_type, target_id);
CREATE INDEX automation_rule_enabled_idx ON automation_rule ((trigger->>'kind')) WHERE enabled;
CREATE INDEX group_member_user_idx_new ON group_member (user_id);
CREATE INDEX issue_assignee_idx_new ON issue (assignee_id) WHERE assignee_id IS NOT NULL;
CREATE INDEX issue_rank_idx_new ON issue (project_id, rank);
CREATE INDEX issue_status_idx_new ON issue (status_id);
CREATE INDEX issue_updated_idx_new ON issue (updated_at DESC);
CREATE INDEX issue_type_hierarchy_idx_new ON issue_type (hierarchy_level, position);
CREATE INDEX issue_watcher_user_idx_new ON issue_watcher (user_id);
CREATE INDEX project_active_idx ON project (key) WHERE archived_at IS NULL;
CREATE INDEX role_assignment_group_idx_new ON role_assignment (group_id) WHERE group_id IS NOT NULL;
CREATE INDEX role_assignment_user_idx_new ON role_assignment (user_id) WHERE user_id IS NOT NULL;
CREATE INDEX saved_filter_shared_idx ON saved_filter (shared);
CREATE INDEX team_member_user_idx_new ON team_member (user_id);
CREATE INDEX theme_shared_idx ON theme (shared);

DROP INDEX automation_rule_name_idx, custom_field_org_name_idx, dashboard_template_org_name_idx,
    holiday_calendar_name_idx, holiday_calendar_one_default_idx, issue_arrangement_scope_idx,
    issue_comment_external_key_idx, issue_link_type_org_name_idx, issue_status_org_name_idx,
    issue_type_org_name_idx, issue_worklog_external_key_idx, label_org_name_idx, org_invite_pending_idx,
    org_role_name_idx, project_org_key_idx, role_assignment_unique_idx, theme_owner_name_idx,
    user_group_org_name_idx, user_group_org_ref_idx, webhook_endpoint_name_idx, workflow_org_name_idx,
    workflow_scheme_one_default_idx, workflow_scheme_org_name_idx,
    absence_days_idx, api_token_org_user_idx, audit_log_org_action_idx, audit_log_org_actor_idx,
    audit_log_org_created_idx, audit_log_target_idx, automation_rule_org_idx, group_member_user_idx,
    issue_assignee_idx, issue_org_project_idx, issue_rank_idx, issue_status_idx, issue_updated_idx,
    issue_link_type_source_idx, issue_type_hierarchy_idx, issue_watcher_user_idx, project_org_active_idx,
    role_assignment_group_idx, role_assignment_user_idx, saved_filter_org_idx, team_member_user_idx,
    theme_org_idx;

ALTER INDEX automation_rule_name_idx_new RENAME TO automation_rule_name_idx;
ALTER INDEX holiday_calendar_name_idx_new RENAME TO holiday_calendar_name_idx;
ALTER INDEX holiday_calendar_one_default_idx_new RENAME TO holiday_calendar_one_default_idx;
ALTER INDEX issue_arrangement_scope_idx_new RENAME TO issue_arrangement_scope_idx;
ALTER INDEX issue_comment_external_key_idx_new RENAME TO issue_comment_external_key_idx;
ALTER INDEX issue_worklog_external_key_idx_new RENAME TO issue_worklog_external_key_idx;
ALTER INDEX org_invite_pending_idx_new RENAME TO org_invite_pending_idx;
ALTER INDEX org_role_name_idx_new RENAME TO org_role_name_idx;
ALTER INDEX role_assignment_unique_idx_new RENAME TO role_assignment_unique_idx;
ALTER INDEX theme_owner_name_idx_new RENAME TO theme_owner_name_idx;
ALTER INDEX webhook_endpoint_name_idx_new RENAME TO webhook_endpoint_name_idx;
ALTER INDEX workflow_scheme_one_default_idx_new RENAME TO workflow_scheme_one_default_idx;
ALTER INDEX absence_days_idx_new RENAME TO absence_days_idx;
ALTER INDEX audit_log_target_idx_new RENAME TO audit_log_target_idx;
ALTER INDEX group_member_user_idx_new RENAME TO group_member_user_idx;
ALTER INDEX issue_assignee_idx_new RENAME TO issue_assignee_idx;
ALTER INDEX issue_rank_idx_new RENAME TO issue_rank_idx;
ALTER INDEX issue_status_idx_new RENAME TO issue_status_idx;
ALTER INDEX issue_updated_idx_new RENAME TO issue_updated_idx;
ALTER INDEX issue_type_hierarchy_idx_new RENAME TO issue_type_hierarchy_idx;
ALTER INDEX issue_watcher_user_idx_new RENAME TO issue_watcher_user_idx;
ALTER INDEX role_assignment_group_idx_new RENAME TO role_assignment_group_idx;
ALTER INDEX role_assignment_user_idx_new RENAME TO role_assignment_user_idx;
ALTER INDEX team_member_user_idx_new RENAME TO team_member_user_idx;

-- A new organization's roles are inferred against the key it now has. The
-- rest of this function is the organization's, and goes with it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION org_builtin_roles(target uuid) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO org_role (org_id, key, name, description, org_wide_only, builtin, permissions, sort_order) VALUES
        (target, 'global_administrator', 'Global administrator',
         'Administers the tenant: its members, its roles, its workflows and every project in it.',
         true, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write', 'sprint.manage', 'team.manage',
                           'board.configure', 'project.administer', 'project.create', 'org.administer'], 10),
        (target, 'project_administrator', 'Project administrator',
         'Configures one project: its boards, its workflow scheme, its teams, and whether it is archived.',
         false, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write', 'sprint.manage', 'team.manage',
                            'board.configure', 'project.administer'], 20),
        (target, 'scrum_master', 'Scrum master',
         'Runs the work in one project: sprints, teams and the planning that goes with them.',
         false, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write', 'sprint.manage', 'team.manage'], 30),
        (target, 'user', 'User',
         'Does the ordinary work: files issues, moves them, comments.',
         false, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write'], 40),
        (target, 'reader', 'Reader',
         'Sees a project and changes nothing in it.',
         false, true, ARRAY['read'], 50)
    ON CONFLICT (key) DO NOTHING;
END;
$$;
-- +goose StatementEnd

-- A key names one request in the installation, so no mail is ambiguous. One
-- recorded as such reached no request, which is what unmatched says.
UPDATE inbound_mail SET outcome = 'unmatched' WHERE outcome = 'ambiguous';
ALTER TABLE inbound_mail DROP CONSTRAINT inbound_mail_outcome_check;
ALTER TABLE inbound_mail ADD CONSTRAINT inbound_mail_outcome_check
    CHECK (outcome IN ('processing', 'failed', 'commented', 'refused', 'unmatched', 'empty', 'automatic'));

-- +goose Down
-- Each key and index takes the organization into account again, as the
-- migrations before this one made them. A mail it called unmatched stays so.
ALTER TABLE inbound_mail DROP CONSTRAINT inbound_mail_outcome_check;
ALTER TABLE inbound_mail ADD CONSTRAINT inbound_mail_outcome_check
    CHECK (outcome IN ('processing', 'failed', 'commented', 'refused', 'unmatched', 'ambiguous', 'empty', 'automatic'));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION org_builtin_roles(target uuid) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO org_role (org_id, key, name, description, org_wide_only, builtin, permissions, sort_order) VALUES
        (target, 'global_administrator', 'Global administrator',
         'Administers the tenant: its members, its roles, its workflows and every project in it.',
         true, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write', 'sprint.manage', 'team.manage',
                           'board.configure', 'project.administer', 'project.create', 'org.administer'], 10),
        (target, 'project_administrator', 'Project administrator',
         'Configures one project: its boards, its workflow scheme, its teams, and whether it is archived.',
         false, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write', 'sprint.manage', 'team.manage',
                            'board.configure', 'project.administer'], 20),
        (target, 'scrum_master', 'Scrum master',
         'Runs the work in one project: sprints, teams and the planning that goes with them.',
         false, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write', 'sprint.manage', 'team.manage'], 30),
        (target, 'user', 'User',
         'Does the ordinary work: files issues, moves them, comments.',
         false, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write'], 40),
        (target, 'reader', 'Reader',
         'Sees a project and changes nothing in it.',
         false, true, ARRAY['read'], 50)
    ON CONFLICT (org_id, key) DO NOTHING;
END;
$$;
-- +goose StatementEnd

CREATE INDEX absence_days_idx_old ON absence (org_id, ends_on, starts_on);
CREATE INDEX api_token_org_user_idx ON api_token (org_id, user_id);
CREATE INDEX audit_log_org_action_idx ON audit_log (org_id, action, created_at DESC);
CREATE INDEX audit_log_org_actor_idx ON audit_log (org_id, actor_user_id, created_at DESC);
CREATE INDEX audit_log_org_created_idx ON audit_log (org_id, created_at DESC);
CREATE INDEX audit_log_target_idx_old ON audit_log (org_id, target_type, target_id);
CREATE INDEX automation_rule_org_idx ON automation_rule (org_id) WHERE enabled;
CREATE INDEX group_member_user_idx_old ON group_member (org_id, user_id);
CREATE INDEX issue_assignee_idx_old ON issue (org_id, assignee_id) WHERE assignee_id IS NOT NULL;
CREATE INDEX issue_org_project_idx ON issue (org_id, project_id);
CREATE INDEX issue_rank_idx_old ON issue (org_id, project_id, rank);
CREATE INDEX issue_status_idx_old ON issue (org_id, status_id);
CREATE INDEX issue_updated_idx_old ON issue (org_id, updated_at DESC);
CREATE INDEX issue_link_type_source_idx ON issue_link (org_id, link_type_id, source_id);
CREATE INDEX issue_type_hierarchy_idx_old ON issue_type (org_id, hierarchy_level, position);
CREATE INDEX issue_watcher_user_idx_old ON issue_watcher (org_id, user_id);
CREATE INDEX project_org_active_idx ON project (org_id) WHERE archived_at IS NULL;
CREATE INDEX role_assignment_group_idx_old ON role_assignment (org_id, group_id) WHERE group_id IS NOT NULL;
CREATE INDEX role_assignment_user_idx_old ON role_assignment (org_id, user_id) WHERE user_id IS NOT NULL;
CREATE INDEX saved_filter_org_idx ON saved_filter (org_id, shared);
CREATE INDEX team_member_user_idx_old ON team_member (org_id, user_id);
CREATE INDEX theme_org_idx ON theme (org_id, shared);

CREATE UNIQUE INDEX automation_rule_name_idx_old ON automation_rule (org_id, project_id, lower(name)) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX custom_field_org_name_idx ON custom_field (org_id, lower(name)) WHERE project_id IS NULL;
CREATE UNIQUE INDEX dashboard_template_org_name_idx ON dashboard_template (org_id, lower(name));
CREATE UNIQUE INDEX holiday_calendar_name_idx_old ON holiday_calendar (org_id, lower(name));
CREATE UNIQUE INDEX holiday_calendar_one_default_idx_old ON holiday_calendar (org_id) WHERE is_default;
CREATE UNIQUE INDEX issue_arrangement_scope_idx_old ON issue_arrangement (org_id, project_id, issue_type_id) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX issue_comment_external_key_idx_old ON issue_comment (org_id, external_key);
CREATE UNIQUE INDEX issue_link_type_org_name_idx ON issue_link_type (org_id, lower(name));
CREATE UNIQUE INDEX issue_status_org_name_idx ON issue_status (org_id, lower(name));
CREATE UNIQUE INDEX issue_type_org_name_idx ON issue_type (org_id, lower(name));
CREATE UNIQUE INDEX issue_worklog_external_key_idx_old ON issue_worklog (org_id, external_key);
CREATE UNIQUE INDEX label_org_name_idx ON label (org_id, lower(name));
CREATE UNIQUE INDEX org_invite_pending_idx_old ON org_invite (org_id, email) WHERE accepted_at IS NULL;
CREATE UNIQUE INDEX org_role_name_idx_old ON org_role (org_id, lower(name));
CREATE UNIQUE INDEX project_org_key_idx ON project (org_id, key);
CREATE UNIQUE INDEX role_assignment_unique_idx_old ON role_assignment (org_id, role, project_id, user_id, group_id) NULLS NOT DISTINCT;
CREATE UNIQUE INDEX theme_owner_name_idx_old ON theme (org_id, owner_id, lower(name));
CREATE UNIQUE INDEX user_group_org_name_idx ON user_group (org_id, lower(name));
CREATE UNIQUE INDEX user_group_org_ref_idx ON user_group (org_id, external_ref) WHERE external_ref IS NOT NULL;
CREATE UNIQUE INDEX webhook_endpoint_name_idx_old ON webhook_endpoint (org_id, lower(name));
CREATE UNIQUE INDEX workflow_org_name_idx ON workflow (org_id, lower(name));
CREATE UNIQUE INDEX workflow_scheme_one_default_idx_old ON workflow_scheme (org_id) WHERE is_default;
CREATE UNIQUE INDEX workflow_scheme_org_name_idx ON workflow_scheme (org_id, lower(name));

DROP INDEX absence_days_idx, api_token_user_idx, audit_log_action_idx, audit_log_actor_idx,
    audit_log_created_idx, audit_log_target_idx, automation_rule_enabled_idx, group_member_user_idx,
    issue_assignee_idx, issue_rank_idx, issue_status_idx, issue_updated_idx, issue_type_hierarchy_idx,
    issue_watcher_user_idx, project_active_idx, role_assignment_group_idx, role_assignment_user_idx,
    saved_filter_shared_idx, team_member_user_idx, theme_shared_idx,
    automation_rule_name_idx, custom_field_installation_name_idx, dashboard_template_name_idx,
    holiday_calendar_name_idx, holiday_calendar_one_default_idx, issue_arrangement_scope_idx,
    issue_comment_external_key_idx, issue_link_type_name_idx, issue_status_name_idx, issue_type_name_idx,
    issue_worklog_external_key_idx, label_name_idx, org_invite_pending_idx, org_role_name_idx,
    project_key_idx, role_assignment_unique_idx, theme_owner_name_idx, user_group_name_idx,
    user_group_external_ref_idx, webhook_endpoint_name_idx, workflow_name_idx,
    workflow_scheme_one_default_idx, workflow_scheme_name_idx;

ALTER INDEX absence_days_idx_old RENAME TO absence_days_idx;
ALTER INDEX audit_log_target_idx_old RENAME TO audit_log_target_idx;
ALTER INDEX group_member_user_idx_old RENAME TO group_member_user_idx;
ALTER INDEX issue_assignee_idx_old RENAME TO issue_assignee_idx;
ALTER INDEX issue_rank_idx_old RENAME TO issue_rank_idx;
ALTER INDEX issue_status_idx_old RENAME TO issue_status_idx;
ALTER INDEX issue_updated_idx_old RENAME TO issue_updated_idx;
ALTER INDEX issue_type_hierarchy_idx_old RENAME TO issue_type_hierarchy_idx;
ALTER INDEX issue_watcher_user_idx_old RENAME TO issue_watcher_user_idx;
ALTER INDEX role_assignment_group_idx_old RENAME TO role_assignment_group_idx;
ALTER INDEX role_assignment_user_idx_old RENAME TO role_assignment_user_idx;
ALTER INDEX team_member_user_idx_old RENAME TO team_member_user_idx;
ALTER INDEX automation_rule_name_idx_old RENAME TO automation_rule_name_idx;
ALTER INDEX holiday_calendar_name_idx_old RENAME TO holiday_calendar_name_idx;
ALTER INDEX holiday_calendar_one_default_idx_old RENAME TO holiday_calendar_one_default_idx;
ALTER INDEX issue_arrangement_scope_idx_old RENAME TO issue_arrangement_scope_idx;
ALTER INDEX issue_comment_external_key_idx_old RENAME TO issue_comment_external_key_idx;
ALTER INDEX issue_worklog_external_key_idx_old RENAME TO issue_worklog_external_key_idx;
ALTER INDEX org_invite_pending_idx_old RENAME TO org_invite_pending_idx;
ALTER INDEX org_role_name_idx_old RENAME TO org_role_name_idx;
ALTER INDEX role_assignment_unique_idx_old RENAME TO role_assignment_unique_idx;
ALTER INDEX theme_owner_name_idx_old RENAME TO theme_owner_name_idx;
ALTER INDEX webhook_endpoint_name_idx_old RENAME TO webhook_endpoint_name_idx;
ALTER INDEX workflow_scheme_one_default_idx_old RENAME TO workflow_scheme_one_default_idx;

ALTER TABLE absence ADD CONSTRAINT absence_no_overlap_old
    EXCLUDE USING gist (org_id WITH =, user_id WITH =, daterange(starts_on, ends_on, '[]') WITH &&);
ALTER TABLE absence DROP CONSTRAINT absence_no_overlap;
ALTER TABLE absence RENAME CONSTRAINT absence_no_overlap_old TO absence_no_overlap;

CREATE UNIQUE INDEX issue_old_key_pkey_old ON issue_old_key (org_id, key);
CREATE UNIQUE INDEX member_schedule_pkey_old ON member_schedule (org_id, user_id);
CREATE UNIQUE INDEX notification_digest_sent_pkey_old ON notification_digest_sent (org_id, user_id);
CREATE UNIQUE INDEX notification_preference_pkey_old ON notification_preference (org_id, user_id);
CREATE UNIQUE INDEX oidc_provider_pkey_old ON oidc_provider (org_id);
CREATE UNIQUE INDEX org_join_request_pkey_old ON org_join_request (org_id, user_id);
CREATE UNIQUE INDEX org_member_pkey_old ON org_member (org_id, user_id);
CREATE UNIQUE INDEX org_role_pkey_old ON org_role (org_id, key);
CREATE UNIQUE INDEX portal_code_pkey_old ON portal_code (org_id, email);
CREATE UNIQUE INDEX user_theme_pkey_old ON user_theme (org_id, user_id);

ALTER TABLE role_assignment DROP CONSTRAINT role_assignment_role_fkey;
ALTER TABLE issue_old_key DROP CONSTRAINT issue_old_key_pkey, ADD CONSTRAINT issue_old_key_pkey PRIMARY KEY USING INDEX issue_old_key_pkey_old;
ALTER TABLE member_schedule DROP CONSTRAINT member_schedule_pkey, ADD CONSTRAINT member_schedule_pkey PRIMARY KEY USING INDEX member_schedule_pkey_old;
ALTER TABLE notification_digest_sent DROP CONSTRAINT notification_digest_sent_pkey, ADD CONSTRAINT notification_digest_sent_pkey PRIMARY KEY USING INDEX notification_digest_sent_pkey_old;
ALTER TABLE notification_preference DROP CONSTRAINT notification_preference_pkey, ADD CONSTRAINT notification_preference_pkey PRIMARY KEY USING INDEX notification_preference_pkey_old;
ALTER TABLE oidc_provider DROP CONSTRAINT oidc_provider_pkey, ADD CONSTRAINT oidc_provider_pkey PRIMARY KEY USING INDEX oidc_provider_pkey_old;
ALTER TABLE org_join_request DROP CONSTRAINT org_join_request_pkey, ADD CONSTRAINT org_join_request_pkey PRIMARY KEY USING INDEX org_join_request_pkey_old;
ALTER TABLE org_member DROP CONSTRAINT org_member_pkey, ADD CONSTRAINT org_member_pkey PRIMARY KEY USING INDEX org_member_pkey_old;
ALTER TABLE org_role DROP CONSTRAINT org_role_pkey, ADD CONSTRAINT org_role_pkey PRIMARY KEY USING INDEX org_role_pkey_old;
ALTER TABLE portal_code DROP CONSTRAINT portal_code_pkey, ADD CONSTRAINT portal_code_pkey PRIMARY KEY USING INDEX portal_code_pkey_old;
ALTER TABLE user_theme DROP CONSTRAINT user_theme_pkey, ADD CONSTRAINT user_theme_pkey PRIMARY KEY USING INDEX user_theme_pkey_old;
ALTER TABLE role_assignment ADD CONSTRAINT role_assignment_role_fkey
    FOREIGN KEY (org_id, role) REFERENCES org_role (org_id, key) ON DELETE CASCADE;

ALTER TABLE oidc_provider DROP COLUMN one_row;
