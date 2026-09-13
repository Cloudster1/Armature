package workflow

import (
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
)

// Engine evaluates workflows. It holds no state beyond the rule registry, so
// one instance is shared by every request.
type Engine struct {
	registry *Registry
}

func NewEngine(registry *Registry) *Engine {
	if registry == nil {
		registry = NewDefaultRegistry()
	}
	return &Engine{registry: registry}
}

func (e *Engine) Registry() *Registry { return e.registry }

var (
	// ErrTransitionNotFound is returned for a transition that is not part of
	// the workflow at all.
	ErrTransitionNotFound = errors.New("no such transition")
	// ErrTransitionNotAvailable is returned for a transition that exists but
	// cannot be taken from the issue's current status, or whose conditions the
	// actor does not satisfy.
	ErrTransitionNotAvailable = errors.New("that transition is not available")
)

// Available returns the transitions the actor may take from the issue's current
// status, in workflow order.
//
// A transition whose conditions fail is left out rather than reported: the
// point of a condition is to not offer the button in the first place.
func (e *Engine) Available(w *Workflow, subject Subject, actor Actor) ([]Transition, error) {
	current, ok := w.StepByStatus(subject.StatusID)
	if !ok {
		// The issue is in a status this workflow does not contain, which
		// happens when a project's scheme is changed underneath live issues.
		// Offering only the global transitions is the recoverable answer.
		current = Step{}
	}

	var out []Transition
	for _, t := range w.Transitions {
		if !e.reachable(t, current, ok) {
			continue
		}
		allowed, _, err := e.conditionsPass(t, subject, actor)
		if err != nil {
			return nil, err
		}
		if allowed {
			out = append(out, t)
		}
	}

	slices.SortStableFunc(out, func(a, b Transition) int { return a.Position - b.Position })
	return out, nil
}

// reachable reports whether a transition leaves the issue's current step.
func (e *Engine) reachable(t Transition, current Step, haveCurrent bool) bool {
	if t.IsGlobal() {
		// A global transition into the status the issue already occupies is
		// not worth offering.
		return !haveCurrent || t.ToStepID != current.ID
	}
	return haveCurrent && *t.FromStepID == current.ID
}

func (e *Engine) conditionsPass(t Transition, subject Subject, actor Actor) (bool, string, error) {
	for _, rule := range rulesOfKind(t.Rules, KindCondition) {
		condition, err := e.registry.buildCondition(rule)
		if err != nil {
			return false, "", fmt.Errorf("transition %q: %w", t.Name, err)
		}
		if ok, reason := condition.Allow(subject, actor); !ok {
			return false, reason, nil
		}
	}
	return true, "", nil
}

// Decision is the outcome of evaluating a chosen transition.
type Decision struct {
	Transition Transition
	// ToStatus is the status the issue ends up in.
	ToStatus Status
	// Effect is what the post-functions want applied alongside the status
	// change. The caller performs it.
	Effect Effect
}

