-- +goose Up
-- Issue hierarchy: initiatives above epics, epics above stories, subtasks
-- below them.
--
-- The parent column has existed since issues did, but it only ever meant "this
-- is a subtask of that". A real hierarchy needs the levels to be named, so that
-- the question "may this be the parent of that" has one answer that does not
-- depend on which of the two things you happen to be looking at.

-- ------------------------------------------------------- hierarchy level ----

-- Where a type sits in the tree. Zero is the ordinary working level, where
-- stories, tasks and bugs live and where almost every issue is created;
-- everything else is expressed relative to it, so adding a level above the top
-- never renumbers what is already there.
--
--     2  Initiative
--     1  Epic
--     0  Story, Task, Bug
--    -1  Subtask
ALTER TABLE issue_type ADD COLUMN hierarchy_level smallint NOT NULL DEFAULT 0;

ALTER TABLE issue_type ADD CONSTRAINT issue_type_hierarchy_level_range
    CHECK (hierarchy_level BETWEEN -1 AND 3);

-- Existing types keep their meaning: anything flagged as a subtask goes below,
-- anything named Epic goes above, and the rest stay where they are.
UPDATE issue_type SET hierarchy_level = -1 WHERE is_subtask;
UPDATE issue_type SET hierarchy_level = 1 WHERE NOT is_subtask AND lower(name) = 'epic';

-- The flag is now a reading of the level rather than a second opinion about it.
-- Two independent columns that must agree eventually do not, and the disagreement
-- surfaces as an issue that is a subtask on one screen and not on another.
ALTER TABLE issue_type DROP COLUMN is_subtask;
ALTER TABLE issue_type ADD COLUMN is_subtask boolean
    GENERATED ALWAYS AS (hierarchy_level < 0) STORED;

CREATE INDEX issue_type_hierarchy_idx ON issue_type (org_id, hierarchy_level, position);

-- Every organization gains the level above Epic. It is empty until somebody
-- uses it, and a hierarchy whose top level has to be created by hand before it
-- can be seen is a hierarchy nobody discovers.
INSERT INTO issue_type (org_id, name, description, icon, hierarchy_level, position)
SELECT o.id, 'Initiative', 'A goal several epics add up to.', 'initiative', 2, 5
FROM org o
WHERE NOT EXISTS (
    SELECT 1 FROM issue_type t WHERE t.org_id = o.id AND lower(t.name) = 'initiative'
);

-- --------------------------------------------------------- the tree rule ----

-- A parent sits exactly one level above its child, in the same project.
--
-- "Exactly one" rather than "somewhere above" is what makes the tree
-- predictable: a story's parent is always an epic, so a roll-up never has to
-- ask how many levels it skipped, and a level can never be quietly bypassed by
-- hanging a task straight off an initiative. It also makes a cycle impossible
-- to express, since every step towards a parent strictly increases the level
-- and the levels are bounded.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_hierarchy_guard() RETURNS trigger AS $$
DECLARE
    child_level  smallint;
    child_type   text;
    parent_level smallint;
    parent_type  text;
    parent_proj  uuid;
BEGIN
    SELECT hierarchy_level, name INTO child_level, child_type
    FROM issue_type WHERE id = NEW.issue_type_id;

    IF NEW.parent_id IS NULL THEN
        IF child_level < 0 THEN
            RAISE EXCEPTION 'a % only exists underneath another issue', lower(child_type)
                USING ERRCODE = 'check_violation';
        END IF;
    ELSE
        -- Row level security applies to this lookup, so a parent in another
        -- organization is not merely refused: it is not there at all.
        SELECT t.hierarchy_level, t.name, i.project_id
        INTO parent_level, parent_type, parent_proj
        FROM issue i JOIN issue_type t ON t.id = i.issue_type_id
        WHERE i.id = NEW.parent_id;

        IF NOT FOUND THEN
            RAISE EXCEPTION 'that parent issue does not exist'
                USING ERRCODE = 'foreign_key_violation';
        END IF;
        IF parent_proj <> NEW.project_id THEN
            RAISE EXCEPTION 'a parent has to be in the same project as its child'
                USING ERRCODE = 'check_violation';
        END IF;
        IF parent_level <> child_level + 1 THEN
            RAISE EXCEPTION 'a % cannot be the parent of a %', lower(parent_type), lower(child_type)
                USING ERRCODE = 'check_violation';
        END IF;
    END IF;

    -- Changing an issue's own type moves it in the tree, which can strand the
    -- children hanging off it. They are not part of this statement, so nothing
    -- else would notice.
    IF TG_OP = 'UPDATE' AND NEW.issue_type_id IS DISTINCT FROM OLD.issue_type_id
       AND EXISTS (
           SELECT 1 FROM issue c
           JOIN issue_type ct ON ct.id = c.issue_type_id
           WHERE c.parent_id = NEW.id AND ct.hierarchy_level <> child_level - 1)
    THEN
        RAISE EXCEPTION 'this issue has children that a % cannot have', lower(child_type)
            USING ERRCODE = 'check_violation';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issue_hierarchy_check
    BEFORE INSERT OR UPDATE OF parent_id, issue_type_id, project_id ON issue
    FOR EACH ROW EXECUTE FUNCTION issue_hierarchy_guard();

-- Moving a whole type up or down the tree would break every issue already using
-- it, one level at a time and silently. Renaming, re-describing and reordering
-- a type stay free.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION issue_type_level_guard() RETURNS trigger AS $$
BEGIN
    IF NEW.hierarchy_level <> OLD.hierarchy_level
       AND EXISTS (SELECT 1 FROM issue WHERE issue_type_id = NEW.id)
    THEN
        RAISE EXCEPTION 'issues already use the % type, so its level cannot change', OLD.name
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER issue_type_level_check
    BEFORE UPDATE OF hierarchy_level ON issue_type
    FOR EACH ROW EXECUTE FUNCTION issue_type_level_guard();

-- Reading an epic's children, and a project's whole tree, are the two queries
-- the hierarchy exists to answer.
CREATE INDEX issue_parent_project_idx ON issue (project_id, parent_id);

-- +goose Down
DROP TRIGGER IF EXISTS issue_type_level_check ON issue_type;
DROP TRIGGER IF EXISTS issue_hierarchy_check ON issue;
DROP FUNCTION IF EXISTS issue_type_level_guard();
DROP FUNCTION IF EXISTS issue_hierarchy_guard();
DROP INDEX IF EXISTS issue_parent_project_idx;
DROP INDEX IF EXISTS issue_type_hierarchy_idx;

DELETE FROM issue_type WHERE lower(name) = 'initiative'
    AND NOT EXISTS (SELECT 1 FROM issue WHERE issue_type_id = issue_type.id);

ALTER TABLE issue_type DROP COLUMN is_subtask;
ALTER TABLE issue_type ADD COLUMN is_subtask boolean NOT NULL DEFAULT false;
UPDATE issue_type SET is_subtask = true WHERE hierarchy_level < 0;
ALTER TABLE issue_type DROP CONSTRAINT issue_type_hierarchy_level_range;
ALTER TABLE issue_type DROP COLUMN hierarchy_level;
