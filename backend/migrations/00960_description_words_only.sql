-- +goose Up
-- A description is searched by the start of its words (#293), so only what
-- people wrote may be indexed: the text nodes. Reading every string of the
-- document also indexed node names and link addresses, and "text ~ p" would
-- then have matched every issue through "paragraph".
DROP INDEX IF EXISTS issue_description_fts_idx;
CREATE INDEX issue_description_words_idx ON issue
    USING gin (jsonb_to_tsvector('simple', jsonb_path_query_array(description, 'strict $.**.text'), '["string"]'));

-- +goose Down
DROP INDEX IF EXISTS issue_description_words_idx;
CREATE INDEX issue_description_fts_idx ON issue
    USING gin (jsonb_to_tsvector('simple', description, '["string"]'));
