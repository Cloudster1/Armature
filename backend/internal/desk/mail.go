package desk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
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

// Mail, Mailer and SMTPMailer live in the mail package now; the desk keeps
// the names so its callers and tests read as before.
type (
	Mail       = mail.Mail
	Mailer     = mail.Mailer
	SMTPMailer = mail.SMTPMailer
)

// Message is what a customer is told, before it is addressed.
type Message struct {
	Subject string
	Body    string
}

// requestLink is where a mail sends its reader: the desk's door, with the
// request behind it. A reader with a session walks straight through.
func requestLink(appURL, orgSlug, key string) string {
	return fmt.Sprintf("%s/desk/%s?next=%s", strings.TrimRight(appURL, "/"), orgSlug, url.QueryEscape("/portal/requests/"+key))
}

// codeMessage words the mail that carries a portal code.
func codeMessage(orgName, code string) Message {
	return Message{
		Subject: fmt.Sprintf("Your code for %s is %s", orgName, code),
		Body: fmt.Sprintf("Your code for %s is %s. It works for ten minutes.\n\nIf you did not ask for it, nobody can use it without this mail; you can ignore it.\n",
			orgName, code),
	}
}

// CodeMail is the code, addressed. The API sends it in the request, since the
// person is waiting for it and a one-time code has no business in the stream.
func CodeMail(to, orgName, code string) Mail {
	m := codeMessage(orgName, code)
	return Mail{To: to, Subject: m.Subject, Body: m.Body}
}

// raisedMessage words the receipt for a request, which for somebody without an
// account is the only one there is.
func raisedMessage(key, summary, link string) Message {
	return Message{
		Subject: fmt.Sprintf("[%s] we have your request", key),
		Body: fmt.Sprintf("We have your request %s: %s\n\nWe will write when there is news. Follow it, or add to it, at %s\n",
			key, summary, link),
	}
}

// replyMessage words the mail for an agent's public reply.
func replyMessage(key, summary, agent, text, link string) Message {
	return Message{
		Subject: fmt.Sprintf("[%s] %s replied to your request", key, agent),
		Body: fmt.Sprintf("%s replied to %s: %s\n\n%s\n\nSee the request at %s\n",
			agent, key, summary, text, link),
	}
}

// assignedMessage words the mail for somebody taking the request on.
func assignedMessage(key, summary, who, link string) Message {
	return Message{
		Subject: fmt.Sprintf("[%s] %s is handling your request", key, who),
		Body: fmt.Sprintf("%s is handling %s: %s\n\nSee the request at %s\n",
			who, key, summary, link),
	}
}

// followingMessage words the mail for being added to follow a request. The
// stop link ends it with one press; only this first mail can carry it.
func followingMessage(key, summary, by, link, stop string) Message {
	body := fmt.Sprintf("%s added you to follow %s: %s\n\nYou will be mailed when the desk replies and when it is resolved. See the request at %s\n",
		by, key, summary, link)
	if stop != "" {
		body += fmt.Sprintf("\nStop following this request: %s\n", stop)
	}
	return Message{Subject: fmt.Sprintf("[%s] %s added you to follow a request", key, by), Body: body}
}

// resolvedMessage words the mail for a request reaching a done status.
func resolvedMessage(key, summary, status, link string) Message {
	return Message{
		Subject: fmt.Sprintf("[%s] your request is %s", key, strings.ToLower(status)),
		Body: fmt.Sprintf("%s: %s is now %s.\n\nIf that is not right, reply on the request and it comes back to us: %s\n",
			key, summary, status, link),
	}
}

// Notifier runs in the worker and tells customers what happened to their
// requests, from the same event stream everything else reads.
type Notifier struct {
	db     *db.Cluster
	redis  *redis.Client
	mailer Mailer
	log    *slog.Logger
	appURL string
	inbox  string
	group  string
	block  time.Duration
}

func NewNotifier(cluster *db.Cluster, rdb *redis.Client, mailer Mailer, appURL string, log *slog.Logger) *Notifier {
	return &Notifier{db: cluster, redis: rdb, mailer: mailer, log: log, appURL: strings.TrimRight(appURL, "/"), group: "desk-mail", block: 5 * time.Second}
}

// WithInbox names the address replies go to. Every mail then carries it as
// Reply-To and says that answering works.
func (n *Notifier) WithInbox(address string) *Notifier {
	n.inbox = strings.TrimSpace(address)
	return n
}

