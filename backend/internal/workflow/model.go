// Package workflow is the state machine issues move through.
//
// The engine is deliberately pure: it is handed a workflow, an actor, the issue
// as it stands and whatever the user typed, and it answers which transitions
// are available and what a chosen one should do. It performs no database work
// of its own. The caller owns the transaction, applies the resulting effect and
// records the change, which keeps the rules unit testable and keeps "what the
// workflow decided" separate from "how the issue was written".
package workflow

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// StatusCategory is the coarse bucket a status falls into. Boards, reports and
// every "is it finished" question key on this rather than on the status name.
type StatusCategory string

const (
	CategoryTodo       StatusCategory = "todo"
	CategoryInProgress StatusCategory = "in_progress"
	CategoryDone       StatusCategory = "done"
)

func (c StatusCategory) Valid() bool {
	switch c {
	case CategoryTodo, CategoryInProgress, CategoryDone:
		return true
	}
	return false
}

// Status is a state an issue can be in.
type Status struct {
	ID          uuid.UUID      `json:"id"`
	Name        string         `json:"name"`
	Category    StatusCategory `json:"category"`
	Description string         `json:"description,omitempty"`
	Position    int            `json:"position"`
}

// Point is where the designer drew a status, in canvas pixels.
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

// Step is a status's place within one workflow.
type Step struct {
	ID        uuid.UUID `json:"id"`
	Status    Status    `json:"status"`
	IsInitial bool      `json:"isInitial"`
	Position  int       `json:"position"`
	// Layout is absent until somebody has placed the status on the canvas.
	Layout *Point `json:"layout,omitempty"`
}

// RuleKind separates the three points at which a rule can intervene.
type RuleKind string

const (
	// KindCondition decides whether a transition is offered at all. A failing
	// condition hides the transition rather than reporting an error, so users
	// are not shown buttons they cannot press.
	KindCondition RuleKind = "condition"
	// KindValidator checks the input of a transition the user has chosen. A
	// failing validator is an error message they can act on.
	KindValidator RuleKind = "validator"
	// KindPostFunction runs after the status has changed.
	KindPostFunction RuleKind = "postfunction"
)

// Valid reports whether the kind is one of the three.
func (k RuleKind) Valid() bool {
	switch k {
	case KindCondition, KindValidator, KindPostFunction:
		return true
	}
	return false
}

// Noun is the kind as a sentence names it.
func (k RuleKind) Noun() string {
	switch k {
	case KindCondition:
		return "condition"
	case KindValidator:
		return "validator"
	case KindPostFunction:
		return "post-function"
	}
	return string(k)
}

// Rule is a configured rule instance attached to a transition.
type Rule struct {
	ID       uuid.UUID       `json:"id"`
	Kind     RuleKind        `json:"kind"`
	Type     string          `json:"type"`
	Config   json.RawMessage `json:"config,omitempty"`
	Position int             `json:"position"`
}

// Transition is an edge in the workflow graph.
type Transition struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// FromStepID is nil for a transition available from every status, which is
	// how "Close" or "Reopen from anywhere" is expressed.
	FromStepID  *uuid.UUID `json:"fromStepId,omitempty"`
	ToStepID    uuid.UUID  `json:"toStepId"`
	Description string     `json:"description,omitempty"`
	Position    int        `json:"position"`
	Rules       []Rule     `json:"-"`
}

// IsGlobal reports whether the transition can be taken from any status.
func (t Transition) IsGlobal() bool { return t.FromStepID == nil }

// Workflow is a set of steps and the transitions between them.
type Workflow struct {
	ID          uuid.UUID    `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Steps       []Step       `json:"steps"`
	Transitions []Transition `json:"transitions"`
}

// InitialStep returns the step new issues start in.
func (w *Workflow) InitialStep() (Step, error) {
	for _, s := range w.Steps {
		if s.IsInitial {
			return s, nil
		}
	}
	return Step{}, fmt.Errorf("workflow %q has no initial step", w.Name)
}

// StepByStatus finds the step holding a status.
func (w *Workflow) StepByStatus(statusID uuid.UUID) (Step, bool) {
	for _, s := range w.Steps {
		if s.Status.ID == statusID {
			return s, true
		}
	}
	return Step{}, false
}

// StepByID finds a step.
func (w *Workflow) StepByID(stepID uuid.UUID) (Step, bool) {
	for _, s := range w.Steps {
		if s.ID == stepID {
			return s, true
		}
	}
	return Step{}, false
}

// Transition finds a transition by id.
func (w *Workflow) Transition(id uuid.UUID) (Transition, bool) {
	for _, t := range w.Transitions {
		if t.ID == id {
			return t, true
		}
	}
	return Transition{}, false
}
