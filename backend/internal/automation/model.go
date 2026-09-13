// Package automation runs rules: when something happens, if it looks like
// this, do these. Rules are run by the worker from the event stream as the
// organization's automation account, once per rule per event.
package automation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/workflow"
)

// Trigger kinds: the event topics a rule may watch, a clock, an incoming
// call, and a hand on the Run button.
const (
	TriggerIssueCreated      = events.TopicIssueCreated
	TriggerIssueTransitioned = events.TopicIssueTransitioned
	TriggerIssueUpdated      = events.TopicIssueUpdated
	TriggerCommentAdded      = events.TopicCommentAdded
	TriggerSLABreached       = events.TopicSLABreached
	TriggerScheduled         = "scheduled"
	TriggerIncoming          = "incoming"
)

// Condition kinds.
const (
	ConditionNQL        = "nql"
	ConditionFieldEqual = "field_equals"
	ConditionActorRole  = "actor_role"
)

// Action kinds.
const (
	ActionSetField       = "set_field"
	ActionTransition     = "transition"
	ActionAssign         = "assign"
	ActionAddLabel       = "add_label"
	ActionAddComment     = "add_comment"
	ActionCreateSubissue = "create_subissue"
	ActionCreateIssue    = "create_issue"
	ActionSendWebhook    = "send_webhook"
	ActionSendMail       = "send_mail"
)

// Schedule units.
const (
	EveryHours = "hours"
	EveryDay   = "daily"
	EveryWeek  = "weekly"
)

// Outcomes of a run.
const (
	OutcomeRunning = "running"
	OutcomeDone    = "done"
	OutcomeSkipped = "skipped"
	OutcomeFailed  = "failed"
	OutcomeCapped  = "capped"
)

const (
	// DefaultHourlyCap is how many runs a rule gets an hour unless it says otherwise.
	DefaultHourlyCap = 100
	// MaxScheduledIssues bounds what one scheduled run touches.
	MaxScheduledIssues = 200
	// DefaultRuns is how many log rows a page shows.
	DefaultRuns = 50
	// maxActions keeps a rule from being a program.
	maxActions = 10
)

// Schedule is when a clock-driven rule runs: every N hours, daily at a time,
// or weekly on a day at a time. Times are in the server's clock.
type Schedule struct {
	Unit    string `json:"unit"`
	Every   int    `json:"every,omitempty"`
	At      string `json:"at,omitempty"`
	Weekday int    `json:"weekday,omitempty"`
}

// Trigger is what starts a rule.
type Trigger struct {
	Kind string `json:"kind"`
	// Field narrows an update trigger to one field changing.
	Field string `json:"field,omitempty"`
	// Query is what a scheduled rule runs over: every issue it matches.
	Query    string    `json:"query,omitempty"`
	Schedule *Schedule `json:"schedule,omitempty"`
	// Token is the path of an incoming hook; whoever knows it may call it.
	Token string `json:"token,omitempty"`
}

// Condition is a test on the issue or the actor.
type Condition struct {
	Kind  string   `json:"kind"`
	Query string   `json:"query,omitempty"`
	Field string   `json:"field,omitempty"`
	Value string   `json:"value,omitempty"`
	Roles []string `json:"roles,omitempty"`
}

// Action is one thing a rule does. Which fields matter depends on the kind.
type Action struct {
	Kind string `json:"kind"`
	// Field and Value for set_field; Value alone for assign, add_label, transition.
	Field string `json:"field,omitempty"`
	Value string `json:"value,omitempty"`
	// Text for a comment, a summary, or a mail's body; Subject for the mail; To for its address.
	Text    string `json:"text,omitempty"`
	Subject string `json:"subject,omitempty"`
	To      string `json:"to,omitempty"`
	// EndpointID names the webhook to post to.
	EndpointID *uuid.UUID `json:"endpointId,omitempty"`
}

