package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

var (
	todo     = uuid.New()
	progress = uuid.New()
	done     = uuid.New()
	stranger = uuid.New()

	statusNames = map[uuid.UUID]string{
		todo: "To Do", progress: "In Progress", done: "Done",
	}
)

func steps(ids ...uuid.UUID) []StepInput {
	out := make([]StepInput, 0, len(ids))
	for i, id := range ids {
		out = append(out, StepInput{StatusID: id, IsInitial: i == 0})
	}
	return out
}

func TestValidateGraphAcceptsAWorkableWorkflow(t *testing.T) {
	err := ValidateGraph(GraphInput{
		Name:  "Simple",
		Steps: steps(todo, progress, done),
		Transitions: []TransitionInput{
			{Name: "Start", FromStatusID: &todo, ToStatusID: progress},
			{Name: "Finish", FromStatusID: &progress, ToStatusID: done},
			// A transition with no source is available everywhere, which is how
			// "Close" is expressed.
			{Name: "Close", ToStatusID: done},
		},
	}, statusNames)
	if err != nil {
		t.Errorf("valid workflow refused: %v", err)
	}
}

func TestValidateGraphRefusals(t *testing.T) {
	cases := []struct {
		name  string
		in    GraphInput
		wants string
	}{
		{"no name", GraphInput{Steps: steps(todo)}, "needs a name"},
		{"no statuses", GraphInput{Name: "Empty"}, "at least one status"},
		{
			"no starting point",
			GraphInput{Name: "Nowhere", Steps: []StepInput{{StatusID: todo}}},
			"where new issues start",
		},
		{
			"two starting points",
			GraphInput{Name: "Both", Steps: []StepInput{
				{StatusID: todo, IsInitial: true}, {StatusID: done, IsInitial: true},
			}},
			"To Do and Done",
		},
		{
			"the same status twice",
			GraphInput{Name: "Doubled", Steps: []StepInput{
				{StatusID: todo, IsInitial: true}, {StatusID: todo},
			}},
			"To Do is listed twice",
		},
		{
			"a status from nowhere",
			GraphInput{Name: "Foreign", Steps: steps(stranger)},
			"no such status",
		},
		{
			"a nameless transition",
			GraphInput{Name: "Mute", Steps: steps(todo, done),
				Transitions: []TransitionInput{{FromStatusID: &todo, ToStatusID: done}}},
			"every transition needs a name",
		},
		{
			"a transition out of the workflow",
			GraphInput{Name: "Dangling", Steps: steps(todo),
				Transitions: []TransitionInput{{Name: "Finish", ToStatusID: done}}},
			"does not contain",
		},
		{
			"a transition into the workflow from outside it",
			GraphInput{Name: "Inbound", Steps: steps(todo, done),
				Transitions: []TransitionInput{{Name: "Finish", FromStatusID: &progress, ToStatusID: done}}},
			"leaves a status this workflow does not contain",
		},
		{
			"a loop back to the same status",
			GraphInput{Name: "Loop", Steps: steps(todo, done),
				Transitions: []TransitionInput{{Name: "Spin", FromStatusID: &done, ToStatusID: done}}},
			"back to itself",
		},
		{
			"two transitions a person cannot tell apart",
			GraphInput{Name: "Ambiguous", Steps: steps(todo, progress, done),
				Transitions: []TransitionInput{
					{Name: "Go", FromStatusID: &todo, ToStatusID: progress},
					{Name: "Go", FromStatusID: &todo, ToStatusID: done},
				}},
			"two transitions called Go leaving To Do",
		},
		{
			"two global transitions with one name",
			GraphInput{Name: "Ambiguous", Steps: steps(todo, progress, done),
				Transitions: []TransitionInput{
					{Name: "Close", ToStatusID: done},
					{Name: "Close", ToStatusID: progress},
				}},
			"called Close available everywhere",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateGraph(tc.in, statusNames)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want it refused as invalid", err)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wants)
			}
		})
	}
}

// The same name may leave two different statuses: "Close" from To Do and
// "Close" from In Progress are two buttons in two places, not an ambiguity.
func TestValidateGraphAllowsANameReusedElsewhere(t *testing.T) {
	err := ValidateGraph(GraphInput{
		Name:  "Reused",
		Steps: steps(todo, progress, done),
		Transitions: []TransitionInput{
			{Name: "Close", FromStatusID: &todo, ToStatusID: done},
			{Name: "Close", FromStatusID: &progress, ToStatusID: done},
		},
	}, statusNames)
	if err != nil {
		t.Errorf("refused: %v", err)
	}
}

func TestValidateSchemeOnlyDemandsAFallbackOfTheOrganizations(t *testing.T) {
	bug := uuid.New()
	typeNames := map[uuid.UUID]string{bug: "Bug"}
	onlyBugs := SchemeInput{Name: "Bugs", Items: []SchemeItemInput{
		{IssueTypeID: &bug, WorkflowID: uuid.New()},
	}}

	// A project's scheme need only cover what the project disagrees about.
	if err := ValidateScheme(onlyBugs, false, typeNames); err != nil {
		t.Errorf("a project scheme with no fallback was refused: %v", err)
	}
	err := ValidateScheme(onlyBugs, true, typeNames)
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "needs a fallback") {
		t.Errorf("error = %v, want the organization's scheme to need a fallback", err)
	}
}

