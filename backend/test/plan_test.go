//go:build integration

package test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/plan"
	"github.com/armature/armature/backend/internal/team"
)

func mustDate(t *testing.T, s string) *time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return &d
}

// schedule sets both ends of an issue's range and fails the test if it cannot.
func (h *harness) schedule(t *testing.T, ws *workspace, key, start, due string) *issue.Issue {
	t.Helper()
	updated, _, err := ws.issues.Schedule(ws.ctx, key, issue.ScheduleInput{
		Start: ptr(mustDate(t, start)),
		Due:   ptr(mustDate(t, due)),
	}, ws.actor)
	if err != nil {
		t.Fatalf("schedule %s: %v", key, err)
	}
	return updated
}

func TestScheduling(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "sched")

	t.Run("both ends are set together and recorded", func(t *testing.T) {
		tr := h.buildTree(t, ws)
		updated := h.schedule(t, ws, tr.story.Key, "2026-03-10", "2026-03-20")

		if updated.StartDate == nil || updated.StartDate.Format("2006-01-02") != "2026-03-10" {
			t.Errorf("start = %v, want 2026-03-10", updated.StartDate)
		}
		if updated.DueDate == nil || updated.DueDate.Format("2006-01-02") != "2026-03-20" {
			t.Errorf("due = %v, want 2026-03-20", updated.DueDate)
		}

		history, err := ws.issues.History(ws.ctx, tr.story.Key)
		if err != nil {
			t.Fatal(err)
		}
		var fields []string
		for _, entry := range history {
			for _, c := range entry.Changes {
				fields = append(fields, c.Field)
			}
		}
		if !containsAll(fields, "startDate", "dueDate") {
			t.Errorf("changelog fields = %v, want both ends recorded", fields)
		}
	})

	t.Run("a range cannot end before it starts", func(t *testing.T) {
		tr := h.buildTree(t, ws)
		_, _, err := ws.issues.Schedule(ws.ctx, tr.story.Key, issue.ScheduleInput{
			Start: ptr(mustDate(t, "2026-03-20")),
			Due:   ptr(mustDate(t, "2026-03-10")),
		}, ws.actor)
		if !errors.Is(err, issue.ErrBackwardsRange) {
			t.Errorf("error = %v, want ErrBackwardsRange", err)
		}
	})

	t.Run("moving one end is checked against the end already set", func(t *testing.T) {
		tr := h.buildTree(t, ws)
		h.schedule(t, ws, tr.story.Key, "2026-03-10", "2026-03-20")

		if _, _, err := ws.issues.Schedule(ws.ctx, tr.story.Key, issue.ScheduleInput{
			Start: ptr(mustDate(t, "2026-04-01")),
		}, ws.actor); !errors.Is(err, issue.ErrBackwardsRange) {
			t.Errorf("error = %v, want ErrBackwardsRange", err)
		}
	})

	t.Run("either end can be cleared", func(t *testing.T) {
		tr := h.buildTree(t, ws)
		h.schedule(t, ws, tr.story.Key, "2026-03-10", "2026-03-20")

		var none *time.Time
		cleared, _, err := ws.issues.Schedule(ws.ctx, tr.story.Key, issue.ScheduleInput{
			Start: &none, Due: &none,
		}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		if cleared.StartDate != nil || cleared.DueDate != nil {
			t.Errorf("dates = %v to %v, want none", cleared.StartDate, cleared.DueDate)
		}
	})

	t.Run("rescheduling to the same days records nothing", func(t *testing.T) {
		tr := h.buildTree(t, ws)
		h.schedule(t, ws, tr.story.Key, "2026-03-10", "2026-03-20")

		before, err := ws.issues.History(ws.ctx, tr.story.Key)
		if err != nil {
			t.Fatal(err)
		}
		h.schedule(t, ws, tr.story.Key, "2026-03-10", "2026-03-20")
		after, err := ws.issues.History(ws.ctx, tr.story.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before) {
			t.Errorf("history grew from %d to %d for a move that did not happen", len(before), len(after))
		}
	})

	t.Run("the database refuses a backwards range written directly", func(t *testing.T) {
		tr := h.buildTree(t, ws)
		_, err := h.super.Exec(context.Background(),
			`UPDATE issue SET start_date = '2026-05-01', due_date = '2026-04-01' WHERE id = $1`, tr.story.ID)
		if err == nil {
			t.Fatal("the constraint let a range end before it starts")
		}
	})
}