// Evaluate checks a chosen transition and works out what it should do.
//
// It runs conditions first, then validators, then post-functions, and returns
// without touching anything. The caller applies the decision inside its own
// transaction, so a failure at any point leaves the issue untouched.
func (e *Engine) Evaluate(w *Workflow, transitionID uuid.UUID, subject Subject, actor Actor, input Input) (*Decision, error) {
	t, ok := w.Transition(transitionID)
	if !ok {
		return nil, ErrTransitionNotFound
	}

	current, haveCurrent := w.StepByStatus(subject.StatusID)
	if !e.reachable(t, current, haveCurrent) {
		return nil, fmt.Errorf("%w: %q does not leave the issue's current status", ErrTransitionNotAvailable, t.Name)
	}

	allowed, reason, err := e.conditionsPass(t, subject, actor)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, fmt.Errorf("%w: %s", ErrTransitionNotAvailable, reason)
	}

	for _, rule := range rulesOfKind(t.Rules, KindValidator) {
		validator, err := e.registry.buildValidator(rule)
		if err != nil {
			return nil, fmt.Errorf("transition %q: %w", t.Name, err)
		}
		if err := validator.Validate(subject, actor, input); err != nil {
			return nil, err
		}
	}

	target, ok := w.StepByID(t.ToStepID)
	if !ok {
		return nil, fmt.Errorf("transition %q points at a step that is not in the workflow", t.Name)
	}

	decision := &Decision{Transition: t, ToStatus: target.Status}

	// The user's own input is applied before the post-functions, so a
	// post-function can deliberately override it.
	if input.Assignee != nil {
		if *input.Assignee == uuid.Nil {
			decision.Effect.ClearAssignee()
		} else {
			decision.Effect.SetAssignee(*input.Assignee)
		}
	}

	for _, rule := range rulesOfKind(t.Rules, KindPostFunction) {
		fn, err := e.registry.buildPostFunction(rule)
		if err != nil {
			return nil, fmt.Errorf("transition %q: %w", t.Name, err)
		}
		if err := fn.Apply(subject, actor, input, &decision.Effect); err != nil {
			return nil, fmt.Errorf("transition %q: %w", t.Name, err)
		}
	}

	return decision, nil
}

// Validate checks that a workflow is coherent before it is saved. Catching
// these here means an administrator sees the problem while editing, rather than
// a user meeting it halfway through a transition.
func (e *Engine) Validate(w *Workflow) error {
	var problems []string

	if len(w.Steps) == 0 {
		problems = append(problems, "a workflow needs at least one status")
	}

	initials := 0
	for _, s := range w.Steps {
		if s.IsInitial {
			initials++
		}
		if !s.Status.Category.Valid() {
			problems = append(problems, fmt.Sprintf("status %q has an unknown category %q", s.Status.Name, s.Status.Category))
		}
	}
	switch {
	case initials == 0:
		problems = append(problems, "no status is marked as where new issues start")
	case initials > 1:
		problems = append(problems, "more than one status is marked as where new issues start")
	}

	for _, t := range w.Transitions {
		if _, ok := w.StepByID(t.ToStepID); !ok {
			problems = append(problems, fmt.Sprintf("transition %q leads outside the workflow", t.Name))
		}
		if t.FromStepID != nil {
			if _, ok := w.StepByID(*t.FromStepID); !ok {
				problems = append(problems, fmt.Sprintf("transition %q starts outside the workflow", t.Name))
			}
		}
		for _, rule := range t.Rules {
			if !e.registry.Known(rule.Kind, rule.Type) {
				problems = append(problems, fmt.Sprintf("transition %q uses unknown %s %q", t.Name, rule.Kind, rule.Type))
				continue
			}
			// Building the rule proves its configuration parses, so a typo in
			// the config is caught now rather than at transition time.
			if err := e.buildRule(rule); err != nil {
				problems = append(problems, fmt.Sprintf("transition %q: %v", t.Name, err))
			}
		}
	}

	// Every status other than the initial one should be reachable, or issues
	// can never get there and the workflow is lying about what it supports.
	for _, s := range w.Steps {
		if s.IsInitial {
			continue
		}
		reachable := slices.ContainsFunc(w.Transitions, func(t Transition) bool { return t.ToStepID == s.ID })
		if !reachable {
			problems = append(problems, fmt.Sprintf("no transition leads to %q", s.Status.Name))
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("workflow %q is not usable: %v", w.Name, problems)
	}
	return nil
}

func (e *Engine) buildRule(rule Rule) error {
	var err error
	switch rule.Kind {
	case KindCondition:
		_, err = e.registry.buildCondition(rule)
	case KindValidator:
		_, err = e.registry.buildValidator(rule)
	case KindPostFunction:
		_, err = e.registry.buildPostFunction(rule)
	}
	return err
}

// rulesOfKind returns the rules of one kind, in configured order.
func rulesOfKind(rules []Rule, kind RuleKind) []Rule {
	var out []Rule
	for _, r := range rules {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	slices.SortStableFunc(out, func(a, b Rule) int { return a.Position - b.Position })
	return out
}
