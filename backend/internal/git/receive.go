package git

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
)

// Receipt says what a delivery amounted to, for the host's log and for tests.
type Receipt struct {
	Ignored      bool     `json:"ignored"`
	Commits      int      `json:"commits"`
	Branches     int      `json:"branches"`
	PullRequests int      `json:"pullRequests"`
	Runs         int      `json:"runs"`
	Linked       []string `json:"linked"`
	// Transitions names each issue moved and how, as "CP-4: Start progress".
	Transitions []string `json:"transitions"`
	// Refused names each command that could not be carried out, and why.
	Refused []string `json:"refused"`
}

// Receive handles a webhook. It arrives with no session, so the delivery is
// authenticated against its repository's secret before any work runs.
func (s *Service) Receive(ctx context.Context, repoID uuid.UUID, r *http.Request, body []byte) (*Receipt, error) {
	var (
		kind   HostKind
		secret string
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT host, webhook_secret FROM git_repository WHERE id = $1`, repoID).
			Scan(&kind, &secret)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find repository for webhook: %w", err)
	}

	host, err := HostFor(kind)
	if err != nil {
		return nil, err
	}
	if err := host.Authenticate(r, body, secret); err != nil {
		return nil, err
	}

	ctx = db.PinPrimary(ctx)
	repo, err := s.ByID(ctx, repoID)
	if err != nil {
		return nil, err
	}
	delivery, err := host.Parse(r, body, repo)
	if errors.Is(err, ErrUnknownEvent) {
		return &Receipt{Ignored: true}, nil
	}
	if err != nil {
		return nil, err
	}
	return s.Record(ctx, repo, delivery)
}

// Record stores what a delivery said and acts on it: links by key, then the
// commands in new commit messages, then the merge transition.
//
// The recording is one transaction. The workflow moves come after it, each on
// its own, so that a transition the workflow refuses does not unrecord the
// commit that asked for it; the refusal is reported instead.
func (s *Service) Record(ctx context.Context, repo *Repository, d *Delivery) (*Receipt, error) {
	receipt := &Receipt{Linked: []string{}, Transitions: []string{}, Refused: []string{}}
	var (
		newCommits []Commit
		merged     []mergedPull
	)

	_, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := forgetDeleted(ctx, tx, repo.ID, d.DeletedBranches); err != nil {
			return err
		}
		mapped, err := s.recordBranches(ctx, tx, repo, d.Branches, receipt)
		if err != nil {
			return err
		}

		newOnBranch := map[string]int{}

		for _, c := range d.Commits {
			var (
				id       uuid.UUID
				inserted bool
			)
			err := tx.QueryRow(ctx, `
				INSERT INTO git_commit (org_id, repository_id, sha, message, author_name, author_email, url, branch, committed_at)
				VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8)
				ON CONFLICT (repository_id, sha) DO UPDATE SET branch = EXCLUDED.branch
				RETURNING id, (xmax = 0)`,
				repo.ID, c.SHA, c.Message, c.AuthorName, c.AuthorEmail, c.URL, c.Branch, c.CommittedAt,
			).Scan(&id, &inserted)
			if err != nil {
				return fmt.Errorf("record commit %s: %w", c.SHA, err)
			}
			receipt.Commits++
			c.ID = id
			if inserted {
				newCommits = append(newCommits, c)
				newOnBranch[c.Branch]++
			}
			// A commit is about the issues its message names, and about the
			// issue its branch is for: work on an issue's branch need not
			// repeat the key in every message to be seen from the issue.
			keys := IssueKeys(c.Message)
			if issueID := mapped[c.Branch]; issueID != nil {
				key, err := s.issueKeyOf(ctx, tx, *issueID)
				if err != nil {
					return err
				}
				keys = appendUnique(keys, key)
			}
			for _, key := range keys {
				linked, err := s.link(ctx, tx, "issue_commit", "commit_id", key, id)
				if err != nil {
					return err
				}
				if linked {
					receipt.Linked = appendUnique(receipt.Linked, key)
					if err := events.Emit(ctx, tx, events.TopicCommitLinked, map[string]any{
						"issueKey": key, "repositoryId": repo.ID, "sha": c.SHA,
					}); err != nil {
						return err
					}
				}
			}
		}

		for name, count := range newOnBranch {
			if _, err := tx.Exec(ctx, `
				UPDATE git_branch SET commit_count = commit_count + $3
				WHERE repository_id = $1 AND name = $2 AND deleted_at IS NULL`, repo.ID, name, count); err != nil {
				return fmt.Errorf("count commits on %q: %w", name, err)
			}
		}

		for _, pr := range d.PullRequests {
			m, err := s.recordPull(ctx, tx, repo, pr, receipt)
			if err != nil {
				return err
			}
			if m != nil {
				merged = append(merged, *m)
			}
		}

		for _, run := range d.Runs {
			if _, err := tx.Exec(ctx, `
				INSERT INTO ci_run (org_id, repository_id, external_id, name, status, url, sha, branch, started_at, finished_at)
				VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9)
				ON CONFLICT (repository_id, external_id) DO UPDATE SET
				    name = EXCLUDED.name, status = EXCLUDED.status, url = EXCLUDED.url,
				    finished_at = EXCLUDED.finished_at`,
				repo.ID, run.ExternalID, run.Name, string(run.Status), run.URL, run.SHA, run.Branch, run.StartedAt, run.FinishedAt,
			); err != nil {
				return fmt.Errorf("record run %s: %w", run.ExternalID, err)
			}
			receipt.Runs++
			if err := events.Emit(ctx, tx, events.TopicCIRunRecorded, map[string]any{
				"repositoryId": repo.ID, "externalId": run.ExternalID, "status": run.Status, "sha": run.SHA,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Commands are carried out on the commit's first arrival, and a squash
	// commit's copy of a branch's command is not a second request either.
	for _, c := range newCommits {
		commands := SmartCommands(c.Message)
		if len(commands) == 0 {
			continue
		}
		actor, ok, err := s.actorFor(ctx, repo, c.AuthorEmail)
		if err != nil {
			return nil, err
		}
		if !ok {
			receipt.Refused = append(receipt.Refused, fmt.Sprintf("%s: nobody here to act as", c.SHA[:7]))
			continue
		}
		for _, key := range IssueKeys(c.Message) {
			for _, cmd := range commands {
				s.carryOutOnce(ctx, receipt, repo, key, cmd, actor, "commit "+c.SHA[:7])
			}
		}
	}

	if repo.TransitionOnMerge != "" {
		for _, m := range merged {
			actor, ok, err := s.actorFor(ctx, repo, "")
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			for _, key := range m.keys {
				s.carryOut(ctx, receipt, key, Command{Transition: TransitionSlug(repo.TransitionOnMerge)}, actor,
					fmt.Sprintf("pull request #%d", m.pr.Number))
			}
		}
	}
	return receipt, nil
}

// mergedPull is a pull request a delivery merged, with the issues it was about.
type mergedPull struct {
	pr   PullRequest
	keys []string
}

// cleanSHA keeps a commit id the database will accept and drops anything else,
// since a head nobody can read is worse than none.
func cleanSHA(sha string) string {
	if len(sha) < 7 || len(sha) > 64 {
		return ""
	}
	for _, r := range sha {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return ""
		}
	}
	return sha
}

// issueKeyOf reads an issue's key, for linking and for the event that says so.
func (s *Service) issueKeyOf(ctx context.Context, tx db.DBTX, id uuid.UUID) (string, error) {
	var projectKey string
	var num int64
	err := tx.QueryRow(ctx, `SELECT p.key, i.key_num FROM issue i JOIN project p ON p.id = i.project_id WHERE i.id = $1`, id).
		Scan(&projectKey, &num)
	if err != nil {
		return "", fmt.Errorf("read issue key: %w", err)
	}
	return issue.FormatKey(projectKey, num), nil
}

// branchIssueKey is the key of the issue a followed branch is for, or empty.
// The branch still on the host answers first, then the latest one deleted.
func (s *Service) branchIssueKey(ctx context.Context, tx db.DBTX, repoID uuid.UUID, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	var issueID *uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT issue_id FROM git_branch WHERE repository_id = $1 AND name = $2
		ORDER BY deleted_at IS NOT NULL, deleted_at DESC LIMIT 1`, repoID, name).Scan(&issueID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && issueID == nil) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return s.issueKeyOf(ctx, tx, *issueID)
}

