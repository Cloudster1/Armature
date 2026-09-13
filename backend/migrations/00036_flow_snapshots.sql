-- +goose Up
-- How many issues stood in each status at the end of each day, per project.
-- The worker writes today's rows on a timer and the day's last write stands;
-- a day nobody wrote is rebuilt once from the changelog and marked so, since
-- the changelog remembers status names and a rename would rewrite the past.

CREATE TABLE project_flow_snapshot (
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    project_id    uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    day           date NOT NULL,
    status_id     uuid NOT NULL REFERENCES issue_status(id) ON DELETE CASCADE,
    count         integer NOT NULL,
    reconstructed boolean NOT NULL DEFAULT false,
    taken_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, day, status_id),
    CONSTRAINT project_flow_snapshot_not_negative CHECK (count >= 0)
);

CREATE INDEX project_flow_snapshot_day_idx ON project_flow_snapshot (project_id, day);

-- ---------------------------------------------------- row level security ----

ALTER TABLE project_flow_snapshot ENABLE ROW LEVEL SECURITY;
ALTER TABLE project_flow_snapshot FORCE  ROW LEVEL SECURITY;

CREATE POLICY project_flow_snapshot_tenant_isolation ON project_flow_snapshot
    USING (org_id = current_org_id())
    WITH CHECK (org_id = current_org_id());

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY project_flow_snapshot_admin_bypass ON project_flow_snapshot TO armature_admin USING (true) WITH CHECK (true);
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON project_flow_snapshot TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS project_flow_snapshot CASCADE;
