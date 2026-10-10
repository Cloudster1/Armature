-- +goose Up
-- A mail the desk could not handle is tried again, so its row counts the tries
-- and says when the desk gave up; the body stays in the box either way.
ALTER TABLE inbound_mail ADD COLUMN attempts integer NOT NULL DEFAULT 1;
ALTER TABLE inbound_mail ADD CONSTRAINT inbound_mail_attempts_check CHECK (attempts > 0);
ALTER TABLE inbound_mail DROP CONSTRAINT inbound_mail_outcome_check;
ALTER TABLE inbound_mail ADD CONSTRAINT inbound_mail_outcome_check
    CHECK (outcome IN ('processing', 'failed', 'commented', 'refused', 'unmatched', 'ambiguous', 'empty'));

-- +goose Down
-- A failed row is taken up again by the old reader only as processing.
UPDATE inbound_mail SET outcome = 'processing' WHERE outcome = 'failed';
ALTER TABLE inbound_mail DROP CONSTRAINT inbound_mail_outcome_check;
ALTER TABLE inbound_mail ADD CONSTRAINT inbound_mail_outcome_check
    CHECK (outcome IN ('processing', 'commented', 'refused', 'unmatched', 'ambiguous', 'empty'));
ALTER TABLE inbound_mail DROP CONSTRAINT inbound_mail_attempts_check;
ALTER TABLE inbound_mail DROP COLUMN attempts;
