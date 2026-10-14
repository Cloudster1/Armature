-- +goose Up
-- What is left of the organization is names (#218). Its row is the
-- installation's settings, membership is a standing on the account, and the
-- roles, invitations and requests to join belong to the installation.

-- A view, trigger or rule on the membership table would go with it unseen, and
-- a function body naming a renamed table breaks only when it runs.
-- +goose StatementBegin
DO $$
DECLARE
    listed CONSTANT int := 10;
    total int;
    names text;
BEGIN
    WITH dependent AS (
        SELECT DISTINCT CASE d.classid
                   WHEN 'pg_rewrite'::regclass THEN format('view %s', (SELECT r.ev_class::regclass FROM pg_rewrite r WHERE r.oid = d.objid))
                   ELSE pg_describe_object(d.classid, d.objid, d.objsubid) END AS what
        FROM pg_depend d
        LEFT JOIN pg_constraint k ON d.classid = 'pg_constraint'::regclass AND k.oid = d.objid
        LEFT JOIN pg_trigger g ON d.classid = 'pg_trigger'::regclass AND g.oid = d.objid
        LEFT JOIN pg_class i ON d.classid = 'pg_class'::regclass AND i.oid = d.objid
        WHERE d.refclassid = 'pg_class'::regclass AND d.refobjid = 'org_member'::regclass
          AND d.classid NOT IN ('pg_type'::regclass, 'pg_attrdef'::regclass)
          AND (k.oid IS NULL OR k.conrelid <> 'org_member'::regclass)
          AND (g.oid IS NULL OR g.tgname <> 'org_member_set_updated_at')
          AND (i.oid IS NULL OR i.relname NOT IN ('org_member_pkey', 'org_member_user_idx') AND i.relkind <> 't')
          AND NOT (d.classid = 'pg_class'::regclass AND d.objid = d.refobjid)
        UNION
        SELECT format('function %s', p.proname)
        FROM pg_proc p
        WHERE p.pronamespace = 'public'::regnamespace
          AND (p.prosrc ~* '\m(org_member|org_role|org_invite|org_join_request)\M'
               OR p.prosrc ~* '\m(from|join|into|update|table)\s+org\M')
          AND p.proname NOT IN ('absence_guard', 'app_user_keeps_an_administrator', 'builtin_roles',
                                'group_member_scope_guard', 'member_schedule_guard', 'org_role_guard',
                                'project_allocation_guard', 'role_assignment_guard', 'saved_filter_share_guard',
                                'team_member_guard', 'theme_share_guard', 'theme_unshared_default')
    )
    SELECT count(*), string_agg(what, ', ' ORDER BY what) FILTER (WHERE n <= listed) INTO total, names
    FROM (SELECT what, row_number() OVER (ORDER BY what) AS n FROM dependent) numbered;
    IF total > 0 THEN
        IF total > listed THEN
            names := names || format(' and %s more', total - listed);
        END IF;
        RAISE EXCEPTION 'The organization''s tables are renamed with this release and org_member goes, and these read them: %. Change or drop them so they no longer name the old tables, then upgrade again.', names
            USING ERRCODE = 'dependent_objects_still_exist';
    END IF;
END $$;
-- +goose StatementEnd

-- Membership becomes the account's standing; NULL is somebody who is no member.
ALTER TABLE app_user ADD COLUMN standing text
    CONSTRAINT app_user_standing_check CHECK (standing IN ('owner', 'admin', 'member', 'customer'));
-- The standing is not an edit of the account, so its updated_at stays.
ALTER TABLE app_user DISABLE TRIGGER app_user_set_updated_at;
UPDATE app_user u SET standing = m.org_role FROM org_member m WHERE m.user_id = u.id;
ALTER TABLE app_user ENABLE TRIGGER app_user_set_updated_at;
DROP TABLE org_member;

ALTER TABLE org RENAME TO installation;
ALTER TABLE org_role RENAME TO role;
ALTER TABLE org_invite RENAME TO invite;
ALTER TABLE org_join_request RENAME TO join_request;
ALTER TABLE role RENAME COLUMN org_wide_only TO installation_wide_only;
ALTER TABLE role RENAME CONSTRAINT org_role_org_wide_only_not_null TO org_role_installation_wide_only_not_null;
ALTER TABLE invite RENAME COLUMN org_role TO standing;
ALTER TABLE invite RENAME CONSTRAINT org_invite_org_role_check TO org_invite_standing_check;
ALTER TABLE invite RENAME CONSTRAINT org_invite_org_role_not_null TO org_invite_standing_not_null;

-- A table's rename leaves its constraints, indexes and triggers named for the
-- old one, the not-null constraints Postgres 18 names among them.
-- +goose StatementBegin
DO $$
DECLARE
    t record;
    o record;
BEGIN
    FOR t IN SELECT * FROM (VALUES ('installation', 'org_', 'installation_'), ('role', 'org_role_', 'role_'),
                                   ('invite', 'org_invite_', 'invite_'), ('join_request', 'org_join_request_', 'join_request_'))
                          AS r (tab, old, new)
    LOOP
        FOR o IN SELECT conname FROM pg_constraint WHERE conrelid = t.tab::regclass AND starts_with(conname, t.old) LOOP
            EXECUTE format('ALTER TABLE %I RENAME CONSTRAINT %I TO %I', t.tab, o.conname, t.new || substr(o.conname, length(t.old) + 1));
        END LOOP;
        FOR o IN SELECT c.relname FROM pg_index x JOIN pg_class c ON c.oid = x.indexrelid
                 WHERE x.indrelid = t.tab::regclass AND starts_with(c.relname, t.old) LOOP
            EXECUTE format('ALTER INDEX %I RENAME TO %I', o.relname, t.new || substr(o.relname, length(t.old) + 1));
        END LOOP;
        FOR o IN SELECT tgname FROM pg_trigger WHERE tgrelid = t.tab::regclass AND NOT tgisinternal AND starts_with(tgname, t.old) LOOP
            EXECUTE format('ALTER TRIGGER %I ON %I RENAME TO %I', o.tgname, t.tab, t.new || substr(o.tgname, length(t.old) + 1));
        END LOOP;
    END LOOP;
END $$;
-- +goose StatementEnd

ALTER FUNCTION org_default_theme_guard() RENAME TO installation_default_theme_guard;
ALTER FUNCTION org_starts_with_roles() RENAME TO installation_starts_with_roles;
ALTER FUNCTION org_role_guard() RENAME TO role_guard;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION builtin_roles() RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO role (key, name, description, installation_wide_only, builtin, permissions, sort_order) VALUES
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

CREATE OR REPLACE FUNCTION role_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        -- A built-in role goes only with the installation, whose row is
        -- already gone by the time the cascade reaches here.
        IF OLD.builtin AND EXISTS (SELECT 1 FROM installation) THEN
            RAISE EXCEPTION 'a built-in role cannot be deleted'
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF NEW.key <> OLD.key OR NEW.builtin <> OLD.builtin THEN
            RAISE EXCEPTION 'a role''s key and standing are fixed'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    IF NEW.key = 'global_administrator' AND NOT ('org.administer' = ANY(NEW.permissions)) THEN
        RAISE EXCEPTION 'the global administrator keeps administering the installation'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION theme_unshared_default() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.shared AND NOT NEW.shared THEN
        UPDATE installation SET default_theme_id = NULL WHERE default_theme_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION app_user_keeps_an_administrator() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.is_active AND NOT NEW.is_active
       AND NEW.standing IN ('owner', 'admin')
       AND NOT EXISTS (SELECT 1 FROM app_user x
                       WHERE x.standing IN ('owner', 'admin') AND x.id <> NEW.id AND x.is_active) THEN
        RAISE EXCEPTION 'the last active administrator cannot be switched off'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION role_assignment_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.project_id IS NOT NULL
       AND (SELECT installation_wide_only FROM role WHERE key = NEW.role) THEN
        RAISE EXCEPTION 'that role answers for the whole installation and cannot be granted over one project'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.user_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM app_user WHERE id = NEW.user_id AND standing IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'a role can only be granted to a member here'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION group_member_scope_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM app_user WHERE id = NEW.user_id AND standing IS NOT NULL) THEN
        RAISE EXCEPTION 'only a member here can join a group'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION absence_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM app_user WHERE id = NEW.user_id AND standing <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member here, not a customer, can be away'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION member_schedule_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    -- A calendar that goes clears itself from the weeks that kept it, whoever
    -- those people are by now.
    IF TG_OP = 'UPDATE' AND NEW.calendar_id IS NULL AND OLD.calendar_id IS NOT NULL
       AND NEW.user_id = OLD.user_id AND NEW.minutes = OLD.minutes THEN
        RETURN NEW;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM app_user WHERE id = NEW.user_id AND standing <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member here, not a customer, has a working week'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION project_allocation_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM app_user WHERE id = NEW.user_id AND standing <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member here, not a customer, gives a project a share of their week'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION saved_filter_share_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.shared AND EXISTS (
        SELECT 1 FROM app_user WHERE id = NEW.owner_id AND standing = 'customer'
    ) THEN
        RAISE EXCEPTION 'a customer cannot share a saved filter'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION team_member_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM app_user WHERE id = NEW.user_id AND standing <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member here, not a customer, can join a team'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION theme_share_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.shared AND EXISTS (
        SELECT 1 FROM app_user WHERE id = NEW.owner_id AND standing = 'customer'
    ) THEN
        RAISE EXCEPTION 'a customer cannot share a theme'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose Down