func TestValidateSchemeRefusals(t *testing.T) {
	bug := uuid.New()
	typeNames := map[uuid.UUID]string{bug: "Bug"}
	workflowID := uuid.New()
	stranger := uuid.New()

	cases := []struct {
		name  string
		in    SchemeInput
		wants string
	}{
		{"no name", SchemeInput{Items: []SchemeItemInput{{WorkflowID: workflowID}}}, "needs a name"},
		{"nothing mapped", SchemeInput{Name: "Empty"}, "decides nothing"},
		{
			"two fallbacks",
			SchemeInput{Name: "Twice", Items: []SchemeItemInput{
				{WorkflowID: workflowID}, {WorkflowID: workflowID},
			}},
			"only have one fallback",
		},
		{
			"one type mapped twice",
			SchemeInput{Name: "Doubled", Items: []SchemeItemInput{
				{IssueTypeID: &bug, WorkflowID: workflowID},
				{IssueTypeID: &bug, WorkflowID: workflowID},
			}},
			"Bug is mapped twice",
		},
		{
			"a type from nowhere",
			SchemeInput{Name: "Foreign", Items: []SchemeItemInput{
				{IssueTypeID: &stranger, WorkflowID: workflowID},
			}},
			"no such issue type",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateScheme(tc.in, false, typeNames)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want it refused as invalid", err)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wants)
			}
		})
	}
}

// A rule the engine cannot run would fail the first time somebody took the
// transition, which is the wrong moment to learn about a typo made in settings.
func TestValidateRulesHoldsRulesAgainstTheRegistry(t *testing.T) {
	registry := NewDefaultRegistry()
	edge := func(rules ...RuleInput) []TransitionInput {
		return []TransitionInput{{Name: "Start", FromStatusID: &todo, ToStatusID: progress, Rules: rules}}
	}

	if err := ValidateRules(edge(
		RuleInput{Kind: KindValidator, Type: ValidatorCommentRequired},
		RuleInput{Kind: KindCondition, Type: ConditionOrgRole, Config: `{"roles":["admin"]}`},
		RuleInput{Kind: KindPostFunction, Type: PostAddComment, Config: `{"text":"Started."}`},
	), registry); err != nil {
		t.Fatalf("well formed rules refused: %v", err)
	}

	cases := []struct {
		name  string
		rule  RuleInput
		wants string
	}{
		{"of an unknown kind", RuleInput{Kind: "trigger", Type: ValidatorCommentRequired}, "kind this tracker does not know"},
		{"of an unknown type", RuleInput{Kind: KindCondition, Type: "condition.moon_phase"}, "condition this tracker does not have"},
		{"with a kind that does not match the type", RuleInput{Kind: KindValidator, Type: ConditionIsAssignee}, "validator this tracker does not have"},
		{"with a configuration it cannot use", RuleInput{Kind: KindCondition, Type: ConditionOrgRole, Config: `{"roles":[]}`}, "at least one role"},
		{"with a comment that says nothing", RuleInput{Kind: KindPostFunction, Type: PostAddComment, Config: `{"text":" "}`}, "needs some text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRules(edge(tc.rule), registry)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want it refused as invalid", err)
			}
			if !strings.Contains(err.Error(), "Start") || !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error = %q, want it to name the transition and mention %q", err, tc.wants)
			}
		})
	}
}

// The designer's rule picker is drawn from the catalogue, so a rule left out
// of it is a rule nobody can attach.
func TestTheCatalogueDescribesEveryRegisteredRule(t *testing.T) {
	registry := NewDefaultRegistry()
	catalogue := registry.Catalogue()

	seen := map[string]RuleType{}
	for _, rt := range catalogue {
		seen[rt.Type] = rt
		if rt.Label == "" || rt.Description == "" {
			t.Errorf("%s has no label or description", rt.Type)
		}
		if rt.Options == nil {
			t.Errorf("%s has nil options; the client expects a list", rt.Type)
		}
		if !registry.Known(rt.Kind, rt.Type) {
			t.Errorf("%s is described as a %s but not registered as one", rt.Type, rt.Kind)
		}
	}
	for _, kind := range []RuleKind{KindCondition, KindValidator, KindPostFunction} {
		for _, name := range registry.RuleTypes(kind) {
			if _, ok := seen[name]; !ok {
				t.Errorf("%s is registered but missing from the catalogue", name)
			}
		}
	}

	// Conditions come first, so the picker reads in the order the engine asks.
	last := map[RuleKind]int{KindCondition: 0, KindValidator: 1, KindPostFunction: 2}
	for i := 1; i < len(catalogue); i++ {
		if last[catalogue[i-1].Kind] > last[catalogue[i].Kind] {
			t.Fatalf("catalogue is out of order at %d: %s after %s", i, catalogue[i].Kind, catalogue[i-1].Kind)
		}
	}
	if got := seen[ConditionOrgRole].Options; len(got) != 1 || got[0].Kind != OptionRoles {
		t.Errorf("%s options = %+v, want one roles option", ConditionOrgRole, got)
	}
}
