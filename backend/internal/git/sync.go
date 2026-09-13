package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/tenant"
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
	// group and consumer name this reader to Redis.
	group, consumer string
	// block is how long one read waits for events before looping, which is
	// also how quickly a shutdown is noticed.
	block time.Duration
}

func NewSync(cluster *db.Cluster, rdb *redis.Client, log *slog.Logger) *Sync {
	return &Sync{
		db: cluster, redis: rdb, client: &http.Client{Timeout: hostRequestTimeout}, log: log,
		group: "git-sync", consumer: "worker", block: 5 * time.Second,
	}
}

// UseHTTPClient swaps the client the hosts are reached with.
func (s *Sync) UseHTTPClient(c *http.Client) { s.client = c }

// Run consumes the stream until the context ends.
func (s *Sync) Run(ctx context.Context) error {
	// The group starts at the stream's end: events from before the sync
	// existed are history, not a backlog of comments to post.
	err := s.redis.XGroupCreateMkStream(ctx, events.StreamKey, s.group, "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create consumer group: %w", err)
	}

	for {
		if ctx.Err() != nil {
			return nil
		}
		streams, err := s.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: s.group, Consumer: s.consumer, Streams: []string{events.StreamKey, ">"},
			Count: 50, Block: s.block,
		}).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.log.Warn("git sync read failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				e := events.FromMessage(msg)
				hctx, done := events.Continue(ctx, e, s.group)
				err := s.Handle(hctx, e)
				done(err)
				if err != nil {
					// Reported and acknowledged: a host that refuses a comment
					// is not a reason to comment again forever.
					s.log.Warn("git sync could not act on an event", "id", msg.ID, "error", err)
				}
				if err := s.redis.XAck(ctx, events.StreamKey, s.group, msg.ID).Err(); err != nil {
					s.log.Warn("git sync could not acknowledge", "id", msg.ID, "error", err)
				}
			}
		}
	}
}

// Handle acts on one event. Only a transition means anything here; a comment
// is posted on each open pull request linked to the issue that moved.
func (s *Sync) Handle(ctx context.Context, e events.Event) error {
	if e.Topic != events.TopicIssueTransitioned || e.OrgID == uuid.Nil {
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

	ctx = db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: e.OrgID}))

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
	for _, t := range targets {
		host, err := HostFor(t.repo.Host)
		if err != nil {
			continue
		}
		if err := host.CommentOnPullRequest(ctx, s.client, &t.repo, t.number, body); err != nil {
			failed = append(failed, fmt.Sprintf("%s#%d: %v", t.repo.Name, t.number, err))
		}
	}
	if len(failed) > 0 {
		return errors.New(strings.Join(failed, "; "))
	}
	return nil
}
