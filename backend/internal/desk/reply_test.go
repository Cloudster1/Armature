package desk

import (
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/workflow"
)

// A customer's reply to a finished request takes the workflow's way back out
// of done, and never a move that leaves it finished.
func TestAReplyToAFinishedRequestTakesTheWayBackOut(t *testing.T) {
	resolvedStep, closedStep, supportStep, progressStep := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	from := func(name string, to uuid.UUID) workflow.Transition {
		return workflow.Transition{ID: uuid.New(), Name: name, FromStepID: &resolvedStep, ToStepID: to}
	}
	anywhere := func(name string, to uuid.UUID) workflow.Transition {
		return workflow.Transition{ID: uuid.New(), Name: name, ToStepID: to}
	}
	leavesDone := func(t workflow.Transition) bool { return t.ToStepID != resolvedStep && t.ToStepID != closedStep }

	reopen := from(Reopen, supportStep)
	back := from("Back to work", progressStep)
	closeIt := from("Close", closedStep)
	resolveAnywhere := anywhere("Resolve", resolvedStep)

	cases := []struct {
		name      string
		available []workflow.Transition
		want      *workflow.Transition
	}{
		{"Reopen first, wherever it is listed", []workflow.Transition{back, reopen}, &reopen},
		{"else the first that leaves done", []workflow.Transition{closeIt, back}, &back},
		{"never one into another done status", []workflow.Transition{closeIt}, nil},
		{"never a resolve from anywhere", []workflow.Transition{resolveAnywhere}, nil},
		{"not even one named Reopen that stays done", []workflow.Transition{from(Reopen, closedStep)}, nil},
		{"nothing when the workflow has no way out", nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pickWay(c.available, Reopen, leavesDone)
			switch {
			case c.want == nil && got != nil:
				t.Errorf("took %q, want nothing", got.Name)
			case c.want != nil && (got == nil || got.ID != c.want.ID):
				t.Errorf("took %v, want %q", got, c.want.Name)
			}
		})
	}
}

// The reply that ends a wait keeps the rule it always had: the transition
// named for it, else the first that is not a resolve from anywhere.
func TestAReplyWhileWaitingTakesTheWayBackToTheDesk(t *testing.T) {
	waitingStep, progressStep, resolvedStep := uuid.New(), uuid.New(), uuid.New()
	responded := workflow.Transition{ID: uuid.New(), Name: CustomerResponded, FromStepID: &waitingStep, ToStepID: progressStep}
	other := workflow.Transition{ID: uuid.New(), Name: "Back to the desk", FromStepID: &waitingStep, ToStepID: progressStep}
	resolve := workflow.Transition{ID: uuid.New(), Name: "Resolve", ToStepID: resolvedStep}
	anywhereAtAll := func(workflow.Transition) bool { return true }

	if got := pickWay([]workflow.Transition{resolve, other, responded}, CustomerResponded, anywhereAtAll); got == nil || got.ID != responded.ID {
		t.Errorf("took %v, want %q", got, CustomerResponded)
	}
	if got := pickWay([]workflow.Transition{resolve, other}, CustomerResponded, anywhereAtAll); got == nil || got.ID != other.ID {
		t.Errorf("took %v, want the first that is not global", got)
	}
	if got := pickWay([]workflow.Transition{resolve}, CustomerResponded, anywhereAtAll); got != nil {
		t.Errorf("took %q, want nothing rather than a resolve from anywhere", got.Name)
	}
}
