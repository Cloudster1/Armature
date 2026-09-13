-- +goose Up
-- A request type is the desk's template: it gains the category it is offered
-- under, the details a requester starts from, and the team the request lands
-- with. Columns rather than a second table, because a template that was not a
-- request type would have to be one before it could be raised through.
ALTER TABLE request_type
    ADD COLUMN category         text NOT NULL DEFAULT '',
    ADD COLUMN details_template text NOT NULL DEFAULT '',
    ADD COLUMN team_id          uuid REFERENCES team(id) ON DELETE SET NULL;

CREATE INDEX request_type_team_idx ON request_type (team_id) WHERE team_id IS NOT NULL;

-- The service checks that the team is the project's; so does the row, since a
-- request type written straight through SQL must not be able to route a
-- project's requests to another project's team.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION request_type_team_guard() RETURNS trigger AS $$
DECLARE
    team_project uuid;
BEGIN
    IF NEW.team_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT project_id INTO team_project FROM team WHERE id = NEW.team_id;
    IF team_project IS DISTINCT FROM NEW.project_id THEN
        RAISE EXCEPTION 'that team belongs to another project'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER request_type_team_check
    BEFORE INSERT OR UPDATE OF team_id, project_id ON request_type
    FOR EACH ROW EXECUTE FUNCTION request_type_team_guard();

-- +goose Down
DROP TRIGGER IF EXISTS request_type_team_check ON request_type;
DROP FUNCTION IF EXISTS request_type_team_guard();
DROP INDEX IF EXISTS request_type_team_idx;
ALTER TABLE request_type
    DROP COLUMN IF EXISTS team_id,
    DROP COLUMN IF EXISTS details_template,
    DROP COLUMN IF EXISTS category;
