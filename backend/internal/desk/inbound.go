package desk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/mailin"
	"github.com/armature/armature/backend/internal/observability"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/tenant"
)

// Outcome is what became of a mail the desk read.
type Outcome string

const (
	// Commented is a reply that became a comment on its request.
	Commented Outcome = "commented"
	// Refused is a mail from somebody not on the request; nothing was written
	// and the sender was told to use the portal.
	Refused Outcome = "refused"
	// Unmatched is a mail that names no request.
	Unmatched Outcome = "unmatched"
	// Ambiguous is a key that fits a request in more than one organization
	// the sender belongs to.
	Ambiguous Outcome = "ambiguous"
	// Empty is a reply with nothing left once the quotes went.
	Empty Outcome = "empty"
	// Skipped is a mail that was not for the desk at all: its own outbound,
	// or something else in a shared box. It is left where it is.
	Skipped Outcome = "skipped"
	// Duplicate is a mail the desk had already read.
	Duplicate Outcome = "duplicate"
)

// Inbound reads the desk's mailbox and turns replies into comments. It runs in
// the worker, on the same footing as the notifier that sent the mails being
// answered.
type Inbound struct {
	db      *db.Cluster
	desk    *Service
	issues  *issue.Service
	mailer  Mailer
	inbox   mailin.Inbox
	log     *slog.Logger
	address string
	from    string
	appURL  string
	// interval is how often the box is looked at.
	interval time.Duration
	// skipped remembers uids left in the box so they are not read again this
	// process; a restart reads them once more, which is cheap.
	skipped map[string]struct{}
}

// InboundConfig is where the mailbox is and what the desk calls itself.
type InboundConfig struct {
	// Address is the inbox replies come to; only mail addressed to it is read.
	Address string
	// From is the desk's own sender; its mail is left alone.
	From     string
	AppURL   string
	Interval time.Duration
}

func NewInbound(cluster *db.Cluster, d *Service, issues *issue.Service, mailer Mailer, inbox mailin.Inbox, cfg InboundConfig, log *slog.Logger) *Inbound {
	if cfg.Interval == 0 {
		cfg.Interval = 30 * time.Second
	}
	return &Inbound{
		db: cluster, desk: d, issues: issues, mailer: mailer, inbox: inbox, log: log,
		address: strings.ToLower(bareAddress(cfg.Address)), from: strings.ToLower(bareAddress(cfg.From)),
		appURL: strings.TrimRight(cfg.AppURL, "/"), interval: cfg.Interval, skipped: map[string]struct{}{},
	}
}

