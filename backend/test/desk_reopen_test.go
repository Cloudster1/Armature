//go:build integration

package test

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/notify"
	"github.com/armature/armature/backend/internal/workflow"
)

// resolvedRequest raises a request as the customer, has the desk work it and
// resolve it, and has the customer rate it from the resolution mail's link.
func resolvedRequest(t *testing.T, h *harness, ws *workspace, d *desk.Service, projectKey, summary string, customer issue.Actor, score int) *issue.Issue {
	t.Helper()
	types, err := d.RequestTypes(ws.ctx, projectKey)
	if err != nil {
		t.Fatal(err)
	}
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Report a problem").ID, Summary: summary}, customer)
	if err != nil {
		t.Fatalf("raise: %v", err)
	}
	ws.move(t, h, raised.Key, "Start work")
	resolved := ws.move(t, h, raised.Key, "Resolve")
	if score > 0 {
		token, err := d.InviteRating(ws.ctx, resolved.ID)
		if err != nil || token == "" {
			t.Fatalf("invite a rating: %q %v", token, err)
		}
		if _, _, err := d.Rate(context.Background(), token, score, "Quick."); err != nil {
			t.Fatalf("rate: %v", err)
		}
	}
	return resolved
}

// lastMove is the latest transition taken on an issue and who took it, as the
// outbox announced it.
func lastMove(t *testing.T, h *harness, ws *workspace, key string) (name string, actor uuid.UUID) {
	t.Helper()
	var actorID string
	if err := h.super.QueryRow(context.Background(), `
		SELECT payload->>'transition', payload->>'actorId' FROM outbox_event
		WHERE topic = $1 AND payload->>'key' = $2
		ORDER BY id DESC LIMIT 1`, events.TopicIssueTransitioned, key).Scan(&name, &actorID); err != nil {
		t.Fatalf("no move announced on %s: %v", key, err)
	}
	return name, uuid.MustParse(actorID)
}

func emailOf(t *testing.T, h *harness, id uuid.UUID) string {
	t.Helper()
	var email string
	if err := h.super.QueryRow(context.Background(), `SELECT email FROM app_user WHERE id = $1`, id).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return email
}

// The resolution mail promises that a reply brings the request back, from the
// reporter or a follower, in the portal or by mail, and a rating stays history.
func TestACustomersReplyReopensAResolvedRequest(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "reopening")
	d, p := ws.aDesk(t, h, "Reopen desk")
	customer := ws.customerOf(t, h, "reporter")
	follower := ws.customerOf(t, h, "follower")

	// Message ids are unique in the table for good, so each run mints its own.
	run := uuid.NewString()[:8]

	reopened := func(t *testing.T, key string, who issue.Actor, rated int) {
		t.Helper()
		after, err := ws.issues.ByKey(ws.ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if after.Status.Category == workflow.CategoryDone || after.Status.Name != "Waiting for support" {
			t.Fatalf("after the reply %s is %q, want back with the desk in Waiting for support", key, after.Status.Name)
		}
		if name, actor := lastMove(t, h, ws, key); name != desk.Reopen || actor != who.UserID {
			t.Errorf("the request was moved by %q as %s, want Reopen taken as the customer %s", name, actor, who.UserID)
		}
		open, err := d.Queue(ws.ctx, p.Key, desk.QueueOpen, ws.actor.UserID, issue.Page{})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(queueKeys(open.Rows), ","), key) {
			t.Errorf("open queue = %v, want %s back in it", queueKeys(open.Rows), key)
		}
		resolution := timerFor(t, mustTimers(t, d, ws, key), desk.Resolution)
		if resolution.CompletedAt != nil || resolution.RunningSince == nil || resolution.ElapsedSeconds > 5 {
			t.Errorf("resolution clock = %+v, want started over", resolution)
		}
		if rated > 0 {
			rating, err := d.Rating(ws.ctx, key)
			if err != nil {
				t.Fatalf("the rating went with the reopen: %v", err)
			}
			if rating.Score == nil || *rating.Score != rated || rating.RatedAt == nil {
				t.Errorf("rating = %+v, want the %d kept as history", rating, rated)
			}
		}
	}

	t.Run("a reply in the portal reopens", func(t *testing.T) {
		resolved := resolvedRequest(t, h, ws, d, p.Key, "The printer is on fire", customer, 4)
		if timerFor(t, mustTimers(t, d, ws, resolved.Key), desk.Resolution).CompletedAt == nil {
			t.Fatal("resolving did not complete the resolution clock")
		}
		if _, _, err := d.Reply(ws.ctx, resolved.Key, "Still burning.", customer); err != nil {
			t.Fatal(err)
		}
		reopened(t, resolved.Key, customer, 4)
	})

	read := func(t *testing.T, uid, raw string) {
		t.Helper()
		inbox := &fakeInbox{mail: map[string]string{uid: raw}}
		reader := desk.NewInbound(h.cluster, d, ws.issues, &fakeMailer{}, inbox,
			desk.InboundConfig{Address: inboxAddress, From: "Armature <no-reply@armature.test>", AppURL: "http://app.test"},
			slog.New(slog.NewTextHandler(os.Stderr, nil)))
		if err := reader.Once(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(inbox.deleted) != 1 {
			t.Fatalf("the mail %s was not consumed", uid)
		}
	}

	t.Run("a reply by mail from the reporter reopens", func(t *testing.T) {
		resolved := resolvedRequest(t, h, ws, d, p.Key, "The lift is stuck", customer, 0)
		read(t, "reporter", rawMail(emailOf(t, h, customer.UserID), inboxAddress, "Re: ["+resolved.Key+"] your request is resolved",
			"reporter-"+run+"@example.com", "It stopped again."))
		reopened(t, resolved.Key, customer, 0)
	})

	t.Run("a reply by mail from a follower reopens", func(t *testing.T) {
		resolved := resolvedRequest(t, h, ws, d, p.Key, "The heating is off", customer, 5)
		if _, _, _, err := ws.issues.AddWatcher(ws.ctx, resolved.Key, follower.UserID, ws.actor); err != nil {
			t.Fatal(err)
		}
		read(t, "follower", rawMail(emailOf(t, h, follower.UserID), inboxAddress, "Re: ["+resolved.Key+"] your request is resolved",
			"follower-"+run+"@example.com", "It is cold on our floor too."))
		reopened(t, resolved.Key, follower, 5)
	})
}

