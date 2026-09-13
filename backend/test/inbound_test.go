//go:build integration

package test

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/mailin"
)

// fakeInbox hands the reader whatever mail it was given, and remembers what
// the reader consumed.
type fakeInbox struct {
	mail    map[string]string
	deleted []string
}

func (f *fakeInbox) Poll(ctx context.Context, skip func(string) bool, handle func(string, []byte) bool) error {
	for uid, raw := range f.mail {
		if skip(uid) {
			continue
		}
		if handle(uid, []byte(raw)) {
			f.deleted = append(f.deleted, uid)
		}
	}
	return nil
}

func rawMail(from, to, subject, id, body string, extra ...string) string {
	headers := []string{"From: " + from, "To: " + to, "Subject: " + subject, "Message-ID: <" + id + ">", "Content-Type: text/plain; charset=utf-8"}
	headers = append(headers, extra...)
	return strings.Join(headers, "\r\n") + "\r\n\r\n" + body
}

const inboxAddress = "support@armature.test"

// A reply by mail becomes a comment when it comes from somebody who could have
// typed it in the portal; anyone else is told to use the portal, and a mail
// delivered twice is one comment.
func TestAReplyByMailBecomesAComment(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "mailin")
	d, p := ws.aDesk(t, h, "Mail desk")
	customer := ws.customerOf(t, h, "writer")
	var customerEmail string
	if err := h.super.QueryRow(context.Background(), `SELECT email FROM app_user WHERE id = $1`, customer.UserID).Scan(&customerEmail); err != nil {
		t.Fatal(err)
	}
	var agentEmail string
	if err := h.super.QueryRow(context.Background(), `SELECT email FROM app_user WHERE id = $1`, ws.actor.UserID).Scan(&agentEmail); err != nil {
		t.Fatal(err)
	}
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "Where is the invoice?"}, customer)
	if err != nil {
		t.Fatal(err)
	}

	// Message ids are unique in the table for good, so each run mints its own.
	run := uuid.NewString()[:8]
	id := func(name string) string { return name + "-" + run + "@example.com" }

	mailer := &fakeMailer{}
	inbox := &fakeInbox{mail: map[string]string{}}
	reader := desk.NewInbound(h.cluster, d, ws.issues, mailer, inbox,
		desk.InboundConfig{Address: inboxAddress, From: "Armature <no-reply@armature.test>", AppURL: "http://app.test"},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	read := func(t *testing.T, uid, raw string) {
		t.Helper()
		inbox.mail = map[string]string{uid: raw}
		if err := reader.Once(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	comments := func(t *testing.T) []issue.Comment {
		t.Helper()
		out, err := ws.issues.Comments(ws.ctx, raised.Key, true)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	outcome := func(t *testing.T, id string) string {
		t.Helper()
		var got string
		if err := h.super.QueryRow(context.Background(), `SELECT outcome FROM inbound_mail WHERE message_id = $1`, id).Scan(&got); err != nil {
			t.Fatalf("no row for %s: %v", id, err)
		}
		return got
	}

	t.Run("the reporter's reply becomes their comment and brings the request back", func(t *testing.T) {
		// The desk starts work, then waits on the customer.
		for _, name := range []string{"Start work", "Wait for customer"} {
			if _, _, err := ws.issues.Transition(ws.ctx, raised.Key, issue.TransitionInput{TransitionID: h.transitionID(t, ws, raised.Key, name)}, ws.actor); err != nil {
				t.Fatal(err)
			}
		}
		read(t, "u1", rawMail(customerEmail, inboxAddress, "Re: ["+raised.Key+"] replied", id("one"),
			"Still missing.\r\n\r\nOn Mon, 1 Sep 2026 Armature wrote:\r\n> Try Billing."))
		got := comments(t)
		if len(got) != 1 || issue.PlainText(got[0].Body) != "Still missing." || got[0].Author == nil || got[0].Author.ID != customer.UserID {
			t.Fatalf("comments = %+v, want the reporter's words without the quote", got)
		}
		after, _ := ws.issues.ByKey(ws.ctx, raised.Key)
		if strings.EqualFold(after.Status.Name, desk.WaitingOnCustomer) {
			t.Errorf("the request is still %s after the reporter answered", after.Status.Name)
		}
		if outcome(t, id("one")) != string(desk.Commented) || len(inbox.deleted) != 1 {
			t.Errorf("outcome %s, deleted %v", outcome(t, id("one")), inbox.deleted)
		}
	})

	t.Run("an agent's reply by mail is an internal note, matched by the thread", func(t *testing.T) {
		read(t, "u2", rawMail(agentEmail, inboxAddress, "Re: your request", id("two"), "It is under Billing.",
			"In-Reply-To: <"+raised.Key+".0190@armature.test>"))
		got := comments(t)
		// Nothing proves a colleague's address, so their words reach the desk
		// rather than the customer; answering stays a deliberate act in the app.
		if len(got) != 2 || !got[1].Internal || got[1].Author.ID != ws.actor.UserID {
			t.Fatalf("comments = %+v, want an internal note by the agent", got)
		}
		timers, _ := d.TimersFor(ws.ctx, raised.Key)
		if first := timerFor(t, timers, desk.FirstResponse); first.CompletedAt != nil {
			t.Error("a note counted as the answer the customer is waiting for")
		}
	})

	t.Run("a sender the receiving server says is forged writes nothing", func(t *testing.T) {
		mailer.sent = nil
		before := len(comments(t))
		read(t, "u2b", rawMail(customerEmail, inboxAddress, "Re: ["+raised.Key+"] replied", id("two-b"), "Pay this instead.",
			"Authentication-Results: mx.armature.test; spf=fail smtp.mailfrom=example.com; dkim=fail; dmarc=fail header.from=example.com"))
		if len(comments(t)) != before {
			t.Error("a forged sender's mail became a comment")
		}
		if got := outcome(t, id("two-b")); got != string(desk.Refused) {
			t.Errorf("outcome = %s, want refused", got)
		}
		if len(mailer.sent) != 0 {
			t.Errorf("a forged sender was written to: %+v", mailer.sent)
		}
	})

	t.Run("a stranger writes nothing and is told to use the portal", func(t *testing.T) {
		mailer.sent = nil
		read(t, "u3", rawMail("stranger@example.com", inboxAddress, "Re: ["+raised.Key+"] replied", id("three"), "Let me in."))
		if len(comments(t)) != 2 {
			t.Error("a stranger's mail became a comment")
		}
		if outcome(t, id("three")) != string(desk.Refused) || len(mailer.sent) != 1 || mailer.sent[0].To != "stranger@example.com" || !strings.Contains(mailer.sent[0].Body, "http://app.test") {
			t.Errorf("outcome %s, mails %+v", outcome(t, id("three")), mailer.sent)
		}
	})

	t.Run("a follower at a domain the desk does not trust is bounced", func(t *testing.T) {
		ben := h.joinExisting(t, ws, "ben-"+run+"@elsewhere.test", "customer")
		if _, _, _, err := ws.issues.AddWatcher(ws.ctx, raised.Key, ben, ws.actor); err != nil {
			t.Fatal(err)
		}
		if _, err := h.super.Exec(context.Background(), `UPDATE project SET trusted_domains = '{armature.test}' WHERE id = $1`, p.ID); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = h.super.Exec(context.Background(), `UPDATE project SET trusted_domains = '{}' WHERE id = $1`, p.ID)
		})
		mailer.sent = nil
		read(t, "u3b", rawMail("ben-"+run+"@elsewhere.test", inboxAddress, "Re: ["+raised.Key+"] replied", id("three-b"), "Me too."))
		if len(comments(t)) != 2 {
			t.Error("a reply from a domain the desk does not trust became a comment")
		}
		if outcome(t, id("three-b")) != string(desk.Refused) || len(mailer.sent) != 1 || !strings.Contains(mailer.sent[0].Body, "armature.test") || !strings.Contains(mailer.sent[0].Body, "elsewhere.test") {
			t.Errorf("outcome %s, mails %+v", outcome(t, id("three-b")), mailer.sent)
		}
		// The customer, at a domain the desk trusts, still gets through.
		mailer.sent = nil
		read(t, "u3c", rawMail(customerEmail, inboxAddress, "Re: ["+raised.Key+"] replied", id("three-c"), "Still me."))
		if outcome(t, id("three-c")) != string(desk.Commented) {
			t.Errorf("the trusted customer was refused: %s", outcome(t, id("three-c")))
		}
		// Taken back so the counts the later subtests expect still hold.
		all := comments(t)
		if _, err := ws.issues.DeleteComment(ws.ctx, all[len(all)-1].ID, ws.actor); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("a mail naming no request is answered once and an auto reply not at all", func(t *testing.T) {
		mailer.sent = nil
		read(t, "u4", rawMail(customerEmail, inboxAddress, "hello?", id("four"), "Anyone there?"))
		if outcome(t, id("four")) != string(desk.Unmatched) || len(mailer.sent) != 1 {
			t.Errorf("outcome %s, mails %+v", outcome(t, id("four")), mailer.sent)
		}
		mailer.sent = nil
		read(t, "u5", rawMail("mailer-daemon@example.com", inboxAddress, "Undeliverable", id("five"), "bounce", "Auto-Submitted: auto-replied"))
		if len(mailer.sent) != 0 {
			t.Errorf("a machine was answered: %+v", mailer.sent)
		}
	})

	t.Run("the desk's own mail and mail for others are left in the box", func(t *testing.T) {
		before := len(inbox.deleted)
		read(t, "u6", rawMail("no-reply@armature.test", customerEmail, "["+raised.Key+"] we have your request", id("six"), "receipt"))
		read(t, "u7", rawMail(customerEmail, "somebody@else.test", "["+raised.Key+"] hi", id("seven"), "not for the desk"))
		if len(inbox.deleted) != before || len(comments(t)) != 2 {
			t.Errorf("deleted %v, comments %d; want nothing consumed and nothing written", inbox.deleted, len(comments(t)))
		}
		var rows int
		_ = h.super.QueryRow(context.Background(), `SELECT count(*) FROM inbound_mail WHERE message_id IN ($1, $2)`, id("six"), id("seven")).Scan(&rows)
		if rows != 0 {
			t.Errorf("skipped mail was recorded %d times", rows)
		}
	})

	t.Run("a reply with nothing but quotes is consumed and not written", func(t *testing.T) {
		read(t, "u8", rawMail(customerEmail, inboxAddress, "Re: ["+raised.Key+"]", id("eight"), "> only\r\n> quotes"))
		if outcome(t, id("eight")) != string(desk.Empty) || len(comments(t)) != 2 {
			t.Errorf("outcome %s, comments %d", outcome(t, id("eight")), len(comments(t)))
		}
	})

	t.Run("the same message twice is one comment, and the row refuses through SQL", func(t *testing.T) {
		read(t, "u9", rawMail(customerEmail, inboxAddress, "Re: ["+raised.Key+"]", id("nine"), "Once."))
		read(t, "u10", rawMail(customerEmail, inboxAddress, "Re: ["+raised.Key+"]", id("nine"), "Once."))
		if len(comments(t)) != 3 {
			t.Errorf("comments = %d, want the reply once", len(comments(t)))
		}
		_, err := h.super.Exec(context.Background(), `INSERT INTO inbound_mail (message_id, from_email, outcome) VALUES ($2, $1, 'commented')`, customerEmail, id("nine"))
		if err == nil || !strings.Contains(err.Error(), "inbound_mail_message_id_key") {
			t.Errorf("a second row for the same message was allowed: %v", err)
		}
	})

	t.Run("mail is the organization's, through SQL", func(t *testing.T) {
		other := h.newWorkspace(t, "elsewhere")
		var theirs int
		err := h.cluster.Read(other.ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM inbound_mail`).Scan(&theirs)
		})
		if err != nil {
			t.Fatal(err)
		}
		if theirs != 0 {
			t.Errorf("another organization sees %d mails", theirs)
		}
	})
}

// The real client against the development mailbox: a mail sent to Mailpit is
// fetched over POP3 and consumed, and nothing else in the box is touched.
func TestTheMailboxIsReadOverPOP3(t *testing.T) {
	addr, smtpAddr := os.Getenv("ARMATURE_POP3_ADDR"), os.Getenv("ARMATURE_SMTP_ADDR")
	if addr == "" || smtpAddr == "" {
		t.Skip("ARMATURE_POP3_ADDR and ARMATURE_SMTP_ADDR are not set")
	}
	// A per-run address, so a box shared with the demo and with other runs
	// only ever shows this test its own mail.
	to := fmt.Sprintf("pop3-%s@armature.test", uuid.NewString()[:8])
	from := "somebody@example.com"
	if err := smtp.SendMail(smtpAddr, nil, from, []string{to}, []byte(rawMail(from, to, "hello over pop3", uuid.NewString()+"@example.com", "fetched"))); err != nil {
		t.Fatal(err)
	}
	inbox := mailin.POP3{Addr: addr, User: os.Getenv("ARMATURE_POP3_USER"), Password: os.Getenv("ARMATURE_POP3_PASSWORD")}
	ours := func(raw []byte) bool {
		m, err := mailin.Parse(raw)
		if err != nil {
			return false
		}
		for _, r := range m.Recipients {
			if r == to {
				return true
			}
		}
		return false
	}
	found := false
	for deadline := time.Now().Add(10 * time.Second); !found && time.Now().Before(deadline); {
		err := inbox.Poll(context.Background(), func(string) bool { return false }, func(uid string, raw []byte) bool {
			if ours(raw) {
				found = true
				return true
			}
			return false
		})
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !found {
		t.Fatal("the mail did not arrive over POP3")
	}
	still := false
	if err := inbox.Poll(context.Background(), func(string) bool { return false }, func(uid string, raw []byte) bool {
		still = still || ours(raw)
		return false
	}); err != nil {
		t.Fatal(err)
	}
	if still {
		t.Error("the consumed mail is still in the box")
	}
}