-- The way back gives every standing its membership row again. When somebody
-- joined was not kept, so a row says they joined when their account was made.

-- +goose StatementBegin
DO $$
DECLARE
    t record;
    o record;
BEGIN
    FOR t IN SELECT * FROM (VALUES ('installation', 'installation_', 'org_'), ('role', 'role_', 'org_role_'),
                                   ('invite', 'invite_', 'org_invite_'), ('join_request', 'join_request_', 'org_join_request_'))
                          AS r (tab, old, new)
    LOOP
        FOR o IN SELECT conname FROM pg_constraint WHERE conrelid = t.tab::regclass AND starts_with(conname, t.old) LOOP
            EXECUTE format('ALTER TABLE %I RENAME CONSTRAINT %I TO %I', t.tab, o.conname, t.new || substr(o.conname, length(t.old) + 1));
        END LOOP;
        FOR o IN SELECT c.relname FROM pg_index x JOIN pg_class c ON c.oid = x.indexrelid
                 WHERE x.indrelid = t.tab::regclass AND starts_with(c.relname, t.old) LOOP
            EXECUTE format('ALTER INDEX %I RENAME TO %I', o.relname, t.new || substr(o.relname, length(t.old) + 1));
        END LOOP;
        FOR o IN SELECT tgname FROM pg_trigger WHERE tgrelid = t.tab::regclass AND NOT tgisinternal AND starts_with(tgname, t.old) LOOP
            EXECUTE format('ALTER TRIGGER %I ON %I RENAME TO %I', o.tgname, t.tab, t.new || substr(o.tgname, length(t.old) + 1));
        END LOOP;
    END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE invite RENAME CONSTRAINT org_invite_standing_not_null TO org_invite_org_role_not_null;
