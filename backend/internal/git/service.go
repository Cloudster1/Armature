package git

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/netguard"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/tenant"
)

// Service connects repositories, records what they send, and reaches back.
type Service struct {
	db     *db.Cluster
	issues *issue.Service
	// client talks to the hosts. Tests point it at a stub.
	client *http.Client
	now    func() time.Time
}

func NewService(cluster *db.Cluster, issues *issue.Service) *Service {
	return &Service{db: cluster, issues: issues, client: netguard.Client(hostRequestTimeout, netguard.FromEnv()), now: time.Now}
}

// UseHTTPClient swaps the client the hosts are reached with.
func (s *Service) UseHTTPClient(c *http.Client) { s.client = c }

// ConnectInput describes a repository to connect.
type ConnectInput struct {
	Host HostKind
	// Name is "owner/name" as the host knows it.
	Name string
	// URL is where a person sees the repository; empty is derived from the host.
	URL string
	// APIBaseURL is empty for the public host.
	APIBaseURL        string
	DefaultBranch     string
	AccessToken       string
	TransitionOnMerge string
}

// Connect attaches a repository to a project and mints its webhook secret. The
// secret is returned on this call alone.
func (s *Service) Connect(ctx context.Context, projectKey string, in ConnectInput, actor uuid.UUID) (*Repository, db.LSN, error) {
	host, err := HostFor(in.Host)
	if err != nil {
		return nil, 0, err
	}
	in.Name = strings.Trim(strings.TrimSpace(in.Name), "/")
	if !strings.Contains(in.Name, "/") {
		return nil, 0, errors.New("name the repository as owner/name, the way the host does")
	}
	if in.URL == "" {
		in.URL = publicURL(in.Host, in.Name)
	}
	if in.APIBaseURL == "" {
		in.APIBaseURL = host.DefaultAPIBaseURL()
	}
	if in.DefaultBranch = strings.TrimSpace(in.DefaultBranch); in.DefaultBranch == "" {
		in.DefaultBranch = "main"
	}
	if err := webAddress(in.URL); err != nil {
		return nil, 0, err
	}
	if err := webAddress(in.APIBaseURL); err != nil {
		return nil, 0, err
	}

	secret, err := newSecret()
	if err != nil {
		return nil, 0, err
	}

	var out *Repository
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var projectID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1 AND archived_at IS NULL`,
			project.NormalizeKey(projectKey)).Scan(&projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return project.ErrNotFound
		}
		if err != nil {
			return err
		}

		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO git_repository
			    (org_id, project_id, host, name, url, api_base_url, default_branch,
			     webhook_secret, access_token, transition_on_merge, connected_by)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			RETURNING id`,
			projectID, string(in.Host), in.Name, in.URL, in.APIBaseURL, in.DefaultBranch,
			secret, in.AccessToken, strings.TrimSpace(in.TransitionOnMerge), actor,
		).Scan(&id)
		if isUniqueViolation(err) {
			return ErrNameTaken
		}
		if isCheckViolation(err) {
			return errors.New("name the repository as owner/name, the way the host does")
		}
		if err != nil {
			return fmt.Errorf("connect repository: %w", err)
		}

		out, err = s.load(ctx, tx, id)
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "vcs.repository.connected", map[string]any{
			"repositoryId": id, "projectId": projectID, "host": in.Host, "name": in.Name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	out.WebhookSecret = secret
	return out, lsn, nil
}

// ErrBadAddress is returned for a repository address that is not an http one.
var ErrBadAddress = errors.New("that is not an http address")

// webAddress refuses anything but an http address with a host: these are
// followed by the server and shown to people as links.
func webAddress(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parsed, err := neturl.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%w: %q", ErrBadAddress, raw)
	}
	return nil
}

// publicURL is where a repository on the public host is browsed.
func publicURL(host HostKind, name string) string {
	switch host {
	case GitLab:
		return "https://gitlab.com/" + name
	case Gitea:
		return "https://gitea.com/" + name
	}
	return "https://github.com/" + name
}

// newSecret mints a webhook secret: 32 random bytes as hex, which every host's
// secret field accepts.
func newSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate webhook secret: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

