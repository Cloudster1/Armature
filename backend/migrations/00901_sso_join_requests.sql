-- +goose Up
-- Somebody the identity provider vouches for, who has not been let in. The
-- row is what an administrator sees on the Users page, so letting them in is
-- a click there rather than an invitation typed out and mailed.
CREATE TABLE org_join_request (
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (org_id, user_id)
);

ALTER TABLE org_join_request ENABLE ROW LEVEL SECURITY;
ALTER TABLE org_join_request FORCE ROW LEVEL SECURITY;
CREATE POLICY org_join_request_tenant_isolation ON org_join_request
    USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY org_join_request_admin_bypass ON org_join_request
            TO armature_admin USING (true) WITH CHECK (true);
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE org_join_request;
