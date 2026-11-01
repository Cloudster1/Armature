//go:build integration

package test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/automation"
	"github.com/armature/armature/backend/internal/events"
)

// A run that names no issue used to test no condition at all, so a rule kept
// to some roles ran for anybody who could start it by hand or by its address.
func TestARunWithoutAnIssueStillAsksItsConditions(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "unissued")
	svc := ws.automation(h)

	var member uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM app_user WHERE email = $1`, h.person(t, "plain member", auth.RoleMember)).Scan(&member); err != nil {
		t.Fatal(err)
	}

	summary := h.name(t, "made by an admin's call")
	rule, _, err := svc.Create(ws.ctx, ws.project.Key, automation.Input{
		Name: "Only an administrator's call", Trigger: automation.Trigger{Kind: automation.TriggerIncoming},
		Conditions: []automation.Condition{{Kind: automation.ConditionActorRole, Roles: []string{"owner", "admin"}}},
		Actions:    []automation.Action{{Kind: automation.ActionCreateIssue, Text: summary}},
	}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	made := func() int {
		t.Helper()
		var n int
		if err := h.super.QueryRow(context.Background(), `
			SELECT count(*) FROM issue WHERE project_id = $1 AND summary = $2`, ws.project.ID, summary).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	run, _, err := svc.RunNow(ws.ctx, rule.ID, "", member)
	if err != nil {
		t.Fatal(err)
	}
	if run.Outcome != automation.OutcomeSkipped || !strings.Contains(run.Reason, "the actor is member") || len(run.Actions) != 0 {
		t.Fatalf("a member's run = %+v, want skipped for the actor's role with no action", run)
	}
	if n := made(); n != 0 {
		t.Fatalf("%d issues made by a run that was skipped", n)
	}

	// A call on the rule's address has no actor, so it holds no role either.
	raw, _ := json.Marshal(map[string]any{"ruleId": rule.ID, "key": "", "body": json.RawMessage(`{}`)})
	if err := svc.Handle(context.Background(), events.Event{ID: uuid.New(), Topic: events.TopicAutomationIncoming, Payload: raw}); err != nil {
		t.Fatal(err)
	}
	runs, err := svc.Runs(ws.ctx, rule.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].Outcome != automation.OutcomeSkipped || !strings.Contains(runs[0].Reason, "the actor is nobody") || len(runs[0].Actions) != 0 {
		t.Fatalf("runs after an incoming call = %+v, want it skipped for the actor", runs)
	}
	if n := made(); n != 0 {
		t.Fatalf("%d issues made by an incoming call that was skipped", n)
	}

	// Somebody who holds the role runs it.
	run, _, err = svc.RunNow(ws.ctx, rule.ID, "", ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Outcome != automation.OutcomeDone || len(run.Actions) != 1 || !run.Actions[0].OK {
		t.Fatalf("the owner's run = %+v, want done with the issue made", run)
	}
	if n := made(); n != 1 {
		t.Fatalf("%d issues made by the owner's run, want 1", n)
	}

	// A condition about the issue cannot hold without one, and says so.
	queried, _, err := svc.Create(ws.ctx, ws.project.Key, automation.Input{
		Name: "Only open work", Trigger: automation.Trigger{Kind: automation.TriggerIncoming},
		Conditions: []automation.Condition{{Kind: automation.ConditionNQL, Query: "statusCategory = todo"}},
		Actions:    []automation.Action{{Kind: automation.ActionCreateIssue, Text: summary}},
	}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err = svc.RunNow(ws.ctx, queried.ID, "", ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Outcome != automation.OutcomeSkipped || !strings.Contains(run.Reason, "needs an issue") || len(run.Actions) != 0 {
		t.Fatalf("a run without an issue for an issue's condition = %+v, want skipped saying it needs one", run)
	}
	if n := made(); n != 1 {
		t.Fatalf("%d issues made after the skipped run, want still 1", n)
	}
}