// Rule is one rule as the page and the runner see it.
type Rule struct {
	ID             uuid.UUID   `json:"id"`
	ProjectID      *uuid.UUID  `json:"projectId,omitempty"`
	ProjectKey     string      `json:"projectKey,omitempty"`
	Name           string      `json:"name"`
	Enabled        bool        `json:"enabled"`
	Trigger        Trigger     `json:"trigger"`
	Conditions     []Condition `json:"conditions"`
	Actions        []Action    `json:"actions"`
	AllowOwnEvents bool        `json:"allowOwnEvents"`
	HourlyCap      int         `json:"hourlyCap"`
	CreatedAt      time.Time   `json:"createdAt"`
	UpdatedAt      time.Time   `json:"updatedAt"`
	LastRunAt      *time.Time  `json:"lastRunAt,omitempty"`
}

// Input is what makes or changes a rule.
type Input struct {
	Name           string      `json:"name"`
	Enabled        *bool       `json:"enabled,omitempty"`
	Trigger        Trigger     `json:"trigger"`
	Conditions     []Condition `json:"conditions"`
	Actions        []Action    `json:"actions"`
	AllowOwnEvents bool        `json:"allowOwnEvents"`
	HourlyCap      int         `json:"hourlyCap,omitempty"`
}

// Run is one time a rule was considered for an event.
type Run struct {
	ID         uuid.UUID      `json:"id"`
	RuleID     uuid.UUID      `json:"ruleId"`
	EventID    uuid.UUID      `json:"eventId"`
	IssueKey   string         `json:"issueKey,omitempty"`
	StartedAt  time.Time      `json:"startedAt"`
	FinishedAt *time.Time     `json:"finishedAt,omitempty"`
	Outcome    string         `json:"outcome"`
	Reason     string         `json:"reason,omitempty"`
	Actions    []ActionResult `json:"actions"`
}

// ActionResult is what one action did, in a sentence.
type ActionResult struct {
	Kind string `json:"kind"`
	Note string `json:"note"`
	OK   bool   `json:"ok"`
}

// Descriptor tells the editor what a trigger, condition or action takes.
type Descriptor struct {
	Kind        string            `json:"kind"`
	Type        string            `json:"type"`
	Label       string            `json:"label"`
	Description string            `json:"description"`
	Options     []workflow.Option `json:"options"`
	// NeedsIssue says the action does nothing without an issue to act on.
	NeedsIssue bool `json:"needsIssue,omitempty"`
}

