-- +goose Up
-- A link that sets a new password, mailed to somebody who forgot theirs. One
-- per person: asking again replaces the link before it. Only the digest of the
-- secret is kept, as for sessions, so a copy of the table opens nothing.
CREATE TABLE password_reset (
    user_id    uuid PRIMARY KEY REFERENCES app_user(id) ON DELETE CASCADE,
    token_hash bytea NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- An account belongs to no one organization, so like user_session there is no
-- tenant to isolate by. It is read and written only before anybody is signed
-- in, on the admin path, so the application role is given nothing, not even
-- what the default privileges would hand it.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_admin') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON password_reset TO armature_admin;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        REVOKE ALL ON password_reset FROM armature_app;
    END IF;
END;
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS password_reset;
