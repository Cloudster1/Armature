-- +goose Up
-- The tables stop carrying the organization (#218): an installation holds one,
-- nothing writes or reads org_id any more, and every key, trigger and query
-- was rebuilt without it. Postgres drops what depends on a column with it, so
-- the upgrade first stops at anything that would go unseen.

-- +goose StatementBegin
DO $$
DECLARE
    listed CONSTANT int := 10;
    total int;
    names text;
BEGIN
    SELECT count(*) INTO total FROM org;
    IF total > 1 THEN
        RAISE EXCEPTION 'This installation holds % organizations, and from this release the tables name none, so a row could not say which it belongs to. Delete the extra ones, then upgrade again.', total
            USING ERRCODE = 'check_violation';
    END IF;

    -- What depends on the columns, beyond each one's own default, not-null
    -- and foreign key to org, and the check that keeps a session's two empty.
    WITH dependent AS (
        SELECT DISTINCT CASE d.classid
                   WHEN 'pg_rewrite'::regclass THEN format('view %s', (SELECT r.ev_class::regclass FROM pg_rewrite r WHERE r.oid = d.objid))
                   ELSE pg_describe_object(d.classid, d.objid, d.objsubid) END AS what
        FROM pg_depend d
        JOIN pg_attribute a ON a.attrelid = d.refobjid AND a.attnum = d.refobjsubid
        JOIN pg_class c ON c.oid = a.attrelid
        LEFT JOIN pg_constraint k ON d.classid = 'pg_constraint'::regclass AND k.oid = d.objid
        WHERE d.refclassid = 'pg_class'::regclass AND c.relnamespace = 'public'::regnamespace
          AND a.attname IN ('org_id', 'current_org_id', 'proof_org_id')
          AND NOT (d.classid = 'pg_class'::regclass AND d.objid = c.oid)
          AND d.classid <> 'pg_attrdef'::regclass
          AND (k.oid IS NULL OR NOT (
                   k.contype = 'n'
                OR (k.contype = 'f' AND k.confrelid = 'org'::regclass AND k.conrelid = c.oid)
                OR (k.conname = 'user_session_names_no_organization' AND k.conrelid = 'user_session'::regclass)))
        UNION
        -- A function's body is not tracked, so it is read: the four this
        -- upgrade replaces or drops are the only ones that may name it.
        SELECT format('function %s', p.proname)
        FROM pg_proc p
        WHERE p.pronamespace = 'public'::regnamespace AND p.prosrc ~ 'org_id'
          AND p.proname NOT IN ('current_org_id', 'installation_org_id', 'org_builtin_roles', 'org_default_theme_guard')
    )
    SELECT count(*), string_agg(what, ', ' ORDER BY what) FILTER (WHERE n <= listed) INTO total, names
    FROM (SELECT what, row_number() OVER (ORDER BY what) AS n FROM dependent) numbered;
    IF total > 0 THEN
        IF total > listed THEN
            names := names || format(' and %s more', total - listed);
        END IF;
        RAISE EXCEPTION 'The organization''s columns go with this release, and these read them: %. Change or drop them so they no longer name org_id, then upgrade again.', names
            USING ERRCODE = 'dependent_objects_still_exist';
    END IF;
END $$;
-- +goose StatementEnd

-- The installation's five roles, made for the installation rather than for an
-- organization's id, and the rule that its default theme is a shared one.
-- +goose StatementBegin
CREATE FUNCTION builtin_roles() RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO org_role (key, name, description, org_wide_only, builtin, permissions, sort_order) VALUES
        ('global_administrator', 'Global administrator',
         'Administers the installation: its members, its roles, its workflows and every project in it.',
         true, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write', 'sprint.manage', 'team.manage',
                           'board.configure', 'project.administer', 'project.create', 'org.administer'], 10),
        ('project_administrator', 'Project administrator',
         'Configures one project: its boards, its workflow scheme, its teams, and whether it is archived.',
         false, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write', 'sprint.manage', 'team.manage',
                            'board.configure', 'project.administer'], 20),
        ('scrum_master', 'Scrum master',
         'Runs the work in one project: sprints, teams and the planning that goes with them.',
         false, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write', 'sprint.manage', 'team.manage'], 30),
        ('user', 'User',
         'Does the ordinary work: files issues, moves them, comments.',
         false, true, ARRAY['read', 'issue.write', 'issue.transition', 'comment.write'], 40),
        ('reader', 'Reader',
         'Sees a project and changes nothing in it.',
         false, true, ARRAY['read'], 50)
    ON CONFLICT (key) DO NOTHING;
END;
$$;

CREATE OR REPLACE FUNCTION org_starts_with_roles() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM builtin_roles();
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION org_default_theme_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.default_theme_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM theme WHERE id = NEW.default_theme_id AND shared
    ) THEN
        RAISE EXCEPTION 'the installation''s default theme has to be a shared theme'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
DROP FUNCTION org_builtin_roles(uuid);

ALTER TABLE user_session DROP CONSTRAINT user_session_names_no_organization;
ALTER TABLE user_session DROP COLUMN current_org_id, DROP COLUMN proof_org_id;

