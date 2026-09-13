-- +goose Up
-- A branch made for an issue is followed from then on: each push moves its
-- head and its count, and merging the pull request from it marks it merged.
-- Gitea joins the hosts, so a stack of this product can carry a git host of
-- its own.

ALTER TYPE git_host ADD VALUE IF NOT EXISTS 'gitea';

ALTER TABLE git_branch
    ADD COLUMN head_sha     text NOT NULL DEFAULT '',
    ADD COLUMN commit_count integer NOT NULL DEFAULT 0,
    ADD COLUMN pushed_at    timestamptz,
    ADD COLUMN merged_at    timestamptz,
    ADD COLUMN updated_at   timestamptz NOT NULL DEFAULT now();

ALTER TABLE git_branch ADD CONSTRAINT git_branch_head_shape CHECK (head_sha = '' OR head_sha ~ '^[0-9a-f]{7,64}$');

CREATE TRIGGER git_branch_set_updated_at BEFORE UPDATE ON git_branch
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TRIGGER IF EXISTS git_branch_set_updated_at ON git_branch;
ALTER TABLE git_branch
    DROP CONSTRAINT IF EXISTS git_branch_head_shape,
    DROP COLUMN IF EXISTS head_sha,
    DROP COLUMN IF EXISTS commit_count,
    DROP COLUMN IF EXISTS pushed_at,
    DROP COLUMN IF EXISTS merged_at,
    DROP COLUMN IF EXISTS updated_at;
