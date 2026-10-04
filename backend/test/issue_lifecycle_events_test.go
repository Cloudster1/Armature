//go:build integration

package test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/automation"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/notify"
	"github.com/armature/armature/backend/internal/webhook"
)

// announced reads the one event on a topic about an issue from the outbox,
// failing unless there is exactly one.
func (h *harness) announced(t *testing.T, ws *workspace, topic, key string) events.Event {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT id, topic, payload FROM outbox_event
		WHERE org_id = $1 AND topic = $2 AND payload->>'key' = $3`, ws.orgID, topic, key)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var found []events.Event
	for rows.Next() {
		e := events.Event{OrgID: ws.orgID}
		if err := rows.Scan(&e.ID, &e.Topic, &e.Payload); err != nil {
			t.Fatal(err)
		}
		found = append(found, e)
	}
	if len(found) != 1 {
		t.Fatalf("%d %s events about %s, want one", len(found), topic, key)
	}
	return found[0]
}

// withoutImported is the same event as an ordinary creation, which shows it is
// the mark alone that keeps a consumer quiet.
func withoutImported(t *testing.T, e events.Event) events.Event {
	t.Helper()
	var p map[string]any
	if err := json.Unmarshal(e.Payload, &p); err != nil {
		t.Fatal(err)
	}
	delete(p, "imported")
	e.ID = uuid.New()
	e.Payload, _ = json.Marshal(p)
	return e
}

// A copy of the issues kept elsewhere stays true only if every way an issue
// comes or goes is announced: filed, cloned, imported and deleted.
func TestEveryIssueThatComesOrGoesIsAnnounced(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "lifecycle")
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	hooks := webhook.NewService(h.cluster, log)
	sink := &receiver{status: http.StatusOK}
	server := httptest.NewServer(sink)
	defer server.Close()
	endpoint, _, err := hooks.Create(ws.ctx, webhook.Input{Name: "Cache", URL: server.URL, Topics: []string{events.TopicIssueCreated, events.TopicIssueDeleted}})
	if err != nil {
		t.Fatalf("subscribe to issue.created and issue.deleted: %v", err)
	}
	t.Cleanup(func() { _, _ = hooks.Delete(ws.ctx, endpoint.ID) })
	deliver := func(e events.Event) string {
		t.Helper()
		before := len(sink.got)
		if err := hooks.Handle(context.Background(), e); err != nil {
			t.Fatal(err)
		}
		if _, err := hooks.SendDue(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(sink.got) != before+1 {
			t.Fatalf("the endpoint received %d deliveries for %s, want one", len(sink.got)-before, e.Topic)
		}
		return sink.got[before].Event
	}

	t.Run("a clone is announced as a new issue", func(t *testing.T) {
		source := ws.newIssue(t, "Template checklist")
		clone, _, err := ws.issues.Clone(ws.ctx, source.Key, issue.CloneOptions{}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		e := h.announced(t, ws, events.TopicIssueCreated, clone.Key)
		if got := deliver(e); got != events.TopicIssueCreated {
			t.Errorf("delivered %q, want issue.created", got)
		}
	})

	t.Run("a deletion is announced, delivered and kept in the audit log", func(t *testing.T) {
		doomed := ws.newIssue(t, "Duplicate of something")
		if _, err := ws.issues.Delete(ws.ctx, doomed.Key, ws.actor); err != nil {
			t.Fatal(err)
		}
		e := h.announced(t, ws, events.TopicIssueDeleted, doomed.Key)
		var p struct {
			IssueID uuid.UUID `json:"issueId"`
			ActorID uuid.UUID `json:"actorId"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil || p.IssueID != doomed.ID || p.ActorID != ws.actor.UserID {
			t.Errorf("payload = %s, want the issue and who deleted it", e.Payload)
		}
		if got := deliver(e); got != events.TopicIssueDeleted {
			t.Errorf("delivered %q, want issue.deleted", got)
		}

		// The issue's own history went with it, so the log is the one record.
		if err := audit.NewConsumer(h.cluster, nil, log).Handle(context.Background(), e); err != nil {
			t.Fatal(err)
		}
		rows, err := audit.NewService(h.cluster).List(ws.ctx, audit.Filter{Action: events.TopicIssueDeleted})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].TargetID == nil || *rows[0].TargetID != doomed.ID || rows[0].ActorID == nil || *rows[0].ActorID != ws.actor.UserID {
			t.Fatalf("audit rows = %+v, want the deletion with its actor", rows)
		}

		// Nobody is told in their inbox, and the vanished row is no error.
		fan := notify.NewFanOut(h.cluster, nil, &fakeMailer{}, "http://app.test", log)
		if err := fan.Handle(context.Background(), e); err != nil {
			t.Errorf("the inbox refused a deletion: %v", err)
		}
	})

	t.Run("an import is announced, marked, and tells nobody", func(t *testing.T) {
		bob := h.joinExisting(t, ws, h.email(t, "bob"), "member")
		importer := ws.actor
		importer.Import = true
		made, _, err := ws.issues.Import(ws.ctx, issue.ImportInput{
			CreateInput: issue.CreateInput{ProjectKey: ws.project.Key, Summary: "Brought over from Jira", AssigneeID: &bob},
			ExternalKey: "jira:OLD-7",
			Source:      "a Jira export",
		}, importer)
		if err != nil {
			t.Fatal(err)
		}
		e := h.announced(t, ws, events.TopicIssueCreated, made.Issue.Key)
		var p struct {
			Imported bool `json:"imported"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil || !p.Imported {
			t.Fatalf("payload = %s, want it marked imported", e.Payload)
		}
		if got := deliver(e); got != events.TopicIssueCreated {
			t.Errorf("delivered %q, want issue.created", got)
		}

		// Importing the same row again corrects it; it is not a new issue.
		again, _, err := ws.issues.Import(ws.ctx, issue.ImportInput{
			CreateInput: issue.CreateInput{ProjectKey: ws.project.Key, Summary: "Brought over from Jira, corrected", AssigneeID: &bob},
			ExternalKey: "jira:OLD-7",
			Source:      "a Jira export",
		}, importer)
		if err != nil || !again.Updated {
			t.Fatalf("second import = %+v, %v", again, err)
		}
		h.announced(t, ws, events.TopicIssueCreated, made.Issue.Key)

		// The inbox stays empty for the imported issue, and fills for the
		// same event unmarked.
		inbox := notify.NewService(h.cluster)
		fan := notify.NewFanOut(h.cluster, nil, &fakeMailer{}, "http://app.test", log)
		if err := fan.Handle(context.Background(), e); err != nil {
			t.Fatal(err)
		}
		if items, _ := inbox.Inbox(ws.ctx, bob, false, 0); len(items) != 0 {
			t.Fatalf("bob was told about an imported issue: %+v", items)
		}
		if err := fan.Handle(context.Background(), withoutImported(t, e)); err != nil {
			t.Fatal(err)
		}
		if items, _ := inbox.Inbox(ws.ctx, bob, false, 0); len(items) != 1 {
			t.Fatalf("bob's inbox for an ordinary creation = %+v, want one", items)
		}

		// A rule watching new issues leaves the record as it arrived.
		rules := automation.NewService(h.cluster, ws.issues, label.NewService(h.cluster), log)
		rule, _, err := rules.Create(ws.ctx, ws.project.Key, automation.Input{
			Name: "Triage new work", Trigger: automation.Trigger{Kind: automation.TriggerIssueCreated},
			Actions: []automation.Action{{Kind: automation.ActionAddLabel, Value: "triage"}},
		}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if err := rules.Handle(context.Background(), e); err != nil {
			t.Fatal(err)
		}
		if runs, _ := rules.Runs(ws.ctx, rule.ID, 0); len(runs) != 0 {
			t.Fatalf("a rule ran on an imported issue: %+v", runs)
		}
		if err := rules.Handle(context.Background(), withoutImported(t, e)); err != nil {
			t.Fatal(err)
		}
		if runs, _ := rules.Runs(ws.ctx, rule.ID, 0); len(runs) != 1 {
			t.Fatalf("rule runs for an ordinary creation = %+v, want one", runs)
		}
	})

	t.Run("an imported request is not given a receipt", func(t *testing.T) {
		d, p := ws.aDesk(t, h, "Imported desk")
		types, _ := d.RequestTypes(ws.ctx, p.Key)
		question := requestTypeNamed(t, types, "Ask a question")
		importer := ws.actor
		importer.Import = true
		made, _, err := ws.issues.Import(ws.ctx, issue.ImportInput{
			CreateInput: issue.CreateInput{ProjectKey: p.Key, TypeID: question.IssueTypeID, Summary: "Old question", RequestTypeID: &question.ID},
			Source:      "a Jira export",
		}, importer)
		if err != nil {
			t.Fatal(err)
		}
		e := h.announced(t, ws, events.TopicIssueCreated, made.Issue.Key)
		mailer := &fakeMailer{}
		notifier := desk.NewNotifier(h.cluster, nil, mailer, "http://app.test", log)
		if err := notifier.Handle(context.Background(), e); err != nil {
			t.Fatal(err)
		}
		if len(mailer.sent) != 0 {
			t.Fatalf("an imported request was mailed a receipt: %+v", mailer.sent)
		}
		if err := notifier.Handle(context.Background(), withoutImported(t, e)); err != nil {
			t.Fatal(err)
		}
		if len(mailer.sent) != 1 {
			t.Fatalf("receipts for an ordinary request = %+v, want one", mailer.sent)
		}
	})
}
