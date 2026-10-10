//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
)

// refusingMailer turns away the addresses it is told to and sends the rest.
type refusingMailer struct {
	fakeMailer
	mu     sync.Mutex
	refuse func(to string) bool
}

func (m *refusingMailer) Send(ctx context.Context, mail desk.Mail) error {
	m.mu.Lock()
	refused := m.refuse(mail.To)
	m.mu.Unlock()
	if refused {
		return errors.New("the mail relay is not answering")
	}
	return m.fakeMailer.Send(ctx, mail)
}

// A failed event comes round again, so a customer mail is retried only while
// nobody has had it: once one went out, a retry would mail that person twice.
func TestADeskMailIsRetriedOnlyWhileNobodyHasHadIt(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "deskretry")
	d, p := ws.aDesk(t, h, "Retry desk")
	customer := ws.customerOf(t, h, "retried")
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "Where is the invoice?"}, customer)
	if err != nil {
		t.Fatal(err)
	}
	followerAddress := h.email(t, "follower")
	follower, err := h.authService().UserForAddress(context.Background(), followerAddress)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ws.issues.AddWatcher(ws.ctx, raised.Key, follower.ID, customer); err != nil {
		t.Fatal(err)
	}
	reply, _, err := ws.issues.AddComment(ws.ctx, raised.Key, issue.TextDocument("It is under Billing."), ws.actor)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"key": raised.Key, "commentId": reply.ID, "internal": false, "actorId": ws.actor.UserID})
	replied := events.Event{OrgID: ws.orgID, Topic: events.TopicCommentAdded, Payload: payload}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	t.Run("nobody mailed: the failure is worth another attempt", func(t *testing.T) {
		mailer := &refusingMailer{refuse: func(string) bool { return true }}
		err := desk.NewNotifier(h.cluster, nil, mailer, "http://app.test", log).Handle(context.Background(), replied)
		if err == nil || events.IsPermanent(err) {
			t.Fatalf("err = %v, want a failure the stream retries", err)
		}
		if len(mailer.sent) != 0 {
			t.Fatalf("mails = %+v, want none", mailer.sent)
		}

		// The retry reaches both, once each.
		mailer.mu.Lock()
		mailer.refuse = func(string) bool { return false }
		mailer.mu.Unlock()
		if err := desk.NewNotifier(h.cluster, nil, mailer, "http://app.test", log).Handle(context.Background(), replied); err != nil {
			t.Fatalf("the retry: %v", err)
		}
		if len(mailer.sent) != 2 {
			t.Errorf("mails after the retry = %+v, want one to the reporter and one to the follower", mailer.sent)
		}
	})

	t.Run("somebody mailed: the failure is final", func(t *testing.T) {
		mailer := &refusingMailer{refuse: func(to string) bool { return to == followerAddress }}
		err := desk.NewNotifier(h.cluster, nil, mailer, "http://app.test", log).Handle(context.Background(), replied)
		if !events.IsPermanent(err) {
			t.Fatalf("err = %v, want a permanent failure, since a retry would mail the reporter again", err)
		}
		if len(mailer.sent) != 1 || mailer.sent[0].To == followerAddress {
			t.Errorf("mails = %+v, want the one to the reporter", mailer.sent)
		}
	})
}
