//go:build integration

package test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/notify"
)

// The people on an issue are told through the inbox, once per event however
// often the stream repeats it, and mail is a copy that a preference can turn
// off without touching the inbox.
func TestPeopleOnAnIssueAreToldOnce(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "inbox")
	bob := h.joinExisting(t, ws, h.email(t, "bob"), "member")
	made := ws.newIssue(t, "Fix the door")

	mailer := &fakeMailer{}
	fan := notify.NewFanOut(h.cluster, nil, mailer, "http://app.test", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	inbox := notify.NewService(h.cluster)
	handle := func(id uuid.UUID, topic string, payload map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(payload)
		if err := fan.Handle(context.Background(), events.Event{ID: id, OrgID: ws.orgID, Topic: topic, Payload: raw}); err != nil {
			t.Fatal(err)
		}
	}

	// Assigning tells the assignee, and telling them twice for one event is once.
	bobPtr := &bob
	if _, _, err := ws.issues.Update(ws.ctx, made.Key, issue.UpdateInput{Assignee: &bobPtr}, ws.actor); err != nil {
		t.Fatal(err)
	}
	assigned := uuid.New()
	payload := map[string]any{"key": made.Key, "actorId": ws.actor.UserID, "changes": []map[string]any{{"field": "assignee", "from": "", "to": "Bob"}}}
	handle(assigned, events.TopicIssueUpdated, payload)
	handle(assigned, events.TopicIssueUpdated, payload)

	items, err := inbox.Inbox(ws.ctx, bob, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != notify.KindAssigned || items[0].IssueKey != made.Key || !strings.Contains(items[0].Title, "assigned "+made.Key+" to you") {
		t.Fatalf("bob's inbox = %+v, want one assignment", items)
	}
	if len(mailer.sent) != 1 || !strings.Contains(mailer.sent[0].Subject, made.Key) || !strings.Contains(mailer.sent[0].Body, "http://app.test/issues/"+made.Key) {
		t.Fatalf("mails = %+v, want one to bob with a link", mailer.sent)
	}
	mine, _ := inbox.Inbox(ws.ctx, ws.actor.UserID, false, 0)
	if len(mine) != 0 {
		t.Errorf("the actor was told about their own act: %+v", mine)
	}

	// A mention makes the person a watcher and tells them why.
	if _, _, err := ws.issues.AddComment(ws.ctx, made.Key, issue.TextDocument("Have a look @Invited Person, please"), ws.actor); err != nil {
		t.Fatal(err)
	}
	watching, _ := ws.issues.Watches(ws.ctx, made.Key, bob)
	if !watching {
		t.Fatal("a mention did not make bob a watcher")
	}
	comments, _ := ws.issues.Comments(ws.ctx, made.Key, true)
	handle(uuid.New(), events.TopicCommentAdded, map[string]any{"key": made.Key, "commentId": comments[len(comments)-1].ID, "actorId": ws.actor.UserID, "mentions": []uuid.UUID{bob}})
	items, _ = inbox.Inbox(ws.ctx, bob, true, 0)
	if len(items) != 2 || items[0].Kind != notify.KindMentioned || !strings.Contains(items[0].Body, "Have a look") {
		t.Fatalf("bob's unread = %+v, want the mention on top", items)
	}
	if n, _ := inbox.Unread(ws.ctx, bob); n != 2 {
		t.Errorf("unread = %d, want 2", n)
	}

	// Mail off for a kind leaves the inbox on.
	prefs := notify.DefaultPreferences()
	prefs.Mail[notify.KindTransitioned] = false
	if _, err := inbox.SavePreferences(ws.ctx, bob, prefs); err != nil {
		t.Fatal(err)
	}
	ws.move(t, h, made.Key, "Start progress")
	before := len(mailer.sent)
	handle(uuid.New(), events.TopicIssueTransitioned, map[string]any{"key": made.Key, "actorId": ws.actor.UserID, "toStatus": "In Progress"})
	items, _ = inbox.Inbox(ws.ctx, bob, true, 0)
	if len(items) != 3 || items[0].Kind != notify.KindTransitioned {
		t.Errorf("bob's unread after the move = %+v, want the move on top", items)
	}
	if len(mailer.sent) != before {
		t.Errorf("a move was mailed with mail for moves off: %+v", mailer.sent[before:])
	}

	// Reading clears the badge.
	if _, err := inbox.MarkRead(ws.ctx, bob, []uuid.UUID{items[0].ID}); err != nil {
		t.Fatal(err)
	}
	if n, _ := inbox.Unread(ws.ctx, bob); n != 2 {
		t.Errorf("unread after one read = %d, want 2", n)
	}
	if _, err := inbox.MarkAllRead(ws.ctx, bob); err != nil {
		t.Fatal(err)
	}
	if n, _ := inbox.Unread(ws.ctx, bob); n != 0 {
		t.Errorf("unread after all read = %d, want 0", n)
	}

	// A notification cannot point at another organization's issue, whatever
	// the service does: the database refuses it.
	other := h.newWorkspace(t, "elsewhere")
	theirs := other.newIssue(t, "Not yours")
	_, err = h.super.Exec(context.Background(), `
		INSERT INTO notification (org_id, user_id, issue_id, kind, title, event_id)
		VALUES ($1, $2, $3, 'watching', 'x', $4)`, ws.orgID, bob, theirs.ID, uuid.New())
	if err == nil || !strings.Contains(err.Error(), "same organization") {
		t.Errorf("a cross-org notification was accepted: %v", err)
	}
}

// A digest bundles what was queued into one mail, on the person's schedule.
func TestADigestBundlesTheQueue(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "digest")
	bob := h.joinExisting(t, ws, h.email(t, "bobdigest"), "member")
	inbox := notify.NewService(h.cluster)
	prefs := notify.DefaultPreferences()
	prefs.Digest = notify.DigestHourly
	if _, err := inbox.SavePreferences(ws.ctx, bob, prefs); err != nil {
		t.Fatal(err)
	}

	mailer := &fakeMailer{}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	fan := notify.NewFanOut(h.cluster, nil, mailer, "http://app.test", log)
	for _, summary := range []string{"First", "Second"} {
		made := ws.newIssue(t, summary)
		bobPtr := &bob
		if _, _, err := ws.issues.Update(ws.ctx, made.Key, issue.UpdateInput{Assignee: &bobPtr}, ws.actor); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"key": made.Key, "actorId": ws.actor.UserID, "changes": []map[string]any{{"field": "assignee"}}})
		if err := fan.Handle(context.Background(), events.Event{ID: uuid.New(), OrgID: ws.orgID, Topic: events.TopicIssueUpdated, Payload: raw}); err != nil {
			t.Fatal(err)
		}
	}
	if len(mailer.sent) != 0 {
		t.Fatalf("mails before the digest = %+v, want none", mailer.sent)
	}

	digest := notify.NewDigest(h.cluster, mailer, "http://app.test", log)
	sent, err := digest.Once(context.Background())
	if err != nil || sent < 1 {
		t.Fatalf("digest sent %d, %v, want bob's", sent, err)
	}
	var bobs []struct{ To, Subject, Body string }
	for _, m := range mailer.sent {
		if strings.HasPrefix(m.To, "bobdigest") {
			bobs = append(bobs, m)
		}
	}
	if len(bobs) != 1 || !strings.Contains(bobs[0].Subject, "2 updates") || strings.Count(bobs[0].Body, "assigned") != 2 || !strings.Contains(bobs[0].Body, "http://app.test/inbox") {
		t.Fatalf("digest mail = %+v, want one with both items", bobs)
	}

	var queued int
	if err := h.cluster.Read(db.PinPrimary(ws.ctx), func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM notification_digest WHERE user_id = $1`, bob).Scan(&queued)
	}); err != nil {
		t.Fatal(err)
	}
	if queued != 0 {
		t.Errorf("queue after the digest = %d, want empty", queued)
	}
	if again, _ := digest.Once(context.Background()); again != 0 {
		t.Errorf("a second run sent %d, want nothing", again)
	}
}

// The inbox over the API is the caller's own, and the preferences page saves
// what it says.
func TestTheInboxOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "inboxapi")

	want(t, c.get("/api/v1/notifications"), http.StatusOK, "inbox")
	want(t, c.get("/api/v1/notifications?unread=true&limit=5"), http.StatusOK, "unread inbox")
	count := want(t, c.get("/api/v1/notifications/unread-count"), http.StatusOK, "unread count")
	if count.Body["unread"].(float64) != 0 {
		t.Errorf("unread = %v, want 0", count.Body["unread"])
	}
	want(t, c.post("/api/v1/notifications/read", map[string]any{}), http.StatusBadRequest, "marking nothing")
	want(t, c.post("/api/v1/notifications/read", map[string]any{"all": true}), http.StatusNoContent, "marking all")

	got := want(t, c.get("/api/v1/notification-preferences"), http.StatusOK, "preferences")
	if got.Body["preferences"].(map[string]any)["digest"] != "off" {
		t.Errorf("default preferences = %v", got.Body)
	}
	want(t, c.put("/api/v1/notification-preferences", map[string]any{"digest": "weekly", "mail": map[string]bool{}, "inapp": map[string]bool{}}), http.StatusUnprocessableEntity, "a bad schedule")
	saved := want(t, c.put("/api/v1/notification-preferences", map[string]any{"digest": "daily", "watchOwn": false, "mail": map[string]bool{"commented": false}, "inapp": map[string]bool{}}), http.StatusOK, "saving")
	p := saved.Body["preferences"].(map[string]any)
	if p["digest"] != "daily" || p["watchOwn"] != false || p["mail"].(map[string]any)["commented"] != false {
		t.Errorf("saved preferences = %v", p)
	}
}
