//go:build integration

package test

import (
	"context"
	"encoding/json"
	"log/slog"
	"mime"
	netmail "net/mail"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/mail"
	"github.com/armature/armature/backend/internal/mailin"
)

// Two mails about one request in German arrive in Mailpit readable and in one
// thread, and an answer to the later one still finds the request.
func TestMailsAboutOneRequestArriveEncodedInOneThread(t *testing.T) {
	popAddr, smtpAddr := os.Getenv("ARMATURE_POP3_ADDR"), os.Getenv("ARMATURE_SMTP_ADDR")
	if popAddr == "" || smtpAddr == "" {
		t.Skip("ARMATURE_POP3_ADDR and ARMATURE_SMTP_ADDR are not set")
	}
	h := newHarness(t)
	ws := h.newProject(t, "mailthread")
	d, p := ws.aDesk(t, h, "Mail desk")
	customer := ws.customerOf(t, h, "threaded")
	ctx := context.Background()
	var customerEmail string
	if err := h.super.QueryRow(ctx, `SELECT email FROM app_user WHERE id = $1`, customer.UserID).Scan(&customerEmail); err != nil {
		t.Fatal(err)
	}
	// The agent's name goes into the reply's subject, so it is the non-ASCII
	// header a relay without SMTPUTF8 would refuse.
	const agentName = "Jürgen Weiß"
	if _, err := h.super.Exec(ctx, `UPDATE app_user SET name = $2 WHERE id = $1`, ws.actor.UserID, agentName); err != nil {
		t.Fatal(err)
	}
	const summary = "Bestellung für Müller"
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: summary}, customer)
	if err != nil {
		t.Fatal(err)
	}

	const from = "Kundendienst Zürich <no-reply@armature.test>"
	notifier := desk.NewNotifier(h.cluster, nil, mail.SMTPMailer{Addr: smtpAddr, From: from}, "http://app.test", slog.New(slog.NewTextHandler(os.Stderr, nil))).
		WithInbox(inboxAddress)
	handle := func(topic string, payload map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(payload)
		if err := notifier.Handle(ctx, events.Event{OrgID: ws.orgID, Topic: topic, Payload: raw}); err != nil {
			t.Fatal(err)
		}
	}
	handle(events.TopicIssueCreated, map[string]any{"key": raised.Key, "actorId": customer.UserID})
	reply, _, err := ws.issues.AddComment(ws.ctx, raised.Key, issue.TextDocument("Die Rechnung liegt unter Abrechnung."), ws.actor)
	if err != nil {
		t.Fatal(err)
	}
	handle(events.TopicCommentAdded, map[string]any{"key": raised.Key, "commentId": reply.ID, "internal": false, "actorId": ws.actor.UserID})

	// The box is shared with the demo and other runs; the customer's address
	// is this run's own, so only these two mails are taken.
	box := mailin.POP3{Addr: popAddr, User: os.Getenv("ARMATURE_POP3_USER"), Password: os.Getenv("ARMATURE_POP3_PASSWORD")}
	var got [][]byte
	for deadline := time.Now().Add(10 * time.Second); len(got) < 2 && time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		err := box.Poll(ctx, func(string) bool { return false }, func(uid string, raw []byte) bool {
			m, err := mailin.Parse(raw)
			if err != nil {
				return false
			}
			for _, r := range m.Recipients {
				if r == strings.ToLower(customerEmail) {
					got = append(got, raw)
					return true
				}
			}
			return false
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 2 {
		t.Fatalf("Mailpit holds %d mails for the customer, want the receipt and the reply", len(got))
	}

	var receipt, answer *mailin.Message
	var answerRaw *netmail.Message
	for _, raw := range got {
		for i, b := range raw {
			if b >= 0x80 {
				t.Fatalf("byte %d of a mail is not ASCII; a relay without SMTPUTF8 would refuse it:\n%s", i, raw)
			}
		}
		m, _ := mailin.Parse(raw)
		parsed, err := netmail.ReadMessage(strings.NewReader(string(raw)))
		if err != nil {
			t.Fatal(err)
		}
		if sender, err := netmail.ParseAddress(parsed.Header.Get("From")); err != nil || sender.Name != "Kundendienst Zürich" {
			t.Errorf("From = %+v (%v), raw %q", sender, err, parsed.Header.Get("From"))
		}
		if !strings.Contains(m.Text, summary) {
			t.Errorf("the body of %q lost the summary: %q", m.Subject, m.Text)
		}
		if strings.Contains(m.Subject, agentName) {
			answer, answerRaw = m, parsed
		} else {
			receipt = m
		}
	}
	if answer == nil || receipt == nil {
		t.Fatalf("no mail has %q in its decoded subject", agentName)
	}
	if decoded, _ := new(mime.WordDecoder).DecodeHeader(answerRaw.Header.Get("Subject")); !strings.Contains(decoded, agentName) {
		t.Errorf("the reply's subject decodes to %q", decoded)
	}

	t.Run("both mails name the same thread", func(t *testing.T) {
		if receipt.InReplyTo == "" || receipt.InReplyTo != answer.InReplyTo {
			t.Fatalf("In-Reply-To %q and %q, want one id for the request", receipt.InReplyTo, answer.InReplyTo)
		}
		for _, m := range []*mailin.Message{receipt, answer} {
			if len(m.References) == 0 || m.References[0] != m.InReplyTo {
				t.Errorf("%q: References %q, want the thread %q", m.Subject, m.References, m.InReplyTo)
			}
		}
		if !strings.HasPrefix(receipt.InReplyTo, raised.Key+".") {
			t.Errorf("the thread %q does not begin with %s", receipt.InReplyTo, raised.Key)
		}
		if receipt.MessageID == answer.MessageID {
			t.Errorf("both mails carry the id %q", receipt.MessageID)
		}
	})

	t.Run("an answer in the thread finds the request", func(t *testing.T) {
		refs := append(append([]string{}, answer.References...), answer.MessageID)
		if key := mailin.IssueKeyIn("Re: meine Bestellung", answer.MessageID, refs); key != raised.Key {
			t.Errorf("the answer was matched to %q, want %s", key, raised.Key)
		}
	})
}
