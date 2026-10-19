-- +goose Up
-- Connected apps (#280): a partner tool registered as an OAuth client gets a
-- person's token with their consent. The api decides what a token may do; the
-- keys and triggers here refuse a grant for a client nobody allowed.

CREATE TABLE oauth_client (
    id                 uuid PRIMARY KEY DEFAULT uuidv7(),
    name               text NOT NULL,
    client_uri         text NOT NULL DEFAULT '',
    redirect_uris      text[] NOT NULL,
    scopes             text[] NOT NULL DEFAULT '{}',
    -- NULL for a public client, which proves itself with PKCE alone.
    secret_hash        bytea,
    auth_method        text NOT NULL DEFAULT 'client_secret_basic',
    status             text NOT NULL DEFAULT 'active',
    keycloak_client_id text NOT NULL DEFAULT '',
    created_by         uuid REFERENCES app_user(id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT oauth_client_name_shape CHECK (btrim(name) <> '' AND char_length(name) <= 60),
    CONSTRAINT oauth_client_redirects_shape CHECK (cardinality(redirect_uris) BETWEEN 1 AND 5),
    CONSTRAINT oauth_client_status_known CHECK (status IN ('active', 'pending')),
    CONSTRAINT oauth_client_auth_method_known CHECK (auth_method IN ('client_secret_basic', 'client_secret_post', 'none')),
    CONSTRAINT oauth_client_secret_matches_method CHECK ((auth_method = 'none') = (secret_hash IS NULL))
);

CREATE INDEX oauth_client_pending_idx ON oauth_client (created_at) WHERE status = 'pending';

CREATE TRIGGER oauth_client_set_updated_at BEFORE UPDATE ON oauth_client
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- A grant is a key with a client: one row per person and client, in the
-- person's token list. token_hash is its current access token.
ALTER TABLE api_token
    ADD COLUMN client_id          uuid REFERENCES oauth_client(id) ON DELETE CASCADE,
    ADD COLUMN refresh_hash       bytea UNIQUE,
    ADD COLUMN access_expires_at  timestamptz,
    ADD CONSTRAINT api_token_grant_shape CHECK (
        (client_id IS NULL AND refresh_hash IS NULL AND access_expires_at IS NULL)
        OR (client_id IS NOT NULL AND refresh_hash IS NOT NULL AND access_expires_at IS NOT NULL AND expires_at IS NOT NULL));

CREATE UNIQUE INDEX api_token_one_grant_idx ON api_token (user_id, client_id) WHERE client_id IS NOT NULL;
CREATE INDEX api_token_client_idx ON api_token (client_id) WHERE client_id IS NOT NULL;

-- Nothing a client asks for works before an administrator allowed it.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION api_token_grant_guard() RETURNS trigger AS $$
BEGIN
    IF NEW.client_id IS NOT NULL
       AND NOT EXISTS (SELECT 1 FROM oauth_client WHERE id = NEW.client_id AND status = 'active') THEN
        RAISE EXCEPTION 'a connected app is allowed by an administrator before anybody connects it'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER api_token_grant_check
    BEFORE INSERT OR UPDATE OF client_id ON api_token
    FOR EACH ROW EXECUTE FUNCTION api_token_grant_guard();

-- A refresh token that was rotated away, kept so presenting it again ends the
-- grant it came from (RFC 9700 section 4.14.2).
CREATE TABLE oauth_rotated_refresh (
    refresh_hash bytea PRIMARY KEY,
    grant_id     uuid NOT NULL REFERENCES api_token(id) ON DELETE CASCADE,
    rotated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX oauth_rotated_refresh_grant_idx ON oauth_rotated_refresh (grant_id);
CREATE INDEX oauth_rotated_refresh_age_idx ON oauth_rotated_refresh (rotated_at);

-- An authorization code, single use. A second use ends the grant it made.
CREATE TABLE oauth_code (
    code_hash    bytea PRIMARY KEY,
    client_id    uuid NOT NULL REFERENCES oauth_client(id) ON DELETE CASCADE,
    user_id      uuid NOT NULL REFERENCES app_user(id) ON DELETE CASCADE,
    scopes       text[] NOT NULL,
    redirect_uri text NOT NULL,
    challenge    text NOT NULL,
    resource     text NOT NULL DEFAULT '',
    expires_at   timestamptz NOT NULL,
    used_at      timestamptz,
    grant_id     uuid REFERENCES api_token(id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX oauth_code_expiry_idx ON oauth_code (expires_at);

-- A narrower, shorter token exchanged from a grant's access token. It belongs
-- to the grant and ends with it, without narrowing it.
CREATE TABLE oauth_exchanged_token (
    token_hash bytea PRIMARY KEY,
    grant_id   uuid NOT NULL REFERENCES api_token(id) ON DELETE CASCADE,
    scopes     text[] NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX oauth_exchanged_token_grant_idx ON oauth_exchanged_token (grant_id);
CREATE INDEX oauth_exchanged_token_expiry_idx ON oauth_exchanged_token (expires_at);

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'armature_app') THEN
        GRANT SELECT, INSERT, UPDATE, DELETE ON oauth_client, oauth_code, oauth_rotated_refresh, oauth_exchanged_token
            TO armature_app, armature_admin;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE IF EXISTS oauth_exchanged_token, oauth_code, oauth_rotated_refresh;
DROP TRIGGER IF EXISTS api_token_grant_check ON api_token;
DROP FUNCTION IF EXISTS api_token_grant_guard();
DELETE FROM api_token WHERE client_id IS NOT NULL;
DROP INDEX IF EXISTS api_token_client_idx;
DROP INDEX IF EXISTS api_token_one_grant_idx;
ALTER TABLE api_token DROP CONSTRAINT IF EXISTS api_token_grant_shape,
    DROP COLUMN IF EXISTS access_expires_at,
    DROP COLUMN IF EXISTS refresh_hash,
    DROP COLUMN IF EXISTS client_id;
DROP TABLE IF EXISTS oauth_client;
