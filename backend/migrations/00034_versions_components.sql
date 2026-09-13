-- +goose Up
-- Versions: what a project ships, in order. An issue names the version that
-- fixes it and the versions it affects through one relation with a role, so
-- one guard serves both. Progress is read off the issues, as milestones taught
-- us. Components: a project's parts, each with somebody who owns it and, when
-- it says so, who new work in it goes to.

CREATE TABLE version (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id  uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    start_on    date,
    release_on  date,
    released_at timestamptz,
    archived_at timestamptz,
    position    integer NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT version_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT version_dates_in_order CHECK (start_on IS NULL OR release_on IS NULL OR start_on <= release_on),
    CONSTRAINT version_archived_after_release CHECK (archived_at IS NULL OR released_at IS NOT NULL)
);

CREATE UNIQUE INDEX version_project_name_idx ON version (project_id, lower(name));
CREATE INDEX version_project_idx ON version (project_id, released_at, release_on);

CREATE TRIGGER version_set_updated_at BEFORE UPDATE ON version
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE issue_version (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    issue_id   uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    version_id uuid NOT NULL REFERENCES version(id) ON DELETE CASCADE,
    role       text NOT NULL,
    PRIMARY KEY (issue_id, version_id, role),
    CONSTRAINT issue_version_role_known CHECK (role IN ('fix', 'affects'))
);

CREATE INDEX issue_version_version_idx ON issue_version (version_id, role);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_version_guard() RETURNS trigger AS $$
DECLARE
    issue_project    uuid;
    version_project  uuid;
    version_org      uuid;
    version_archived timestamptz;
BEGIN
    SELECT project_id INTO issue_project FROM issue WHERE id = NEW.issue_id;
    SELECT project_id, org_id, archived_at INTO version_project, version_org, version_archived FROM version WHERE id = NEW.version_id;
    IF version_org IS DISTINCT FROM NEW.org_id OR version_project IS DISTINCT FROM issue_project THEN
        RAISE EXCEPTION 'an issue can only name a version of its own project'
            USING ERRCODE = 'check_violation';
    END IF;
    IF version_archived IS NOT NULL THEN
        RAISE EXCEPTION 'that version is archived'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issue_version_check
    BEFORE INSERT OR UPDATE ON issue_version
    FOR EACH ROW EXECUTE FUNCTION issue_version_guard();

CREATE TABLE component (
    id                  uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id              uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id          uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name                text NOT NULL,
    description         text NOT NULL DEFAULT '',
    lead_id             uuid REFERENCES app_user(id) ON DELETE SET NULL,
    default_assignee_id uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT component_name_not_blank CHECK (btrim(name) <> '')
);

CREATE UNIQUE INDEX component_project_name_idx ON component (project_id, lower(name));

CREATE TRIGGER component_set_updated_at BEFORE UPDATE ON component
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE issue_component (
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    issue_id     uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    component_id uuid NOT NULL REFERENCES component(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, component_id)
);

CREATE INDEX issue_component_component_idx ON issue_component (component_id);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_component_guard() RETURNS trigger AS $$
DECLARE
    issue_project     uuid;
    component_project uuid;
    component_org     uuid;
BEGIN
    SELECT project_id INTO issue_project FROM issue WHERE id = NEW.issue_id;
    SELECT project_id, org_id INTO component_project, component_org FROM component WHERE id = NEW.component_id;
    IF component_org IS DISTINCT FROM NEW.org_id OR component_project IS DISTINCT FROM issue_project THEN
        RAISE EXCEPTION 'an issue can only be in a component of its own project'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issue_component_check
    BEFORE INSERT OR UPDATE ON issue_component
    FOR EACH ROW EXECUTE FUNCTION issue_component_guard();

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['version', 'issue_version', 'component', 'issue_component']
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
        GRANT SELECT, INSERT, UPDATE, DELETE ON version, issue_version, component, issue_component TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS issue_component CASCADE;
DROP TABLE IF EXISTS component CASCADE;
DROP TABLE IF EXISTS issue_version CASCADE;
DROP TABLE IF EXISTS version CASCADE;
DROP FUNCTION IF EXISTS issue_component_guard();
DROP FUNCTION IF EXISTS issue_version_guard();
