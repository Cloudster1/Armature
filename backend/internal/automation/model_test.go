package automation

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
)

func TestValidateRefusesWhatCannotRun(t *testing.T) {
	good := Input{Name: "Label moved work", Trigger: Trigger{Kind: TriggerIssueTransitioned}, Actions: []Action{{Kind: ActionAddLabel, Value: "moved"}}}
	if err := good.Validate(); err != nil {
		t.Fatalf("a plain rule was refused: %v", err)
	}
	bad := []Input{
		{Name: "", Trigger: good.Trigger, Actions: good.Actions},
		{Name: "x", Trigger: Trigger{Kind: "issue.exploded"}, Actions: good.Actions},
		{Name: "x", Trigger: good.Trigger},
		{Name: "x", Trigger: good.Trigger, Actions: []Action{{Kind: ActionTransition}}},
		{Name: "x", Trigger: Trigger{Kind: TriggerScheduled, Query: "statusCategory != done"}, Actions: good.Actions},
		{Name: "x", Trigger: Trigger{Kind: TriggerScheduled, Query: "x", Schedule: &Schedule{Unit: EveryDay, At: "25:00"}}, Actions: good.Actions},
		{Name: "x", Trigger: good.Trigger, Conditions: []Condition{{Kind: ConditionActorRole}}, Actions: good.Actions},
		{Name: "x", Trigger: good.Trigger, Actions: []Action{{Kind: ActionSendWebhook}}},
	}
	for i, in := range bad {
		if err := in.Validate(); err == nil {
			t.Errorf("case %d was accepted: %+v", i, in)
		}
	}
}

func TestScheduleDue(t *testing.T) {
	now := time.Date(2026, 9, 7, 9, 5, 0, 0, time.UTC)
	hourly := Schedule{Unit: EveryHours, Every: 2}
	if hourly.Due(now.Add(-time.Hour), now) || !hourly.Due(now.Add(-3*time.Hour), now) {
		t.Error("every 2 hours: due after 3, not after 1")
	}
	daily := Schedule{Unit: EveryDay, At: "09:00"}
	if !daily.Due(now.Add(-24*time.Hour), now) || daily.Due(now.Add(-time.Minute), now) {
		t.Error("daily at 09:00: due once after 09:00, not again at 09:05")
	}
	early := time.Date(2026, 9, 7, 8, 0, 0, 0, time.UTC)
	if daily.Due(early.Add(-time.Hour), early) {
		t.Error("daily at 09:00 is not due at 08:00 when it ran at 07:00 and yesterday's moment is past")
	}
	weekly := Schedule{Unit: EveryWeek, At: "09:00", Weekday: int(time.Monday)}
	if !weekly.Due(now.AddDate(0, 0, -7), now) || weekly.Due(now.Add(-time.Minute), now) {
		t.Errorf("weekly on Monday 09:00 (today is %s)", now.Weekday())
	}
}

func TestFieldEqualsAndSetField(t *testing.T) {
	who := uuid.New()
	i := &issue.Issue{Priority: issue.PriorityHigh, Assignee: &issue.UserRef{ID: who, Name: "Ada"}, Labels: []issue.LabelRef{{Name: "Urgent"}}}
	i.Status.Name = "In Progress"
	i.Type.Name = "Bug"
	if !fieldEquals(i, "status", "in progress") || !fieldEquals(i, "priority", "HIGH") || !fieldEquals(i, "type", "bug") || !fieldEquals(i, "assignee", who.String()) || !fieldEquals(i, "label", "urgent") {
		t.Error("loose matches failed")
	}
	if fieldEquals(i, "assignee", "none") || fieldEquals(i, "label", "calm") || fieldEquals(i, "colour", "red") {
		t.Error("a non-match matched")
	}
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	in, note, err := setFieldInput(Action{Field: "due", Value: "+3"}, now)
	if err != nil || (*in.DueDate).Format("2006-01-02") != "2026-09-10" || note == "" {
		t.Errorf("due +3: %+v %q %v", in, note, err)
	}
	if _, _, err := setFieldInput(Action{Field: "priority", Value: "urgent"}, now); err == nil {
		t.Error("an unknown priority was accepted")
	}
	if _, _, err := setFieldInput(Action{Field: "assignee", Value: "x"}, now); err == nil {
		t.Error("assignee is not set_field's to set")
	}
}
