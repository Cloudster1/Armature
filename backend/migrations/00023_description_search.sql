-- +goose Up
-- A query can look for words in a description as well as a summary. The
-- description is a rich text document, so the words are read out of its
-- string nodes; indexing that expression is what keeps "text ~ login" from
-- reading every document in the organization.
CREATE INDEX issue_description_fts_idx ON issue
    USING gin (jsonb_to_tsvector('simple', description, '["string"]'));

-- +goose Down
DROP INDEX IF EXISTS issue_description_fts_idx;
