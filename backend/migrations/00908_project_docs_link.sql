-- +goose Up
-- Where a project's documentation lives, one click from its board. A label
-- names the link and means nothing without an address.
ALTER TABLE project
    ADD COLUMN docs_url text,
    ADD COLUMN docs_label text,
    -- The same bounds the service checks, so a row written any other way is
    -- held to them too.
    ADD CONSTRAINT project_docs_url_shape CHECK (docs_url IS NULL OR (docs_url ~* '^https?://[^\s/]+' AND char_length(docs_url) <= 2048)),
    ADD CONSTRAINT project_docs_label_shape CHECK (docs_label IS NULL OR (docs_url IS NOT NULL AND btrim(docs_label) <> '' AND char_length(docs_label) <= 60));

-- +goose Down
ALTER TABLE project DROP COLUMN IF EXISTS docs_label, DROP COLUMN IF EXISTS docs_url;
