-- +goose Up
-- A one-time code proves a mail address to the portal. One row per address
-- per organization: a new request replaces the old code, requested_at is the
-- cooldown, and attempts is how many wrong guesses the code has taken.
CREATE TABLE portal_code (
    org_id       uuid NOT NULL REFERENCES org(id) ON DELETE CASCADE,
    email        citext NOT NULL,
    code_hash    bytea NOT NULL,
    attempts     integer NOT NULL DEFAULT 0,
    requested_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    PRIMARY KEY (org_id, email)
);

-- Written on the admin path, since nobody is signed in yet; the tenant policy
-- is there so that nothing else can read another organization's codes.
-- +goose StatementBegin
DO $$
BEGIN
    ALTER TABLE portal_code ENABLE ROW LEVEL SECURITY;
    ALTER TABLE portal_code FORCE ROW LEVEL SECURITY;
    CREATE POLICY portal_code_tenant_isolation ON portal_code
        USING (org_id = current_org_id()) WITH CHECK (org_id = current_org_id());
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        CREATE POLICY portal_code_admin_bypass ON portal_code TO armature_admin USING (true) WITH CHECK (true);
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON portal_code TO armature_app, armature_admin;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS portal_code;
