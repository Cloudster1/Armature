-- +goose Up
-- A manager may change only the absences they recorded, so who recorded one is
-- what decides; it is written once and never handed to somebody else. It goes
-- to nobody only when that account goes, which the foreign key does.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION absence_recorder_kept() RETURNS trigger AS $$
BEGIN
    IF NEW.created_by IS DISTINCT FROM OLD.created_by AND NEW.created_by IS NOT NULL THEN
        RAISE EXCEPTION 'who recorded an absence does not change; record a new one instead'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER absence_recorder_kept
    BEFORE UPDATE OF created_by ON absence
    FOR EACH ROW EXECUTE FUNCTION absence_recorder_kept();

-- The person is told when somebody else records, changes or removes their
-- days, which is a reason of its own to hear and to turn off.
ALTER TABLE notification DROP CONSTRAINT notification_kind_known;
ALTER TABLE notification ADD CONSTRAINT notification_kind_known CHECK (kind IN
    ('assigned', 'mentioned', 'commented', 'transitioned', 'watching', 'sla_breached', 'rule', 'filter', 'absence'));

-- +goose Down
DELETE FROM notification WHERE kind = 'absence';
ALTER TABLE notification DROP CONSTRAINT notification_kind_known;
ALTER TABLE notification ADD CONSTRAINT notification_kind_known CHECK (kind IN
    ('assigned', 'mentioned', 'commented', 'transitioned', 'watching', 'sla_breached', 'rule', 'filter'));
DROP TRIGGER IF EXISTS absence_recorder_kept ON absence;
DROP FUNCTION IF EXISTS absence_recorder_kept();
