//go:build integration

package test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/mailin"
)

// mailers sends every mail through each of its mailers in turn, so a test can
// read what was sent and also find it in the real mailbox.
type mailers []desk.Mailer

func (m mailers) Send(ctx context.Context, mail desk.Mail) error {
	for _, each := range m {
		if err := each.Send(ctx, mail); err != nil {
			return err
		}
	}
	return nil
}

// The rating link is the customer's alone: a colleague who follows the
// request and an agent watching it hear it is resolved, but cannot rate it.
func TestOnlyTheReporterIsAskedToRate(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "rateasked")
	d, p := ws.aDesk(t, h, "Asked desk")
	customer := ws.customerOf(t, h, "asker")
	var reporter string
	if err := h.super.QueryRow(context.Background(), `SELECT email FROM app_user WHERE id = $1`, customer.UserID).Scan(&reporter); err != nil {
		t.Fatal(err)
	}
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "Who may rate me"}, customer)
	if err != nil {
		t.Fatal(err)
	}

	follower := h.email(t, "colleague")
	colleague, err := h.authService().UserForAddress(context.Background(), follower)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ws.issues.AddWatcher(ws.ctx, raised.Key, colleague.ID, customer); err != nil {
		t.Fatal(err)
	}
	watcher := h.email(t, "watchingagent")
	agent := h.joinExisting(t, ws, watcher, "member")
	if _, _, _, err := ws.issues.AddWatcher(ws.ctx, raised.Key, agent, ws.actor); err != nil {
		t.Fatal(err)
	}
	ws.move(t, h, raised.Key, "Resolve")

	kept := &fakeMailer{}
	send := mailers{kept}
	smtpAddr, pop3Addr := os.Getenv("ARMATURE_SMTP_ADDR"), os.Getenv("ARMATURE_POP3_ADDR")
	if smtpAddr != "" && pop3Addr != "" {
		send = append(send, desk.SMTPMailer{Addr: smtpAddr, From: "Armature <desk@armature.test>"})
	}
	notifier := desk.NewNotifier(h.cluster, nil, send, "http://app.test", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	raw, _ := json.Marshal(map[string]any{"key": raised.Key, "transition": "Resolve", "fromStatus": "Waiting for support", "toStatus": "Resolved", "actorId": ws.actor.UserID})
	if err := notifier.Handle(context.Background(), events.Event{ID: uuid.New(), OrgID: ws.orgID, Topic: events.TopicIssueTransitioned, Payload: raw}); err != nil {
		t.Fatal(err)
	}

	everyone := []string{reporter, follower, watcher}
	onlyTheReporterRates := func(t *testing.T, bodies map[string]string) {
		t.Helper()
		for _, address := range everyone {
			body, ok := bodies[address]
			if !ok {
				t.Errorf("%s was not told the request is resolved; mailed %v", address, addressesOf(bodies))
				continue
			}
			if !strings.Contains(body, raised.Key) {
				t.Errorf("the mail to %s does not name %s: %q", address, raised.Key, body)
			}
			if rates := strings.Contains(body, "/rate/"); rates != (address == reporter) {
				t.Errorf("the mail to %s carries the rating link: %v, want only the reporter's to: %q", address, rates, body)
			}
		}
	}

	t.Run("as sent", func(t *testing.T) {
		bodies := map[string]string{}
		for _, m := range kept.sent {
			bodies[m.To] = m.Body
		}
		if len(kept.sent) != len(everyone) {
			t.Errorf("mails went to %v, want one each to %v", addressesOf(bodies), everyone)
		}
		onlyTheReporterRates(t, bodies)
	})

	t.Run("as found in Mailpit", func(t *testing.T) {
		if len(send) == 1 {
			t.Skip("ARMATURE_SMTP_ADDR and ARMATURE_POP3_ADDR are not set")
		}
		inbox := mailin.POP3{Addr: pop3Addr, User: os.Getenv("ARMATURE_POP3_USER"), Password: os.Getenv("ARMATURE_POP3_PASSWORD")}
		bodies := map[string]string{}
		for deadline := time.Now().Add(10 * time.Second); len(bodies) < len(everyone) && time.Now().Before(deadline); {
			err := inbox.Poll(context.Background(), func(string) bool { return false }, func(uid string, raw []byte) bool {
				m, err := mailin.Parse(raw)
				if err != nil {
					return false
				}
				for _, r := range m.Recipients {
					if contains(everyone, r) {
						bodies[r] = m.Text
						return true
					}
				}
				return false
			})
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(200 * time.Millisecond)
		}
		onlyTheReporterRates(t, bodies)
	})
}

func addressesOf(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
