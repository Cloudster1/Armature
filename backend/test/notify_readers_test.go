//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/notify"
)

// accountOf reads the account behind an address the test made.
func (h *harness) accountOf(t *testing.T, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM app_user WHERE email = $1`, email).Scan(&id); err != nil {
		t.Fatalf("find %s: %v", email, err)
	}
	return id
}

// Somebody who may not see a project cannot be made to watch its issues, by
// the API, by a mention or straight in the database (#409).
func TestOnlyAReaderCanWatchAnIssue(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "readers")
	made := ws.newIssue(t, "Next year's prices")
	wallEmail := h.outsider(t, "wall")
	wall := h.accountOf(t, wallEmail)
	watches := func(t *testing.T) int {
		t.Helper()
		var n int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM issue_watcher WHERE issue_id = $1 AND user_id = $2`, made.ID, wall).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, ws.owner.Principal.User.Email)
	refused := want(t, owner.post("/api/v1/issues/"+made.Key+"/watchers", map[string]any{"userId": wall}), http.StatusUnprocessableEntity, "a watcher who may not see the project")
	if refused.ErrorCode() != "validation_failed" || !strings.Contains(refused.Raw, "may not see this project") || !strings.Contains(refused.Raw, "Give them a role in the project") {
		t.Errorf("refusal = %s, want a sentence saying why and what to do", refused.Raw)
	}
	if n := watches(t); n != 0 {
		t.Fatalf("the refused watcher was written anyway: %d rows", n)
	}

	if _, err := h.super.Exec(context.Background(), `INSERT INTO issue_watcher (issue_id, user_id) VALUES ($1, $2)`, made.ID, wall); err == nil {
		t.Error("the database took a watcher who may not see the project")
	} else if pgErr := (*pgconn.PgError)(nil); !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("the database refused with %v, want a check violation", err)
	}

	// Naming them in a comment keeps the comment and makes no watcher.
	if _, _, err := ws.issues.AddComment(ws.ctx, made.Key, issue.TextDocument("Ask @"+nameOf(wallEmail)+" maybe"), ws.actor); err != nil {
		t.Fatalf("a comment naming somebody outside was refused: %v", err)
	}
	if n := watches(t); n != 0 {
		t.Errorf("a mention made somebody outside the project a watcher: %d rows", n)
	}

	// Once they may read the project, they may watch it.
	if _, err := h.super.Exec(context.Background(), `INSERT INTO role_assignment (role, user_id, project_id) VALUES ('reader', $1, $2)`, wall, ws.project.ID); err != nil {
		t.Fatal(err)
	}
	want(t, owner.post("/api/v1/issues/"+made.Key+"/watchers", map[string]any{"userId": wall}), http.StatusCreated, "a reader of the project")
	if n := watches(t); n != 1 {
		t.Errorf("watcher rows = %d, want the reader's one", n)
	}
}

// Access is asked when a notification goes out, not when the watching began:
// somebody whose role is taken away hears nothing more (#409).
func TestAWatcherWhoLosesAccessIsToldNothingMore(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "lostaccess")
	made := ws.newIssue(t, "The merger")
	leaverEmail := h.outsider(t, "leaver")
	leaver := h.accountOf(t, leaverEmail)
	if _, err := h.super.Exec(context.Background(), `INSERT INTO role_assignment (role, user_id, project_id) VALUES ('reader', $1, $2)`, leaver, ws.project.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ws.issues.AddWatcher(ws.ctx, made.Key, leaver, ws.actor); err != nil {
		t.Fatal(err)
	}

	mailer := &fakeMailer{}
	fan := notify.NewFanOut(h.cluster, nil, mailer, "http://app.test", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	comment := func(text string) {
		t.Helper()
		c, _, err := ws.issues.AddComment(ws.ctx, made.Key, issue.TextDocument(text), ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"key": made.Key, "commentId": c.ID, "actorId": ws.actor.UserID})
		if err := fan.Handle(context.Background(), events.Event{ID: uuid.New(), Topic: events.TopicCommentAdded, Payload: raw}); err != nil {
			t.Fatal(err)
		}
	}
	told := func() (rows, mails int) {
		t.Helper()
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM notification WHERE user_id = $1`, leaver).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		mailer.mu.Lock()
		defer mailer.mu.Unlock()
		for _, m := range mailer.sent {
			if m.To == leaverEmail {
				mails++
			}
		}
		return rows, mails
	}

	comment("The numbers are in.")
	if rows, mails := told(); rows != 1 || mails != 1 {
		t.Fatalf("while a reader: %d rows and %d mails, want one of each", rows, mails)
	}

	if _, err := h.super.Exec(context.Background(), `DELETE FROM role_assignment WHERE user_id = $1`, leaver); err != nil {
		t.Fatal(err)
	}
	comment("The price is agreed.")
	if rows, mails := told(); rows != 1 || mails != 1 {
		t.Errorf("after losing access: %d rows and %d mails, want nothing new", rows, mails)
	}
	mailer.mu.Lock()
	defer mailer.mu.Unlock()
	for _, m := range mailer.sent {
		if m.To == leaverEmail && strings.Contains(m.Body, "The price is agreed.") {
			t.Errorf("the comment reached somebody who may no longer read it: %+v", m)
		}
	}
}

// The inbox and mail are separate switches: a kind wanted by mail alone is
// mailed once, however often the event comes, and never shown (#410).
func TestAKindCanBeMailedWithoutTheInbox(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "mailonly")
	made := ws.newIssue(t, "Paint the fence")
	bobEmail := h.email(t, "mailonly")
	bob := h.joinAsMember(t, bobEmail)
	if _, _, _, err := ws.issues.AddWatcher(ws.ctx, made.Key, bob, ws.actor); err != nil {
		t.Fatal(err)
	}
	inbox := notify.NewService(h.cluster)
	prefs := notify.DefaultPreferences()
	prefs.InApp[notify.KindCommented] = false
	prefs.Mail[notify.KindCommented] = true
	if _, err := inbox.SavePreferences(ws.ctx, bob, prefs); err != nil {
		t.Fatal(err)
	}

	mailer := &fakeMailer{}
	fan := notify.NewFanOut(h.cluster, nil, mailer, "http://app.test", slog.New(slog.NewTextHandler(os.Stderr, nil)))
	c, _, err := ws.issues.AddComment(ws.ctx, made.Key, issue.TextDocument("Green, please."), ws.actor)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"key": made.Key, "commentId": c.ID, "actorId": ws.actor.UserID})
	e := events.Event{ID: uuid.New(), Topic: events.TopicCommentAdded, Payload: raw}
	for range 2 {
		if err := fan.Handle(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	}

	mailer.mu.Lock()
	var mails []string
	for _, m := range mailer.sent {
		if m.To == bobEmail {
			mails = append(mails, m.Body)
		}
	}
	mailer.mu.Unlock()
	if len(mails) != 1 || !strings.Contains(mails[0], "Green, please.") {
		t.Errorf("mails to bob = %q, want the comment once", mails)
	}
	if items, err := inbox.Inbox(ws.ctx, bob, false, 0); err != nil || len(items) != 0 {
		t.Errorf("bob's inbox = %+v %v, want nothing shown with the inbox off", items, err)
	}
	if n, err := inbox.Unread(ws.ctx, bob); err != nil || n != 0 {
		t.Errorf("bob's unread = %d %v, want 0", n, err)
	}
	var hidden int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM notification WHERE user_id = $1 AND hidden`, bob).Scan(&hidden); err != nil || hidden != 1 {
		t.Errorf("hidden rows = %d %v, want the one that keeps the mail from going twice", hidden, err)
	}
}