func TestIssueLinks(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "linking")

	t.Run("a blocking link is readable from both ends", func(t *testing.T) {
		first := h.buildTree(t, ws)
		second := h.buildTree(t, ws)

		created, _, err := ws.issues.AddLink(ws.ctx, first.story.Key, issue.LinkInput{
			TypeName: issue.LinkTypeBlocks, TargetKey: second.story.Key,
		}, ws.actor)
		if err != nil {
			t.Fatalf("link: %v", err)
		}
		if created.Direction != "outward" || created.Issue.Key != second.story.Key {
			t.Errorf("link = %+v, want an outward link to %s", created, second.story.Key)
		}
		if created.Phrase != "blocks" {
			t.Errorf("phrase = %q, want the outward wording", created.Phrase)
		}

		back, err := ws.issues.Links(ws.ctx, second.story.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(back) != 1 || back[0].Direction != "inward" || back[0].Phrase != "is blocked by" {
			t.Errorf("the other end reads %+v, want the inward wording", back)
		}
	})

	t.Run("the same pair cannot be linked twice the same way", func(t *testing.T) {
		first := h.buildTree(t, ws)
		second := h.buildTree(t, ws)
		in := issue.LinkInput{TypeName: issue.LinkTypeBlocks, TargetKey: second.story.Key}

		if _, _, err := ws.issues.AddLink(ws.ctx, first.story.Key, in, ws.actor); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ws.issues.AddLink(ws.ctx, first.story.Key, in, ws.actor); err == nil {
			t.Error("the same link was accepted twice")
		}

		// And not from the other end either: "A blocks B" already says it.
		reverse := issue.LinkInput{TypeName: issue.LinkTypeBlocks, TargetKey: first.story.Key}
		if _, _, err := ws.issues.AddLink(ws.ctx, second.story.Key, reverse, ws.actor); err == nil {
			t.Error("the reverse of an existing link was accepted")
		}
	})

	t.Run("an issue cannot be linked to itself", func(t *testing.T) {
		tr := h.buildTree(t, ws)
		_, _, err := ws.issues.AddLink(ws.ctx, tr.story.Key, issue.LinkInput{
			TypeName: issue.LinkTypeBlocks, TargetKey: tr.story.Key,
		}, ws.actor)
		if !errors.Is(err, issue.ErrLinkToSelf) {
			t.Errorf("error = %v, want ErrLinkToSelf", err)
		}
	})

	t.Run("a link can be removed from either end", func(t *testing.T) {
		first := h.buildTree(t, ws)
		second := h.buildTree(t, ws)
		created, _, err := ws.issues.AddLink(ws.ctx, first.story.Key, issue.LinkInput{
			TypeName: issue.LinkTypeBlocks, TargetKey: second.story.Key,
		}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := ws.issues.RemoveLink(ws.ctx, first.story.Key, created.ID); err != nil {
			t.Fatalf("remove: %v", err)
		}
		links, err := ws.issues.Links(ws.ctx, second.story.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(links) != 0 {
			t.Errorf("links = %v, want none", links)
		}
	})

	t.Run("a link to another organization's issue is not found", func(t *testing.T) {
		other := h.newWorkspace(t, "linkbeta")
		otherTree := h.buildTree(t, other)
		mine := h.buildTree(t, ws)

		_, _, err := ws.issues.AddLink(ws.ctx, mine.story.Key, issue.LinkInput{
			TypeName: issue.LinkTypeBlocks, TargetKey: otherTree.story.Key,
		}, ws.actor)
		if !errors.Is(err, issue.ErrNotFound) {
			t.Errorf("error = %v, want ErrNotFound", err)
		}
	})
}

func TestPlanForProject(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "planning")
	plans := ws.plans

	tr := h.buildTree(t, ws)
	h.schedule(t, ws, tr.story.Key, "2026-03-10", "2026-03-20")
	h.schedule(t, ws, tr.subtask.Key, "2026-03-12", "2026-03-25")

	t.Run("a parent with no dates spans its children", func(t *testing.T) {
		got, err := plans.ForProject(ws.ctx, ws.project.Key)
		if err != nil {
			t.Fatal(err)
		}

		epic := findItem(t, got.Items, tr.epic.Key)
		if !epic.Derived {
			t.Error("the epic's range is not marked as derived")
		}
		if epic.Start == nil || epic.Start.Format("2006-01-02") != "2026-03-10" {
			t.Errorf("epic starts %v, want its earliest child's start", epic.Start)
		}
		// The roll-up stops at the story, which has dates of its own. Its
		// subtask running past them is a disagreement to report, not a reason
		// to quietly stretch the dates somebody chose.
		if epic.Due == nil || epic.Due.Format("2006-01-02") != "2026-03-20" {
			t.Errorf("epic ends %v, want its child's own end", epic.Due)
		}

		var reported bool
		for _, w := range got.Warnings {
			if w.IssueKey == tr.subtask.Key && w.Kind == plan.WarnOutsideParent {
				reported = true
			}
		}
		if !reported {
			t.Errorf("warnings = %+v, want the subtask reported as outside its parent", got.Warnings)
		}
	})

	t.Run("a derived parent passes its grandchildren upwards", func(t *testing.T) {
		ws := h.newWorkspace(t, "deriving")
		plans := ws.plans
		tr := h.buildTree(t, ws)
		h.schedule(t, ws, tr.subtask.Key, "2026-05-01", "2026-05-09")

		got, err := plans.ForProject(ws.ctx, ws.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		// Nothing between the subtask and the initiative has dates of its own,
		// so the range reaches all the way to the top.
		initiative := findItem(t, got.Items, tr.initiative.Key)
		if initiative.Start == nil || initiative.Start.Format("2006-01-02") != "2026-05-01" {
			t.Errorf("initiative starts %v, want the subtask's start", initiative.Start)
		}
		if initiative.Due == nil || initiative.Due.Format("2006-01-02") != "2026-05-09" {
			t.Errorf("initiative ends %v, want the subtask's end", initiative.Due)
		}
	})

	t.Run("the window covers the work", func(t *testing.T) {
		got, err := plans.ForProject(ws.ctx, ws.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		if !got.From.Before(*mustDate(t, "2026-03-10")) || !got.To.After(*mustDate(t, "2026-03-25")) {
			t.Errorf("window %s to %s does not cover the scheduled work", got.From, got.To)
		}
	})

	t.Run("a dependency scheduled too early is reported, not refused", func(t *testing.T) {
		blocked, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
			ProjectKey: ws.project.Key,
			Summary:    "waits on the story",
			TypeID:     h.issueTypeID(t, ws, bootstrap.TypeStory),
			ParentKey:  tr.epic.Key,
		}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}
		h.schedule(t, ws, blocked.Key, "2026-03-15", "2026-03-30")

		if _, _, err := ws.issues.AddLink(ws.ctx, tr.story.Key, issue.LinkInput{
			TypeName: issue.LinkTypeBlocks, TargetKey: blocked.Key,
		}, ws.actor); err != nil {
			t.Fatal(err)
		}

		got, err := plans.ForProject(ws.ctx, ws.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Dependencies) != 1 {
			t.Fatalf("dependencies = %v, want one", got.Dependencies)
		}

		var found *plan.Warning
		for i, w := range got.Warnings {
			if w.IssueKey == blocked.Key && w.Kind == plan.WarnBlockedTooEarly {
				found = &got.Warnings[i]
			}
		}
		if found == nil {
			t.Fatalf("warnings = %+v, want one about %s starting too early", got.Warnings, blocked.Key)
		}
		if !strings.Contains(found.Message, tr.story.Key) {
			t.Errorf("message %q does not name the blocker", found.Message)
		}

		// The schedule was still written: a plan is a draft, not a promise.
		reread, err := ws.issues.ByKey(ws.ctx, blocked.Key)
		if err != nil {
			t.Fatal(err)
		}
		if reread.StartDate == nil {
			t.Error("the warning prevented the schedule from being saved")
		}
	})

	t.Run("unscheduled work keeps its row and is counted", func(t *testing.T) {
		before, err := plans.ForProject(ws.ctx, ws.project.Key)
		if err != nil {
			t.Fatal(err)
		}

		loose, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
			ProjectKey: ws.project.Key,
			Summary:    "nobody has planned this",
			TypeID:     h.issueTypeID(t, ws, bootstrap.TypeTask),
		}, ws.actor)
		if err != nil {
			t.Fatal(err)
		}

		after, err := plans.ForProject(ws.ctx, ws.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		if after.Unscheduled != before.Unscheduled+1 {
			t.Errorf("unscheduled = %d, was %d before adding one", after.Unscheduled, before.Unscheduled)
		}

		// It still has a row: a plan that hides what is not scheduled hides
		// exactly the work somebody needs to schedule.
		item := findItem(t, after.Items, loose.Key)
		if item.Scheduled() {
			t.Errorf("%s was given dates nobody asked for", loose.Key)
		}
	})

	t.Run("another organization's plan is empty, not readable", func(t *testing.T) {
		other := h.newWorkspace(t, "planbeta")
		otherPlans := other.plans

		got, err := otherPlans.ForProject(other.ctx, ws.project.Key)
		if err == nil && len(got.Items) > 0 {
			t.Errorf("beta read %d rows of alpha's plan", len(got.Items))
		}
	})
}

