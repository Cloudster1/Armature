-- +goose Up
-- Each project allows its own issue types (#465). Every existing project allows
-- every type, in the order the library lists them, so nothing changes for it.
CREATE TABLE project_issue_type (
    project_id    uuid NOT NULL REFERENCES project (id) ON DELETE CASCADE,
    issue_type_id uuid NOT NULL REFERENCES issue_type (id) ON DELETE CASCADE,
    position      int  NOT NULL DEFAULT 0,
    PRIMARY KEY (project_id, issue_type_id)
);
CREATE INDEX project_issue_type_type_idx ON project_issue_type (issue_type_id);

INSERT INTO project_issue_type (project_id, issue_type_id, position)
SELECT p.id, t.id, row_number() OVER (PARTITION BY p.id ORDER BY t.hierarchy_level DESC, t.position, t.name)
FROM project p CROSS JOIN issue_type t;

-- An issue and a request type keep to their project's types. No action rather
-- than restrict, so a project's delete cascades through both before the check.
CREATE INDEX issue_project_type_idx ON issue (project_id, issue_type_id);
CREATE INDEX request_type_project_type_idx ON request_type (project_id, issue_type_id);
ALTER TABLE issue ADD CONSTRAINT issue_project_type_fkey FOREIGN KEY (project_id, issue_type_id)
    REFERENCES project_issue_type (project_id, issue_type_id) NOT VALID;
ALTER TABLE issue VALIDATE CONSTRAINT issue_project_type_fkey;
ALTER TABLE request_type ADD CONSTRAINT request_type_project_type_fkey FOREIGN KEY (project_id, issue_type_id)
    REFERENCES project_issue_type (project_id, issue_type_id) NOT VALID;
ALTER TABLE request_type VALIDATE CONSTRAINT request_type_project_type_fkey;

-- A retired type stays where it is and goes nowhere new.
-- +goose StatementBegin
CREATE FUNCTION issue_type_not_retired() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    retired_name text;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.issue_type_id IS NOT DISTINCT FROM OLD.issue_type_id THEN
        RETURN NEW;
    END IF;
    SELECT name INTO retired_name FROM issue_type WHERE id = NEW.issue_type_id AND retired_at IS NOT NULL;
    IF FOUND THEN
        RAISE EXCEPTION 'the % type is retired and is offered for nothing new', retired_name
            USING ERRCODE = 'check_violation', CONSTRAINT = 'issue_type_not_retired';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER issue_type_not_retired
    BEFORE INSERT OR UPDATE OF issue_type_id ON issue
    FOR EACH ROW EXECUTE FUNCTION issue_type_not_retired();
CREATE TRIGGER project_issue_type_not_retired
    BEFORE INSERT OR UPDATE OF issue_type_id ON project_issue_type
    FOR EACH ROW EXECUTE FUNCTION issue_type_not_retired();

-- A new project starts with every built-in type that is not retired.
-- +goose StatementBegin
CREATE FUNCTION project_starts_with_builtin_types() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO project_issue_type (project_id, issue_type_id, position)
    SELECT NEW.id, t.id, row_number() OVER (ORDER BY t.hierarchy_level DESC, t.position, t.name)
    FROM issue_type t
    WHERE t.builtin AND t.retired_at IS NULL;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER project_starts_with_builtin_types
    AFTER INSERT ON project
    FOR EACH ROW EXECUTE FUNCTION project_starts_with_builtin_types();

-- Every project keeps a standard (level 0) type that is not retired, which is
-- what an issue filed without a type becomes. Checked at commit, so a set can
-- be replaced in one transaction; a project being deleted is not held to it.
-- +goose StatementBegin
CREATE FUNCTION project_keeps_a_standard_type() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    lacking text;
BEGIN
    IF TG_TABLE_NAME = 'project_issue_type' THEN
        SELECT p.key INTO lacking FROM project p
        WHERE p.id IN (OLD.project_id, NEW.project_id)
          AND NOT EXISTS (
              SELECT 1 FROM project_issue_type pit JOIN issue_type t ON t.id = pit.issue_type_id
              WHERE pit.project_id = p.id AND t.hierarchy_level = 0 AND t.retired_at IS NULL)
        LIMIT 1;
    ELSE
        SELECT p.key INTO lacking FROM project p
        WHERE EXISTS (SELECT 1 FROM project_issue_type WHERE project_id = p.id AND issue_type_id = NEW.id)
          AND NOT EXISTS (
              SELECT 1 FROM project_issue_type pit JOIN issue_type t ON t.id = pit.issue_type_id
              WHERE pit.project_id = p.id AND t.hierarchy_level = 0 AND t.retired_at IS NULL)
        LIMIT 1;
    END IF;
    IF lacking IS NOT NULL THEN
        RAISE EXCEPTION '% would be left without a standard issue type that is not retired', lacking
            USING ERRCODE = 'check_violation', CONSTRAINT = 'project_keeps_a_standard_type';
    END IF;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

CREATE CONSTRAINT TRIGGER project_keeps_a_standard_type
    AFTER INSERT OR UPDATE OR DELETE ON project_issue_type
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION project_keeps_a_standard_type();
CREATE CONSTRAINT TRIGGER issue_type_keeps_projects_a_standard_type
    AFTER UPDATE OF retired_at, hierarchy_level ON issue_type
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION project_keeps_a_standard_type();

-- +goose Down
DROP TRIGGER IF EXISTS issue_type_keeps_projects_a_standard_type ON issue_type;
DROP TRIGGER IF EXISTS project_keeps_a_standard_type ON project_issue_type;
DROP FUNCTION IF EXISTS project_keeps_a_standard_type();
DROP TRIGGER IF EXISTS project_starts_with_builtin_types ON project;
DROP FUNCTION IF EXISTS project_starts_with_builtin_types();
DROP TRIGGER IF EXISTS issue_type_not_retired ON issue;
DROP TRIGGER IF EXISTS project_issue_type_not_retired ON project_issue_type;
DROP FUNCTION IF EXISTS issue_type_not_retired();
ALTER TABLE request_type DROP CONSTRAINT IF EXISTS request_type_project_type_fkey;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_project_type_fkey;
DROP INDEX IF EXISTS request_type_project_type_idx;
DROP INDEX IF EXISTS issue_project_type_idx;
DROP TABLE IF EXISTS project_issue_type;
