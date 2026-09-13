package workflow

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// builder assembles a workflow for tests without repeating uuid plumbing.
type builder struct {
	wf    Workflow
	steps map[string]Step
}

func newBuilder(name string) *builder {
	return &builder{wf: Workflow{ID: uuid.New(), Name: name}, steps: map[string]Step{}}
}

func (b *builder) step(name string, category StatusCategory, initial bool) *builder {
	s := Step{
		ID:        uuid.New(),
		Status:    Status{ID: uuid.New(), Name: name, Category: category},
		IsInitial: initial,
		Position:  len(b.wf.Steps),
	}
	b.steps[name] = s
	b.wf.Steps = append(b.wf.Steps, s)
	return b
}

// transition adds an edge. An empty from means the transition is global.
func (b *builder) transition(name, from, to string, rules ...Rule) *builder {
	t := Transition{
		ID:       uuid.New(),
		Name:     name,
		ToStepID: b.steps[to].ID,
		Position: len(b.wf.Transitions),
		Rules:    rules,
	}
	if from != "" {
		id := b.steps[from].ID
		t.FromStepID = &id
	}
	b.wf.Transitions = append(b.wf.Transitions, t)
	return b
}

func (b *builder) build() *Workflow { return &b.wf }

func (b *builder) statusID(name string) uuid.UUID { return b.steps[name].Status.ID }

func (b *builder) transitionID(name string) uuid.UUID {
	for _, t := range b.wf.Transitions {
		if t.Name == name {
			return t.ID
		}
	}
	panic("no transition named " + name)
}

func rule(kind RuleKind, ruleType string, config string) Rule {
	r := Rule{ID: uuid.New(), Kind: kind, Type: ruleType}
	if config != "" {
		r.Config = json.RawMessage(config)
	}
	return r
}

// simple is the shape almost every project starts with.
func simple() *builder {
	return newBuilder("Simple").
		step("To Do", CategoryTodo, true).
		step("In Progress", CategoryInProgress, false).
		step("Done", CategoryDone, false).
		transition("Start", "To Do", "In Progress").
		transition("Finish", "In Progress", "Done").
		transition("Reopen", "Done", "To Do")
}

func names(ts []Transition) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Name
	}
	return out
}

func TestAvailableFollowsTheGraph(t *testing.T) {
	b := simple()
	engine := NewEngine(nil)
	actor := Actor{UserID: uuid.New(), OrgRole: "member"}

	tests := []struct {
		from string
		want []string
	}{
		{from: "To Do", want: []string{"Start"}},
		{from: "In Progress", want: []string{"Finish"}},
		{from: "Done", want: []string{"Reopen"}},
	}
	for _, tt := range tests {
		got, err := engine.Available(b.build(), Subject{StatusID: b.statusID(tt.from)}, actor)
		if err != nil {
			t.Fatalf("from %s: %v", tt.from, err)
		}
		if strings.Join(names(got), ",") != strings.Join(tt.want, ",") {
			t.Errorf("from %s: available = %v, want %v", tt.from, names(got), tt.want)
		}
	}
}

func TestGlobalTransitionIsAvailableEverywhereButWhereItLeads(t *testing.T) {
	b := simple().transition("Close", "", "Done")
	engine := NewEngine(nil)
	actor := Actor{UserID: uuid.New(), OrgRole: "member"}

	for _, from := range []string{"To Do", "In Progress"} {
		got, err := engine.Available(b.build(), Subject{StatusID: b.statusID(from)}, actor)
		if err != nil {
			t.Fatal(err)
		}
		if !containsName(got, "Close") {
			t.Errorf("from %s: the global transition is missing from %v", from, names(got))
		}
	}

	// Offering "Close" to an issue that is already closed is noise.
	got, err := engine.Available(b.build(), Subject{StatusID: b.statusID("Done")}, actor)
	if err != nil {
		t.Fatal(err)
	}
	if containsName(got, "Close") {
		t.Errorf("the global transition was offered from the status it leads to: %v", names(got))
	}
}

