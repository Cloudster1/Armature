package git

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
)

// ForIssue reads everything the repositories know about one issue.
func (s *Service) ForIssue(ctx context.Context, key string) (*Development, error) {
	projectKey, num, err := issue.ParseKey(key)
	if err != nil {
		return nil, err
	}
	out := &Development{Branches: []Branch{}, PullRequests: []PullRequest{}, Commits: []Commit{}, Runs: []CIRun{}}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			issueID uuid.UUID
			summary string
		)
		err := tx.QueryRow(ctx, `
			SELECT i.id, i.summary FROM issue i JOIN project p ON p.id = i.project_id
			WHERE p.key = $1 AND i.key_num = $2`, projectKey, num).Scan(&issueID, &summary)
		if errors.Is(err, pgx.ErrNoRows) {
			return issue.ErrNotFound
		}
		if err != nil {
			return err
		}
		out.BranchName = BranchName(issue.FormatKey(projectKey, num), summary)

		rows, err := tx.Query(ctx, `
			SELECT b.id, r.name, b.name, b.url, b.head_sha, b.commit_count, b.pushed_at, b.merged_at, b.deleted_at, b.created_at
			FROM git_branch b JOIN git_repository r ON r.id = b.repository_id
			WHERE b.issue_id = $1 ORDER BY b.created_at DESC`, issueID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var b Branch
			if err := rows.Scan(&b.ID, &b.Repository, &b.Name, &b.URL, &b.HeadSHA, &b.CommitCount, &b.PushedAt, &b.MergedAt, &b.DeletedAt, &b.CreatedAt); err != nil {
				rows.Close()
				return err
			}
			out.Branches = append(out.Branches, b)
		}
		rows.Close()

		rows, err = tx.Query(ctx, `
			SELECT q.id, r.name, q.number, q.title, q.url, q.state, q.source_branch, q.target_branch,
			       q.author_name, q.head_sha, q.opened_at, q.merged_at
			FROM issue_pull_request l
			JOIN pull_request q ON q.id = l.pull_request_id
			JOIN git_repository r ON r.id = q.repository_id
			WHERE l.issue_id = $1 ORDER BY q.opened_at DESC`, issueID)
		if err != nil {
			return err
		}
		shas := map[string]bool{}
		for rows.Next() {
			var pr PullRequest
			if err := rows.Scan(&pr.ID, &pr.Repository, &pr.Number, &pr.Title, &pr.URL, &pr.State, &pr.SourceBranch,
				&pr.TargetBranch, &pr.AuthorName, &pr.HeadSHA, &pr.OpenedAt, &pr.MergedAt); err != nil {
				rows.Close()
				return err
			}
			if pr.HeadSHA != "" {
				shas[pr.HeadSHA] = true
			}
			out.PullRequests = append(out.PullRequests, pr)
		}
		rows.Close()

		const recentCommits = 50
		rows, err = tx.Query(ctx, `
			SELECT c.id, r.name, c.sha, c.message, c.author_name, c.author_email, c.url, c.branch, c.committed_at
			FROM issue_commit l
			JOIN git_commit c ON c.id = l.commit_id
			JOIN git_repository r ON r.id = c.repository_id
			WHERE l.issue_id = $1 ORDER BY c.committed_at DESC LIMIT $2`, issueID, recentCommits)
		if err != nil {
			return err
		}
		for rows.Next() {
			var c Commit
			if err := rows.Scan(&c.ID, &c.Repository, &c.SHA, &c.Message, &c.AuthorName, &c.AuthorEmail, &c.URL, &c.Branch, &c.CommittedAt); err != nil {
				rows.Close()
				return err
			}
			shas[c.SHA] = true
			out.Commits = append(out.Commits, c)
		}
		rows.Close()

		if len(shas) == 0 {
			return nil
		}
		list := make([]string, 0, len(shas))
		for sha := range shas {
			list = append(list, sha)
		}
		rows, err = tx.Query(ctx, `
			SELECT u.id, r.name, u.external_id, u.name, u.status, u.url, u.sha, u.branch, u.started_at, u.finished_at
			FROM ci_run u JOIN git_repository r ON r.id = u.repository_id
			WHERE u.sha = ANY($1) ORDER BY u.started_at DESC`, list)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var run CIRun
			if err := rows.Scan(&run.ID, &run.Repository, &run.ExternalID, &run.Name, &run.Status, &run.URL, &run.SHA, &run.Branch, &run.StartedAt, &run.FinishedAt); err != nil {
				return err
			}
			out.Runs = append(out.Runs, run)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}

	// Every check on a head counts toward "is it green", not only the newest.
	checks := checksBySHA(out.Runs)
	for i := range out.PullRequests {
		out.PullRequests[i].CI = checks[out.PullRequests[i].HeadSHA]
	}
	for i := range out.Commits {
		out.Commits[i].CI = checks[out.Commits[i].SHA]
	}
	return out, nil
}

// CreateBranch makes a branch for an issue on the host and records it as the
// issue's. An empty name takes the suggested one; an empty base takes the
// repository's default branch.
func (s *Service) CreateBranch(ctx context.Context, issueKey string, repoID uuid.UUID, name, from string, actor uuid.UUID) (*Branch, db.LSN, error) {
	repo, err := s.ByID(ctx, repoID)
	if err != nil {
		return nil, 0, err
	}
	if repo.token == "" {
		return nil, 0, ErrNoToken
	}
	found, err := s.issues.ByKey(ctx, issueKey)
	if err != nil {
		return nil, 0, err
	}
	if name = strings.TrimSpace(name); name == "" {
		name = BranchName(found.Key, found.Summary)
	}
	if !ValidBranchName(name) {
		return nil, 0, ErrBadBranchName
	}
	if from = strings.TrimSpace(from); from == "" {
		from = repo.DefaultBranch
	}

	host, err := HostFor(repo.Host)
	if err != nil {
		return nil, 0, err
	}
	url, head, err := host.CreateBranch(ctx, s.client, repo, name, from)
	if err != nil {
		return nil, 0, err
	}

	var out Branch
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO git_branch (repository_id, name, url, issue_id, head_sha)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (repository_id, name) WHERE deleted_at IS NULL DO UPDATE SET url = EXCLUDED.url, issue_id = EXCLUDED.issue_id,
			    head_sha = CASE WHEN EXCLUDED.head_sha = '' THEN git_branch.head_sha ELSE EXCLUDED.head_sha END
			RETURNING id, name, url, head_sha, commit_count, pushed_at, merged_at, created_at`,
			repo.ID, name, url, found.ID, cleanSHA(head),
		).Scan(&out.ID, &out.Name, &out.URL, &out.HeadSHA, &out.CommitCount, &out.PushedAt, &out.MergedAt, &out.CreatedAt)
		if err != nil {
			return fmt.Errorf("record branch: %w", err)
		}
		out.Repository = repo.Name
		return events.Emit(ctx, tx, "vcs.branch.created", map[string]any{
			"issueKey": found.Key, "repositoryId": repo.ID, "branch": name, "from": from, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return &out, lsn, nil
}
