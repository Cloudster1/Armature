-- +goose Up
-- The smart commands a repository's commits have carried out, one row for each
-- command on each issue. A squash or merge commit copies the branch's messages
-- into a new SHA, and this is what keeps it from asking the same thing twice.
-- The command is kept as a hash of its text, which is all a match needs.
CREATE TABLE git_smart_command (
    org_id        uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    repository_id uuid NOT NULL REFERENCES git_repository(id) ON DELETE CASCADE,
    issue_id      uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    command_hash  text NOT NULL,
    carried_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (repository_id, issue_id, command_hash),
    CONSTRAINT git_smart_command_hash_shape CHECK (command_hash ~ '^[0-9a-f]{64}$')
);

CREATE INDEX git_smart_command_issue_idx ON git_smart_command (issue_id);

-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE git_smart_command ENABLE ROW LEVEL SECURITY;
    ALTER TABLE git_smart_command FORCE ROW LEVEL SECURITY;
    CREATE POLICY git_smart_command_tenant_isolation ON git_smart_command
        USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY git_smart_command_admin_bypass ON git_smart_command TO armature_admin USING (true) WITH CHECK (true);
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON git_smart_command TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS git_smart_command CASCADE;
