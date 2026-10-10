package git

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// recordPull stores one pull request and links it to the issues it is about.
// It returns the merge to act on only when this delivery is what merged it.
func (s *Service) recordPull(ctx context.Context, tx db.DBTX, repo *Repository, pr PullRequest, receipt *Receipt) (*mergedPull, error) {
	var (
		id       uuid.UUID
		wasState PullState
	)
	// The state before this delivery is read and locked in a statement of its
	// own: inside the upsert a locking read would skip the row being updated.
	err := tx.QueryRow(ctx, `
		SELECT state::text FROM pull_request WHERE repository_id = $1 AND number = $2 FOR UPDATE`,
		repo.ID, pr.Number).Scan(&wasState)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("read pull request %d: %w", pr.Number, err)
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO pull_request (org_id, repository_id, number, title, url, state, source_branch,
		    target_branch, author_name, head_sha, opened_at, merged_at)
		VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (repository_id, number) DO UPDATE SET
		    title = EXCLUDED.title, url = EXCLUDED.url, state = EXCLUDED.state,
		    source_branch = EXCLUDED.source_branch, target_branch = EXCLUDED.target_branch,
		    head_sha = CASE WHEN EXCLUDED.head_sha = '' THEN pull_request.head_sha ELSE EXCLUDED.head_sha END,
		    merged_at = COALESCE(EXCLUDED.merged_at, pull_request.merged_at)
		RETURNING id`,
		repo.ID, pr.Number, pr.Title, pr.URL, string(pr.State), pr.SourceBranch,
		pr.TargetBranch, pr.AuthorName, pr.HeadSHA, pr.OpenedAt, pr.MergedAt,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("record pull request %d: %w", pr.Number, err)
	}
	receipt.PullRequests++
	pr.ID = id

	// A pull request is about the issues its title and branch name
	// mention, and about the issue its source branch was made for.
	keys := IssueKeys(pr.Title)
	keys = append(keys, IssueKeys(pr.SourceBranch)...)
	if key, err := s.branchIssueKey(ctx, tx, repo.ID, pr.SourceBranch); err != nil {
		return nil, err
	} else if key != "" {
		keys = appendUnique(keys, key)
	}
	for _, key := range keys {
		linked, err := s.link(ctx, tx, "issue_pull_request", "pull_request_id", key, id)
		if err != nil {
			return nil, err
		}
		if linked {
			receipt.Linked = appendUnique(receipt.Linked, key)
			if err := events.EmitInTenant(ctx, tx, events.TopicPullRequestLinked, map[string]any{
				"issueKey": key, "repositoryId": repo.ID, "number": pr.Number,
			}); err != nil {
				return nil, err
			}
		}
	}
	if pr.State != PullMerged || wasState == PullMerged {
		return nil, nil
	}
	// Merging the pull request is what merges the branch; the branch's own
	// row says so from now on.
	if _, err := tx.Exec(ctx, `
		UPDATE git_branch SET merged_at = COALESCE(merged_at, $3, now())
		WHERE repository_id = $1 AND name = $2`, repo.ID, pr.SourceBranch, pr.MergedAt); err != nil {
		return nil, fmt.Errorf("mark %q merged: %w", pr.SourceBranch, err)
	}
	return &mergedPull{pr: pr, keys: unique(keys)}, nil
}
