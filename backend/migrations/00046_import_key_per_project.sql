-- +goose Up
-- The name another tracker gave a row identifies it inside the project it was
-- imported into. Per organization was wrong for the reason it was written down:
-- two projects may both be told the same file, and the second must make its own
-- issues rather than correct the first one's.
DROP INDEX issue_external_key_idx;
CREATE UNIQUE INDEX issue_external_key_idx ON issue (project_id, external_key);

-- +goose Down
DROP INDEX issue_external_key_idx;
CREATE UNIQUE INDEX issue_external_key_idx ON issue (org_id, external_key);