// carryOut takes one command on one issue and writes the outcome into the receipt.
func (s *Service) carryOut(ctx context.Context, receipt *Receipt, key string, cmd Command, actor issue.Actor, via string) {
	if cmd.Comment != "" {
		if _, _, err := s.issues.AddComment(ctx, key, issue.TextDocument(cmd.Comment+"\n\n(from "+via+")"), actor); err != nil {
			receipt.Refused = append(receipt.Refused, fmt.Sprintf("%s: comment: %v", key, err))
		}
		return
	}

	available, err := s.issues.Transitions(ctx, key, actor)
	if err != nil {
		receipt.Refused = append(receipt.Refused, fmt.Sprintf("%s: %v", key, err))
		return
	}
	for _, t := range available {
		if TransitionSlug(t.Name) != cmd.Transition {
			continue
		}
		if _, _, err := s.issues.Transition(ctx, key, issue.TransitionInput{TransitionID: t.ID}, actor); err != nil {
			receipt.Refused = append(receipt.Refused, fmt.Sprintf("%s: %s: %v", key, t.Name, err))
			return
		}
		receipt.Transitions = append(receipt.Transitions, key+": "+t.Name)
		return
	}
	receipt.Refused = append(receipt.Refused, fmt.Sprintf("%s: no transition called %q is available from here", key, cmd.Transition))
}

