-- +goose Up
-- Roles, groups and who holds what.
--
-- org_member.org_role stays: it answers "does this person consume a seat" and
-- "are they a portal customer". What somebody may *do* is decided here instead,
-- by roles granted to them directly or to a group they belong to.

CREATE TYPE app_role AS ENUM (
    'global_administrator',
    'project_administrator',
    'scrum_master',
    'user',
    'reader'
);

-- ---------------------------------------------------------------- groups ----

-- A group is a name for a set of people. Its members come either from the
-- identity provider, in which case the product does not edit them, or from
-- somebody adding them by hand.
CREATE TYPE group_source AS ENUM ('local', 'oidc');

CREATE TABLE user_group (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    name         text NOT NULL,
    description  text NOT NULL DEFAULT '',
    source       group_source NOT NULL DEFAULT 'local',
    -- external_ref is the value the identity provider sends in its groups
    -- claim. It is what a claim is matched against, so it is unique per tenant.
    external_ref text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT user_group_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT user_group_external_ref_belongs_to_oidc
        CHECK (source = 'oidc' OR external_ref IS NULL)
);

CREATE UNIQUE INDEX user_group_org_name_idx ON user_group (org_id, lower(name));
CREATE UNIQUE INDEX user_group_org_ref_idx ON user_group (org_id, external_ref)
    WHERE external_ref IS NOT NULL;

CREATE TRIGGER user_group_set_updated_at BEFORE UPDATE ON user_group
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE group_member (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    group_id   uuid NOT NULL REFERENCES user_group(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX group_member_user_idx ON group_member (org_id, user_id);

-- --------------------------------------------------------- who holds what ----

-- A grant is a role, a scope and a subject. The scope is one project or, when
-- null, the whole organization. The subject is a person or a group, never both.
CREATE TABLE role_assignment (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    role       app_role NOT NULL,
    project_id uuid REFERENCES project(id) ON DELETE CASCADE,
    user_id    uuid REFERENCES app_user(id) ON DELETE CASCADE,
    group_id   uuid REFERENCES user_group(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    created_by uuid REFERENCES app_user(id) ON DELETE SET NULL,
    CONSTRAINT role_assignment_one_subject
        CHECK ((user_id IS NULL) <> (group_id IS NULL)),
    -- Global administration is administration of the whole tenant. Scoping it
    -- to one project would read as a smaller thing than it is.
    CONSTRAINT role_assignment_global_is_org_wide
        CHECK (role <> 'global_administrator' OR project_id IS NULL)
);

-- The same grant twice is the same grant. NULLS NOT DISTINCT makes that true
-- for the organization-wide ones too, where project_id is null.
CREATE UNIQUE INDEX role_assignment_unique_idx
    ON role_assignment (org_id, role, project_id, user_id, group_id) NULLS NOT DISTINCT;

CREATE INDEX role_assignment_user_idx ON role_assignment (org_id, user_id)
    WHERE user_id IS NOT NULL;
CREATE INDEX role_assignment_group_idx ON role_assignment (org_id, group_id)
    WHERE group_id IS NOT NULL;
CREATE INDEX role_assignment_project_idx ON role_assignment (project_id)
    WHERE project_id IS NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION role_assignment_guard() RETURNS trigger AS $$
DECLARE
    scope_org   uuid;
    subject_org uuid;
BEGIN
    IF NEW.project_id IS NOT NULL THEN
        SELECT org_id INTO scope_org FROM project WHERE id = NEW.project_id;
        IF scope_org IS DISTINCT FROM NEW.org_id THEN
            RAISE EXCEPTION 'a role cannot be granted over another organization''s project'
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

    -- A role granted to somebody who is not a member would be a way in.
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

CREATE TRIGGER role_assignment_check
    BEFORE INSERT OR UPDATE ON role_assignment
    FOR EACH ROW EXECUTE FUNCTION role_assignment_guard();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION group_member_scope_guard() RETURNS trigger AS $$
DECLARE
    group_org uuid;
BEGIN
    SELECT org_id INTO group_org FROM user_group WHERE id = NEW.group_id;
    IF group_org IS DISTINCT FROM NEW.org_id THEN
        RAISE EXCEPTION 'a group member cannot cross organizations'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM org_member WHERE org_id = NEW.org_id AND user_id = NEW.user_id
    ) THEN
        RAISE EXCEPTION 'only a member of the organization can join one of its groups'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER group_member_scope_check
    BEFORE INSERT OR UPDATE ON group_member
    FOR EACH ROW EXECUTE FUNCTION group_member_scope_guard();

-- ------------------------------------------------------ identity provider ----

-- One provider per organization. The secret is stored because the code exchange
-- is a back channel call the server makes; there is nowhere else to keep it.
CREATE TABLE oidc_provider (
    org_id        uuid PRIMARY KEY REFERENCES org(id) ON DELETE CASCADE,
    issuer        text NOT NULL,
    client_id     text NOT NULL,
    client_secret text NOT NULL,
    -- The claim listing the groups a person belongs to. Providers disagree
    -- about its name, so it is configuration rather than a constant.
    groups_claim  text NOT NULL DEFAULT 'groups',
    scopes        text NOT NULL DEFAULT 'openid profile email',
    -- Whether a group the provider names that nobody has created yet should be
    -- created on sight. Off by default: a group appearing on its own is a
    -- surprise, and an empty group grants nothing anyway.
    create_groups boolean NOT NULL DEFAULT false,
    enabled       boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT oidc_provider_issuer_not_blank CHECK (btrim(issuer) <> ''),
    CONSTRAINT oidc_provider_client_id_not_blank CHECK (btrim(client_id) <> '')
);

CREATE TRIGGER oidc_provider_set_updated_at BEFORE UPDATE ON oidc_provider
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A login in flight. The state ties the callback to the browser that started
-- it, and the nonce ties the id token to this one request.
CREATE TABLE oidc_login (
    state      text PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    nonce      text NOT NULL,
    redirect   text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

CREATE INDEX oidc_login_expiry_idx ON oidc_login (expires_at);

-- --------------------------------------------------------------- backfill ----

-- Everybody who could administer the tenant keeps being able to, and everybody
-- else who holds a seat becomes an ordinary user of every project.
INSERT INTO role_assignment (org_id, role, user_id)
SELECT org_id, 'global_administrator', user_id FROM org_member
 WHERE org_role IN ('owner', 'admin')
ON CONFLICT DO NOTHING;

INSERT INTO role_assignment (org_id, role, user_id)
SELECT org_id, 'user', user_id FROM org_member WHERE org_role = 'member'
ON CONFLICT DO NOTHING;

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['user_group', 'group_member', 'role_assignment',
                             'oidc_provider', 'oidc_login']
    LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format(
            'CREATE POLICY %I ON %I USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id())',
            t || '_tenant_isolation', t);

        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
            EXECUTE format(
                'CREATE POLICY %I ON %I TO armature_admin USING (true) WITH CHECK (true)',
                t || '_admin_bypass', t);
        END IF;
    END LOOP;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE
           ON user_group, group_member, role_assignment, oidc_provider, oidc_login
           TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS oidc_login, oidc_provider CASCADE;
DROP TRIGGER IF EXISTS group_member_scope_check ON group_member;
DROP FUNCTION IF EXISTS group_member_scope_guard();
DROP TRIGGER IF EXISTS role_assignment_check ON role_assignment;
DROP FUNCTION IF EXISTS role_assignment_guard();
DROP TABLE IF EXISTS role_assignment, group_member, user_group CASCADE;
DROP TYPE IF EXISTS group_source, app_role;
