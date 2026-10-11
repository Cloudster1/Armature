-- +goose Up
-- An out-of-office to a request is recorded as a machine's mail rather than
-- posted, so the log can say why the request went on waiting. The table, its
-- policies and its grants are as 00028 left them; only the outcomes grow.
ALTER TABLE inbound_mail DROP CONSTRAINT inbound_mail_outcome_check;
ALTER TABLE inbound_mail ADD CONSTRAINT inbound_mail_outcome_check
    CHECK (outcome IN ('processing', 'commented', 'refused', 'unmatched', 'ambiguous', 'empty', 'automatic'));

-- +goose Down
DELETE FROM inbound_mail WHERE outcome = 'automatic';
ALTER TABLE inbound_mail DROP CONSTRAINT inbound_mail_outcome_check;
ALTER TABLE inbound_mail ADD CONSTRAINT inbound_mail_outcome_check
    CHECK (outcome IN ('processing', 'commented', 'refused', 'unmatched', 'ambiguous', 'empty'));
