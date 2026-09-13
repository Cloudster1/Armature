-- +goose Up
-- The organization's own dashboard templates. The built-in ones are code;
-- these are rows because they are the organization's arrangement, and a
-- dashboard made from one is a copy that never follows it.
CREATE TABLE dashboard_template (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id      uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    name        text NOT NULL,
    description text NOT NULL DEFAULT '',
    widgets     jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_by  uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dashboard_template_name_not_blank CHECK (btrim(name) <> '')
);

CREATE UNIQUE INDEX dashboard_template_org_name_idx ON dashboard_template (org_id, lower(name));

CREATE TRIGGER dashboard_template_set_updated_at BEFORE UPDATE ON dashboard_template
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['dashboard_template']
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
        GRANT SELECT, INSERT, UPDATE, DELETE ON dashboard_template TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS dashboard_template CASCADE;