// Catalog is what the editor offers, in the order it shows them.
func Catalog() []Descriptor {
	text := func(name, label string, required bool) workflow.Option {
		return workflow.Option{Name: name, Label: label, Kind: workflow.OptionText, Required: required}
	}
	out := []Descriptor{
		{Kind: "trigger", Type: TriggerIssueCreated, Label: "An issue is created", Description: "Runs for every new issue."},
		{Kind: "trigger", Type: TriggerIssueTransitioned, Label: "An issue moves", Description: "Runs when an issue takes a transition."},
		{Kind: "trigger", Type: TriggerIssueUpdated, Label: "A field changes", Description: "Runs when an issue is edited; name a field to watch only that one.", Options: []workflow.Option{text("field", "Field (optional)", false)}},
		{Kind: "trigger", Type: TriggerCommentAdded, Label: "A comment is added", Description: "Runs for every comment, notes included."},
		{Kind: "trigger", Type: TriggerSLABreached, Label: "A request misses its goal", Description: "Runs when a service desk clock runs out."},
		{Kind: "trigger", Type: TriggerScheduled, Label: "On a schedule", Description: "Runs every N hours, daily or weekly, over every issue a query matches.", Options: []workflow.Option{text("query", "Query", true)}},
		{Kind: "trigger", Type: TriggerIncoming, Label: "An incoming call", Description: "Runs when something posts to the rule's address; the body may name an issueKey."},

		{Kind: "condition", Type: ConditionNQL, Label: "The issue matches a query", Description: "Only when the issue is in the query's result.", Options: []workflow.Option{text("query", "Query", true)}},
		{Kind: "condition", Type: ConditionFieldEqual, Label: "A field equals", Description: "status, priority, type, assignee (an id, or none) or label.", Options: []workflow.Option{text("field", "Field", true), text("value", "Value", true)}},
		{Kind: "condition", Type: ConditionActorRole, Label: "The actor holds a role", Description: "Only when whoever caused the event holds one of these.", Options: []workflow.Option{{Name: "roles", Label: "Roles", Kind: workflow.OptionRoles, Required: true, Choices: workflow.OrgRoles}}},

		{Kind: "action", Type: ActionSetField, Label: "Set a field", Description: "priority (lowest to highest), summary, or due as +N days.", NeedsIssue: true, Options: []workflow.Option{text("field", "Field", true), text("value", "Value", true)}},
		{Kind: "action", Type: ActionTransition, Label: "Take a transition", Description: "By the transition's name; refused when the workflow does not offer it.", NeedsIssue: true, Options: []workflow.Option{text("value", "Transition", true)}},
		{Kind: "action", Type: ActionAssign, Label: "Assign", Description: "To reporter, to the actor, to none, or to a person by id.", NeedsIssue: true, Options: []workflow.Option{text("value", "Who", true)}},
		{Kind: "action", Type: ActionAddLabel, Label: "Add a label", Description: "Coined if it is new.", NeedsIssue: true, Options: []workflow.Option{text("value", "Label", true)}},
		{Kind: "action", Type: ActionAddComment, Label: "Add a comment", Description: "{{issue.key}}, {{issue.summary}} and {{actor.name}} are filled in.", NeedsIssue: true, Options: []workflow.Option{text("text", "Comment", true)}},
		{Kind: "action", Type: ActionCreateSubissue, Label: "Create a subtask", Description: "Under the issue, with this summary.", NeedsIssue: true, Options: []workflow.Option{text("text", "Summary", true)}},
		{Kind: "action", Type: ActionCreateIssue, Label: "Create an issue", Description: "In the rule's project, with this summary; what an incoming call usually does.", Options: []workflow.Option{text("text", "Summary", true)}},
		{Kind: "action", Type: ActionSendWebhook, Label: "Send a webhook", Description: "Posts the event to one of the organization's endpoints.", Options: []workflow.Option{text("endpointId", "Endpoint", true)}},
		{Kind: "action", Type: ActionSendMail, Label: "Send a mail", Description: "To assignee, reporter, watchers, or an address.", Options: []workflow.Option{text("to", "To", true), text("subject", "Subject", true), text("text", "Body", true)}},
	}
	for i := range out {
		if out[i].Options == nil {
			out[i].Options = []workflow.Option{}
		}
		for j := range out[i].Options {
			if out[i].Options[j].Choices == nil {
				out[i].Options[j].Choices = []string{}
			}
		}
	}
	return out
}

var (
	triggerKinds   = map[string]bool{TriggerIssueCreated: true, TriggerIssueTransitioned: true, TriggerIssueUpdated: true, TriggerCommentAdded: true, TriggerSLABreached: true, TriggerScheduled: true, TriggerIncoming: true}
	conditionKinds = map[string]bool{ConditionNQL: true, ConditionFieldEqual: true, ConditionActorRole: true}
	actionKinds    = map[string]bool{ActionSetField: true, ActionTransition: true, ActionAssign: true, ActionAddLabel: true, ActionAddComment: true, ActionCreateSubissue: true, ActionCreateIssue: true, ActionSendWebhook: true, ActionSendMail: true}
)

