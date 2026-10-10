-- +goose Up
-- The share of a person's week a project has, in percent. No row is a whole
-- week, so only a share somebody set is kept.
CREATE TABLE project_allocation (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    percent    smallint NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id),
    CONSTRAINT project_allocation_percent_range CHECK (percent BETWEEN 0 AND 100)
);

CREATE INDEX project_allocation_user_idx ON project_allocation (user_id);

CREATE TRIGGER project_allocation_set_updated_at BEFORE UPDATE ON project_allocation
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Only somebody who works in the organization gives one of its projects a
-- share of their week; the policy checks the row's own organization, and this
-- checks the project and the person are in it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION project_allocation_guard() RETURNS trigger AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM project WHERE id = NEW.project_id AND org_id = NEW.org_id) THEN
        RAISE EXCEPTION 'a share must be of a project in the same organization'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM org_member
         WHERE org_id = NEW.org_id AND user_id = NEW.user_id
           AND org_role <> 'customer'
    ) THEN
        RAISE EXCEPTION 'only a member of the organization gives a project a share of their week'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER project_allocation_check
    BEFORE INSERT OR UPDATE ON project_allocation
    FOR EACH ROW EXECUTE FUNCTION project_allocation_guard();

-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE project_allocation ENABLE ROW LEVEL SECURITY;
    ALTER TABLE project_allocation FORCE ROW LEVEL SECURITY;
    CREATE POLICY project_allocation_tenant_isolation ON project_allocation
        USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY project_allocation_admin_bypass ON project_allocation TO armature_admin USING (true) WITH CHECK (true);
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON project_allocation TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS project_allocation CASCADE;
DROP FUNCTION IF EXISTS project_allocation_guard();
