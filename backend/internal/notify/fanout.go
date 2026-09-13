package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/mail"
	"github.com/armature/armature/backend/internal/tenant"
)

// FanOut runs in the worker: every event about an issue becomes inbox rows for
// the people on it, and mail for those who want it.
type FanOut struct {
	db     *db.Cluster
	redis  *redis.Client
	mailer mail.Mailer
	log    *slog.Logger
	appURL string
	group  string
	block  time.Duration
}

// NewFanOut builds the consumer. A nil mailer means the inbox alone is written.
func NewFanOut(cluster *db.Cluster, rdb *redis.Client, mailer mail.Mailer, appURL string, log *slog.Logger) *FanOut {
	return &FanOut{db: cluster, redis: rdb, mailer: mailer, log: log, appURL: strings.TrimRight(appURL, "/"), group: "notify", block: 5 * time.Second}
}

// Run consumes the stream until the context ends.
func (f *FanOut) Run(ctx context.Context) error {
	err := f.redis.XGroupCreateMkStream(ctx, events.StreamKey, f.group, "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create consumer group: %w", err)
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		streams, err := f.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: f.group, Consumer: "worker", Streams: []string{events.StreamKey, ">"}, Count: 50, Block: f.block,
		}).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			f.log.Warn("notify read failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				e := events.FromMessage(msg)
				hctx, done := events.Continue(ctx, e, f.group)
				err := f.Handle(hctx, e)
				done(err)
				if err != nil {
					f.log.Warn("notify could not act on an event", "id", msg.ID, "error", err)
				}
				if err := f.redis.XAck(ctx, events.StreamKey, f.group, msg.ID).Err(); err != nil {
					f.log.Warn("notify could not acknowledge", "id", msg.ID, "error", err)
				}
			}
		}
	}
}

// payload is every field any topic carries; a topic reads the ones it has.
type payload struct {
	Key       string      `json:"key"`
	IssueKey  string      `json:"issueKey"`
	ActorID   uuid.UUID   `json:"actorId"`
	UserID    uuid.UUID   `json:"userId"`
	CommentID uuid.UUID   `json:"commentId"`
	Internal  bool        `json:"internal"`
	Mentions  []uuid.UUID `json:"mentions"`
	ToStatus  string      `json:"toStatus"`
	Metric    string      `json:"metric"`
	Changes   []struct {
		Field string `json:"field"`
	} `json:"changes"`
}

func (p payload) key() string {
	if p.Key != "" {
		return p.Key
	}
	return p.IssueKey
}

// subject is the issue an event is about, as far as a notification needs it.
type subject struct {
	id        uuid.UUID
	key       string
	summary   string
	assignee  *uuid.UUID
	reporter  *uuid.UUID
	isRequest bool
	actor     string
	watchers  []uuid.UUID
	comment   string
}

// tell is one row to write: who, why, and the words.
type tell struct {
	user  uuid.UUID
	kind  string
	title string
	body  string
}

// Handle turns one event into inbox rows and mail. It is safe to call twice
// with the same event: the second pass finds every row already there.
func (f *FanOut) Handle(ctx context.Context, e events.Event) error {
	if e.OrgID == uuid.Nil {
		return nil
	}
	var p payload
	if err := json.Unmarshal(e.Payload, &p); err != nil || p.key() == "" {
		return nil
	}
	ctx = db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: e.OrgID}))
	s, err := f.lookup(ctx, p, e.Topic)
	if err != nil || s == nil {
		return err
	}
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	for _, t := range tellsFor(e.Topic, p, s) {
		if err := f.deliver(ctx, e.ID, s, t); err != nil {
			return err
		}
	}
	return nil
}

// tellsFor decides who hears what. Pure, so the rules can be read in a test.
func tellsFor(topic string, p payload, s *subject) []tell {
	link := s.key
	participants := func(except ...uuid.UUID) []uuid.UUID {
		seen := map[uuid.UUID]bool{p.ActorID: true}
		for _, id := range except {
			seen[id] = true
		}
		var out []uuid.UUID
		add := func(id *uuid.UUID) {
			if id != nil && !seen[*id] {
				seen[*id] = true
				out = append(out, *id)
			}
		}
		add(s.assignee)
		add(s.reporter)
		for i := range s.watchers {
			add(&s.watchers[i])
		}
		return out
	}
	var out []tell
	switch topic {
	case events.TopicIssueCreated:
		if s.assignee != nil && *s.assignee != p.ActorID {
			out = append(out, tell{*s.assignee, KindAssigned, fmt.Sprintf("%s assigned %s to you", s.actor, link), s.summary})
		}
		for _, id := range participants(orNil(s.assignee)) {
			out = append(out, tell{id, KindWatching, fmt.Sprintf("%s filed %s", s.actor, link), s.summary})
		}
	case events.TopicIssueUpdated:
		var fields []string
		assigned := false
		for _, c := range p.Changes {
			if c.Field == "assignee" {
				assigned = true
			}
			fields = append(fields, c.Field)
		}
		if len(fields) == 0 {
			return nil
		}
		if assigned && s.assignee != nil && *s.assignee != p.ActorID {
			out = append(out, tell{*s.assignee, KindAssigned, fmt.Sprintf("%s assigned %s to you", s.actor, link), s.summary})
		}
		title := fmt.Sprintf("%s changed %s on %s", s.actor, strings.Join(fields, ", "), link)
		var except uuid.UUID
		if assigned && s.assignee != nil {
			except = *s.assignee
		}
		for _, id := range participants(except) {
			out = append(out, tell{id, KindWatching, title, s.summary})
		}
	case events.TopicIssueTransitioned:
		title := fmt.Sprintf("%s moved %s to %s", s.actor, link, p.ToStatus)
		for _, id := range participants() {
			out = append(out, tell{id, KindTransitioned, title, s.summary})
		}
	case events.TopicCommentAdded:
		mentioned := map[uuid.UUID]bool{}
		for _, id := range p.Mentions {
			if id == p.ActorID || mentioned[id] {
				continue
			}
			mentioned[id] = true
			out = append(out, tell{id, KindMentioned, fmt.Sprintf("%s mentioned you on %s", s.actor, link), s.comment})
		}
		title := fmt.Sprintf("%s commented on %s", s.actor, link)
		if p.Internal {
			title = fmt.Sprintf("%s left a note on %s", s.actor, link)
		}
		for _, id := range participants(p.Mentions...) {
			out = append(out, tell{id, KindCommented, title, s.comment})
		}
	case events.TopicWatcherAdded:
		if p.UserID != uuid.Nil && p.UserID != p.ActorID {
			out = append(out, tell{p.UserID, KindWatching, fmt.Sprintf("%s added you to watch %s", s.actor, link), s.summary})
		}
	case events.TopicSLABreached:
		title := fmt.Sprintf("%s missed its %s goal", link, strings.ReplaceAll(p.Metric, "_", " "))
		for _, id := range participants() {
			out = append(out, tell{id, KindSLABreached, title, s.summary})
		}
	}
	return out
}

