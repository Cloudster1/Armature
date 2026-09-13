-- +goose Up

-- ------------------------------------------------------------- audit log ----

-- The log gains a reader, so it gains the index that reader filters by, and a
-- way to copy an event from the stream exactly once.
ALTER TABLE audit_log ADD COLUMN event_id uuid;
ALTER TABLE audit_log ADD CONSTRAINT audit_log_action_not_blank CHECK (btrim(action) <> '');
CREATE UNIQUE INDEX audit_log_event_idx ON audit_log (event_id) WHERE event_id IS NOT NULL;
CREATE INDEX audit_log_org_action_idx ON audit_log (org_id, action, created_at DESC);
CREATE INDEX audit_log_org_actor_idx ON audit_log (org_id, actor_user_id, created_at DESC);

-- ---------------------------------------------------- organization fields ----

-- A field without a project belongs to every project of the organization.
ALTER TABLE custom_field ALTER COLUMN project_id DROP NOT NULL;
CREATE UNIQUE INDEX custom_field_org_name_idx ON custom_field (org_id, lower(name)) WHERE project_id IS NULL;

-- A name means one field: a project may not coin a field named like one of the
-- organization's, and the organization may not coin one named like any project's.
-- +goose StatementBegin
CREATE FUNCTION custom_field_name_unshadowed() RETURNS trigger AS $$
BEGIN
    IF NEW.project_id IS NULL THEN
        IF EXISTS (SELECT 1 FROM custom_field f
                   WHERE f.org_id = NEW.org_id AND f.project_id IS NOT NULL
                     AND lower(f.name) = lower(NEW.name) AND f.id <> NEW.id) THEN
            RAISE EXCEPTION 'a project already has a field named %', NEW.name
                USING ERRCODE = 'unique_violation', CONSTRAINT = 'custom_field_name_unshadowed';
        END IF;
    ELSE
        IF EXISTS (SELECT 1 FROM custom_field f
                   WHERE f.org_id = NEW.org_id AND f.project_id IS NULL
                     AND lower(f.name) = lower(NEW.name) AND f.id <> NEW.id) THEN
            RAISE EXCEPTION 'the organization already has a field named %', NEW.name
                USING ERRCODE = 'unique_violation', CONSTRAINT = 'custom_field_name_unshadowed';
        END IF;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- Checked on the statement, except that a promotion defers it to the commit
-- so it may take the name before the fields it folds are gone.
CREATE CONSTRAINT TRIGGER custom_field_name_unshadowed AFTER INSERT OR UPDATE OF name, project_id ON custom_field
    DEFERRABLE INITIALLY IMMEDIATE
    FOR EACH ROW EXECUTE FUNCTION custom_field_name_unshadowed();

-- An organization field fits any issue of its organization; a project field
-- still fits only its project's.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_field_value_same_project() RETURNS trigger AS $$
DECLARE
    issue_project uuid;
    issue_org     uuid;
    field_project uuid;
    field_org     uuid;
BEGIN
    SELECT project_id, org_id INTO issue_project, issue_org FROM issue WHERE id = NEW.issue_id;
    SELECT project_id, org_id INTO field_project, field_org FROM custom_field WHERE id = NEW.field_id;
    IF field_org IS DISTINCT FROM issue_org
       OR (field_project IS NOT NULL AND issue_project IS DISTINCT FROM field_project) THEN
        RAISE EXCEPTION 'field % is not a field of the project issue % is in', NEW.field_id, NEW.issue_id
            USING ERRCODE = 'check_violation', CONSTRAINT = 'issue_field_value_same_project';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- --------------------------------------------------------- status updates ----

CREATE TABLE project_status_update (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    author_id  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    status     text NOT NULL CHECK (status IN ('on_track', 'at_risk', 'off_track')),
    note       text NOT NULL DEFAULT '',
    target_on  date,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX project_status_update_project_idx ON project_status_update (project_id, created_at DESC);

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['project_status_update']
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
        GRANT SELECT, INSERT, UPDATE, DELETE ON project_status_update TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS project_status_update CASCADE;
DROP TRIGGER IF EXISTS custom_field_name_unshadowed ON custom_field;
DROP FUNCTION IF EXISTS custom_field_name_unshadowed();
DROP INDEX IF EXISTS custom_field_org_name_idx;
DELETE FROM custom_field WHERE project_id IS NULL;
ALTER TABLE custom_field ALTER COLUMN project_id SET NOT NULL;
DROP INDEX IF EXISTS audit_log_org_actor_idx;
DROP INDEX IF EXISTS audit_log_org_action_idx;
DROP INDEX IF EXISTS audit_log_event_idx;
ALTER TABLE audit_log DROP CONSTRAINT IF EXISTS audit_log_action_not_blank;
ALTER TABLE audit_log DROP COLUMN IF EXISTS event_id;
