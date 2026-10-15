package desk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/mailin"
	"github.com/armature/armature/backend/internal/project"
)

// mailSummary is what a request by mail is called when its subject says nothing.
const mailSummary = "Request by mail"

// mailDesk is a desk a new mail may raise a request at.
type mailDesk struct {
	key            string
	requestTypeID  uuid.UUID
	trustedDomains []string
}

// mailDesk finds the desk a plus address names, when it raises requests from
// mail at all. Nil is no such desk, or one that does not.
func (n *Inbound) mailDesk(ctx context.Context, key string) (*mailDesk, error) {
	if key == "" {
		return nil, nil
	}
	var out *mailDesk
	err := n.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var d mailDesk
		err := tx.QueryRow(ctx, `
			SELECT key, mail_request_type_id, trusted_domains FROM project
			WHERE key = $1 AND kind = 'service' AND archived_at IS NULL AND mail_request_type_id IS NOT NULL`, key).
			Scan(&d.key, &d.requestTypeID, &d.trustedDomains)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err == nil {
			out = &d
		}
		return err
	})
	return out, err
}

// raise turns a new mail to a desk into a request by its sender. Machines and
// colleagues raise nothing: nobody asked, and nothing proves a colleague's address.
func (n *Inbound) raise(ctx context.Context, messageID string, m *mailin.Message, at *mailDesk, out **placement) (Outcome, error) {
	if m.AutoSubmitted || isDaemon(m.From) {
		return n.finish(ctx, messageID, nil, Automatic, "sent by a machine, so no request was raised at "+at.key, m, Message{})
	}
	if m.SenderFailedChecks() {
		return n.finish(ctx, messageID, nil, Refused, "the sender's own server says this is not from them", m, Message{})
	}
	agent, err := n.isAgent(ctx, m.From)
	if err != nil {
		return "", err
	}
	if agent {
		return n.finish(ctx, messageID, nil, Unmatched, "an agent's mail raises no request", m, unmatchedMessage(n.appURL))
	}
	// Checked before an account is made, so a stranger's mail leaves no trace.
	if !project.Trusts(at.trustedDomains, m.From) {
		return n.finish(ctx, messageID, nil, Refused, "sender's domain not trusted by "+at.key, m, notTrustedNewMessage(at.trustedDomains, m.From, n.appURL))
	}

	ctx = db.PinPrimary(ctx)
	// An attempt that raised the request and failed after leaves its id on the
	// row; the next one finishes that request rather than raising another.
	if p, err := n.raisedBefore(ctx, messageID); err != nil || p != nil {
		if err != nil {
			return "", err
		}
		*out = p
		return n.finish(ctx, messageID, p, Raised, "", m, Message{})
	}
	var customer auth.User
	_, err = n.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		customer, err = auth.CustomerForAddress(ctx, tx, m.From)
		return err
	})
	switch {
	case errors.Is(err, auth.ErrHasAccount):
		return n.finish(ctx, messageID, nil, Unmatched, "an agent's mail raises no request", m, unmatchedMessage(n.appURL))
	case errors.Is(err, auth.ErrUserInactive), errors.Is(err, auth.ErrBadAddress):
		return n.finish(ctx, messageID, nil, Refused, "the sender cannot raise requests: "+err.Error(), m, Message{})
	case err != nil:
		return "", err
	}

	actor := issue.Actor{UserID: customer.ID, OrgRole: auth.RoleCustomer}
	raised, _, err := n.desk.Raise(ctx, RaiseInput{
		RequestTypeID: at.requestTypeID,
		Summary:       summaryOf(m.Subject),
		Description:   mailin.StripQuotes(m.Text),
	}, actor)
	if err != nil {
		return "", fmt.Errorf("raise a request at %s: %w", at.key, err)
	}
	p := placement{issueID: raised.ID, key: raised.Key, userID: customer.ID, role: auth.RoleCustomer, reporter: true, trustedDomains: at.trustedDomains}
	*out = &p
	if _, err := n.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE inbound_mail SET issue_id = $2 WHERE message_id = $1`, messageID, raised.ID)
		return err
	}); err != nil {
		return "", err
	}
	n.attach(ctx, &p, m.Attachments, actor)
	return n.finish(ctx, messageID, &p, Raised, "", m, Message{})
}

// isAgent says the address is a colleague's.
func (n *Inbound) isAgent(ctx context.Context, from string) (bool, error) {
	var agent bool
	err := n.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM app_user WHERE email = $1 AND standing <> 'customer')`, from).Scan(&agent)
	})
	return agent, err
}

// raisedBefore is the request an earlier attempt at this mail raised, if any.
func (n *Inbound) raisedBefore(ctx context.Context, messageID string) (*placement, error) {
	var out *placement
	err := n.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var p placement
		err := tx.QueryRow(ctx, `
			SELECT i.id, pr.key || '-' || i.key_num, i.reporter_id
			FROM inbound_mail m JOIN issue i ON i.id = m.issue_id JOIN project pr ON pr.id = i.project_id
			WHERE m.message_id = $1`, messageID).Scan(&p.issueID, &p.key, &p.userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err == nil {
			p.role, p.reporter = auth.RoleCustomer, true
			out = &p
		}
		return err
	})
	return out, err
}

// attach puts the mail's files on the request it raised. A file that cannot go
// on is named in a note for the agents; the request stands without it.
func (n *Inbound) attach(ctx context.Context, p *placement, files []mailin.Attachment, customer issue.Actor) {
	var left []string
	for _, f := range files {
		_, _, err := n.desk.Attach(ctx, p.key, attachment.UploadInput{FileName: f.Name, ContentType: f.ContentType, Body: bytes.NewReader(f.Body)}, customer)
		if err == nil {
			continue
		}
		n.log.Warn("a file from a mail was not added", "request", p.key, "file", f.Name, "error", err)
		reason := "it could not be stored"
		if errors.Is(err, attachment.ErrTooLarge) || errors.Is(err, attachment.ErrEmpty) || errors.Is(err, attachment.ErrUnavailable) {
			reason = err.Error()
		}
		left = append(left, fmt.Sprintf("%s (%s)", f.Name, reason))
	}
	if len(left) == 0 {
		return
	}
	_, err := n.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO issue_comment (issue_id, author_id, body, is_internal)
			VALUES ($1, NULL, $2, true)`, p.issueID, filesLeftNote(left))
		return err
	})
	if err != nil {
		n.log.Warn("the note about files left out could not be written", "request", p.key, "error", err)
	}
}

// filesLeftNote tells the agents which files of the mail did not come along.
func filesLeftNote(left []string) []byte {
	return issue.TextDocument(fmt.Sprintf(
		"The mail that raised this request carried files that could not be added: %s. Ask the customer to add them in the portal, or to send them some other way.",
		strings.Join(left, ", ")))
}

// summaryOf is a request's summary from a mail's subject: one line, cut to
// what a summary holds, and something when the subject is empty.
func summaryOf(subject string) string {
	summary := strings.Join(strings.Fields(subject), " ")
	if summary == "" {
		return mailSummary
	}
	if runes := []rune(summary); len(runes) > issue.MaxSummary {
		summary = strings.TrimSpace(string(runes[:issue.MaxSummary]))
	}
	return summary
}