// Run polls until the context ends.
func (n *Inbound) Run(ctx context.Context) error {
	ticker := time.NewTicker(n.interval)
	defer ticker.Stop()
	for {
		if err := observability.Run(ctx, "mail-inbound", n.Once); err != nil {
			n.log.Warn("the mailbox could not be read", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Once reads the box one time.
func (n *Inbound) Once(ctx context.Context) error {
	// What the box still holds this time; a uid gone from the box is
	// forgotten, so the set is never larger than the box.
	present := map[string]struct{}{}
	err := n.inbox.Poll(ctx,
		func(uid string) bool {
			present[uid] = struct{}{}
			_, seen := n.skipped[uid]
			return seen
		},
		func(uid string, raw []byte) bool {
			outcome, err := n.Handle(ctx, raw)
			if err != nil {
				// Left in the box for the next look: a database or a relay
				// that is down should not lose a reply.
				n.log.Warn("a mail could not be handled yet", "uid", uid, "error", err)
				return false
			}
			if outcome == Skipped {
				n.skipped[uid] = struct{}{}
				return false
			}
			n.log.Info("mail read", "uid", uid, "outcome", outcome)
			return true
		})
	if err == nil {
		for uid := range n.skipped {
			if _, still := present[uid]; !still {
				delete(n.skipped, uid)
			}
		}
	}
	return err
}

// Handle decides what one mail is and acts on it. It returns an error only
// when the decision could not be made; a decided mail, whatever the decision,
// is consumed.
func (n *Inbound) Handle(ctx context.Context, raw []byte) (Outcome, error) {
	m, err := mailin.Parse(raw)
	if err != nil {
		// Not a mail at all; there is nothing to come back to.
		n.log.Warn("unreadable mail left aside", "error", err)
		return Skipped, nil
	}
	if !n.forTheDesk(m) {
		return Skipped, nil
	}
	messageID := m.MessageID
	if messageID == "" {
		sum := sha256.Sum256(raw)
		messageID = "sha256:" + hex.EncodeToString(sum[:])
	}

	// The row first, so a mail delivered twice is refused by the database
	// however many workers are looking.
	claimed, err := n.claim(ctx, messageID, m)
	if err != nil {
		return "", err
	}
	if !claimed {
		return Duplicate, nil
	}

	key := mailin.IssueKeyIn(m.Subject, m.InReplyTo, m.References)
	if key == "" {
		return n.finish(ctx, messageID, nil, Unmatched, "no request named", m, unmatchedMessage(n.appURL))
	}
	where, err := n.place(ctx, key, m.From)
	if err != nil {
		return "", err
	}
	switch len(where) {
	case 0:
		return n.finish(ctx, messageID, nil, Refused, "sender not on "+key, m, notYoursMessage(key, n.appURL))
	case 1:
	default:
		return n.finish(ctx, messageID, nil, Ambiguous, key+" is a request in more than one organization", m, unmatchedMessage(n.appURL))
	}
	p := where[0]
	// A From header is a claim. When the receiving server checked it and found
	// it false, the mail is nobody's word, least of all this sender's.
	if m.SenderFailedChecks() {
		return n.finish(ctx, messageID, &p, Refused, "the sender's own server says this is not from them", m, Message{})
	}
	// The list is about customers; an agent's own domain is often not on it.
	if !p.role.IsAgent() && !project.Trusts(p.trustedDomains, m.From) {
		return n.finish(ctx, messageID, &p, Refused, "sender's domain not trusted by "+key, m, notTrustedMessage(key, p.trustedDomains, m.From, n.appURL))
	}
	text := mailin.StripQuotes(m.Text)
	if text == "" {
		return n.finish(ctx, messageID, &p, Empty, "nothing left once the quotes went", m, Message{})
	}

	orgCtx := db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: p.orgID}))
	actor := issue.Actor{UserID: p.userID, OrgRole: p.role}
	var comment *issue.Comment
	switch {
	case p.role.IsAgent():
		// Nothing proves a colleague's address, so their mail is filed where
		// the desk can see it and the customer cannot; they answer in the app.
		comment, _, err = n.issues.AddNote(orgCtx, key, issue.TextDocument(text), actor)
	case !p.reporter:
		// A follower's word is a comment like any other.
		comment, _, err = n.issues.AddComment(orgCtx, key, issue.TextDocument(text), actor)
	default:
		comment, _, err = n.desk.Reply(orgCtx, key, text, actor)
	}
	if err != nil {
		return "", fmt.Errorf("comment on %s: %w", key, err)
	}
	p.commentID = &comment.ID
	return n.finish(ctx, messageID, &p, Commented, "", m, Message{})
}

// forTheDesk says whether a mail is one to read: addressed to the inbox, and
// not the desk's own. A shared box, as Mailpit is, holds plenty that is not.
func (n *Inbound) forTheDesk(m *mailin.Message) bool {
	if m.From == "" || m.From == n.address || m.From == n.from {
		return false
	}
	for _, to := range m.Recipients {
		if to == n.address {
			return true
		}
	}
	return false
}

// placement is where a mail landed: the request, and who the sender is there.
type placement struct {
	orgID          uuid.UUID
	issueID        uuid.UUID
	userID         uuid.UUID
	role           auth.OrgRole
	reporter       bool
	trustedDomains []string
	commentID      *uuid.UUID
}

// place finds every organization in which the key is a request the sender may
// reply to: as its reporter, as a follower, or as an agent. A key alone is
// ambiguous across tenants, so the sender is part of the question.
func (n *Inbound) place(ctx context.Context, key, from string) ([]placement, error) {
	projectKey, num, err := issue.ParseKey(key)
	if err != nil {
		return nil, nil
	}
	var out []placement
	err = n.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT p.org_id, i.id, u.id, m.org_role,
			       i.reporter_id = u.id,
			       EXISTS (SELECT 1 FROM issue_watcher w WHERE w.issue_id = i.id AND w.user_id = u.id),
			       p.trusted_domains
			FROM project p
			JOIN issue i ON i.project_id = p.id AND i.key_num = $2
			JOIN app_user u ON u.email = $3 AND u.is_active
			JOIN org_member m ON m.org_id = p.org_id AND m.user_id = u.id
			WHERE p.key = $1 AND p.kind = 'service' AND i.request_type_id IS NOT NULL`, projectKey, num, from)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				p        placement
				watching bool
			)
			if err := rows.Scan(&p.orgID, &p.issueID, &p.userID, &p.role, &p.reporter, &watching, &p.trustedDomains); err != nil {
				return err
			}
			if p.reporter || watching || p.role.IsAgent() {
				out = append(out, p)
			}
		}
		return rows.Err()
	})
	return out, err
}

// claim writes the mail's row as processing and says whether it was ours to
// handle. A row already there is a duplicate, unless it is our own attempt
// that died mid-way, which is taken up again after a while.
func (n *Inbound) claim(ctx context.Context, messageID string, m *mailin.Message) (bool, error) {
	var claimed bool
	_, err := n.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO inbound_mail (message_id, from_email, subject, outcome)
			VALUES ($1, $2, $3, 'processing')
			ON CONFLICT (message_id) DO UPDATE
			SET received_at = now()
			WHERE inbound_mail.outcome = 'processing' AND inbound_mail.received_at < now() - interval '5 minutes'`,
			messageID, m.From, m.Subject)
		if err != nil {
			return err
		}
		claimed = tag.RowsAffected() == 1
		return nil
	})
	return claimed, err
}

