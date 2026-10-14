package desk

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/mailin"
)

// mailAttempts is how often a mail is tried before the desk gives up on it.
// A try is counted only when its row could be written, so an outage spends none.
const mailAttempts = 10

// claimState is what the mail's row said when the desk tried to take it.
type claimState int

const (
	// claimTaken is a mail this attempt now holds.
	claimTaken claimState = iota
	// claimBusy is a mail another attempt holds, recently enough to be alive.
	claimBusy
	// claimGivenUp is a mail that failed every one of its attempts.
	claimGivenUp
	// claimDone is a mail already decided.
	claimDone
)

type claimResult struct {
	state    claimState
	attempts int
}

// claim writes the mail's row as processing. A failed attempt is taken up again
// until the attempts run out, one that died mid-way after five minutes.
func (n *Inbound) claim(ctx context.Context, messageID string, m *mailin.Message) (claimResult, error) {
	var out claimResult
	_, err := n.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO inbound_mail (message_id, from_email, subject, outcome)
			VALUES ($1, $2, $3, 'processing')
			ON CONFLICT (message_id) DO UPDATE
			SET outcome = 'processing', received_at = now(), attempts = inbound_mail.attempts + 1
			WHERE (inbound_mail.outcome = 'failed' AND inbound_mail.attempts < $4)
			   OR (inbound_mail.outcome = 'processing' AND inbound_mail.received_at < now() - interval '5 minutes')
			RETURNING attempts`,
			messageID, m.From, m.Subject, mailAttempts).Scan(&out.attempts)
		if err == nil {
			out.state = claimTaken
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var outcome string
		err = tx.QueryRow(ctx, `SELECT outcome FROM inbound_mail WHERE message_id = $1`, messageID).Scan(&outcome)
		switch {
		case errors.Is(err, pgx.ErrNoRows), err == nil && outcome == "processing":
			out.state = claimBusy
		case err != nil:
			return err
		case outcome == string(Failed):
			out.state = claimGivenUp
		default:
			out.state = claimDone
		}
		return nil
	})
	return out, err
}

// fail records why an attempt failed, so the next poll takes the mail up again
// rather than mistaking it for a duplicate. The last attempt gives up instead.
func (n *Inbound) fail(ctx context.Context, messageID string, attempts int, p *placement, cause error) (Outcome, bool, error) {
	givenUp := attempts >= mailAttempts
	// The poll may be ending, and a row left processing would hold the mail
	// for five minutes; the record is written regardless.
	_, err := n.db.WriteAdmin(context.WithoutCancel(ctx), func(ctx context.Context, tx db.DBTX) error {
		var issueID any
		if p != nil {
			issueID = p.issueID
		}
		if _, err := tx.Exec(ctx, `
			UPDATE inbound_mail SET outcome = 'failed', detail = $2, issue_id = $3
			WHERE message_id = $1`, messageID, cause.Error(), issueID); err != nil {
			return err
		}
		if !givenUp || p == nil {
			return nil
		}
		// Written with the record, so a given-up mail is never one nobody hears of.
		_, err := tx.Exec(ctx, `
			INSERT INTO issue_comment (issue_id, author_id, body, is_internal)
			VALUES ($1, NULL, $2, true)`, p.issueID, gaveUpNote(attempts, cause))
		return err
	})
	if err != nil {
		return "", false, errors.Join(cause, fmt.Errorf("record the failure: %w", err))
	}
	if !givenUp {
		return "", false, cause
	}
	n.log.Error("a mail was given up and left in the box", "message_id", messageID, "attempts", attempts, "error", cause)
	return Failed, false, nil
}

// gaveUpNote tells the agents on a request about a reply that never reached it.
// The sender is left out: the note outlives an erasure, the mailbox does not.
func gaveUpNote(attempts int, cause error) []byte {
	return issue.TextDocument(fmt.Sprintf(
		"A reply by mail to this request could not be added after %d attempts and is still in the desk's mailbox. Copy it from there into a comment, or ask its sender to reply in the portal. The last error was: %s",
		attempts, cause))
}
