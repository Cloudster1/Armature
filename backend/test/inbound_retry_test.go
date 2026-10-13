//go:build integration

package test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/issue"
)

// givenUp is the outcome of a mail the desk stopped trying; spelled out so the
// test states it without leaning on a name the fix chose.
const givenUp = desk.Outcome("failed")

// refusedComment is what the database says while a reply cannot be written.
const refusedComment = "the test refuses this comment"

// refuseComments makes the database refuse every comment whose body holds the
// marker, the way a blip or a broken table would, until the returned func runs.
func refuseComments(t *testing.T, h *harness, marker string) func() {
	t.Helper()
	name := "refuse_comment_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	for _, q := range []string{
		fmt.Sprintf(`CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN RAISE EXCEPTION '%s'; END $$`, name, refusedComment),
		fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON issue_comment FOR EACH ROW
			WHEN (NEW.body::text LIKE '%%%s%%') EXECUTE FUNCTION %s()`, name, marker, name),
	} {
		if _, err := h.super.Exec(context.Background(), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	lift := func() {
		_, _ = h.super.Exec(context.Background(), fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON issue_comment`, name))
		_, _ = h.super.Exec(context.Background(), fmt.Sprintf(`DROP FUNCTION IF EXISTS %s()`, name))
	}
	t.Cleanup(lift)
	return lift
}

