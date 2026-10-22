-- +goose Up
-- A partner opens pull requests and, through the git proxy, pushes only to a
-- branch made for an issue (#332, #333). The default branch is never one: it
-- takes work through a pull request alone, so the database refuses mapping it
-- to an issue, and a repository whose default branch moves lets go of it.

UPDATE git_branch b SET issue_id = NULL
FROM git_repository r
WHERE r.id = b.repository_id AND b.name = r.default_branch AND b.issue_id IS NOT NULL;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION git_branch_not_default_guard() RETURNS trigger AS $$
BEGIN
    IF NEW.issue_id IS NOT NULL AND EXISTS (
        SELECT 1 FROM git_repository WHERE id = NEW.repository_id AND default_branch = NEW.name
    ) THEN
        RAISE EXCEPTION 'the default branch of a repository is never an issue''s branch'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER git_branch_not_default_check
    BEFORE INSERT OR UPDATE OF name, issue_id, repository_id ON git_branch
    FOR EACH ROW EXECUTE FUNCTION git_branch_not_default_guard();

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION git_repository_frees_default_branch() RETURNS trigger AS $$
BEGIN
    IF NEW.default_branch IS DISTINCT FROM OLD.default_branch THEN
        UPDATE git_branch SET issue_id = NULL
        WHERE repository_id = NEW.id AND name = NEW.default_branch AND issue_id IS NOT NULL;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE TRIGGER git_repository_frees_default_branch AFTER UPDATE OF default_branch ON git_repository
    FOR EACH ROW EXECUTE FUNCTION git_repository_frees_default_branch();

-- +goose Down
DROP TRIGGER IF EXISTS git_repository_frees_default_branch ON git_repository;
DROP FUNCTION IF EXISTS git_repository_frees_default_branch();
DROP TRIGGER IF EXISTS git_branch_not_default_check ON git_branch;
DROP FUNCTION IF EXISTS git_branch_not_default_guard();
