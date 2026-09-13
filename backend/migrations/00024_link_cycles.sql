-- +goose Up
-- A blocks link that comes back around to where it started says that an issue
-- waits on itself, which is nonsense, and both ends can now be joined from the
-- plan by dragging. The service refuses it with a sentence; this refuses the
-- same thing for a row written any other way. Relates and duplicates are
-- symmetric, so a triangle of those is fine and only blocks is walked.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_link_cycle_guard() RETURNS trigger AS $$
DECLARE
    type_name text;
    comes_back boolean;
BEGIN
    SELECT name INTO type_name FROM issue_link_type WHERE id = NEW.link_type_id;
    IF lower(type_name) <> 'blocks' THEN
        RETURN NEW;
    END IF;
    WITH RECURSIVE downstream AS (
        SELECT l.target_id, ARRAY[l.target_id] AS path
        FROM issue_link l
        WHERE l.link_type_id = NEW.link_type_id AND l.source_id = NEW.target_id
        UNION ALL
        SELECT l.target_id, d.path || l.target_id
        FROM issue_link l
        JOIN downstream d ON l.source_id = d.target_id
        WHERE l.link_type_id = NEW.link_type_id
          AND array_length(d.path, 1) < 32
          AND NOT l.target_id = ANY (d.path)
    )
    SELECT EXISTS (SELECT 1 FROM downstream WHERE target_id = NEW.source_id) INTO comes_back;
    IF comes_back THEN
        RAISE EXCEPTION 'a blocks link cannot come back around to where it started'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issue_link_cycle_check
    BEFORE INSERT OR UPDATE ON issue_link
    FOR EACH ROW EXECUTE FUNCTION issue_link_cycle_guard();

-- +goose Down
DROP TRIGGER IF EXISTS issue_link_cycle_check ON issue_link;
DROP FUNCTION IF EXISTS issue_link_cycle_guard();