// A reply the desk could not post is posted on a later poll; one that never can
// be is given up and shown to the agents. Nothing undecided is ever deleted.
func TestAReplyThatFailsToPostIsReadAgain(t *testing.T) {
	h := newHarness(t)
	ws := h.newProject(t, "mailretry")
	d, p := ws.aDesk(t, h, "Retry desk")
	customer := ws.customerOf(t, h, "writer")
	var customerEmail string
	if err := h.super.QueryRow(context.Background(), `SELECT email FROM app_user WHERE id = $1`, customer.UserID).Scan(&customerEmail); err != nil {
		t.Fatal(err)
	}
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "Where is the refund?"}, customer)
	if err != nil {
		t.Fatal(err)
	}

	run := uuid.NewString()[:8]
	id := func(name string) string { return name + "-" + run + "@example.com" }
	inbox := &fakeInbox{mail: map[string]string{}}
	reader := desk.NewInbound(h.cluster, d, ws.issues, &fakeMailer{}, inbox,
		desk.InboundConfig{Address: inboxAddress, From: "Armature <no-reply@armature.test>", AppURL: "http://app.test"},
		slog.New(slog.NewTextHandler(os.Stderr, nil)))
	poll := func(t *testing.T, uid, raw string) {
		t.Helper()
		inbox.mail = map[string]string{uid: raw}
		inbox.deleted = nil
		if err := reader.Once(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	saying := func(t *testing.T, text string, internal bool) []issue.Comment {
		t.Helper()
		all, err := ws.issues.Comments(ws.ctx, raised.Key, internal)
		if err != nil {
			t.Fatal(err)
		}
		var out []issue.Comment
		for _, c := range all {
			if strings.Contains(issue.PlainText(c.Body), text) {
				out = append(out, c)
			}
		}
		return out
	}
	outcome := func(t *testing.T, id string) string {
		t.Helper()
		var got string
		err := h.super.QueryRow(context.Background(), `SELECT outcome FROM inbound_mail WHERE message_id = $1`, id).Scan(&got)
		if err != nil {
			return "no row"
		}
		return got
	}

	t.Run("a reply whose comment fails is kept and posted on the next poll", func(t *testing.T) {
		text := "Refunded on Friday " + run
		raw := rawMail(customerEmail, inboxAddress, "Re: ["+raised.Key+"] replied", id("blip"), text)
		lift := refuseComments(t, h, text)
		poll(t, "u1", raw)
		if len(inbox.deleted) != 0 {
			t.Fatalf("a reply that was not posted was deleted: %v", inbox.deleted)
		}
		if got := outcome(t, id("blip")); got == "processing" {
			t.Errorf("the failed attempt left its row %s, so the next poll takes it for somebody else's", got)
		}

		// The database is back well inside the five minutes a live attempt
		// is given, which is when the reply used to be deleted as a duplicate.
		lift()
		poll(t, "u1", raw)
		if got := saying(t, text, false); len(got) != 1 || got[0].Author == nil || got[0].Author.ID != customer.UserID {
			t.Fatalf("comments saying %q = %+v, want the customer's reply once", text, got)
		}
		if got := outcome(t, id("blip")); got != string(desk.Commented) || len(inbox.deleted) != 1 || inbox.deleted[0] != "u1" {
			t.Errorf("outcome %s, deleted %v; want commented and the mail consumed only now", got, inbox.deleted)
		}
	})

	t.Run("a mail another attempt still holds is left in the box", func(t *testing.T) {
		text := "Still holding " + run
		raw := rawMail(customerEmail, inboxAddress, "Re: ["+raised.Key+"] replied", id("held"), text)
		if _, err := h.super.Exec(context.Background(),
			`INSERT INTO inbound_mail (message_id, from_email, subject, outcome) VALUES ($1, $2, 'held', 'processing')`,
			id("held"), customerEmail); err != nil {
			t.Fatal(err)
		}
		got, err := reader.Handle(context.Background(), []byte(raw))
		if err != nil || got != desk.Skipped {
			t.Errorf("Handle = %s, %v; want %s, since the other attempt may still fail", got, err, desk.Skipped)
		}
		poll(t, "u2", raw)
		if len(inbox.deleted) != 0 {
			t.Errorf("a mail still being handled elsewhere was deleted: %v", inbox.deleted)
		}
		if outcome(t, id("held")) != "processing" || len(saying(t, text, true)) != 0 {
			t.Errorf("the held mail was taken over: outcome %s", outcome(t, id("held")))
		}
	})

	t.Run("a reply that keeps failing is given up, kept, and shown to the agents", func(t *testing.T) {
		text := "Never lands " + run
		raw := rawMail(customerEmail, inboxAddress, "Re: ["+raised.Key+"] replied", id("stuck"), text)
		refuseComments(t, h, text)

		// Generous: the bound is the fix's to choose, but it must be one.
		const patience = 50
		tries := 0
		for ; tries < patience; tries++ {
			got, err := reader.Handle(context.Background(), []byte(raw))
			if err == nil && got == givenUp {
				break
			}
			if err == nil {
				t.Fatalf("attempt %d decided %s for a reply that cannot be written", tries+1, got)
			}
		}
		if tries == patience {
			t.Fatalf("the desk was still retrying after %d attempts", patience)
		}

		var attempts int
		var detail string
		if err := h.super.QueryRow(context.Background(),
			`SELECT attempts, detail FROM inbound_mail WHERE message_id = $1 AND outcome = 'failed'`, id("stuck")).Scan(&attempts, &detail); err != nil {
			t.Fatalf("no given-up row: %v", err)
		}
		if attempts < 2 || !strings.Contains(detail, refusedComment) {
			t.Errorf("attempts %d, detail %q; want several attempts and the database's error", attempts, detail)
		}

		// The agents read it on the request; the customer does not.
		notes := saying(t, refusedComment, true)
		if len(notes) != 1 || !notes[0].Internal || notes[0].Author != nil || !strings.Contains(issue.PlainText(notes[0].Body), refusedComment) {
			t.Fatalf("notes giving the error = %+v, want one internal note from the desk with the error", notes)
		}
		if len(saying(t, refusedComment, false)) != 0 {
			t.Error("the customer was shown the desk's own note")
		}

		// Given up is not deleted: the body lives only in the box, so it stays
		// there for a person, and the desk stops trying.
		poll(t, "u3", raw)
		if len(inbox.deleted) != 0 {
			t.Errorf("a given-up reply was deleted: %v", inbox.deleted)
		}
		again, err := reader.Handle(context.Background(), []byte(raw))
		var after int
		_ = h.super.QueryRow(context.Background(), `SELECT attempts FROM inbound_mail WHERE message_id = $1`, id("stuck")).Scan(&after)
		if err != nil || again != givenUp || after != attempts || len(saying(t, refusedComment, true)) != 1 {
			t.Errorf("after giving up: %s, %v, attempts %d (was %d); want no further attempt and one note", again, err, after, attempts)
		}
	})

	t.Run("the row refuses an outcome or a count it has no meaning for, through SQL", func(t *testing.T) {
		for _, q := range []string{
			`INSERT INTO inbound_mail (message_id, from_email, outcome) VALUES ($1, 'x@example.com', 'lost')`,
			`INSERT INTO inbound_mail (message_id, from_email, outcome, attempts) VALUES ($1, 'x@example.com', 'failed', 0)`,
		} {
			if _, err := h.super.Exec(context.Background(), q, id("guard-"+uuid.NewString()[:4])); err == nil || !strings.Contains(err.Error(), "check") {
				t.Errorf("%s was allowed: %v", q, err)
			}
		}
		if _, err := h.super.Exec(context.Background(),
			`INSERT INTO inbound_mail (message_id, from_email, outcome, attempts, detail) VALUES ($1, 'x@example.com', 'failed', 3, 'down')`, id("guard-ok")); err != nil {
			t.Errorf("a given-up row was refused: %v", err)
		}
	})
}