func orNil(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}

// lookup reads the issue and who is on it. Customers are never told here: the
// desk speaks to them on their own terms.
func (f *FanOut) lookup(ctx context.Context, p payload, topic string) (*subject, error) {
	projectKey, num, err := issue.ParseKey(p.key())
	if err != nil {
		return nil, nil
	}
	s := &subject{}
	err = f.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			SELECT i.id, p.key || '-' || i.key_num, i.summary, i.assignee_id, i.reporter_id,
			       p.kind = 'service' AND i.request_type_id IS NOT NULL,
			       COALESCE((SELECT name FROM app_user WHERE id = $3), 'Somebody')
			FROM issue i JOIN project p ON p.id = i.project_id
			WHERE p.key = $1 AND i.key_num = $2`, projectKey, num, p.ActorID).
			Scan(&s.id, &s.key, &s.summary, &s.assignee, &s.reporter, &s.isRequest, &s.actor)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT w.user_id FROM issue_watcher w
			JOIN app_user u ON u.id = w.user_id AND u.is_active
			JOIN org_member m ON m.user_id = w.user_id AND m.org_id = current_org_id() AND m.org_role <> 'customer'
			WHERE w.issue_id = $1 ORDER BY w.created_at`, s.id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			s.watchers = append(s.watchers, id)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if topic == events.TopicCommentAdded && p.CommentID != uuid.Nil {
			var body []byte
			if err := tx.QueryRow(ctx, `SELECT body FROM issue_comment WHERE id = $1`, p.CommentID).Scan(&body); err == nil {
				s.comment = issue.PlainText(body)
			}
		}
		// The assignee and the reporter count only when they are members who
		// are not customers; a customer reporter is the desk's to tell.
		for _, who := range []**uuid.UUID{&s.assignee, &s.reporter} {
			if *who == nil {
				continue
			}
			var ok bool
			err := tx.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM org_member m JOIN app_user u ON u.id = m.user_id AND u.is_active
				               WHERE m.user_id = $1 AND m.org_id = current_org_id() AND m.org_role <> 'customer')`, **who).Scan(&ok)
			if err != nil {
				return err
			}
			if !ok {
				*who = nil
			}
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// deliver writes one row and, when it is new and wanted, mails it or queues
// it for the digest. A request's people are mailed by the desk, not here.
func (f *FanOut) deliver(ctx context.Context, eventID uuid.UUID, s *subject, t tell) error {
	var (
		prefs    Preferences
		inserted bool
		id       uuid.UUID
		to       string
	)
	_, err := f.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := readPreferences(ctx, tx, t.user, &prefs); err != nil {
			return err
		}
		if !prefs.shows(t.kind) {
			return nil
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO notification (org_id, user_id, issue_id, kind, title, body, link, event_id)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (user_id, event_id, kind) DO NOTHING
			RETURNING id`, t.user, s.id, t.kind, t.title, t.body, f.appURL+"/issues/"+s.key, eventID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		inserted = true
		if !prefs.mails(t.kind) || s.isRequest || f.mailer == nil {
			return nil
		}
		if prefs.Digest != DigestOff {
			_, err := tx.Exec(ctx, `INSERT INTO notification_digest (org_id, user_id, notification_id) VALUES (current_org_id(), $1, $2)`, t.user, id)
			return err
		}
		return tx.QueryRow(ctx, `SELECT email FROM app_user WHERE id = $1`, t.user).Scan(&to)
	})
	if err != nil || !inserted || to == "" {
		return err
	}
	return f.mailer.Send(ctx, mail.Mail{
		To:      to,
		Subject: fmt.Sprintf("[%s] %s", s.key, t.title),
		Body:    fmt.Sprintf("%s\n\n%s\n\nOpen it at %s/issues/%s\n", t.title, t.body, f.appURL, s.key),
	})
}
