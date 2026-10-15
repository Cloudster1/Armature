-- +goose Up
-- A desk may name the request type a new mail to its address becomes (#72);
-- without one, mail naming no request is answered as before and raises nothing.
ALTER TABLE project
    ADD COLUMN mail_request_type_id uuid REFERENCES request_type(id) ON DELETE SET NULL;

-- A row written straight through SQL must not have a desk raise mail through
-- another project's request type, nor a project that is no desk raise any.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION project_mail_request_type_guard() RETURNS trigger AS $$
DECLARE
    type_project uuid;
BEGIN
    IF NEW.mail_request_type_id IS NULL THEN
        RETURN NEW;
    END IF;
    IF NEW.kind <> 'service' THEN
        RAISE EXCEPTION 'only a service desk raises requests by mail'
            USING ERRCODE = 'check_violation';
    END IF;
    SELECT project_id INTO type_project FROM request_type WHERE id = NEW.mail_request_type_id;
    IF type_project IS DISTINCT FROM NEW.id THEN
        RAISE EXCEPTION 'that request type belongs to another project'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER project_mail_request_type_check
    BEFORE INSERT OR UPDATE OF mail_request_type_id, kind ON project
    FOR EACH ROW EXECUTE FUNCTION project_mail_request_type_guard();

ALTER TABLE inbound_mail DROP CONSTRAINT inbound_mail_outcome_check;
ALTER TABLE inbound_mail ADD CONSTRAINT inbound_mail_outcome_check
    CHECK (outcome IN ('processing', 'failed', 'commented', 'raised', 'refused', 'unmatched', 'empty', 'automatic'));

-- +goose Down
-- A raised mail made its request; to the old reader it was a comment's kind of done.
UPDATE inbound_mail SET outcome = 'commented' WHERE outcome = 'raised';
ALTER TABLE inbound_mail DROP CONSTRAINT inbound_mail_outcome_check;
ALTER TABLE inbound_mail ADD CONSTRAINT inbound_mail_outcome_check
    CHECK (outcome IN ('processing', 'failed', 'commented', 'refused', 'unmatched', 'empty', 'automatic'));
DROP TRIGGER IF EXISTS project_mail_request_type_check ON project;
DROP FUNCTION IF EXISTS project_mail_request_type_guard();
ALTER TABLE project DROP COLUMN IF EXISTS mail_request_type_id;