// finish records the outcome and, when there is something to say, says it to
// the sender. Machines are not answered, nor daemons, nor the desk itself.
func (n *Inbound) finish(ctx context.Context, messageID string, p *placement, outcome Outcome, detail string, m *mailin.Message, bounce Message) (Outcome, error) {
	_, err := n.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var orgID, issueID, commentID *uuid.UUID
		if p != nil {
			orgID, issueID, commentID = &p.orgID, &p.issueID, p.commentID
		}
		_, err := tx.Exec(ctx, `
			UPDATE inbound_mail SET outcome = $2, detail = $3, org_id = $4, issue_id = $5, comment_id = $6
			WHERE message_id = $1`, messageID, string(outcome), detail, orgID, issueID, commentID)
		return err
	})
	if err != nil {
		return "", err
	}
	if bounce.Subject != "" && !m.AutoSubmitted && !isDaemon(m.From) && n.mailer != nil {
		if err := n.mailer.Send(ctx, Mail{To: m.From, Subject: bounce.Subject, Body: bounce.Body}); err != nil {
			n.log.Warn("the sender could not be told", "to", m.From, "error", err)
		}
	}
	return outcome, nil
}

func isDaemon(address string) bool {
	local, _, _ := strings.Cut(address, "@")
	local = strings.ToLower(local)
	return local == "mailer-daemon" || local == "postmaster" || strings.HasPrefix(local, "no-reply") || strings.HasPrefix(local, "noreply")
}

// bareAddress takes the address out of "Name <address>".
func bareAddress(raw string) string {
	if start := strings.LastIndex(raw, "<"); start >= 0 {
		if end := strings.LastIndex(raw, ">"); end > start {
			return strings.TrimSpace(raw[start+1 : end])
		}
	}
	return strings.TrimSpace(raw)
}

// notYoursMessage tells a sender their reply was not taken.
func notYoursMessage(key, appURL string) Message {
	return Message{
		Subject: fmt.Sprintf("[%s] your reply could not be added", key),
		Body: fmt.Sprintf("Only the person who raised %s, the people following it and the desk's agents may reply by mail, and this address is none of them.\n\nWrite from the address you raised it with, or use the portal at %s\n",
			key, appURL),
	}
}

// notTrustedMessage tells a sender the desk takes mail from other domains.
func notTrustedMessage(key string, domains []string, from, appURL string) Message {
	return Message{
		Subject: fmt.Sprintf("[%s] your reply could not be added", key),
		Body: fmt.Sprintf("%s belongs to a desk that takes requests from addresses at %s only, and this address is at %s.\n\nWrite from an address at one of them, or use the portal at %s\n",
			key, strings.Join(domains, ", "), project.DomainOf(from), appURL),
	}
}

// unmatchedMessage tells a sender their mail named no request.
func unmatchedMessage(appURL string) Message {
	return Message{
		Subject: "Your mail could not be matched to a request",
		Body:    fmt.Sprintf("We could not tell which request this is about. Reply to a mail from the desk, keeping its subject, or use the portal at %s\n", appURL),
	}
}

var _ = errors.Is
var _ = pgx.ErrNoRows
