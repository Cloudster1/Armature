//go:build integration

package test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/automation"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/webhook"
)

func (ws *workspace) automation(h *harness) *automation.Service {
	return automation.NewService(h.cluster, ws.issues, label.NewService(h.cluster), slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

// A rule runs once per event however often the stream repeats it, does not
// chase its own tail, and stops at its hourly cap; the run log says why.
func TestARuleRunsOncePerEventAndNotOnItself(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "rules")
	svc := ws.automation(h)

	rule, _, err := svc.Create(ws.ctx, ws.project.Key, automation.Input{
		Name: "Mark moved work", Trigger: automation.Trigger{Kind: automation.TriggerIssueTransitioned},
		Conditions: []automation.Condition{{Kind: automation.ConditionNQL, Query: "statusCategory = in_progress"}},
		Actions:    []automation.Action{{Kind: automation.ActionAddLabel, Value: "moving"}, {Kind: automation.ActionAddComment, Text: "{{actor.name}} moved {{issue.key}}"}},
	}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	made := ws.newIssue(t, "Fix the gate")
	ws.move(t, h, made.Key, "Start progress")

	eventID := uuid.New()
	payload, _ := json.Marshal(map[string]any{"key": made.Key, "actorId": ws.actor.UserID, "toStatus": "In Progress"})
	for range 2 {
		if err := svc.Handle(context.Background(), events.Event{ID: eventID, OrgID: ws.orgID, Topic: events.TopicIssueTransitioned, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := ws.issues.ByKey(ws.ctx, made.Key)
	if len(after.Labels) != 1 || after.Labels[0].Name != "moving" {
		t.Fatalf("labels = %+v, want moving once", after.Labels)
	}
	comments, _ := ws.issues.Comments(ws.ctx, made.Key, true)
	if len(comments) != 1 || !strings.Contains(string(comments[0].Body), "moved "+made.Key) {
		t.Fatalf("comments = %d, want one from the rule", len(comments))
	}
	runs, _ := svc.Runs(ws.ctx, rule.ID, 0)
	if len(runs) != 1 || runs[0].Outcome != automation.OutcomeDone || len(runs[0].Actions) != 2 || !runs[0].Actions[0].OK {
		t.Fatalf("runs = %+v, want one done run with two actions", runs)
	}

	// The comment the rule added came back as an event by the automation
	// account; a rule watching comments does not run on it.
	commenter, _, _ := svc.Create(ws.ctx, ws.project.Key, automation.Input{
		Name: "Echo comments", Trigger: automation.Trigger{Kind: automation.TriggerCommentAdded},
		Actions: []automation.Action{{Kind: automation.ActionAddLabel, Value: "echoed"}},
	}, ws.actor.UserID)
	var automationUser uuid.UUID
	if err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT automation_user_id FROM org WHERE id = current_org_id()`).Scan(&automationUser)
	}); err != nil {
		t.Fatal(err)
	}
	own, _ := json.Marshal(map[string]any{"key": made.Key, "actorId": automationUser, "commentId": comments[0].ID})
	_ = svc.Handle(context.Background(), events.Event{ID: uuid.New(), OrgID: ws.orgID, Topic: events.TopicCommentAdded, Payload: own})
	runs, _ = svc.Runs(ws.ctx, commenter.ID, 0)
	if len(runs) != 1 || runs[0].Outcome != automation.OutcomeSkipped || runs[0].Reason != "actor is automation" {
		t.Fatalf("runs on own event = %+v, want skipped for the actor", runs)
	}

	// A condition that fails is a skip that says which.
	other := ws.newIssue(t, "Still to do")
	payload, _ = json.Marshal(map[string]any{"key": other.Key, "actorId": ws.actor.UserID})
	_ = svc.Handle(context.Background(), events.Event{ID: uuid.New(), OrgID: ws.orgID, Topic: events.TopicIssueTransitioned, Payload: payload})
	runs, _ = svc.Runs(ws.ctx, rule.ID, 0)
	if len(runs) != 2 || runs[0].Outcome != automation.OutcomeSkipped || !strings.Contains(runs[0].Reason, "does not match") {
		t.Fatalf("runs after a miss = %+v, want a skip naming the query", runs)
	}

	// The cap: a rule allowed one run an hour is capped on its second.
	capped, _, _ := svc.Create(ws.ctx, ws.project.Key, automation.Input{
		Name: "Once an hour", Trigger: automation.Trigger{Kind: automation.TriggerIssueCreated}, HourlyCap: 1,
		Actions: []automation.Action{{Kind: automation.ActionSetField, Field: "priority", Value: "high"}},
	}, ws.actor.UserID)
	for _, key := range []string{made.Key, other.Key} {
		raw, _ := json.Marshal(map[string]any{"key": key, "actorId": ws.actor.UserID})
		_ = svc.Handle(context.Background(), events.Event{ID: uuid.New(), OrgID: ws.orgID, Topic: events.TopicIssueCreated, Payload: raw})
	}
	runs, _ = svc.Runs(ws.ctx, capped.ID, 0)
	if len(runs) != 2 || runs[0].Outcome != automation.OutcomeCapped || runs[1].Outcome != automation.OutcomeDone {
		t.Fatalf("capped runs = %+v", runs)
	}

	// A rule cannot watch another organization's project, whatever the service says.
	stranger := h.newWorkspace(t, "strangerrules")
	_, err = h.super.Exec(context.Background(), `
		INSERT INTO automation_rule (org_id, project_id, name, trigger, actions)
		VALUES ($1, $2, 'x', '{"kind":"issue.created"}', '[{"kind":"add_label","value":"x"}]')`, ws.orgID, stranger.project.ID)
	if err == nil || !strings.Contains(err.Error(), "own organization") {
		t.Errorf("a cross-org rule was accepted: %v", err)
	}
}

// A scheduled rule runs over its query when it is due, and a manual run
// reads back what happened.
func TestScheduledAndManualRuns(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "schedules")
	svc := ws.automation(h)
	ws.newIssue(t, "Old one")
	ws.newIssue(t, "Older one")

	rule, _, err := svc.Create(ws.ctx, ws.project.Key, automation.Input{
		Name: "Nudge the open work", Trigger: automation.Trigger{Kind: automation.TriggerScheduled, Query: "statusCategory = todo", Schedule: &automation.Schedule{Unit: automation.EveryHours, Every: 1}},
		Actions: []automation.Action{{Kind: automation.ActionAddLabel, Value: "nudged"}},
	}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	// Just made, so not due; then, with its last run pushed back, due.
	if n, _ := svc.Tick(context.Background()); n != 0 {
		t.Fatalf("tick fired %d rules right after creation", n)
	}
	if _, err := h.super.Exec(context.Background(), `UPDATE automation_rule SET created_at = now() - interval '2 hours' WHERE id = $1`, rule.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := svc.Tick(context.Background()); n < 1 {
		t.Fatal("tick did not fire the due rule")
	}
	runs, _ := svc.Runs(ws.ctx, rule.ID, 0)
	if len(runs) != 1 || runs[0].Outcome != automation.OutcomeDone || runs[0].Reason != "2 issues matched" || len(runs[0].Actions) != 2 {
		t.Fatalf("scheduled run = %+v", runs)
	}
	if n, _ := svc.Tick(context.Background()); n != 0 {
		t.Fatal("tick fired again within the hour")
	}

	manual, _, _ := svc.Create(ws.ctx, ws.project.Key, automation.Input{
		Name: "By hand", Trigger: automation.Trigger{Kind: automation.TriggerIssueCreated},
		Actions: []automation.Action{{Kind: automation.ActionTransition, Value: "Nowhere"}},
	}, ws.actor.UserID)
	target := ws.newIssue(t, "Try me")
	run, err := svc.RunNow(ws.ctx, manual.ID, target.Key, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Outcome != automation.OutcomeFailed || len(run.Actions) != 1 || run.Actions[0].OK || !strings.Contains(run.Actions[0].Note, "does not offer") {
		t.Fatalf("manual run = %+v, want a failed transition that says so", run)
	}
}

// receiver is an endpoint that keeps what it was sent and answers as told.
type receiver struct {
	mu     sync.Mutex
	got    []struct{ Body, Signature, Event string }
	status int
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	r.got = append(r.got, struct{ Body, Signature, Event string }{string(body), req.Header.Get("X-Armature-Signature-256"), req.Header.Get("X-Armature-Event")})
	status := r.status
	r.mu.Unlock()
	w.WriteHeader(status)
}

// A webhook is signed with its secret, retried with backoff after a failure,
// and redelivered by hand; a rule's send webhook action lands in the same log.
func TestAWebhookIsSignedRetriedAndRedelivered(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "hooks")
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	hooks := webhook.NewService(h.cluster, log)
	sink := &receiver{status: http.StatusOK}
	server := httptest.NewServer(sink)
	defer server.Close()

	made, _, err := hooks.Create(ws.ctx, webhook.Input{Name: "Chat", URL: server.URL + "/in", Topics: []string{events.TopicIssueCreated}})
	if err != nil {
		t.Fatal(err)
	}
	// The database outlives the run; an endpoint left behind would keep its
	// retries due for hours, pointed at a server that is gone.
	t.Cleanup(func() { _, _ = hooks.Delete(ws.ctx, made.ID) })
	if !strings.HasPrefix(made.Secret, "armature_whs_") {
		t.Fatalf("secret = %q", made.Secret)
	}
	if _, _, err := hooks.Create(ws.ctx, webhook.Input{Name: "Bad", URL: "ftp://x", Topics: nil}); err == nil {
		t.Error("an ftp address was accepted")
	}
	// The automation's own topics never leave: an endpoint fed them and
	// posting to an incoming hook would be feeding itself.
	if _, _, err := hooks.Create(ws.ctx, webhook.Input{Name: "Loop", URL: server.URL, Topics: []string{events.TopicAutomationIncoming}}); err == nil {
		t.Error("a webhook on an automation topic was accepted")
	}
	_ = hooks.Handle(context.Background(), events.Event{ID: uuid.New(), OrgID: ws.orgID, Topic: events.TopicAutomationIncoming, Payload: json.RawMessage(`{}`)})

	// Subscribed topic is queued and sent; another is not.
	issueMade := uuid.New()
	_ = hooks.Handle(context.Background(), events.Event{ID: issueMade, OrgID: ws.orgID, Topic: events.TopicIssueCreated, Payload: json.RawMessage(`{"key":"X-1"}`)})
	_ = hooks.Handle(context.Background(), events.Event{ID: uuid.New(), OrgID: ws.orgID, Topic: events.TopicCommentAdded, Payload: json.RawMessage(`{}`)})
	if n, err := hooks.SendDue(context.Background()); err != nil || n < 1 {
		t.Fatalf("send due: %d %v", n, err)
	}
	if len(sink.got) != 1 || sink.got[0].Event != events.TopicIssueCreated || !webhook.Verify([]byte(sink.got[0].Body), made.Secret, sink.got[0].Signature) {
		t.Fatalf("received = %+v, want one signed issue.created", sink.got)
	}
	var envelope struct {
		ID      uuid.UUID       `json:"id"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal([]byte(sink.got[0].Body), &envelope); err != nil || envelope.ID != issueMade || !strings.Contains(string(envelope.Payload), "X-1") {
		t.Errorf("body = %s, want the payload and the event id", sink.got[0].Body)
	}

	// A failure schedules the next attempt with backoff; the log shows both.
	sink.status = http.StatusInternalServerError
	failing := uuid.New()
	_ = hooks.Handle(context.Background(), events.Event{ID: failing, OrgID: ws.orgID, Topic: events.TopicIssueCreated, Payload: json.RawMessage(`{}`)})
	_, _ = hooks.SendDue(context.Background())
	deliveries, _ := hooks.Deliveries(ws.ctx, made.ID, 0)
	var attempts []webhook.Delivery
	for _, d := range deliveries {
		if d.EventID == failing {
			attempts = append(attempts, d)
		}
	}
	if len(attempts) != 2 {
		t.Fatalf("deliveries for the failing event = %+v, want the failed one and the scheduled retry", attempts)
	}
	var failed, retry webhook.Delivery
	for _, d := range attempts {
		if d.Attempt == 1 {
			failed = d
		} else {
			retry = d
		}
	}
	if failed.Status == nil || *failed.Status != 500 || !strings.Contains(failed.Error, "500") || failed.NextAttemptAt != nil {
		t.Errorf("failed attempt = %+v", failed)
	}
	if retry.Attempt != 2 || retry.NextAttemptAt == nil || retry.DeliveredAt != nil {
		t.Errorf("retry = %+v", retry)
	}

	// Redelivery by hand goes now, and succeeds once the endpoint is back.
	sink.status = http.StatusNoContent
	again, _, err := hooks.Redeliver(ws.ctx, failed.ID)
	if err != nil || again.DeliveredAt == nil || again.Attempt != 3 {
		t.Fatalf("redeliver = %+v %v", again, err)
	}
	ping, _, err := hooks.Test(ws.ctx, made.ID)
	if err != nil || ping.DeliveredAt == nil || ping.Topic != webhook.TopicPing {
		t.Fatalf("test = %+v %v", ping, err)
	}

	// A rule's send webhook action lands in the same log.
	rules := automation.NewService(h.cluster, ws.issues, label.NewService(h.cluster), log).WithWebhooks(hooks)
	rule, _, err := rules.Create(ws.ctx, ws.project.Key, automation.Input{
		Name: "Tell chat", Trigger: automation.Trigger{Kind: automation.TriggerIssueTransitioned},
		Actions: []automation.Action{{Kind: automation.ActionSendWebhook, EndpointID: &made.ID}},
	}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	moved := ws.newIssue(t, "Moving")
	viaRule := uuid.New()
	raw, _ := json.Marshal(map[string]any{"key": moved.Key, "actorId": ws.actor.UserID})
	_ = rules.Handle(context.Background(), events.Event{ID: viaRule, OrgID: ws.orgID, Topic: events.TopicIssueTransitioned, Payload: raw})
	runs, _ := rules.Runs(ws.ctx, rule.ID, 0)
	if len(runs) != 1 || runs[0].Outcome != automation.OutcomeDone {
		t.Fatalf("rule runs = %+v", runs)
	}
	_, _ = hooks.SendDue(context.Background())
	deliveries, _ = hooks.Deliveries(ws.ctx, made.ID, 0)
	found := false
	for _, d := range deliveries {
		if d.EventID == viaRule && d.DeliveredAt != nil && d.Topic == events.TopicIssueTransitioned {
			found = true
		}
	}
	if !found {
		t.Errorf("the rule's webhook was not delivered: %+v", deliveries)
	}
}

// Every operation over the API, the way the pages use them: a project's
// rules, the organization's, an incoming hook, and the webhook pages.
func TestAutomationOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "automationapi")
	project := want(t, c.post("/api/v1/projects", map[string]any{"name": "Rules", "key": "RUL" + strings.ToUpper(uuid.New().String()[:3]), "template": "kanban"}), http.StatusCreated, "project")
	key := obj(t, project, "project")["key"].(string)

	catalog := want(t, c.get("/api/v1/automation/catalog"), http.StatusOK, "catalog")
	if len(list(t, catalog, "catalog")) < 10 || len(list(t, catalog, "topics")) < 5 {
		t.Errorf("catalog = %s", catalog.Raw)
	}
	want(t, c.post("/api/v1/projects/"+key+"/automation/rules", map[string]any{"name": "", "trigger": map[string]any{"kind": "issue.created"}, "actions": []any{}}), http.StatusUnprocessableEntity, "a nameless rule")
	made := want(t, c.post("/api/v1/projects/"+key+"/automation/rules", map[string]any{
		"name": "Label new work", "trigger": map[string]any{"kind": "issue.created"},
		"actions": []any{map[string]any{"kind": "add_label", "value": "new"}},
	}), http.StatusCreated, "a rule")
	ruleID := idOf(t, made, "rule")
	want(t, c.get("/api/v1/projects/"+key+"/automation/rules"), http.StatusOK, "the project's rules")
	want(t, c.get("/api/v1/automation/rules/"+ruleID), http.StatusOK, "one rule")
	want(t, c.patch("/api/v1/automation/rules/"+ruleID, map[string]any{
		"name": "Label new work", "enabled": false, "trigger": map[string]any{"kind": "issue.created"},
		"actions": []any{map[string]any{"kind": "add_label", "value": "fresh"}},
	}), http.StatusOK, "editing")

	issueMade := want(t, c.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "Run me"}), http.StatusCreated, "issue")
	issueKey := obj(t, issueMade, "issue")["key"].(string)
	ran := want(t, c.post("/api/v1/automation/rules/"+ruleID+"/run", map[string]any{"issueKey": issueKey}), http.StatusOK, "run now")
	if obj(t, ran, "run")["outcome"] != "done" {
		t.Errorf("run = %s", ran.Raw)
	}
	if runs := list(t, want(t, c.get("/api/v1/automation/rules/"+ruleID+"/runs"), http.StatusOK, "runs"), "runs"); len(runs) != 1 {
		t.Errorf("runs = %d", len(runs))
	}

	// The organization's rules, and an incoming hook that creates an issue.
	orgRule := want(t, c.post("/api/v1/automation/rules", map[string]any{
		"name": "From outside", "trigger": map[string]any{"kind": "incoming"},
		"actions": []any{map[string]any{"kind": "create_issue", "text": "An alert came in"}},
	}), http.StatusCreated, "an org rule")
	want(t, c.get("/api/v1/automation/rules"), http.StatusOK, "the org's rules")
	token := obj(t, orgRule, "rule")["trigger"].(map[string]any)["token"].(string)
	anonymous := api.client(t)
	want(t, anonymous.post("/api/v1/automation/hooks/"+token, map[string]any{"source": "pager"}), http.StatusAccepted, "an incoming call")
	want(t, anonymous.post("/api/v1/automation/hooks/not-a-token", map[string]any{}), http.StatusNotFound, "a wrong address")
	want(t, c.delete("/api/v1/automation/rules/"+idOf(t, orgRule, "rule")), http.StatusNoContent, "deleting the org rule")
	want(t, c.delete("/api/v1/automation/rules/"+ruleID), http.StatusNoContent, "deleting the rule")

	// Webhooks.
	sink := &receiver{status: http.StatusOK}
	server := httptest.NewServer(sink)
	defer server.Close()
	want(t, c.post("/api/v1/webhooks", map[string]any{"name": "Chat", "url": "nowhere", "topics": []string{"*"}}), http.StatusUnprocessableEntity, "a bad address")
	hook := want(t, c.post("/api/v1/webhooks", map[string]any{"name": "Chat", "url": server.URL, "topics": []string{"*"}}), http.StatusCreated, "a webhook")
	hookID := idOf(t, hook, "webhook")
	secret := obj(t, hook, "webhook")["secret"].(string)
	want(t, c.get("/api/v1/webhooks"), http.StatusOK, "webhooks")
	want(t, c.patch("/api/v1/webhooks/"+hookID, map[string]any{"name": "Chat room", "url": server.URL, "topics": []string{"issue.created"}}), http.StatusOK, "editing")
	rotated := want(t, c.post("/api/v1/webhooks/"+hookID+"/rotate-secret", nil), http.StatusOK, "rotating")
	if obj(t, rotated, "webhook")["secret"] == secret {
		t.Error("rotating kept the secret")
	}
	tested := want(t, c.post("/api/v1/webhooks/"+hookID+"/test", nil), http.StatusOK, "testing")
	deliveryID := idOf(t, tested, "delivery")
	if len(sink.got) != 1 || sink.got[0].Event != "ping" {
		t.Errorf("received = %+v, want a ping", sink.got)
	}
	want(t, c.get("/api/v1/webhooks/"+hookID+"/deliveries"), http.StatusOK, "deliveries")
	want(t, c.post("/api/v1/webhooks/"+hookID+"/deliveries/"+deliveryID+"/redeliver", nil), http.StatusOK, "redelivering")
	want(t, c.post("/api/v1/webhooks/"+hookID+"/deliveries/"+uuid.New().String()+"/redeliver", nil), http.StatusNotFound, "redelivering nothing")
	want(t, c.delete("/api/v1/webhooks/"+hookID), http.StatusNoContent, "deleting")
	want(t, c.get("/api/v1/webhooks/"+hookID+"/deliveries"), http.StatusNotFound, "a gone webhook")
}