const selectRepository = `
SELECT r.id, r.project_id, p.key, r.host, r.name, r.url, r.api_base_url, r.default_branch,
       r.access_token, r.webhook_secret, r.transition_on_merge, r.created_at, r.updated_at,
       (SELECT count(*) FROM git_commit c WHERE c.repository_id = r.id),
       (SELECT count(*) FROM pull_request q WHERE q.repository_id = r.id AND q.state = 'open')
FROM git_repository r
JOIN project p ON p.id = r.project_id`

func scanRepository(row pgx.Row) (*Repository, error) {
	var r Repository
	err := row.Scan(&r.ID, &r.ProjectID, &r.ProjectKey, &r.Host, &r.Name, &r.URL, &r.APIBaseURL, &r.DefaultBranch,
		&r.token, &r.secret, &r.TransitionOnMerge, &r.CreatedAt, &r.UpdatedAt, &r.CommitCount, &r.OpenPullCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read repository: %w", err)
	}
	r.HasToken = r.token != ""
	return &r, nil
}

func (s *Service) load(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Repository, error) {
	return scanRepository(tx.QueryRow(ctx, selectRepository+` WHERE r.id = $1`, id))
}

// List returns a project's repositories.
func (s *Service) List(ctx context.Context, projectKey string) ([]Repository, error) {
	out := []Repository{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, selectRepository+` WHERE p.key = $1 ORDER BY r.name`, project.NormalizeKey(projectKey))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			r, err := scanRepository(rows)
			if err != nil {
				return err
			}
			out = append(out, *r)
		}
		return rows.Err()
	})
	return out, err
}

// ByID reads one repository in the caller's organization.
func (s *Service) ByID(ctx context.Context, id uuid.UUID) (*Repository, error) {
	var out *Repository
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = s.load(ctx, tx, id)
		return err
	})
	return out, err
}

// UpdateInput carries the fields an edit may change. A nil field is left alone;
// an empty token takes the token away.
type UpdateInput struct {
	URL               *string
	DefaultBranch     *string
	AccessToken       *string
	TransitionOnMerge *string
}

// Update changes how a repository is reached and what merging does.
func (s *Service) Update(ctx context.Context, id uuid.UUID, in UpdateInput, actor uuid.UUID) (*Repository, db.LSN, error) {
	var out *Repository
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		before, err := s.load(ctx, tx, id)
		if err != nil {
			return err
		}
		url, branch, token, onMerge := before.URL, before.DefaultBranch, before.token, before.TransitionOnMerge
		if in.URL != nil {
			url = strings.TrimSpace(*in.URL)
			if err := webAddress(url); err != nil {
				return err
			}
		}
		if in.DefaultBranch != nil {
			if branch = strings.TrimSpace(*in.DefaultBranch); branch == "" {
				return errors.New("the default branch needs a name")
			}
		}
		if in.AccessToken != nil {
			token = strings.TrimSpace(*in.AccessToken)
		}
		if in.TransitionOnMerge != nil {
			onMerge = strings.TrimSpace(*in.TransitionOnMerge)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE git_repository SET url = $2, default_branch = $3, access_token = $4, transition_on_merge = $5
			WHERE id = $1`, id, url, branch, token, onMerge); err != nil {
			return fmt.Errorf("update repository: %w", err)
		}
		out, err = s.load(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// RotateSecret replaces the webhook secret, for when the old one has leaked or
// been lost. The host has to be given the new one; the response carries it.
func (s *Service) RotateSecret(ctx context.Context, id uuid.UUID, actor uuid.UUID) (*Repository, db.LSN, error) {
	secret, err := newSecret()
	if err != nil {
		return nil, 0, err
	}
	var out *Repository
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE git_repository SET webhook_secret = $2 WHERE id = $1`, id, secret)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		out, err = s.load(ctx, tx, id)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	out.WebhookSecret = secret
	return out, lsn, nil
}