// A workflow scheme can be changed underneath issues that are already in a
// status the new workflow does not contain. They must not become stuck with no
// way out at all.
func TestIssueInAnUnknownStatusStillGetsGlobalTransitions(t *testing.T) {
	b := simple().transition("Close", "", "Done")
	engine := NewEngine(nil)

	got, err := engine.Available(b.build(), Subject{StatusID: uuid.New()}, Actor{OrgRole: "member"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names(got), ",") != "Close" {
		t.Errorf("available = %v, want only the global transition", names(got))
	}
}

func TestConditionsHideRatherThanFail(t *testing.T) {
	b := simple()
	b.wf.Transitions[1].Rules = []Rule{rule(KindCondition, ConditionOrgRole, `{"roles":["admin","owner"]}`)}
	engine := NewEngine(nil)
	subject := Subject{StatusID: b.statusID("In Progress")}

	member, err := engine.Available(b.build(), subject, Actor{UserID: uuid.New(), OrgRole: "member"})
	if err != nil {
		t.Fatal(err)
	}
	if containsName(member, "Finish") {
		t.Error("a member was offered a transition restricted to admins")
	}

	admin, err := engine.Available(b.build(), subject, Actor{UserID: uuid.New(), OrgRole: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if !containsName(admin, "Finish") {
		t.Error("an admin was not offered the transition")
	}
}

func TestAssigneeAndReporterConditions(t *testing.T) {
	me := uuid.New()
	someoneElse := uuid.New()

	b := simple()
	b.wf.Transitions[0].Rules = []Rule{rule(KindCondition, ConditionIsAssignee, "")}
	engine := NewEngine(nil)
	statusID := b.statusID("To Do")

	cases := []struct {
		name     string
		assignee *uuid.UUID
		want     bool
	}{
		{name: "the assignee may", assignee: &me, want: true},
		{name: "somebody else may not", assignee: &someoneElse, want: false},
		{name: "unassigned means nobody may", assignee: nil, want: false},
	}
	for _, tc := range cases {
		got, err := engine.Available(b.build(), Subject{StatusID: statusID, AssigneeID: tc.assignee}, Actor{UserID: me})
		if err != nil {
			t.Fatal(err)
		}
		if containsName(got, "Start") != tc.want {
			t.Errorf("%s: available = %v, want offered=%v", tc.name, names(got), tc.want)
		}
	}
}

func TestEvaluateRejectsATransitionThatDoesNotLeaveTheCurrentStatus(t *testing.T) {
	b := simple()
	engine := NewEngine(nil)

	// "Finish" leaves In Progress, so it is not available from To Do.
	_, err := engine.Evaluate(b.build(), b.transitionID("Finish"),
		Subject{StatusID: b.statusID("To Do")}, Actor{OrgRole: "member"}, Input{})
	if !errors.Is(err, ErrTransitionNotAvailable) {
		t.Fatalf("error = %v, want ErrTransitionNotAvailable", err)
	}
}

func TestEvaluateRejectsAnUnknownTransition(t *testing.T) {
	engine := NewEngine(nil)
	_, err := engine.Evaluate(simple().build(), uuid.New(), Subject{}, Actor{}, Input{})
	if !errors.Is(err, ErrTransitionNotFound) {
		t.Fatalf("error = %v, want ErrTransitionNotFound", err)
	}
}

func TestEvaluateRunsValidators(t *testing.T) {
	b := simple()
	b.wf.Transitions[0].Rules = []Rule{rule(KindValidator, ValidatorCommentRequired, "")}
	engine := NewEngine(nil)
	subject := Subject{StatusID: b.statusID("To Do")}

	_, err := engine.Evaluate(b.build(), b.transitionID("Start"), subject, Actor{}, Input{})
	var ruleErr *RuleError
	if !errors.As(err, &ruleErr) {
		t.Fatalf("error = %v, want a RuleError", err)
	}
	if ruleErr.Field != "comment" {
		t.Errorf("field = %q, want comment", ruleErr.Field)
	}

	if _, err := engine.Evaluate(b.build(), b.transitionID("Start"), subject, Actor{}, Input{Comment: "starting"}); err != nil {
		t.Errorf("a satisfied validator still failed: %v", err)
	}
}

func TestAssigneeRequiredAcceptsAssignmentInTheSameStep(t *testing.T) {
	b := simple()
	b.wf.Transitions[0].Rules = []Rule{rule(KindValidator, ValidatorAssigneeRequired, "")}
	engine := NewEngine(nil)
	subject := Subject{StatusID: b.statusID("To Do")}
	me := uuid.New()

	if _, err := engine.Evaluate(b.build(), b.transitionID("Start"), subject, Actor{UserID: me}, Input{}); err == nil {
		t.Error("an unassigned issue passed a transition requiring an assignee")
	}

	// Assigning as part of the transition is the normal way to pick work up.
	if _, err := engine.Evaluate(b.build(), b.transitionID("Start"), subject, Actor{UserID: me}, Input{Assignee: &me}); err != nil {
		t.Errorf("assigning during the transition was rejected: %v", err)
	}

	// An issue that is already assigned needs nothing extra.
	assigned := Subject{StatusID: b.statusID("To Do"), AssigneeID: &me}
	if _, err := engine.Evaluate(b.build(), b.transitionID("Start"), assigned, Actor{UserID: me}, Input{}); err != nil {
		t.Errorf("an already assigned issue was rejected: %v", err)
	}
}

func TestPostFunctionsProduceEffects(t *testing.T) {
	me := uuid.New()

	t.Run("assign to the person taking the transition", func(t *testing.T) {
		b := simple()
		b.wf.Transitions[0].Rules = []Rule{rule(KindPostFunction, PostAssignToActor, "")}
		decision, err := NewEngine(nil).Evaluate(b.build(), b.transitionID("Start"),
			Subject{StatusID: b.statusID("To Do")}, Actor{UserID: me}, Input{})
		if err != nil {
			t.Fatal(err)
		}
		if decision.Effect.Assignee == nil || *decision.Effect.Assignee != me {
			t.Errorf("assignee = %v, want the actor", decision.Effect.Assignee)
		}
	})

	t.Run("mark resolved on the way to done", func(t *testing.T) {
		b := simple()
		b.wf.Transitions[1].Rules = []Rule{rule(KindPostFunction, PostSetResolved, "")}
		decision, err := NewEngine(nil).Evaluate(b.build(), b.transitionID("Finish"),
			Subject{StatusID: b.statusID("In Progress")}, Actor{UserID: me}, Input{})
		if err != nil {
			t.Fatal(err)
		}
		if decision.Effect.Resolved == nil || !*decision.Effect.Resolved {
			t.Errorf("resolved = %v, want true", decision.Effect.Resolved)
		}
		if decision.ToStatus.Category != CategoryDone {
			t.Errorf("target category = %q, want done", decision.ToStatus.Category)
		}
	})

	t.Run("clear the resolution on reopening", func(t *testing.T) {
		b := simple()
		b.wf.Transitions[2].Rules = []Rule{
			rule(KindPostFunction, PostClearResolved, ""),
			rule(KindPostFunction, PostClearAssignee, ""),
		}
		decision, err := NewEngine(nil).Evaluate(b.build(), b.transitionID("Reopen"),
			Subject{StatusID: b.statusID("Done"), AssigneeID: &me}, Actor{UserID: me}, Input{})
		if err != nil {
			t.Fatal(err)
		}
		if decision.Effect.Resolved == nil || *decision.Effect.Resolved {
			t.Errorf("resolved = %v, want false", decision.Effect.Resolved)
		}
		if !decision.Effect.ClearAssign {
			t.Error("the assignee was not cleared")
		}
	})

	// Post-functions run after the user's own input, so a workflow that always
	// assigns to the actor wins over whatever the form said.
	t.Run("a post-function overrides the user's input", func(t *testing.T) {
		other := uuid.New()
		b := simple()
		b.wf.Transitions[0].Rules = []Rule{rule(KindPostFunction, PostAssignToActor, "")}
		decision, err := NewEngine(nil).Evaluate(b.build(), b.transitionID("Start"),
			Subject{StatusID: b.statusID("To Do")}, Actor{UserID: me}, Input{Assignee: &other})
		if err != nil {
			t.Fatal(err)
		}
		if *decision.Effect.Assignee != me {
			t.Error("the user's assignee survived a post-function that should have overridden it")
		}
	})

	t.Run("the user's input is honoured when no post-function interferes", func(t *testing.T) {
		other := uuid.New()
		b := simple()
		decision, err := NewEngine(nil).Evaluate(b.build(), b.transitionID("Start"),
			Subject{StatusID: b.statusID("To Do")}, Actor{UserID: me}, Input{Assignee: &other})
		if err != nil {
			t.Fatal(err)
		}
		if decision.Effect.Assignee == nil || *decision.Effect.Assignee != other {
			t.Errorf("assignee = %v, want the one the user chose", decision.Effect.Assignee)
		}
	})

	t.Run("the nil uuid means unassign", func(t *testing.T) {
		b := simple()
		decision, err := NewEngine(nil).Evaluate(b.build(), b.transitionID("Start"),
			Subject{StatusID: b.statusID("To Do"), AssigneeID: &me}, Actor{UserID: me},
			Input{Assignee: &uuid.Nil})
		if err != nil {
			t.Fatal(err)
		}
		if !decision.Effect.ClearAssign {
			t.Error("passing the nil uuid did not unassign")
		}
	})
}

func TestRulesRunInConfiguredOrder(t *testing.T) {
	b := simple()
	first := rule(KindPostFunction, PostAddComment, `{"text":"first"}`)
	first.Position = 0
	second := rule(KindPostFunction, PostAddComment, `{"text":"second"}`)
	second.Position = 1
	// Deliberately out of order in the slice; position decides.
	b.wf.Transitions[0].Rules = []Rule{second, first}

	decision, err := NewEngine(nil).Evaluate(b.build(), b.transitionID("Start"),
		Subject{StatusID: b.statusID("To Do")}, Actor{}, Input{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(decision.Effect.Comments, ",") != "first,second" {
		t.Errorf("comments = %v, want them in position order", decision.Effect.Comments)
	}
}

func TestValidate(t *testing.T) {
	engine := NewEngine(nil)

	t.Run("accepts a coherent workflow", func(t *testing.T) {
		if err := engine.Validate(simple().build()); err != nil {
			t.Errorf("a valid workflow was rejected: %v", err)
		}
	})

	t.Run("rejects a workflow with no starting status", func(t *testing.T) {
		w := newBuilder("Headless").
			step("A", CategoryTodo, false).
			step("B", CategoryDone, false).
			transition("Go", "A", "B").
			build()
		if err := engine.Validate(w); err == nil || !strings.Contains(err.Error(), "start") {
			t.Errorf("error = %v, want a complaint about the starting status", err)
		}
	})

	t.Run("rejects two starting statuses", func(t *testing.T) {
		w := newBuilder("Twoheaded").
			step("A", CategoryTodo, true).
			step("B", CategoryDone, true).
			transition("Go", "A", "B").
			build()
		if err := engine.Validate(w); err == nil || !strings.Contains(err.Error(), "more than one") {
			t.Errorf("error = %v, want a complaint about two starting statuses", err)
		}
	})

	// A status nothing leads to is a promise the workflow cannot keep.
	t.Run("rejects an unreachable status", func(t *testing.T) {
		w := newBuilder("Marooned").
			step("A", CategoryTodo, true).
			step("B", CategoryInProgress, false).
			step("Island", CategoryDone, false).
			transition("Go", "A", "B").
			build()
		if err := engine.Validate(w); err == nil || !strings.Contains(err.Error(), "Island") {
			t.Errorf("error = %v, want a complaint that Island is unreachable", err)
		}
	})

	t.Run("rejects an unknown rule type", func(t *testing.T) {
		b := simple()
		b.wf.Transitions[0].Rules = []Rule{rule(KindCondition, "condition.invented", "")}
		if err := engine.Validate(b.build()); err == nil || !strings.Contains(err.Error(), "condition.invented") {
			t.Errorf("error = %v, want a complaint about the unknown rule", err)
		}
	})

	// A rule whose configuration does not parse must be caught while the
	// administrator is editing, not when a user tries to move an issue.
	t.Run("rejects a rule with unusable configuration", func(t *testing.T) {
		b := simple()
		b.wf.Transitions[0].Rules = []Rule{rule(KindCondition, ConditionOrgRole, `{"roles":[]}`)}
		if err := engine.Validate(b.build()); err == nil {
			t.Error("a role condition naming no roles was accepted")
		}
	})

	t.Run("rejects a transition leading outside the workflow", func(t *testing.T) {
		b := simple()
		b.wf.Transitions[0].ToStepID = uuid.New()
		if err := engine.Validate(b.build()); err == nil || !strings.Contains(err.Error(), "outside") {
			t.Errorf("error = %v, want a complaint about leaving the workflow", err)
		}
	})
}

func TestRegistryListsItsRuleTypes(t *testing.T) {
	r := NewDefaultRegistry()
	for _, kind := range []RuleKind{KindCondition, KindValidator, KindPostFunction} {
		types := r.RuleTypes(kind)
		if len(types) == 0 {
			t.Errorf("no %s types are registered", kind)
		}
		for _, name := range types {
			if !r.Known(kind, name) {
				t.Errorf("%s %q is listed but not known", kind, name)
			}
		}
	}
	if r.Known(KindCondition, "condition.does.not.exist") {
		t.Error("an unregistered rule reported itself as known")
	}
}

func containsName(ts []Transition, name string) bool {
	for _, t := range ts {
		if t.Name == name {
			return true
		}
	}
	return false
}
