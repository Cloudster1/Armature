package git

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
)

// forgetDeleted keeps a merged branch marked deleted, since hosts delete one on
// merging and its issue needs the merge; an unmerged one goes, commits linked.
func forgetDeleted(ctx context.Context, tx db.DBTX, repoID uuid.UUID, names []string) error {
	for _, name := range names {
		if _, err := tx.Exec(ctx, `
			DELETE FROM git_branch
			WHERE repository_id = $1 AND name = $2 AND deleted_at IS NULL AND merged_at IS NULL`, repoID, name); err != nil {
			return fmt.Errorf("forget branch %q: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE git_branch SET deleted_at = now()
			WHERE repository_id = $1 AND name = $2 AND deleted_at IS NULL`, repoID, name); err != nil {
			return fmt.Errorf("mark branch %q deleted: %w", name, err)
		}
	}
	return nil
}

// recordBranches answers, by name, the issue each branch is for. Only a branch
// still on the host moves; one deleted and pushed again is a new row.
func (s *Service) recordBranches(ctx context.Context, tx db.DBTX, repo *Repository, branches []Branch, receipt *Receipt) (map[string]*uuid.UUID, error) {
	mapped := map[string]*uuid.UUID{}
	for _, b := range branches {
		// A branch gets a row when it is new, names its issue or was made here;
		// any other push, to main say, moves nothing.
		issueID, err := s.issueIDFor(ctx, tx, firstKey(b.Name))
		if err != nil {
			return nil, err
		}
		head := cleanSHA(b.HeadSHA)
		var followed *uuid.UUID
		if b.created || issueID != nil {
			err = tx.QueryRow(ctx, `
				INSERT INTO git_branch (org_id, repository_id, name, url, issue_id, head_sha, pushed_at)
				VALUES (current_org_id(), $1, $2, $3, $4, $5, CASE WHEN $5 = '' THEN NULL ELSE now() END)
				ON CONFLICT (repository_id, name) WHERE deleted_at IS NULL DO UPDATE SET url = EXCLUDED.url,
				    issue_id = COALESCE(git_branch.issue_id, EXCLUDED.issue_id),
				    head_sha = CASE WHEN EXCLUDED.head_sha = '' THEN git_branch.head_sha ELSE EXCLUDED.head_sha END,
				    pushed_at = CASE WHEN EXCLUDED.head_sha = '' THEN git_branch.pushed_at ELSE now() END
				RETURNING issue_id`, repo.ID, b.Name, b.URL, issueID, head).Scan(&followed)
		} else {
			err = tx.QueryRow(ctx, `
				UPDATE git_branch SET head_sha = CASE WHEN $3 = '' THEN head_sha ELSE $3 END, pushed_at = now()
				WHERE repository_id = $1 AND name = $2 AND deleted_at IS NULL RETURNING issue_id`, repo.ID, b.Name, head).Scan(&followed)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
		}
		if err != nil {
			return nil, fmt.Errorf("record branch %q: %w", b.Name, err)
		}
		receipt.Branches++
		mapped[b.Name] = followed
	}
	return mapped, nil
}