ALTER TABLE invite RENAME CONSTRAINT org_invite_standing_check TO org_invite_org_role_check;
ALTER TABLE invite RENAME COLUMN standing TO org_role;
ALTER TABLE role RENAME CONSTRAINT org_role_installation_wide_only_not_null TO org_role_org_wide_only_not_null;
ALTER TABLE role RENAME COLUMN installation_wide_only TO org_wide_only;
ALTER TABLE join_request RENAME TO org_join_request;
ALTER TABLE invite RENAME TO org_invite;
ALTER TABLE role RENAME TO org_role;
ALTER TABLE installation RENAME TO org;

ALTER FUNCTION installation_default_theme_guard() RENAME TO org_default_theme_guard;
ALTER FUNCTION installation_starts_with_roles() RENAME TO org_starts_with_roles;
ALTER FUNCTION role_guard() RENAME TO org_role_guard;

CREATE TABLE org_member (
    user_id    uuid        NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    org_role   text        NOT NULL CHECK (org_role IN ('owner', 'admin', 'member', 'customer')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id)
);
CREATE INDEX org_member_user_idx ON org_member (user_id);
CREATE TRIGGER org_member_set_updated_at BEFORE UPDATE ON org_member
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
INSERT INTO org_member (user_id, org_role, created_at)
SELECT id, standing, created_at FROM app_user WHERE standing IS NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION builtin_roles() RETURNS void
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

