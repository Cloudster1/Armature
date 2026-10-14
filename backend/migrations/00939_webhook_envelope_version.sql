-- +goose Up
-- An endpoint keeps the envelope version it was registered for (#242). A
-- delivery keeps the one its body was built in, since the body is frozen when
-- it is queued and a retry has to say what it carries. Every endpoint and
-- delivery so far is version 1, the envelope as it was before it had a number.
ALTER TABLE webhook_endpoint
    ADD COLUMN version integer NOT NULL DEFAULT 1,
    ADD CONSTRAINT webhook_endpoint_version_positive CHECK (version >= 1);

-- NOT VALID spares the log a scan under the lock: every row already there has
-- the default, and every write from now on is held to the check.
ALTER TABLE webhook_delivery
    ADD COLUMN version integer NOT NULL DEFAULT 1,
    ADD CONSTRAINT webhook_delivery_version_positive CHECK (version >= 1) NOT VALID;

-- +goose Down
ALTER TABLE webhook_delivery DROP COLUMN IF EXISTS version;
ALTER TABLE webhook_endpoint DROP COLUMN IF EXISTS version;