// Validate refuses a rule the runner could not run.
func (in Input) Validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("a rule needs a name")
	}
	if !triggerKinds[in.Trigger.Kind] {
		return fmt.Errorf("%q is not a trigger", in.Trigger.Kind)
	}
	if in.Trigger.Kind == TriggerScheduled {
		if strings.TrimSpace(in.Trigger.Query) == "" {
			return errors.New("a scheduled rule needs a query to run over")
		}
		if in.Trigger.Schedule == nil {
			return errors.New("a scheduled rule needs a schedule")
		}
		if err := in.Trigger.Schedule.validate(); err != nil {
			return err
		}
	}
	for _, c := range in.Conditions {
		if !conditionKinds[c.Kind] {
			return fmt.Errorf("%q is not a condition", c.Kind)
		}
		switch c.Kind {
		case ConditionNQL:
			if strings.TrimSpace(c.Query) == "" {
				return errors.New("a query condition needs a query")
			}
		case ConditionFieldEqual:
			if c.Field == "" {
				return errors.New("a field condition needs a field")
			}
		case ConditionActorRole:
			if len(c.Roles) == 0 {
				return errors.New("a role condition needs at least one role")
			}
		}
	}
	if len(in.Actions) == 0 {
		return errors.New("a rule needs at least one action")
	}
	if len(in.Actions) > maxActions {
		return fmt.Errorf("a rule can do at most %d things", maxActions)
	}
	for _, a := range in.Actions {
		if !actionKinds[a.Kind] {
			return fmt.Errorf("%q is not an action", a.Kind)
		}
		switch a.Kind {
		case ActionSetField:
			if a.Field == "" || a.Value == "" {
				return errors.New("set a field needs the field and the value")
			}
		case ActionTransition, ActionAssign, ActionAddLabel:
			if strings.TrimSpace(a.Value) == "" {
				return fmt.Errorf("%s needs a value", strings.ReplaceAll(a.Kind, "_", " "))
			}
		case ActionAddComment, ActionCreateSubissue, ActionCreateIssue:
			if strings.TrimSpace(a.Text) == "" {
				return fmt.Errorf("%s needs its text", strings.ReplaceAll(a.Kind, "_", " "))
			}
		case ActionSendWebhook:
			if a.EndpointID == nil {
				return errors.New("send a webhook needs an endpoint")
			}
		case ActionSendMail:
			if a.To == "" || a.Subject == "" {
				return errors.New("send a mail needs an address and a subject")
			}
		}
	}
	if in.HourlyCap < 0 {
		return errors.New("the hourly cap cannot be negative")
	}
	return nil
}

func (s Schedule) validate() error {
	switch s.Unit {
	case EveryHours:
		if s.Every <= 0 {
			return errors.New("every N hours needs N to be at least 1")
		}
	case EveryDay, EveryWeek:
		if _, err := time.Parse("15:04", s.At); err != nil {
			return errors.New("the time has to be written as HH:MM")
		}
		if s.Unit == EveryWeek && (s.Weekday < 0 || s.Weekday > 6) {
			return errors.New("the weekday is 0 for Sunday to 6 for Saturday")
		}
	default:
		return errors.New("a schedule is every N hours, daily or weekly")
	}
	return nil
}

// Due says whether a schedule fires between last and now. A rule that never
// ran fires at its first due moment after it was made.
func (s Schedule) Due(last, now time.Time) bool {
	switch s.Unit {
	case EveryHours:
		return now.Sub(last) >= time.Duration(s.Every)*time.Hour
	case EveryDay, EveryWeek:
		at, err := time.Parse("15:04", s.At)
		if err != nil {
			return false
		}
		// The most recent moment the clock said At; due when it is after last.
		moment := time.Date(now.Year(), now.Month(), now.Day(), at.Hour(), at.Minute(), 0, 0, now.Location())
		if moment.After(now) {
			moment = moment.AddDate(0, 0, -1)
		}
		if s.Unit == EveryWeek {
			for moment.Weekday() != time.Weekday(s.Weekday) {
				moment = moment.AddDate(0, 0, -1)
			}
		}
		return moment.After(last)
	}
	return false
}

func (r *Rule) marshal() (trigger, conditions, actions []byte) {
	trigger, _ = json.Marshal(r.Trigger)
	if r.Conditions == nil {
		r.Conditions = []Condition{}
	}
	conditions, _ = json.Marshal(r.Conditions)
	actions, _ = json.Marshal(r.Actions)
	return
}
