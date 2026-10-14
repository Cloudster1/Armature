-- +goose Up
-- What a link is to its issue, so a requirement it implements shows apart from
-- a page that documents it (#223). Every link before this was a page.
ALTER TABLE issue_remote_link
    ADD COLUMN relation text NOT NULL DEFAULT 'page',
    ADD CONSTRAINT issue_remote_link_relation_known CHECK (relation IN ('page', 'requirement'));

-- +goose Down
ALTER TABLE issue_remote_link
    DROP CONSTRAINT IF EXISTS issue_remote_link_relation_known,
    DROP COLUMN IF EXISTS relation;