// With no way out of done the reply is kept and the request stays put, so the
// agents hear of it in the inbox: the assignee, else whoever finished it.
func TestAReplyNoWayOutOfDoneCanTakeIsToldToTheAgents(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "noway")
	d, p := ws.aDesk(t, h, "Dead end desk")
	customer := ws.customerOf(t, h, "stuck")
	bob := h.joinExisting(t, h.email(t, "bob"), "member")

	// The desk follows a copy of the service request workflow that has no way
	// back out of Resolved; every other desk keeps the original.
	var serviceFlow uuid.UUID
	if err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT id FROM workflow WHERE name = 'Service request workflow'`).Scan(&serviceFlow)
	}); err != nil {
		t.Fatalf("find the service request workflow: %v", err)
	}
	deadEnd, _, err := ws.admin.CopyWorkflow(ws.ctx, serviceFlow, h.name(t, "Dead end workflow"), ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM workflow_transition WHERE workflow_id = $1 AND name = 'Reopen'`, deadEnd.ID)
		if err == nil && tag.RowsAffected() != 1 {
			t.Fatalf("removed %d Reopen transitions, want the copy's one", tag.RowsAffected())
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	scheme, _, err := ws.admin.CreateScheme(ws.ctx, workflow.SchemeInput{Name: h.name(t, "Dead end scheme"), Items: []workflow.SchemeItemInput{{WorkflowID: deadEnd.ID}}}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.projects.SetWorkflowScheme(ws.ctx, p.Key, &scheme.ID, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}

	assigned := resolvedRequest(t, h, ws, d, p.Key, "The door squeaks", customer, 3)
	bobPtr := &bob
	if _, _, err := ws.issues.Update(ws.ctx, assigned.Key, issue.UpdateInput{Assignee: &bobPtr}, ws.actor); err != nil {
		t.Fatal(err)
	}
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	unassigned, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "Where is the key?"}, customer)
	if err != nil {
		t.Fatal(err)
	}
	ws.move(t, h, unassigned.Key, "Resolve")

	fan := notify.NewFanOut(h.cluster, nil, nil, "http://app.test", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	inbox := notify.NewService(h.cluster)
	toldAbout := func(t *testing.T, user uuid.UUID, key string) []notify.Notification {
		t.Helper()
		items, err := inbox.Inbox(ws.ctx, user, false, 0)
		if err != nil {
			t.Fatal(err)
		}
		var out []notify.Notification
		for _, n := range items {
			if n.IssueKey == key && strings.Contains(n.Title, "stays Resolved") {
				out = append(out, n)
			}
		}
		return out
	}

	for _, c := range []struct {
		name   string
		key    string
		told   uuid.UUID
		silent uuid.UUID
	}{
		{"the assignee hears of a reply the workflow cannot take back", assigned.Key, bob, ws.actor.UserID},
		{"whoever resolved it hears when nobody is assigned", unassigned.Key, ws.actor.UserID, bob},
	} {
		t.Run(c.name, func(t *testing.T) {
			comment, _, err := d.Reply(ws.ctx, c.key, "It is back.", customer)
			if err != nil {
				t.Fatalf("the reply was refused: %v", err)
			}
			if comment == nil || issue.PlainText(comment.Body) != "It is back." {
				t.Errorf("comment = %+v, want the reply kept", comment)
			}
			after, _ := ws.issues.ByKey(ws.ctx, c.key)
			if after.Status.Name != "Resolved" {
				t.Errorf("the request moved to %q with no way out of Resolved", after.Status.Name)
			}

			e := h.announced(t, ws, events.TopicRequestStaysDone, c.key)
			if err := fan.Handle(context.Background(), e); err != nil {
				t.Fatal(err)
			}
			if err := fan.Handle(context.Background(), e); err != nil {
				t.Fatal(err)
			}
			got := toldAbout(t, c.told, c.key)
			if len(got) != 1 || got[0].Kind != notify.KindCommented || !strings.Contains(got[0].Body, "It is back.") || !strings.Contains(got[0].Body, "Move it by hand") {
				t.Errorf("inbox = %+v, want one row with the reply and what to do", got)
			}
			if others := toldAbout(t, c.silent, c.key); len(others) != 0 {
				t.Errorf("somebody else was told too: %+v", others)
			}
		})
	}

	if rating, err := d.Rating(ws.ctx, assigned.Key); err != nil || rating.Score == nil || *rating.Score != 3 {
		t.Errorf("rating after the reply = %+v %v, want the 3 untouched", rating, err)
	}
}
