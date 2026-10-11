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

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/netguard"
	"github.com/armature/armature/backend/internal/project"
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

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}