// Run consumes the stream until the context ends.
func (n *Notifier) Run(ctx context.Context) error {
	err := n.redis.XGroupCreateMkStream(ctx, events.StreamKey, n.group, "$").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create consumer group: %w", err)
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		streams, err := n.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: n.group, Consumer: "worker", Streams: []string{events.StreamKey, ">"}, Count: 50, Block: n.block,
		}).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			n.log.Warn("desk mail read failed", "error", err)
			time.Sleep(time.Second)
			continue
		}
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				e := events.FromMessage(msg)
				hctx, done := events.Continue(ctx, e, n.group)
				err := n.Handle(hctx, e)
				done(err)
				if err != nil {
					n.log.Warn("desk mail could not act on an event", "id", msg.ID, "error", err)
				}
				if err := n.redis.XAck(ctx, events.StreamKey, n.group, msg.ID).Err(); err != nil {
					n.log.Warn("desk mail could not acknowledge", "id", msg.ID, "error", err)
				}
			}
		}
	}
}

// about is what every mail about a request needs to know about it. Only an
// issue raised through the portal in a service project is a request; anything
// else is not mailed about.
type about struct {
	id       uuid.UUID
	key      string
	summary  string
	slug     string
	reporter *uuid.UUID
}

func (n *Notifier) lookup(ctx context.Context, key string) (*about, error) {
	a := about{key: key}
	err := n.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT i.id, i.summary, o.slug, i.reporter_id
			FROM issue i
			JOIN project p ON p.id = i.project_id
			JOIN org o ON o.id = p.org_id
			WHERE p.key || '-' || i.key_num = $1 AND p.kind = 'service' AND i.request_type_id IS NOT NULL`, key).
			Scan(&a.id, &a.summary, &a.slug, &a.reporter)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// recipients is who hears about a request: the reporter and the watchers, less
// the person whose act it was, who needs no mail about what they just did.
func (n *Notifier) recipients(ctx context.Context, a *about, except uuid.UUID) ([]string, error) {
	var out []string
	err := n.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT DISTINCT u.email FROM app_user u
			WHERE u.is_active AND u.id <> $2
			  AND (u.id = (SELECT reporter_id FROM issue WHERE id = $1)
			       OR u.id IN (SELECT user_id FROM issue_watcher WHERE issue_id = $1))
			ORDER BY u.email`, a.id, except)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var email string
			if err := rows.Scan(&email); err != nil {
				return err
			}
			out = append(out, email)
		}
		return rows.Err()
	})
	return out, err
}

