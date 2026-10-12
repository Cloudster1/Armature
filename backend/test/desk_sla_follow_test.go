//go:build integration

package test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/template"
)

// A clock that was just started has run for a moment at most.
const freshClockSeconds = 5

// deskBeside makes another service desk watched by the same desk service, so
// one issue moving between the two is seen by one observer.
func (ws *workspace) deskBeside(t *testing.T, h *harness, d *desk.Service, name string) *project.Project {
	t.Helper()
	templates := template.NewService(h.cluster, ws.projects, ws.admin).WithDesk(d)
	p, _, err := templates.Create(ws.ctx, "service-desk", project.CreateInput{
		Name: name, Key: strings.ToUpper(name[:min(len(name), 3)] + uuid.New().String()[:3]),
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create the second desk: %v", err)
	}
	return p
}

func reprioritise(t *testing.T, ws *workspace, key string, to issue.Priority) {
	t.Helper()
	if _, _, err := ws.issues.Update(ws.ctx, key, issue.UpdateInput{Priority: &to}, ws.actor); err != nil {
		t.Fatalf("set %s to %s: %v", key, to, err)
	}
}

func moveTo(t *testing.T, ws *workspace, key, projectKey string, statusID uuid.UUID) *issue.Issue {
	t.Helper()
	moved, _, err := ws.issues.Move(ws.ctx, key, issue.MoveInput{ProjectKey: projectKey, StatusID: &statusID}, ws.actor)
	if err != nil {
		t.Fatalf("move %s to %s: %v", key, projectKey, err)
	}
	return moved
}

// timerRows counts an issue's clocks straight in the table, past every reader.
func timerRows(t *testing.T, h *harness, issueID uuid.UUID) int {
	t.Helper()
	var n int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM sla_timer WHERE issue_id = $1`, issueID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func policyOf(t *testing.T, d *desk.Service, ws *workspace, projectKey string, metric desk.Metric) desk.Policy {
	t.Helper()
	policies, err := d.Policies(ws.ctx, projectKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range policies {
		if p.Metric == metric {
			return p
		}
	}
	t.Fatalf("%s has no %s policy", projectKey, metric)
	return desk.Policy{}
}

func TestAPriorityChangeMovesTheGoalAndKeepsTheTimeRun(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newWorkspace(t, "reprioritised")
	d, p := ws.aDesk(t, h, "Priority desk")
	customer := ws.customerOf(t, h, "urgent")
	types, _ := d.RequestTypes(ws.ctx, p.Key)

	// Medium: a first answer in 8 hours, resolved in 3 days.
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "Everything is down"}, customer)
	if err != nil {
		t.Fatal(err)
	}
	ageTimer(t, h, ws, raised.Key, desk.FirstResponse, 90)
	ageTimer(t, h, ws, raised.Key, desk.Resolution, 120)
	startedAt := timerFor(t, mustTimers(t, d, ws, raised.Key), desk.Resolution).StartedAt

	t.Run("raising it to highest moves both goals and keeps the time run", func(t *testing.T) {
		reprioritise(t, ws, raised.Key, issue.PriorityHighest)
		timers := mustTimers(t, d, ws, raised.Key)
		resolution := timerFor(t, timers, desk.Resolution)
		if resolution.GoalMinutes != 8*60 {
			t.Errorf("resolution goal = %d minutes, want the highest priority's 8 hours", resolution.GoalMinutes)
		}
		if resolution.ElapsedSeconds < 120*60 || resolution.ElapsedSeconds > 120*60+60 {
			t.Errorf("resolution clock has run %d seconds, want the two hours it had", resolution.ElapsedSeconds)
		}
		if resolution.RunningSince == nil || resolution.Paused || !resolution.StartedAt.Equal(startedAt) {
			t.Errorf("resolution clock = %+v, want the same clock still running", resolution)
		}
		if got := timerFor(t, timers, desk.FirstResponse).GoalMinutes; got != 60 {
			t.Errorf("first response goal = %d minutes, want the highest priority's hour", got)
		}
	})

	t.Run("a goal the time run already used up is a breach at once", func(t *testing.T) {
		first := timerFor(t, mustTimers(t, d, ws, raised.Key), desk.FirstResponse)
		if first.BreachedAt == nil || !first.Breached {
			t.Errorf("first response clock = %+v, want 90 minutes against an hour recorded as breached", first)
		}
		var breaches int
		if err := h.super.QueryRow(context.Background(),
			`SELECT count(*) FROM outbox_event WHERE org_id = $1 AND topic = $2`, ws.orgID, events.TopicSLABreached).Scan(&breaches); err != nil {
			t.Fatal(err)
		}
		if breaches != 1 {
			t.Errorf("%d breach events, want the one", breaches)
		}
	})

	t.Run("lowering it again lengthens the goals and the breach stays recorded", func(t *testing.T) {
		reprioritise(t, ws, raised.Key, issue.PriorityLowest)
		timers := mustTimers(t, d, ws, raised.Key)
		first := timerFor(t, timers, desk.FirstResponse)
		if first.GoalMinutes != 48*60 || first.BreachedAt == nil {
			t.Errorf("first response clock = %+v, want 48 hours with the breach kept", first)
		}
		resolution := timerFor(t, timers, desk.Resolution)
		if resolution.GoalMinutes != 10*24*60 || resolution.ElapsedSeconds < 120*60 {
			t.Errorf("resolution clock = %+v, want 10 days with the two hours kept", resolution)
		}
	})

	t.Run("a finished clock keeps the goal it finished against", func(t *testing.T) {
		if _, _, err := ws.issues.AddComment(ws.ctx, raised.Key, issue.TextDocument("Looking now."), ws.actor); err != nil {
			t.Fatal(err)
		}
		reprioritise(t, ws, raised.Key, issue.PriorityHigh)
		timers := mustTimers(t, d, ws, raised.Key)
		if first := timerFor(t, timers, desk.FirstResponse); first.CompletedAt == nil || first.GoalMinutes != 48*60 {
			t.Errorf("first response clock = %+v, want it completed against the 48 hours it had", first)
		}
		if got := timerFor(t, timers, desk.Resolution).GoalMinutes; got != 24*60 {
			t.Errorf("resolution goal = %d minutes, want the high priority's day", got)
		}
	})

	t.Run("a paused clock takes the new goal and stays paused", func(t *testing.T) {
		ws.move(t, h, raised.Key, "Start work")
		ws.move(t, h, raised.Key, "Wait for customer")
		reprioritise(t, ws, raised.Key, issue.PriorityMedium)
		resolution := timerFor(t, mustTimers(t, d, ws, raised.Key), desk.Resolution)
		if resolution.GoalMinutes != 3*24*60 || !resolution.Paused || resolution.ElapsedSeconds < 120*60 {
			t.Errorf("resolution clock while waiting = %+v, want paused at 3 days with the time kept", resolution)
		}
	})

	t.Run("a priority the policy does not measure takes its open clock off", func(t *testing.T) {
		policy := policyOf(t, d, ws, p.Key, desk.Resolution)
		goals := map[issue.Priority]int{}
		for priority, minutes := range policy.Goals {
			if priority != issue.PriorityLowest {
				goals[priority] = minutes
			}
		}
		if _, _, err := d.UpdatePolicy(ws.ctx, policy.ID, desk.PolicyInput{Goals: goals}, ws.actor.UserID); err != nil {
			t.Fatal(err)
		}
		reprioritise(t, ws, raised.Key, issue.PriorityLowest)
		timers := mustTimers(t, d, ws, raised.Key)
		if len(timers) != 1 || timers[0].Metric != desk.FirstResponse {
			t.Fatalf("timers = %+v, want only the finished first response", timers)
		}

		reprioritise(t, ws, raised.Key, issue.PriorityHigh)
		resolution := timerFor(t, mustTimers(t, d, ws, raised.Key), desk.Resolution)
		if resolution.GoalMinutes != 24*60 || !resolution.Paused || resolution.CompletedAt != nil {
			t.Errorf("resolution clock = %+v, want a day's goal, paused as the status says", resolution)
		}
	})
}

func TestMovingARequestOutOfTheDeskStopsItsClocks(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newWorkspace(t, "moved-out")
	d, p := ws.aDesk(t, h, "Leaving desk")
	customer := ws.customerOf(t, h, "leaver")
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Report a problem").ID, Summary: "This is really a bug"}, customer)
	if err != nil {
		t.Fatal(err)
	}
	// Nearly out of time: left where it was, the watch would breach it.
	ageTimer(t, h, ws, raised.Key, desk.FirstResponse, 4*60-1)

	moved := moveTo(t, ws, raised.Key, ws.project.Key, h.statusID(t, ws, "To Do"))

	if timers := mustTimers(t, d, ws, moved.Key); len(timers) != 0 {
		t.Errorf("timers after leaving the desk = %+v, want none", timers)
	}
	if n := timerRows(t, h, moved.ID); n != 0 {
		t.Errorf("%d clock rows are left behind for the watch to breach, want none", n)
	}

	t.Run("its workflow there starts nothing", func(t *testing.T) {
		ws.move(t, h, moved.Key, "Start progress")
		if n := timerRows(t, h, moved.ID); n != 0 {
			t.Errorf("a transition outside the desk left %d clocks, want none", n)
		}
	})

	t.Run("an edit there starts nothing either", func(t *testing.T) {
		reprioritise(t, ws, moved.Key, issue.PriorityHighest)
		if n := timerRows(t, h, moved.ID); n != 0 {
			t.Errorf("a priority change outside the desk left %d clocks, want none", n)
		}
	})
}

func TestMovingAnIssueIntoTheDeskStartsItsClocks(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newWorkspace(t, "moved-in")
	d, p := ws.aDesk(t, h, "Arrival desk")
	waitingForSupport := h.statusID(t, ws, "Waiting for support")

	inProject := func(t *testing.T, summary string) *issue.Issue {
		t.Helper()
		made, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: ws.project.Key, Summary: summary, Priority: issue.PriorityHigh}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		if n := timerRows(t, h, made.ID); n != 0 {
			t.Fatalf("an issue outside any desk has %d clocks", n)
		}
		return made
	}

	t.Run("both clocks start from nothing at its priority's goals", func(t *testing.T) {
		moved := moveTo(t, ws, inProject(t, "A customer is waiting on this").Key, p.Key, waitingForSupport)
		timers := mustTimers(t, d, ws, moved.Key)
		if len(timers) != 2 {
			t.Fatalf("timers after arriving = %+v, want two", timers)
		}
		for _, tm := range timers {
			if tm.RunningSince == nil || tm.Paused || tm.ElapsedSeconds > freshClockSeconds {
				t.Errorf("%s clock = %+v, want just started and running", tm.Metric, tm)
			}
		}
		if got := timerFor(t, timers, desk.FirstResponse).GoalMinutes; got != 4*60 {
			t.Errorf("first response goal = %d minutes, want the high priority's 4 hours", got)
		}
		if got := timerFor(t, timers, desk.Resolution).GoalMinutes; got != 24*60 {
			t.Errorf("resolution goal = %d minutes, want the high priority's day", got)
		}
	})

	t.Run("arriving in a pause status starts them paused", func(t *testing.T) {
		moved := moveTo(t, ws, inProject(t, "Asked them for logs").Key, p.Key, h.statusID(t, ws, "Waiting for customer"))
		timers := mustTimers(t, d, ws, moved.Key)
		if len(timers) != 2 {
			t.Fatalf("timers = %+v, want two", timers)
		}
		for _, tm := range timers {
			if !tm.Paused || tm.RunningSince != nil {
				t.Errorf("%s clock = %+v, want paused while the desk waits on the customer", tm.Metric, tm)
			}
		}
	})

	t.Run("arriving resolved starts no clock", func(t *testing.T) {
		moved := moveTo(t, ws, inProject(t, "Already sorted").Key, p.Key, h.statusID(t, ws, "Resolved"))
		if n := timerRows(t, h, moved.ID); n != 0 {
			t.Errorf("a resolved arrival has %d clocks, want none", n)
		}
	})

	t.Run("moving from one desk to another trades its clocks for the new desk's", func(t *testing.T) {
		other := ws.deskBeside(t, h, d, "Second desk")
		made, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: p.Key, Summary: "Belongs to the other team", Priority: issue.PriorityHigh}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		ageTimer(t, h, ws, made.Key, desk.Resolution, 60)
		moved := moveTo(t, ws, made.Key, other.Key, waitingForSupport)

		timers := mustTimers(t, d, ws, moved.Key)
		if len(timers) != 2 {
			t.Fatalf("timers = %+v, want the second desk's two", timers)
		}
		theirs := map[uuid.UUID]bool{
			policyOf(t, d, ws, other.Key, desk.FirstResponse).ID: true,
			policyOf(t, d, ws, other.Key, desk.Resolution).ID:    true,
		}
		for _, tm := range timers {
			if !theirs[tm.PolicyID] || tm.ElapsedSeconds > freshClockSeconds {
				t.Errorf("%s clock = %+v, want a fresh one under the second desk's policy", tm.Metric, tm)
			}
		}
		if n := timerRows(t, h, moved.ID); n != 2 {
			t.Errorf("%d clock rows, want only the second desk's two", n)
		}
	})
}
