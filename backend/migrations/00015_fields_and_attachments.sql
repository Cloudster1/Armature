-- +goose Up
-- Custom fields: what a project wants to record about its issues beyond what
-- every tracker records. A field is defined once per project and a value is
-- stored per issue, as JSON, so one table serves every kind of field. The kind
-- is text rather than an enum for the same reason a widget's is: adding one is
-- code, and the service refuses a kind it does not know.

CREATE TABLE custom_field (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name       text NOT NULL,
    kind       text NOT NULL,
    -- The choices a select field offers; empty for every other kind.
    options    jsonb NOT NULL DEFAULT '[]'::jsonb,
    position   integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT custom_field_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT custom_field_kind_not_blank CHECK (btrim(kind) <> ''),
    CONSTRAINT custom_field_options_are_a_list CHECK (jsonb_typeof(options) = 'array')
);

CREATE UNIQUE INDEX custom_field_project_name_idx ON custom_field (project_id, lower(name));
CREATE INDEX custom_field_project_idx ON custom_field (project_id, position);

CREATE TRIGGER custom_field_set_updated_at BEFORE UPDATE ON custom_field
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A value is one issue's answer to one field. An issue with no row for a field
-- has no value for it; a cleared value is a deleted row, never a JSON null.
CREATE TABLE issue_field_value (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    issue_id   uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    field_id   uuid NOT NULL REFERENCES custom_field(id) ON DELETE CASCADE,
    value      jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (issue_id, field_id),
    CONSTRAINT issue_field_value_not_null CHECK (jsonb_typeof(value) <> 'null')
);

CREATE INDEX issue_field_value_field_idx ON issue_field_value (field_id);

CREATE TRIGGER issue_field_value_set_updated_at BEFORE UPDATE ON issue_field_value
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A field belongs to a project and so does an issue; a value only makes sense
-- where the two agree. The service checks this and so does the database, so a
-- value cannot be smuggled onto an issue in another project through SQL.
-- +goose StatementBegin
CREATE FUNCTION issue_field_value_same_project() RETURNS trigger AS $$
DECLARE
    issue_project uuid;
    field_project uuid;
BEGIN
    SELECT project_id INTO issue_project FROM issue WHERE id = NEW.issue_id;
    SELECT project_id INTO field_project FROM custom_field WHERE id = NEW.field_id;
    IF issue_project IS DISTINCT FROM field_project THEN
        RAISE EXCEPTION 'field % is not a field of the project issue % is in', NEW.field_id, NEW.issue_id
            USING ERRCODE = 'check_violation', CONSTRAINT = 'issue_field_value_same_project';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issue_field_value_same_project BEFORE INSERT OR UPDATE ON issue_field_value
    FOR EACH ROW EXECUTE FUNCTION issue_field_value_same_project();

-- Attachments: files on an issue. The bytes live in an S3 bucket under
-- object_key; this row is what the tracker knows about them, and it is what
-- row level security guards. No row, no way to reach the object.
CREATE TABLE attachment (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    issue_id     uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    uploader_id  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    file_name    text NOT NULL,
    content_type text NOT NULL DEFAULT 'application/octet-stream',
    size_bytes   bigint NOT NULL,
    object_key   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT attachment_name_not_blank CHECK (btrim(file_name) <> ''),
    CONSTRAINT attachment_size_not_negative CHECK (size_bytes >= 0),
    CONSTRAINT attachment_object_key_unique UNIQUE (object_key)
);

CREATE INDEX attachment_issue_idx ON attachment (issue_id, created_at);

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['custom_field', 'issue_field_value', 'attachment']
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
        GRANT SELECT, INSERT, UPDATE, DELETE ON custom_field, issue_field_value, attachment TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS attachment, issue_field_value, custom_field CASCADE;
DROP FUNCTION IF EXISTS issue_field_value_same_project();
