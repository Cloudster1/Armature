-- +goose Up
-- What another tracker called a row. An import that runs twice corrects what
-- it made rather than making it again, and a key is unique per organization
-- because the name comes from outside and two projects may both be told it.
ALTER TABLE issue ADD COLUMN external_key text;
CREATE UNIQUE INDEX issue_external_key_idx ON issue (org_id, external_key);

ALTER TABLE issue_comment ADD COLUMN external_key text;
CREATE UNIQUE INDEX issue_comment_external_key_idx ON issue_comment (org_id, external_key);

ALTER TABLE issue_worklog ADD COLUMN external_key text;
CREATE UNIQUE INDEX issue_worklog_external_key_idx ON issue_worklog (org_id, external_key);

-- An import is reproducing a record and says when its rows last changed; every
-- other edit is still stamped with the time it happened.
DROP TRIGGER issue_set_updated_at ON issue;
CREATE TRIGGER issue_set_updated_at BEFORE UPDATE ON issue
    FOR EACH ROW WHEN (NEW.updated_at IS NOT DISTINCT FROM OLD.updated_at)
    EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TRIGGER issue_set_updated_at ON issue;
CREATE TRIGGER issue_set_updated_at BEFORE UPDATE ON issue
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
DROP INDEX IF EXISTS issue_worklog_external_key_idx;
ALTER TABLE issue_worklog DROP COLUMN IF EXISTS external_key;
DROP INDEX IF EXISTS issue_comment_external_key_idx;
ALTER TABLE issue_comment DROP COLUMN IF EXISTS external_key;
DROP INDEX IF EXISTS issue_external_key_idx;
ALTER TABLE issue DROP COLUMN IF EXISTS external_key;
