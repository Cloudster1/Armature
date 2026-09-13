-- +goose Up
-- A watcher is told about a request. Who added them says whose doing it was;
-- the token, kept as a digest, is the one-click way out that a mail carries.
ALTER TABLE issue_watcher
    ADD COLUMN added_by           uuid REFERENCES app_user(id) ON DELETE SET NULL,
    ADD COLUMN unwatch_token_hash bytea UNIQUE;

-- +goose Down
ALTER TABLE issue_watcher
    DROP COLUMN IF EXISTS unwatch_token_hash,
    DROP COLUMN IF EXISTS added_by;
