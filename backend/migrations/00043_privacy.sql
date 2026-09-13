-- +goose Up
-- An erased person keeps their row as a tombstone so what they wrote stays
-- attributed; the date says the identity is gone.
ALTER TABLE app_user ADD COLUMN erased_at timestamptz;

-- The retention sweeps delete by age; without these each pass reads whole
-- tables.
CREATE INDEX IF NOT EXISTS notification_created_idx ON notification (created_at);
CREATE INDEX IF NOT EXISTS outbox_event_published_idx ON outbox_event (published_at) WHERE published_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS webhook_delivery_created_idx ON webhook_delivery (created_at);
CREATE INDEX IF NOT EXISTS automation_run_started_idx ON automation_run (started_at);
CREATE INDEX IF NOT EXISTS inbound_mail_received_idx ON inbound_mail (received_at);
CREATE INDEX IF NOT EXISTS import_job_created_idx ON import_job (created_at);
CREATE INDEX IF NOT EXISTS org_invite_expiry_idx ON org_invite (expires_at);
CREATE INDEX IF NOT EXISTS api_token_expiry_idx ON api_token (expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS portal_code_expiry_idx ON portal_code (expires_at);

-- +goose Down
DROP INDEX IF EXISTS portal_code_expiry_idx;
DROP INDEX IF EXISTS api_token_expiry_idx;
DROP INDEX IF EXISTS org_invite_expiry_idx;
DROP INDEX IF EXISTS import_job_created_idx;
DROP INDEX IF EXISTS inbound_mail_received_idx;
DROP INDEX IF EXISTS automation_run_started_idx;
DROP INDEX IF EXISTS webhook_delivery_created_idx;
DROP INDEX IF EXISTS outbox_event_published_idx;
DROP INDEX IF EXISTS notification_created_idx;
ALTER TABLE app_user DROP COLUMN IF EXISTS erased_at;
