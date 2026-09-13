-- +goose Up
-- Dashboards: a project's own view of how it is going, made of widgets over
-- the project's data. The data is computed on read; what is stored is which
-- views somebody wanted, and in what order.

CREATE TABLE dashboard (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name       text NOT NULL,
    position   integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dashboard_name_not_blank CHECK (btrim(name) <> '')
);

CREATE UNIQUE INDEX dashboard_project_name_idx ON dashboard (project_id, lower(name));
CREATE INDEX dashboard_project_idx ON dashboard (project_id, position);

CREATE TRIGGER dashboard_set_updated_at BEFORE UPDATE ON dashboard
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A widget is a kind of report and how it is drawn. The kind is text rather
-- than an enum so that adding a report is code, not a migration; the service
-- refuses a kind it does not know.
CREATE TABLE dashboard_widget (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    dashboard_id uuid NOT NULL REFERENCES dashboard(id) ON DELETE CASCADE,
    kind         text NOT NULL,
    title        text NOT NULL DEFAULT '',
    -- How many of the two columns the widget spans.
    width        integer NOT NULL DEFAULT 1 CHECK (width IN (1, 2)),
    position     integer NOT NULL DEFAULT 0,
    -- What the report is asked with: a window in days, a team, and so on.
    config       jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT dashboard_widget_kind_not_blank CHECK (btrim(kind) <> '')
);

CREATE INDEX dashboard_widget_dashboard_idx ON dashboard_widget (dashboard_id, position);

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['dashboard', 'dashboard_widget']
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
        GRANT SELECT, INSERT, UPDATE, DELETE ON dashboard, dashboard_widget TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS dashboard_widget, dashboard CASCADE;