-- Without CASCADE, so anything the check above did not foresee stops the drop.
-- +goose StatementBegin
DO $$
DECLARE
    t record;
BEGIN
    FOR t IN
        SELECT c.oid::regclass AS name
        FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid
        WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
          AND a.attname = 'org_id' AND NOT a.attisdropped
        ORDER BY c.relname
    LOOP
        EXECUTE format('ALTER TABLE %s DROP COLUMN org_id', t.name);
    END LOOP;
END $$;
-- +goose StatementEnd

DROP FUNCTION current_org_id();
DROP FUNCTION installation_org_id();

-- Nothing names the row any more, so nothing could tell a second one apart.
CREATE UNIQUE INDEX org_one_row_idx ON org ((true));

-- +goose Down
DROP INDEX org_one_row_idx;

-- +goose StatementBegin
CREATE FUNCTION installation_org_id() RETURNS uuid
LANGUAGE plpgsql STABLE PARALLEL SAFE AS $$
DECLARE
    total int;
    found uuid;
BEGIN
    SELECT count(*), (array_agg(o.id))[1] INTO total, found FROM org o;
    IF total > 1 THEN
        RAISE EXCEPTION 'This installation holds % organizations, and Armature runs one per installation, so a row cannot tell which it belongs to. Delete the extra ones, then try again.', total
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN found;
END $$;

CREATE FUNCTION current_org_id() RETURNS uuid
LANGUAGE sql STABLE PARALLEL SAFE AS $$
    SELECT id FROM org
$$;
-- +goose StatementEnd

-- Every table gets its column back, filled with the installation's id. The
-- default is found once per table, so this does not rewrite a row.
-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY[
        'absence', 'api_token', 'api_token_project', 'attachment', 'audit_log', 'automation_rule', 'automation_run',
        'board', 'board_swimlane', 'board_swimlane_status', 'business_calendar', 'canned_response', 'ci_run',
        'component', 'csat_rating', 'custom_field', 'dashboard', 'dashboard_share', 'dashboard_template',
        'dashboard_widget', 'git_branch', 'git_commit', 'git_repository', 'git_smart_command', 'group_member',
        'holiday', 'holiday_calendar', 'import_job', 'issue', 'issue_arrangement', 'issue_arrangement_slot',
        'issue_comment', 'issue_commit', 'issue_component', 'issue_field_value', 'issue_history', 'issue_label',
        'issue_link', 'issue_link_type', 'issue_old_key', 'issue_pull_request', 'issue_remote_link', 'issue_status',
        'issue_type', 'issue_version', 'issue_watcher', 'issue_worklog', 'kb_article', 'label', 'member_schedule',
        'milestone', 'notification', 'notification_digest', 'notification_digest_sent', 'notification_preference',
        'oidc_login', 'oidc_provider', 'org_invite', 'org_join_request', 'org_member', 'org_role', 'outbox_event',
        'portal_code', 'project', 'project_allocation', 'project_flow_snapshot', 'project_status_update',
        'pull_request', 'request_type', 'role_assignment', 'saved_filter', 'saved_filter_star',
        'saved_filter_subscription', 'sla_policy', 'sla_timer', 'sprint', 'sprint_snapshot', 'team', 'team_member',
        'theme', 'theme_asset', 'transition_rule', 'user_group', 'user_theme', 'version', 'webhook_delivery',
        'webhook_endpoint', 'workflow', 'workflow_scheme', 'workflow_scheme_item', 'workflow_step',
        'workflow_transition'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ADD COLUMN org_id uuid NOT NULL DEFAULT installation_org_id() REFERENCES org (id) ON DELETE CASCADE', t);
    END LOOP;
END $$;
-- +goose StatementEnd

-- The two that may name none, and a session's two that 00931 emptied.
ALTER TABLE inbound_mail ADD COLUMN org_id uuid REFERENCES org (id) ON DELETE CASCADE;
ALTER TABLE attachment_tombstone ADD COLUMN org_id uuid REFERENCES org (id) ON DELETE SET NULL;
ALTER TABLE user_session
    ADD COLUMN current_org_id uuid REFERENCES org (id) ON DELETE SET NULL,
    ADD COLUMN proof_org_id uuid REFERENCES org (id) ON DELETE SET NULL,
    ADD CONSTRAINT user_session_names_no_organization CHECK (current_org_id IS NULL AND proof_org_id IS NULL);

-- +goose StatementBegin
CREATE FUNCTION org_builtin_roles(target uuid) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO org_role (org_id, key, name, description, org_wide_only, builtin, permissions, sort_order) VALUES
        (target, 'global_administrator', 'Global administrator',
         'Administers the installation: its members, its roles, its workflows and every project in it.',
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

CREATE OR REPLACE FUNCTION org_starts_with_roles() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM org_builtin_roles(NEW.id);
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION org_default_theme_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.default_theme_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM theme WHERE id = NEW.default_theme_id AND org_id = NEW.id AND shared
    ) THEN
        RAISE EXCEPTION 'the organization''s default theme has to be one of its shared themes'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
DROP FUNCTION builtin_roles();
