-- +goose Up
-- A tombstone is an object whose row is gone and whose bytes are still in the
-- bucket. It is written in the transaction that removes the row, so the two
-- can never disagree, and cleared once the bytes are gone.

CREATE TABLE attachment_tombstone (
    object_key text PRIMARY KEY,
    org_id     uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX attachment_tombstone_created_idx ON attachment_tombstone (created_at);

-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE attachment_tombstone ENABLE ROW LEVEL SECURITY;
    ALTER TABLE attachment_tombstone FORCE ROW LEVEL SECURITY;
    CREATE POLICY attachment_tombstone_tenant_isolation ON attachment_tombstone
        USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY attachment_tombstone_admin_bypass ON attachment_tombstone TO armature_admin USING (true) WITH CHECK (true);
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON attachment_tombstone TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS attachment_tombstone;