func findItem(t *testing.T, items []plan.Item, key string) plan.Item {
	t.Helper()
	for _, item := range plan.Flatten(items) {
		if item.Issue.Key == key {
			return item
		}
	}
	t.Fatalf("%s is not in the plan", key)
	return plan.Item{}
}

func containsAll(haystack []string, needles ...string) bool {
	for _, needle := range needles {
		found := false
		for _, straw := range haystack {
			if straw == needle {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// The plan reads the load per team: what is scheduled into each week against
// what the team said it could take, and the work no team carries beside it.
func TestThePlanReadsTheLoadPerTeam(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "loaded")
	alpha := ws.newTeam(t, "Alpha")
	ten := 10.0
	if _, _, err := ws.teams.Update(ws.ctx, alpha.ID, team.UpdateInput{WeeklyCapacity: &ten, SetCapacity: true}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}

	big := ws.newIssue(t, "eight points for Alpha")
	small := ws.newIssue(t, "six points for Alpha")
	loose := ws.newIssue(t, "three points for nobody")
	ws.hand(t, big.Key, &alpha.ID)
	ws.hand(t, small.Key, &alpha.ID)
	for key, pts := range map[string]float64{big.Key: 8, small.Key: 6, loose.Key: 3} {
		p := pts
		if _, _, err := ws.issues.SetEstimate(ws.ctx, key, &p, ws.actor); err != nil {
			t.Fatal(err)
		}
		// Monday to Friday of one week, so the whole estimate lands in it.
		h.schedule(t, ws, key, "2026-09-07", "2026-09-11")
	}

	got := ws.planOf(t)
	if len(got.Load.Rows) != 3 || got.Load.Rows[0].Team != "Alpha" || got.Load.Rows[1].Kind != plan.LoadUnassigned || got.Load.Rows[2].Kind != plan.LoadTotal {
		t.Fatalf("rows = %+v, want Alpha, Unassigned, Total", got.Load.Rows)
	}
	weekOf := func(row plan.TeamLoad) plan.LoadWeek {
		t.Helper()
		for _, w := range row.Weeks {
			if w.Start.Format("2006-01-02") == "2026-09-07" {
				return w
			}
		}
		t.Fatalf("%s has no week of 7 September: %+v", row.Team, row.Weeks)
		return plan.LoadWeek{}
	}
	if w := weekOf(got.Load.Rows[0]); w.Load != 14 || w.Capacity == nil || *w.Capacity != 10 || w.Issues != 2 {
		t.Errorf("Alpha's week = %+v, want 14 of 10 from two issues", w)
	}
	if w := weekOf(got.Load.Rows[1]); w.Load != 3 {
		t.Errorf("Unassigned's week = %+v, want the loose 3", w)
	}
	if w := weekOf(got.Load.Rows[2]); w.Load != 17 || w.Capacity == nil || *w.Capacity != 10 {
		t.Errorf("Total's week = %+v, want 17 against 10", w)
	}

	var warned bool
	for _, w := range got.Warnings {
		if w.Kind == plan.WarnOverLoad && w.Team == "Alpha" && w.IssueKey == "" {
			warned = true
		}
	}
	if !warned {
		t.Errorf("warnings = %+v, want Alpha over its week", got.Warnings)
	}
}