CREATE OR REPLACE FUNCTION org_role_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        -- A built-in role goes only with the installation, whose row is
        -- already gone by the time the cascade reaches here.
        IF OLD.builtin AND EXISTS (SELECT 1 FROM org) THEN
            RAISE EXCEPTION 'a built-in role cannot be deleted'
                USING ERRCODE = 'check_violation';
        END IF;
        RETURN OLD;
    END IF;
    IF TG_OP = 'UPDATE' THEN
        IF NEW.key <> OLD.key OR NEW.builtin <> OLD.builtin THEN
            RAISE EXCEPTION 'a role''s key and standing are fixed'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;
    IF NEW.key = 'global_administrator' AND NOT ('org.administer' = ANY(NEW.permissions)) THEN
        RAISE EXCEPTION 'the global administrator keeps administering the installation'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION theme_unshared_default() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.shared AND NOT NEW.shared THEN
        UPDATE org SET default_theme_id = NULL WHERE default_theme_id = NEW.id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION app_user_keeps_an_administrator() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.is_active AND NOT NEW.is_active
       AND EXISTS (SELECT 1 FROM org_member WHERE user_id = NEW.id AND org_role IN ('owner', 'admin'))
       AND NOT EXISTS (SELECT 1 FROM org_member x JOIN app_user xu ON xu.id = x.user_id
                       WHERE x.org_role IN ('owner', 'admin') AND x.user_id <> NEW.id AND xu.is_active) THEN
        RAISE EXCEPTION 'the last active administrator cannot be switched off'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION role_assignment_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.project_id IS NOT NULL
       AND (SELECT org_wide_only FROM org_role WHERE key = NEW.role) THEN
        RAISE EXCEPTION 'that role answers for the whole installation and cannot be granted over one project'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.user_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.user_id
    ) THEN
        RAISE EXCEPTION 'a role can only be granted to a member here'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION group_member_scope_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM org_member WHERE user_id = NEW.user_id) THEN
        RAISE EXCEPTION 'only a member here can join a group'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION absence_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.user_id AND org_role <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member here, not a customer, can be away'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION member_schedule_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    -- A calendar that goes clears itself from the weeks that kept it, whoever
    -- those people are by now.
    IF TG_OP = 'UPDATE' AND NEW.calendar_id IS NULL AND OLD.calendar_id IS NOT NULL
       AND NEW.user_id = OLD.user_id AND NEW.minutes = OLD.minutes THEN
        RETURN NEW;
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.user_id AND org_role <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member here, not a customer, has a working week'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION project_allocation_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.user_id AND org_role <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member here, not a customer, gives a project a share of their week'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION saved_filter_share_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.shared AND EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.owner_id AND org_role = 'customer'
    ) THEN
        RAISE EXCEPTION 'a customer cannot share a saved filter'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION team_member_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.user_id AND org_role <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member here, not a customer, can join a team'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION theme_share_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.shared AND EXISTS (
        SELECT 1 FROM org_member WHERE user_id = NEW.owner_id AND org_role = 'customer'
    ) THEN
        RAISE EXCEPTION 'a customer cannot share a theme'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER TABLE app_user DROP COLUMN standing;
