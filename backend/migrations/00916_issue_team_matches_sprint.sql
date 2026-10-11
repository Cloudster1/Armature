-- +goose Up
-- A sprint plans only its own team's work: an issue of another team in it
-- shows on neither team's Sprints page and still counts in the sprint.

-- Work already in another stream's open sprint goes to its own team's backlog,
-- with the changelog saying so. Row security is lifted for the repair because
-- migrations may run as a plain owner, which forced security shows no rows.
ALTER TABLE issue NO FORCE ROW LEVEL SECURITY;
ALTER TABLE sprint NO FORCE ROW LEVEL SECURITY;
ALTER TABLE issue_history NO FORCE ROW LEVEL SECURITY;

WITH strays AS (
    UPDATE issue i SET sprint_id = NULL
      FROM sprint s
     WHERE s.id = i.sprint_id
       AND s.state <> 'closed'
       AND s.team_id IS DISTINCT FROM i.team_id
    RETURNING i.id, i.org_id, s.name
)
INSERT INTO issue_history (org_id, issue_id, actor_id, changes)
SELECT org_id, id, NULL, jsonb_build_array(jsonb_build_object('field', 'sprint', 'from', name))
  FROM strays;

ALTER TABLE issue_history FORCE ROW LEVEL SECURITY;
ALTER TABLE sprint FORCE ROW LEVEL SECURITY;
ALTER TABLE issue FORCE ROW LEVEL SECURITY;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_sprint_guard() RETURNS trigger AS $$
DECLARE
    sprint_project uuid;
    sprint_org     uuid;
    sprint_team    uuid;
    joined_state   sprint_state;
BEGIN
    IF NEW.sprint_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT project_id, org_id, state, team_id INTO sprint_project, sprint_org, joined_state, sprint_team
      FROM sprint WHERE id = NEW.sprint_id;

    IF sprint_org IS DISTINCT FROM NEW.org_id OR sprint_project IS DISTINCT FROM NEW.project_id THEN
        RAISE EXCEPTION 'an issue can only join a sprint in its own project'
            USING ERRCODE = 'check_violation';
    END IF;
    -- Work cannot be added to a sprint that is already over; the report on it
    -- has been written.
    IF joined_state = 'closed' AND (TG_OP = 'INSERT' OR OLD.sprint_id IS DISTINCT FROM NEW.sprint_id) THEN
        RAISE EXCEPTION 'that sprint is over'
            USING ERRCODE = 'check_violation';
    END IF;
    -- A closed sprint is history and keeps its work whoever carries it now. A
    -- team already gone is a delete still cascading, where both sides clear.
    IF joined_state <> 'closed' AND sprint_team IS DISTINCT FROM NEW.team_id
       AND (sprint_team IS NULL OR EXISTS (SELECT 1 FROM team WHERE id = sprint_team)) THEN
        RAISE EXCEPTION 'an issue can only be in a sprint its own team plans'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

DROP TRIGGER issue_sprint_check ON issue;
CREATE TRIGGER issue_sprint_check
    BEFORE INSERT OR UPDATE OF sprint_id, team_id, project_id, org_id ON issue
    FOR EACH ROW EXECUTE FUNCTION issue_sprint_guard();

-- +goose Down
DROP TRIGGER IF EXISTS issue_sprint_check ON issue;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_sprint_guard() RETURNS trigger AS $$
DECLARE
    sprint_project uuid;
    sprint_org     uuid;
    joined_state   sprint_state;
BEGIN
    IF NEW.sprint_id IS NULL THEN
        RETURN NEW;
    END IF;
    SELECT project_id, org_id, state INTO sprint_project, sprint_org, joined_state
      FROM sprint WHERE id = NEW.sprint_id;

    IF sprint_org IS DISTINCT FROM NEW.org_id OR sprint_project IS DISTINCT FROM NEW.project_id THEN
        RAISE EXCEPTION 'an issue can only join a sprint in its own project'
            USING ERRCODE = 'check_violation';
    END IF;
    IF joined_state = 'closed' AND (TG_OP = 'INSERT' OR OLD.sprint_id IS DISTINCT FROM NEW.sprint_id) THEN
        RAISE EXCEPTION 'that sprint is over'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issue_sprint_check
    BEFORE INSERT OR UPDATE OF sprint_id, project_id, org_id ON issue
    FOR EACH ROW EXECUTE FUNCTION issue_sprint_guard();