// actorFor decides who a commit acts as: its author when they are a member
// here, otherwise whoever connected the repository. A commit by a stranger to a
// repository connected by nobody who is still here acts as nobody.
func (s *Service) actorFor(ctx context.Context, repo *Repository, email string) (issue.Actor, bool, error) {
	var (
		userID uuid.UUID
		role   auth.OrgRole
		found  bool
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if email != "" {
			err := tx.QueryRow(ctx, `
				SELECT u.id, m.org_role FROM app_user u
				JOIN org_member m ON m.user_id = u.id AND m.org_id = current_org_id()
				WHERE lower(u.email) = lower($1) AND m.org_role <> 'customer'`, email).Scan(&userID, &role)
			if err == nil {
				found = true
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		err := tx.QueryRow(ctx, `
			SELECT u.id, m.org_role FROM git_repository r
			JOIN app_user u ON u.id = r.connected_by
			JOIN org_member m ON m.user_id = u.id AND m.org_id = current_org_id()
			WHERE r.id = $1`, repo.ID).Scan(&userID, &role)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err == nil {
			found = true
		}
		return err
	})
	if err != nil {
		return issue.Actor{}, false, err
	}
	return issue.Actor{UserID: userID, OrgRole: role}, found, nil
}

// issueIDFor resolves a key to an issue in the current organization, or nil for
// a key that names nothing here.
func (s *Service) issueIDFor(ctx context.Context, tx db.DBTX, key string) (*uuid.UUID, error) {
	if key == "" {
		return nil, nil
	}
	projectKey, num, err := issue.ParseKey(key)
	if err != nil {
		return nil, nil
	}
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT i.id FROM issue i JOIN project p ON p.id = i.project_id
		WHERE p.key = $1 AND i.key_num = $2`, projectKey, num).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// link joins an issue named by key to a commit or pull request, reporting
// whether the key named an issue here.
func (s *Service) link(ctx context.Context, tx db.DBTX, table, column, key string, id uuid.UUID) (bool, error) {
	issueID, err := s.issueIDFor(ctx, tx, key)
	if err != nil || issueID == nil {
		return false, err
	}
	_, err = tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (org_id, issue_id, %s) VALUES (current_org_id(), $1, $2)
		ON CONFLICT DO NOTHING`, table, column), *issueID, id)
	if err != nil {
		return false, fmt.Errorf("link %s to %s: %w", key, table, err)
	}
	return true, nil
}

func firstKey(text string) string {
	keys := IssueKeys(strings.ToUpper(text))
	if len(keys) == 0 {
		return ""
	}
	return keys[0]
}

func unique(keys []string) []string {
	var out []string
	for _, k := range keys {
		out = appendUnique(out, k)
	}
	return out
}

func appendUnique(list []string, value string) []string {
	for _, have := range list {
		if have == value {
			return list
		}
	}
	return append(list, value)
}
