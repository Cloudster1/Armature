package events

import (
	"errors"
	"fmt"
	"testing"
)

// A handler wraps its errors on the way out; a permanent one must still be
// known as permanent, and still be the error it was.
func TestAPermanentErrorIsKnownThroughWrapping(t *testing.T) {
	cause := errors.New("the host refused the comment")
	err := fmt.Errorf("tell pull request 9: %w", Permanent(cause))
	if !IsPermanent(err) {
		t.Errorf("%v is not known as permanent once wrapped", err)
	}
	if !errors.Is(err, cause) {
		t.Errorf("%v no longer is its cause", err)
	}
	if IsPermanent(cause) {
		t.Error("an ordinary error is taken for a permanent one")
	}
	if Permanent(nil) != nil {
		t.Error("nothing wrong became a permanent error")
	}
}

// The worker's own policy must retry at all, give up eventually, and claim
// only what has been left long enough that nobody is still working on it.
func TestTheWorkersPolicyRetriesAndGivesUp(t *testing.T) {
	p := DefaultPolicy
	if p.MaxDeliveries < 2 {
		t.Errorf("MaxDeliveries = %d, want at least one retry", p.MaxDeliveries)
	}
	if p.MinIdle <= 0 || p.ClaimEvery <= 0 || p.Grace <= 0 {
		t.Errorf("policy = %+v, want every interval set", p)
	}
}
