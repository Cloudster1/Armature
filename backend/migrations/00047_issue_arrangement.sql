-- +goose Up
-- Which of an issue's fields are shown, where, and in what order. The
-- organization keeps a default and a project disagrees with it per issue type,
-- the way workflows are decided: a row for no project is the organization's,
-- and a row for no issue type answers for every type it does not name.
CREATE TABLE issue_arrangement (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id    uuid REFERENCES project(id) ON DELETE CASCADE,
    issue_type_id uuid REFERENCES issue_type(id) ON DELETE CASCADE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- One arrangement per scope and type. NULLS NOT DISTINCT is what makes that
-- true of the organization's own row and of a row for every type.
CREATE UNIQUE INDEX issue_arrangement_scope_idx
    ON issue_arrangement (org_id, project_id, issue_type_id) NULLS NOT DISTINCT;

CREATE TRIGGER issue_arrangement_set_updated_at BEFORE UPDATE ON issue_arrangement
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A slot is a field this tracker knows by name, or one the project defined,
-- never both and never neither.
CREATE TABLE issue_arrangement_slot (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id          uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    arrangement_id  uuid NOT NULL REFERENCES issue_arrangement(id) ON DELETE CASCADE,
    area            text NOT NULL,
    position        integer NOT NULL,
    builtin         text,
    custom_field_id uuid REFERENCES custom_field(id) ON DELETE CASCADE,
    CONSTRAINT issue_arrangement_slot_is_one_thing CHECK (num_nonnulls(builtin, custom_field_id) = 1),
    CONSTRAINT issue_arrangement_slot_area_not_blank CHECK (btrim(area) <> '')
);

-- Two slots cannot tie for a place, or the order is the planner's opinion.
CREATE UNIQUE INDEX issue_arrangement_slot_place_idx ON issue_arrangement_slot (arrangement_id, area, position);
CREATE INDEX issue_arrangement_slot_field_idx ON issue_arrangement_slot (custom_field_id) WHERE custom_field_id IS NOT NULL;

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['issue_arrangement', 'issue_arrangement_slot']
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
        GRANT SELECT, INSERT, UPDATE, DELETE ON issue_arrangement, issue_arrangement_slot TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS issue_arrangement_slot;
DROP TABLE IF EXISTS issue_arrangement;
