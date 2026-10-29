-- +goose Up
-- A sign-in through the provider proves the browser that asked is the one
-- that comes back, with PKCE, and may be tried silently first (#325). The
-- verifier and whether the attempt was silent are kept with its state.
ALTER TABLE oidc_login ADD COLUMN code_verifier text NOT NULL DEFAULT '';
ALTER TABLE oidc_login ADD COLUMN silent boolean NOT NULL DEFAULT false;

-- A session from the provider keeps the id token it came with, so signing out
-- can end the provider's session too; it goes when the session does.
ALTER TABLE user_session ADD COLUMN id_token text;

-- +goose Down
ALTER TABLE user_session DROP COLUMN id_token;
ALTER TABLE oidc_login DROP COLUMN silent;
ALTER TABLE oidc_login DROP COLUMN code_verifier;
