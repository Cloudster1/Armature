//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/mail"
	"github.com/armature/armature/backend/internal/mail/mailtest"
)

// A wrong password names the settings to check and leaves the receipt to be
// tried again, and the retry with the right one delivers it.
func TestADeskMailGoesThroughARelayThatWantsALogin(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "mailauth")
	d, p := ws.aDesk(t, h, "Login desk")
	customer := ws.customerOf(t, h, "loggedin")
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "Where is the invoice?"}, customer)
	if err != nil {
		t.Fatal(err)
	}

	relay := mailtest.Start(t, mailtest.Options{User: "armature", Password: "right"})
	var logged bytes.Buffer
	mailer := &mail.SMTPMailer{
		Addr: relay.Addr, From: "Armature <desk@armature.test>", User: "armature", Password: "wrong",
		InsecureTLS: true, Log: slog.New(slog.NewTextHandler(&logged, nil)),
	}
	notifier := desk.NewNotifier(h.cluster, nil, mailer, "http://app.test", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	payload, _ := json.Marshal(map[string]any{"key": raised.Key, "actorId": customer.UserID})
	created := events.Event{Topic: events.TopicIssueCreated, Payload: payload}

	err = notifier.Handle(context.Background(), created)
	if err == nil || events.IsPermanent(err) {
		t.Fatalf("err = %v, want a failure the stream retries", err)
	}
	sentence := "The mail server at " + relay.Addr + " refused the login for armature; check ARMATURE_SMTP_USER and ARMATURE_SMTP_PASSWORD."
	if !strings.Contains(logged.String(), sentence) {
		t.Errorf("the log should carry %q, got:\n%s", sentence, logged.String())
	}
	select {
	case body := <-relay.Mails:
		t.Fatalf("the relay took a mail without the login: %q", body)
	default:
	}

	mailer.Password = "right"
	if err := notifier.Handle(context.Background(), created); err != nil {
		t.Fatalf("the retry: %v", err)
	}
	select {
	case body := <-relay.Mails:
		if !strings.Contains(body, raised.Key) {
			t.Errorf("the relay took %q, want the receipt for %s", body, raised.Key)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the retry delivered nothing to the relay")
	}
}
