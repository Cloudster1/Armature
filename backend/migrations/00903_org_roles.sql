-- +goose Up
-- Roles become the organization's own: the five built-in ones are rows now,
-- their permissions editable, and an organization may add roles of its own.
-- What a permission is stays fixed in code; which role holds which is data.
CREATE TABLE org_role (
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    key           text NOT NULL,
    name          text NOT NULL,
    description   text NOT NULL DEFAULT '',
    org_wide_only boolean NOT NULL DEFAULT false,
    builtin       boolean NOT NULL DEFAULT false,
    permissions   text[] NOT NULL DEFAULT '{}',
    sort_order    integer NOT NULL DEFAULT 100,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, key),
    CONSTRAINT org_role_key_shape CHECK (key ~ '^[a-z][a-z0-9_]{1,39}$'),
    CONSTRAINT org_role_name_not_blank CHECK (btrim(name) <> ''),
    -- The permissions the code knows. A row naming another would grant a
    -- word nothing checks for.
    CONSTRAINT org_role_known_permissions CHECK (
        permissions <@ ARRAY['read', 'issue.write', 'issue.transition', 'comment.write',
                             'sprint.manage', 'team.manage', 'board.configure',
                             'project.administer', 'project.create', 'org.administer']
    )
);

CREATE UNIQUE INDEX org_role_name_idx ON org_role (org_id, lower(name));

CREATE TRIGGER org_role_set_updated_at BEFORE UPDATE ON org_role
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The five every organization starts with, as the code had them.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION org_builtin_roles(target uuid) RETURNS void AS $$
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
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION org_starts_with_roles() RETURNS trigger AS $$
BEGIN
    PERFORM org_builtin_roles(NEW.id);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER org_starts_with_roles AFTER INSERT ON org
    FOR EACH ROW EXECUTE FUNCTION org_starts_with_roles();

-- Every organization there is gets the five. The org table forces row
-- security on its owner as well, and the migrations run as the owner, so
-- without lifting it for the loop the loop would see no organizations at
-- all and quietly fill none.
ALTER TABLE org NO FORCE ROW LEVEL SECURITY;
-- +goose StatementBegin
DO $$
DECLARE
    o uuid;
BEGIN
    FOR o IN SELECT id FROM org LOOP
        PERFORM org_builtin_roles(o);
    END LOOP;
END $$;
-- +goose StatementEnd
ALTER TABLE org FORCE ROW LEVEL SECURITY;

-- What a built-in role is stays: it cannot go, its key cannot change, and
-- the global administrator keeps administering the organization, or an
-- edit could lock every administrator out of the page that undoes it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION org_role_guard() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        -- A built-in role goes only with its organization, whose row is
        -- already gone by the time the cascade reaches here.
        IF OLD.builtin AND EXISTS (SELECT 1 FROM org WHERE id = OLD.org_id) THEN
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
        RAISE EXCEPTION 'the global administrator keeps administering the organization'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER org_role_check
    BEFORE INSERT OR UPDATE OR DELETE ON org_role
    FOR EACH ROW EXECUTE FUNCTION org_role_guard();

-- A grant names one of the organization's roles. The enum goes; the row
-- decides. Deleting a role takes its grants with it.
ALTER TABLE role_assignment DROP CONSTRAINT role_assignment_global_is_org_wide;
ALTER TABLE role_assignment ALTER COLUMN role TYPE text USING role::text;
ALTER TABLE role_assignment
    ADD CONSTRAINT role_assignment_role_fkey
    FOREIGN KEY (org_id, role) REFERENCES org_role(org_id, key) ON DELETE CASCADE;
DROP TYPE app_role;

-- The scope rule moves from the enum's constraint to the role's own flag.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION role_assignment_guard() RETURNS trigger AS $$
DECLARE
    scope_org   uuid;
    subject_org uuid;
    whole_org   boolean;
BEGIN
    IF NEW.project_id IS NOT NULL THEN
        SELECT org_id INTO scope_org FROM project WHERE id = NEW.project_id;
        IF scope_org IS DISTINCT FROM NEW.org_id THEN
            RAISE EXCEPTION 'a role cannot be granted over another organization''s project'
                USING ERRCODE = 'check_violation';
        END IF;
        SELECT org_wide_only INTO whole_org FROM org_role WHERE org_id = NEW.org_id AND key = NEW.role;
        IF whole_org THEN
            RAISE EXCEPTION 'that role answers for the whole organization and cannot be granted over one project'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;

    IF NEW.group_id IS NOT NULL THEN
        SELECT org_id INTO subject_org FROM user_group WHERE id = NEW.group_id;
        IF subject_org IS DISTINCT FROM NEW.org_id THEN
            RAISE EXCEPTION 'a role cannot be granted to another organization''s group'
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;

    IF NEW.user_id IS NOT NULL AND NOT EXISTS (
        SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.user_id
    ) THEN
        RAISE EXCEPTION 'a role can only be granted to a member of the organization'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

ALTER TABLE org_role ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_role FORCE ROW LEVEL SECURITY;
CREATE POLICY org_role_tenant_isolation ON org_role
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY org_role_admin_bypass ON org_role
            TO armature_admin USING (true) WITH CHECK (true);
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
DELETE FROM role_assignment WHERE role NOT IN ('global_administrator', 'project_administrator', 'scrum_master', 'user', 'reader');
CREATE TYPE app_role AS ENUM ('global_administrator', 'project_administrator', 'scrum_master', 'user', 'reader');
ALTER TABLE role_assignment DROP CONSTRAINT role_assignment_role_fkey;
ALTER TABLE role_assignment ALTER COLUMN role TYPE app_role USING role::app_role;
ALTER TABLE role_assignment
    ADD CONSTRAINT role_assignment_global_is_org_wide
    CHECK (role <> 'global_administrator' OR project_id IS NULL);
DROP TRIGGER IF EXISTS org_starts_with_roles ON org;
DROP FUNCTION IF EXISTS org_starts_with_roles();
DROP FUNCTION IF EXISTS org_builtin_roles(uuid);
DROP TABLE org_role;