// Handle mails the people on a request: the receipt when it is raised, an
// agent's public reply, who took it on, its resolution, and being added to
// follow it. Anything else is not theirs to hear about.
func (n *Notifier) Handle(ctx context.Context, e events.Event) error {
	if e.OrgID == uuid.Nil {
		return nil
	}
	ctx = db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: e.OrgID}))

	switch e.Topic {
	case events.TopicIssueCreated:
		var p struct {
			Key     string    `json:"key"`
			ActorID uuid.UUID `json:"actorId"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil || p.Key == "" {
			return nil
		}
		a, err := n.lookup(ctx, p.Key)
		// A request an agent files for somebody is theirs to explain; the
		// receipt is for the person who raised it themselves.
		if err != nil || a == nil || a.reporter == nil || *a.reporter != p.ActorID {
			return err
		}
		return n.tell(ctx, a, uuid.Nil, raisedMessage(a.key, a.summary, n.link(a)))

	case events.TopicCommentAdded:
		var p struct {
			Key       string    `json:"key"`
			CommentID uuid.UUID `json:"commentId"`
			Internal  bool      `json:"internal"`
			ActorID   uuid.UUID `json:"actorId"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil || p.Internal || p.Key == "" {
			return nil
		}
		a, err := n.lookup(ctx, p.Key)
		if err != nil || a == nil {
			return err
		}
		var (
			agent, text  string
			actorIsAgent bool
		)
		err = n.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `
				SELECT COALESCE(au.name, ''), c.body::text, COALESCE(m.org_role <> 'customer', false)
				FROM issue_comment c
				LEFT JOIN app_user au ON au.id = c.author_id
				LEFT JOIN org_member m ON m.user_id = c.author_id AND m.org_id = current_org_id()
				WHERE c.id = $1`, p.CommentID).Scan(&agent, &text, &actorIsAgent)
		})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && !actorIsAgent) {
			return nil
		}
		if err != nil {
			return err
		}
		return n.tell(ctx, a, p.ActorID, replyMessage(a.key, a.summary, agent, plainText(text), n.link(a)))

	case events.TopicIssueTransitioned:
		var p struct {
			Key      string    `json:"key"`
			ToStatus string    `json:"toStatus"`
			ActorID  uuid.UUID `json:"actorId"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil || p.Key == "" {
			return nil
		}
		a, err := n.lookup(ctx, p.Key)
		if err != nil || a == nil {
			return err
		}
		var done bool
		err = n.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `
				SELECT s.category = 'done' FROM issue i JOIN issue_status s ON s.id = i.status_id WHERE i.id = $1`, a.id).Scan(&done)
		})
		if err != nil || !done {
			return err
		}
		// The resolution mail carries the one chance to rate; the token is
		// minted here, once, and the customer alone holds it.
		var token string
		if _, err := n.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
			var err error
			token, err = invite(ctx, tx, a.id)
			return err
		}); err != nil {
			return err
		}
		msg := resolvedMessage(a.key, a.summary, p.ToStatus, n.link(a))
		if token != "" {
			msg.Body += fmt.Sprintf("\nHow did we do? Tell us in one click: %s/rate/%s\n", n.appURL, url.PathEscape(token))
		}
		return n.tell(ctx, a, p.ActorID, msg)

	case events.TopicIssueUpdated:
		var p struct {
			Key     string    `json:"key"`
			ActorID uuid.UUID `json:"actorId"`
			Changes []struct {
				Field string `json:"field"`
				To    string `json:"to"`
			} `json:"changes"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil || p.Key == "" {
			return nil
		}
		var handler string
		for _, c := range p.Changes {
			if c.Field == "assignee" && c.To != "" {
				handler = c.To
			}
		}
		if handler == "" {
			return nil
		}
		a, err := n.lookup(ctx, p.Key)
		if err != nil || a == nil {
			return err
		}
		return n.tell(ctx, a, p.ActorID, assignedMessage(a.key, a.summary, handler, n.link(a)))

	case events.TopicWatcherAdded:
		var p struct {
			Key     string    `json:"key"`
			UserID  uuid.UUID `json:"userId"`
			ActorID uuid.UUID `json:"actorId"`
			Token   string    `json:"token"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil || p.Key == "" || p.UserID == p.ActorID {
			return nil
		}
		a, err := n.lookup(ctx, p.Key)
		if err != nil || a == nil {
			return err
		}
		var to, by string
		err = n.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `
				SELECT COALESCE((SELECT email FROM app_user WHERE id = $1 AND is_active), ''),
				       COALESCE((SELECT name FROM app_user WHERE id = $2), 'Somebody')`, p.UserID, p.ActorID).Scan(&to, &by)
		})
		if err != nil || to == "" {
			return err
		}
		stop := ""
		if p.Token != "" {
			stop = n.appURL + "/unwatch?token=" + url.QueryEscape(p.Token)
		}
		return n.mailer.Send(ctx, n.mail(a, to, followingMessage(a.key, a.summary, by, n.link(a), stop)))
	}
	return nil
}

func (n *Notifier) link(a *about) string { return requestLink(n.appURL, a.slug, a.key) }

// tell mails everyone on a request but the actor. Every address is tried; the
// first failure is what comes back.
func (n *Notifier) tell(ctx context.Context, a *about, except uuid.UUID, msg Message) error {
	to, err := n.recipients(ctx, a, except)
	if err != nil {
		return err
	}
	var first error
	for _, address := range to {
		if err := n.mailer.Send(ctx, n.mail(a, address, msg)); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// mail addresses a message. Its id begins with the request's key so that an
// answer to it can be placed even without the subject, and, when there is an
// inbox, the reader is told that answering works.
func (n *Notifier) mail(a *about, to string, msg Message) Mail {
	m := Mail{To: to, Subject: msg.Subject, Body: msg.Body, MessageID: messageID(a.key, n.from())}
	if n.inbox != "" {
		m.ReplyTo = n.inbox
		m.Body += "\nYou can answer this mail; your words are added to the request.\n"
	}
	return m
}

// from is the host the desk's ids are minted under: the inbox's, else a fixed
// one, since an id only has to be unique and shaped like an address.
func (n *Notifier) from() string {
	if _, host, ok := strings.Cut(n.inbox, "@"); ok && host != "" {
		return host
	}
	return "armature.local"
}

// messageID mints <KEY-12.random@host>. The key is what a reply is matched by
// when its subject was rewritten; the rest keeps two mails apart.
func messageID(key, host string) string {
	return fmt.Sprintf("<%s.%s@%s>", key, uuid.New().String(), host)
}

// plainText reads a comment's document the way the issue package renders
// it for mail; text that is not a document comes back as it was.
func plainText(doc string) string {
	if !json.Valid([]byte(doc)) {
		return doc
	}
	return issue.PlainText(json.RawMessage(doc))
}
