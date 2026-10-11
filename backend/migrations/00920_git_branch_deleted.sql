-- +goose Up
-- Hosts delete a merged branch on their own, and the merge is what its issue
-- needs to remember, so a merged branch the host deletes is kept and marked
-- deleted. An unmerged one is still removed: nothing about it is worth more
-- than the commits, which stay linked. A branch of the same name pushed later
-- is a new branch, so a name is unique only among the branches still there.

ALTER TABLE git_branch ADD COLUMN deleted_at timestamptz;

ALTER TABLE git_branch ADD CONSTRAINT git_branch_deleted_only_merged
    CHECK (deleted_at IS NULL OR merged_at IS NOT NULL);

DROP INDEX git_branch_repository_name_idx;
CREATE UNIQUE INDEX git_branch_repository_name_idx ON git_branch (repository_id, name) WHERE deleted_at IS NULL;

-- +goose Down
-- The old index allows one row per name, so the branches kept after deletion
-- go first.
DELETE FROM git_branch WHERE deleted_at IS NOT NULL;
DROP INDEX IF EXISTS git_branch_repository_name_idx;
CREATE UNIQUE INDEX git_branch_repository_name_idx ON git_branch (repository_id, name);
ALTER TABLE git_branch
    DROP CONSTRAINT IF EXISTS git_branch_deleted_only_merged,
    DROP COLUMN IF EXISTS deleted_at;
