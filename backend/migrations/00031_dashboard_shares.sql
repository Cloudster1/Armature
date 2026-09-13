-- +goose Up
-- A link to one dashboard that needs no sign-in. The token lives here only as
-- a digest; a revoked link keeps its row, so who shared what stays on record.
CREATE TABLE dashboard_share (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    dashboard_id uuid NOT NULL REFERENCES dashboard(id) ON DELETE CASCADE,
    name         text NOT NULL,
    query        text NOT NULL DEFAULT '',
    token_hash   bytea NOT NULL UNIQUE,
    internal     boolean NOT NULL DEFAULT false,
    created_by   uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz,
    revoked_at   timestamptz,
    CONSTRAINT dashboard_share_name_not_blank CHECK (btrim(name) <> '')
);

CREATE INDEX dashboard_share_dashboard_idx ON dashboard_share (dashboard_id);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION dashboard_share_guard() RETURNS trigger AS $$
DECLARE
    dashboard_org uuid;
BEGIN
    SELECT org_id INTO dashboard_org FROM dashboard WHERE id = NEW.dashboard_id;
    IF dashboard_org IS DISTINCT FROM NEW.org_id THEN
        RAISE EXCEPTION 'a link can only open a dashboard of its own organization'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER dashboard_share_check
    BEFORE INSERT OR UPDATE OF dashboard_id, org_id ON dashboard_share
    FOR EACH ROW EXECUTE FUNCTION dashboard_share_guard();

-- +goose StatementBegin
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['dashboard_share']
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
        GRANT SELECT, INSERT, UPDATE, DELETE ON dashboard_share TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS dashboard_share CASCADE;
DROP FUNCTION IF EXISTS dashboard_share_guard();
