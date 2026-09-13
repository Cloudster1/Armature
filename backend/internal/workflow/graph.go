package workflow

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var (
	// ErrInvalid marks a workflow an editor asked for that could not work.
	ErrInvalid = errors.New("invalid workflow")
	// ErrInUse is returned when deleting something a project still relies on.
	ErrInUse = errors.New("still in use")
)

// GraphInput is a whole workflow as an editor sends it: the statuses it is made
// of, and the transitions between them. Statuses are named rather than steps,
// because a step only exists once the workflow does.
type GraphInput struct {
	Name        string
	Description string
	Steps       []StepInput
	Transitions []TransitionInput
}

// StepInput places one status in the workflow.
type StepInput struct {
	StatusID  uuid.UUID
	IsInitial bool
	// Layout is where the designer drew it; nil leaves the status unplaced.
	Layout *Point
}

// TransitionInput is one edge.
type TransitionInput struct {
	// ID names a transition that already exists, so that the rules hanging off
	// it survive an edit. Zero for one being added.
	ID           uuid.UUID
	Name         string
	Description  string
	FromStatusID *uuid.UUID
	ToStatusID   uuid.UUID
	// Rules, when non-nil, are the transition's whole set of rules and replace
	// whatever it had. A nil Rules leaves an existing transition's rules alone,
	// which is how an editor that never looked at them keeps them across a save.
	Rules []RuleInput
}

// RuleInput is one condition, validator or post-function on a new transition.
type RuleInput struct {
	Kind RuleKind
	Type string
	// Config is the rule's JSON configuration; empty means none.
	Config string
}

// FromAnywhere reports whether the transition is available from every status.
func (t TransitionInput) FromAnywhere() bool { return t.FromStatusID == nil }

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// ValidateGraph checks a workflow an editor is about to save. The status names
// are passed in so that a refusal can say which status it means.
func ValidateGraph(in GraphInput, names map[uuid.UUID]string) error {
	if in.Name == "" {
		return invalid("a workflow needs a name")
	}
	if len(in.Steps) == 0 {
		return invalid("a workflow needs at least one status")
	}

	var initial string
	seen := map[uuid.UUID]bool{}
	for _, step := range in.Steps {
		name, known := names[step.StatusID]
		if !known {
			return invalid("this organization has no such status")
		}
		if seen[step.StatusID] {
			return invalid("%s is listed twice", name)
		}
		seen[step.StatusID] = true

		if step.IsInitial {
			if initial != "" {
				return invalid("new issues can only start in one status, and both %s and %s are marked", initial, name)
			}
			initial = name
		}
	}
	if initial == "" {
		return invalid("one status has to be where new issues start")
	}

	return validateEdges(in.Transitions, seen, names)
}

func validateEdges(transitions []TransitionInput, inWorkflow map[uuid.UUID]bool, names map[uuid.UUID]string) error {
	// A person picks a transition by its name, so two with the same name
	// leaving the same status would be a choice they cannot make.
	type edge struct {
		from uuid.UUID
		name string
	}
	byName := map[edge]bool{}

	for _, t := range transitions {
		if t.Name == "" {
			return invalid("every transition needs a name")
		}
		if !inWorkflow[t.ToStatusID] {
			return invalid("%s leads to a status this workflow does not contain", t.Name)
		}
		from := uuid.Nil
		if !t.FromAnywhere() {
			if !inWorkflow[*t.FromStatusID] {
				return invalid("%s leaves a status this workflow does not contain", t.Name)
			}
			if *t.FromStatusID == t.ToStatusID {
				return invalid("%s goes from %s back to itself", t.Name, names[t.ToStatusID])
			}
			from = *t.FromStatusID
		}

		key := edge{from: from, name: t.Name}
		if byName[key] {
			if from == uuid.Nil {
				return invalid("there are two transitions called %s available everywhere", t.Name)
			}
			return invalid("there are two transitions called %s leaving %s", t.Name, names[from])
		}
		byName[key] = true
	}
	return nil
}

// ValidateRules holds the rules an editor attached against what the registry
// can run, so a workflow is refused when it is saved rather than failing the
// first time somebody takes the transition.
func ValidateRules(transitions []TransitionInput, registry *Registry) error {
	for _, t := range transitions {
		for _, rule := range t.Rules {
			if !rule.Kind.Valid() {
				return invalid("%s has a rule of a kind this tracker does not know: %s", t.Name, rule.Kind)
			}
			if !registry.Known(rule.Kind, rule.Type) {
				return invalid("%s uses a %s this tracker does not have: %s", t.Name, rule.Kind.Noun(), rule.Type)
			}
			if err := registry.Check(rule.Kind, rule.Type, json.RawMessage(rule.Config)); err != nil {
				return invalid("%s: %v", t.Name, err)
			}
		}
	}
	return nil
}
