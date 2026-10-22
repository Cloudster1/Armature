package git

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
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
		if b.Name == repo.DefaultBranch {
			// The database refuses the default branch as an issue's, and a key
			// in its name is no reason to fail the whole delivery.
			issueID = nil
		}
		head := cleanSHA(b.HeadSHA)
		var followed *uuid.UUID
		if b.created || issueID != nil {
			err = tx.QueryRow(ctx, `
				INSERT INTO git_branch (repository_id, name, url, issue_id, head_sha, pushed_at)
				VALUES ($1, $2, $3, $4, $5, CASE WHEN $5 = '' THEN NULL ELSE now() END)
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

// issueRepository reads an issue and a repository of its own project, so a
// right over the issue never reaches another project's repository.
func (s *Service) issueRepository(ctx context.Context, issueKey string, repoID uuid.UUID) (*Repository, *issue.Issue, error) {
	repo, err := s.ByID(ctx, repoID)
	if err != nil {
		return nil, nil, err
	}
	found, err := s.issues.ByKey(ctx, issueKey)
	if err != nil {
		return nil, nil, err
	}
	if repo.ProjectID != found.ProjectID {
		return nil, nil, ErrNotFound
	}
	return repo, found, nil
}

// RepositoryOfIssue finds the repository of an issue's project by the
// owner/name the host knows it by.
func (s *Service) RepositoryOfIssue(ctx context.Context, issueKey, name string) (uuid.UUID, error) {
	found, err := s.issues.ByKey(ctx, issueKey)
	if err != nil {
		return uuid.Nil, err
	}
	var ids []uuid.UUID
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT id FROM git_repository WHERE project_id = $1 AND lower(name) = lower($2) ORDER BY host`,
			found.ProjectID, strings.Trim(strings.TrimSpace(name), "/"))
		if err != nil {
			return err
		}
		ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	})
	switch {
	case err != nil:
		return uuid.Nil, fmt.Errorf("find repository %q: %w", name, err)
	case len(ids) == 0:
		return uuid.Nil, ErrNotFound
	case len(ids) > 1:
		return uuid.Nil, ErrAmbiguousName
	}
	return ids[0], nil
}

// issueBranch refuses a branch that is not the issue's own: the default one,
// one nobody made for it, or one made for another issue.
func (s *Service) issueBranch(ctx context.Context, repo *Repository, issueID uuid.UUID, name string) error {
	if name == repo.DefaultBranch {
		return ErrDefaultBranch
	}
	var mapped *uuid.UUID
	// The primary, so a branch made a moment ago is already there.
	err := s.db.Read(db.PinPrimary(ctx), func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT issue_id FROM git_branch WHERE repository_id = $1 AND name = $2 AND deleted_at IS NULL`,
			repo.ID, name).Scan(&mapped)
	})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (mapped == nil || *mapped != issueID)) {
		return ErrNotIssueBranch
	}
	if err != nil {
		return fmt.Errorf("read branch %q: %w", name, err)
	}
	return nil
}
