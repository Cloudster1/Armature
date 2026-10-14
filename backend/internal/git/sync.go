package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// Sync is the outbound half that runs in the worker: when an issue moves, every
// open pull request about it is told.
//
// It reads the event stream the relay publishes, through a consumer group, so a
// worker restart picks up where it left off and two workers share the work
// rather than both commenting.
type Sync struct {
	db     *db.Cluster
	redis  *redis.Client
	client *http.Client
	log    *slog.Logger
	group  string
}

func NewSync(cluster *db.Cluster, rdb *redis.Client, log *slog.Logger) *Sync {
	return &Sync{
		db: cluster, redis: rdb, client: &http.Client{Timeout: hostRequestTimeout}, log: log,
		group: "git-sync",
	}
}

// UseHTTPClient swaps the client the hosts are reached with.
func (s *Sync) UseHTTPClient(c *http.Client) { s.client = c }

// Run consumes the stream until the context ends.
func (s *Sync) Run(ctx context.Context) error {
	return events.NewConsumer(s.db, s.redis, s.log, s.group, s.Handle).Run(ctx)
}

// Handle acts on one event. Only a transition means anything here; a comment
// is posted on each open pull request linked to the issue that moved.
func (s *Sync) Handle(ctx context.Context, e events.Event) error {
	if e.Topic != events.TopicIssueTransitioned {
		return nil
	}
	var moved struct {
		Key        string `json:"key"`
		Transition string `json:"transition"`
		FromStatus string `json:"fromStatus"`
		ToStatus   string `json:"toStatus"`
	}
	if err := json.Unmarshal(e.Payload, &moved); err != nil || moved.Key == "" {
		return nil
	}

	ctx = db.PinPrimary(ctx)

	type target struct {
		repo   Repository
		number int64
	}
	var targets []target
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT r.id, r.host, r.name, r.url, r.api_base_url, r.access_token, q.number
			FROM issue i
			JOIN project p ON p.id = i.project_id
			JOIN issue_pull_request l ON l.issue_id = i.id
			JOIN pull_request q ON q.id = l.pull_request_id AND q.state = 'open'
			JOIN git_repository r ON r.id = q.repository_id
			WHERE p.key || '-' || i.key_num = $1 AND r.access_token <> ''`, moved.Key)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t target
			if err := rows.Scan(&t.repo.ID, &t.repo.Host, &t.repo.Name, &t.repo.URL, &t.repo.APIBaseURL, &t.repo.token, &t.number); err != nil {
				return err
			}
			targets = append(targets, t)
		}
		return rows.Err()
	})
	if err != nil {
		return fmt.Errorf("find pull requests for %s: %w", moved.Key, err)
	}

	body := fmt.Sprintf("**%s** moved from %s to **%s** (%s).", moved.Key, moved.FromStatus, moved.ToStatus, moved.Transition)
	var failed []string
	told := 0
	for _, t := range targets {
		host, err := HostFor(t.repo.Host)
		if err != nil {
			continue
		}
		if err := host.CommentOnPullRequest(ctx, s.client, &t.repo, t.number, body); err != nil {
			failed = append(failed, fmt.Sprintf("%s#%d: %v", t.repo.Name, t.number, err))
		} else {
			told++
		}
	}
	if len(failed) == 0 {
		return nil
	}
	// Another attempt would comment twice on the pull requests already told.
	err = errors.New(strings.Join(failed, "; "))
	if told > 0 {
		return events.Permanent(err)
	}
	return err
}
