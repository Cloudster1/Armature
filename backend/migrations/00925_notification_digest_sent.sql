-- +goose Up
-- When each organization last mailed a person their digest. Kept here, not in
-- the worker, so a restart or a second worker neither resends nor skips one.
CREATE TABLE notification_digest_sent (
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    last_sent_at timestamptz NOT NULL,
    PRIMARY KEY (org_id, user_id)
);

CREATE INDEX notification_digest_sent_user_idx ON notification_digest_sent (user_id);

-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE notification_digest_sent ENABLE ROW LEVEL SECURITY;
    ALTER TABLE notification_digest_sent FORCE ROW LEVEL SECURITY;
    CREATE POLICY notification_digest_sent_tenant_isolation ON notification_digest_sent
        USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY notification_digest_sent_admin_bypass ON notification_digest_sent TO armature_admin USING (true) WITH CHECK (true);
    END IF;

    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON notification_digest_sent TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS notification_digest_sent CASCADE;