// Disconnect removes a repository and everything recorded from it. The issues
// keep their own history; what goes is the mirror of the host's.
func (s *Service) Disconnect(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM git_repository WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

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

// Receive handles a webhook. It arrives with no session, so the repository is
// looked up outside any tenant, the delivery is authenticated against that
// repository's secret, and only then does the work run inside its organization.
func (s *Service) Receive(ctx context.Context, repoID uuid.UUID, r *http.Request, body []byte) (*Receipt, error) {
	var (
		orgID  uuid.UUID
		kind   HostKind
		secret string
	)
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT org_id, host, webhook_secret FROM git_repository WHERE id = $1`, repoID).
			Scan(&orgID, &kind, &secret)
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

	ctx = db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: orgID}))
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
		for _, name := range d.DeletedBranches {
			if _, err := tx.Exec(ctx, `DELETE FROM git_branch WHERE repository_id = $1 AND name = $2`, repo.ID, name); err != nil {
				return err
			}
		}

		// A branch gets a row when it is new, when its name says which issue
		// it is for, or when it already has one from being made here. Any
		// other push, to main say, moves nothing.
		mapped := map[string]*uuid.UUID{}
		for _, b := range d.Branches {
			issueID, err := s.issueIDFor(ctx, tx, firstKey(b.Name))
			if err != nil {
				return err
			}
			head := cleanSHA(b.HeadSHA)
			var followed *uuid.UUID
			if b.created || issueID != nil {
				err = tx.QueryRow(ctx, `
					INSERT INTO git_branch (org_id, repository_id, name, url, issue_id, head_sha, pushed_at)
					VALUES (current_org_id(), $1, $2, $3, $4, $5, CASE WHEN $5 = '' THEN NULL ELSE now() END)
					ON CONFLICT (repository_id, name) DO UPDATE SET url = EXCLUDED.url,
					    issue_id = COALESCE(git_branch.issue_id, EXCLUDED.issue_id),
					    head_sha = CASE WHEN EXCLUDED.head_sha = '' THEN git_branch.head_sha ELSE EXCLUDED.head_sha END,
					    pushed_at = CASE WHEN EXCLUDED.head_sha = '' THEN git_branch.pushed_at ELSE now() END
					RETURNING issue_id`, repo.ID, b.Name, b.URL, issueID, head).Scan(&followed)
			} else {
				err = tx.QueryRow(ctx, `
					UPDATE git_branch SET head_sha = CASE WHEN $3 = '' THEN head_sha ELSE $3 END, pushed_at = now()
					WHERE repository_id = $1 AND name = $2 RETURNING issue_id`, repo.ID, b.Name, head).Scan(&followed)
				if errors.Is(err, pgx.ErrNoRows) {
					continue
				}
			}
			if err != nil {
				return fmt.Errorf("record branch %q: %w", b.Name, err)
			}
			receipt.Branches++
			mapped[b.Name] = followed
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
					if err := events.EmitInTenant(ctx, tx, events.TopicCommitLinked, map[string]any{
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
				WHERE repository_id = $1 AND name = $2`, repo.ID, name, count); err != nil {
				return fmt.Errorf("count commits on %q: %w", name, err)
			}
		}

		for _, pr := range d.PullRequests {
			var (
				id       uuid.UUID
				wasState PullState
			)
			err := tx.QueryRow(ctx, `
				INSERT INTO pull_request (org_id, repository_id, number, title, url, state, source_branch,
				    target_branch, author_name, head_sha, opened_at, merged_at)
				VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
				ON CONFLICT (repository_id, number) DO UPDATE SET
				    title = EXCLUDED.title, url = EXCLUDED.url, state = EXCLUDED.state,
				    source_branch = EXCLUDED.source_branch, target_branch = EXCLUDED.target_branch,
				    head_sha = CASE WHEN EXCLUDED.head_sha = '' THEN pull_request.head_sha ELSE EXCLUDED.head_sha END,
				    merged_at = COALESCE(EXCLUDED.merged_at, pull_request.merged_at)
				RETURNING id, COALESCE((SELECT state::text FROM pull_request q WHERE q.repository_id = $1 AND q.number = $2 AND q.id <> pull_request.id), '')`,
				repo.ID, pr.Number, pr.Title, pr.URL, string(pr.State), pr.SourceBranch,
				pr.TargetBranch, pr.AuthorName, pr.HeadSHA, pr.OpenedAt, pr.MergedAt,
			).Scan(&id, &wasState)
			if err != nil {
				return fmt.Errorf("record pull request %d: %w", pr.Number, err)
			}
			receipt.PullRequests++
			pr.ID = id

			// A pull request is about the issues its title and branch name
			// mention, and about the issue its source branch was made for.
			keys := IssueKeys(pr.Title)
			keys = append(keys, IssueKeys(pr.SourceBranch)...)
			if key, err := s.branchIssueKey(ctx, tx, repo.ID, pr.SourceBranch); err != nil {
				return err
			} else if key != "" {
				keys = appendUnique(keys, key)
			}
			for _, key := range keys {
				linked, err := s.link(ctx, tx, "issue_pull_request", "pull_request_id", key, id)
				if err != nil {
					return err
				}
				if linked {
					receipt.Linked = appendUnique(receipt.Linked, key)
					if err := events.EmitInTenant(ctx, tx, events.TopicPullRequestLinked, map[string]any{
						"issueKey": key, "repositoryId": repo.ID, "number": pr.Number,
					}); err != nil {
						return err
					}
				}
			}
			if pr.State == PullMerged {
				// Merging the pull request is what merges the branch; the
				// branch's own row says so from now on.
				if _, err := tx.Exec(ctx, `
					UPDATE git_branch SET merged_at = COALESCE(merged_at, $3, now())
					WHERE repository_id = $1 AND name = $2`, repo.ID, pr.SourceBranch, pr.MergedAt); err != nil {
					return fmt.Errorf("mark %q merged: %w", pr.SourceBranch, err)
				}
				merged = append(merged, mergedPull{pr: pr, keys: unique(keys)})
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
			if err := events.EmitInTenant(ctx, tx, events.TopicCIRunRecorded, map[string]any{
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

	// Commands are carried out once, on the commit's first arrival: a push of
	// the same commit to a second branch is not a second request to close.
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
				s.carryOut(ctx, receipt, key, cmd, actor, "commit "+c.SHA[:7])
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
func (s *Service) branchIssueKey(ctx context.Context, tx db.DBTX, repoID uuid.UUID, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	var issueID *uuid.UUID
	err := tx.QueryRow(ctx, `SELECT issue_id FROM git_branch WHERE repository_id = $1 AND name = $2`, repoID, name).Scan(&issueID)
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
			SELECT b.id, r.name, b.name, b.url, b.head_sha, b.commit_count, b.pushed_at, b.merged_at, b.created_at
			FROM git_branch b JOIN git_repository r ON r.id = b.repository_id
			WHERE b.issue_id = $1 ORDER BY b.created_at DESC`, issueID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var b Branch
			if err := rows.Scan(&b.ID, &b.Repository, &b.Name, &b.URL, &b.HeadSHA, &b.CommitCount, &b.PushedAt, &b.MergedAt, &b.CreatedAt); err != nil {
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

	// Each pull request carries the newest run against its head, which is the
	// one answer to "is it green".
	for i := range out.PullRequests {
		for _, run := range out.Runs {
			if run.SHA == out.PullRequests[i].HeadSHA {
				r := run
				out.PullRequests[i].CI = &r
				break
			}
		}
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
			INSERT INTO git_branch (org_id, repository_id, name, url, issue_id, head_sha)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)
			ON CONFLICT (repository_id, name) DO UPDATE SET url = EXCLUDED.url, issue_id = EXCLUDED.issue_id,
			    head_sha = CASE WHEN EXCLUDED.head_sha = '' THEN git_branch.head_sha ELSE EXCLUDED.head_sha END
			RETURNING id, name, url, head_sha, commit_count, pushed_at, merged_at, created_at`,
			repo.ID, name, url, found.ID, cleanSHA(head),
		).Scan(&out.ID, &out.Name, &out.URL, &out.HeadSHA, &out.CommitCount, &out.PushedAt, &out.MergedAt, &out.CreatedAt)
		if err != nil {
			return fmt.Errorf("record branch: %w", err)
		}
		out.Repository = repo.Name
		return events.EmitInTenant(ctx, tx, "vcs.branch.created", map[string]any{
			"issueKey": found.Key, "repositoryId": repo.ID, "branch": name, "from": from, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return &out, lsn, nil
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

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}
