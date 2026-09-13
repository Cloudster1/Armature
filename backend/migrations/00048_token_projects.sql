-- +goose Up
-- Which projects a key reaches. No rows means every project its owner reaches,
-- which is what every key made before this migration meant.
--
-- Project ids rather than keys: a project that is renamed keeps its key in
-- every key that named it, and one that is deleted drops out of them.
CREATE TABLE api_token_project (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    token_id   uuid NOT NULL REFERENCES api_token(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    PRIMARY KEY (token_id, project_id)
);

CREATE INDEX api_token_project_project_idx ON api_token_project (project_id);

-- ---------------------------------------------------- row level security ----

-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE api_token_project ENABLE ROW LEVEL SECURITY;
    ALTER TABLE api_token_project FORCE ROW LEVEL SECURITY;

    CREATE POLICY api_token_project_tenant_isolation ON api_token_project
        USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY api_token_project_admin_bypass ON api_token_project
            TO armature_admin USING (true) WITH CHECK (true);
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON api_token_project TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS api_token_project;
