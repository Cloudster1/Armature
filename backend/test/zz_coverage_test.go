//go:build integration

package test

import (
	"strings"
	"testing"
)

// This file sorts last, so the test runs after every other one has made its
// calls. Every operation in the document has to have been answered with a
// success and with a refusal by then: an endpoint nobody exercised is an
// endpoint whose description nobody has checked.
func TestEveryOperationWasExercisedBothWays(t *testing.T) {
	if runFiltered() {
		t.Skip("a filtered run cannot cover the whole surface")
	}
	neverSucceeded, neverRefused := apiContract.uncovered()
	// A GET with no input and no session cannot be refused; there is nothing
	// to refuse it for.
	unrefusable := map[string]bool{"GET /healthz": true, "GET /readyz": true, "GET /openapi.json": true, "GET /auth/signup": true}
	kept := neverRefused[:0]
	for _, key := range neverRefused {
		if !unrefusable[key] {
			kept = append(kept, key)
		}
	}
	neverRefused = kept
	if len(neverSucceeded) > 0 {
		t.Errorf("%d operations were never answered successfully:\n  %s", len(neverSucceeded), strings.Join(neverSucceeded, "\n  "))
	}
	if len(neverRefused) > 0 {
		t.Errorf("%d operations were never refused:\n  %s", len(neverRefused), strings.Join(neverRefused, "\n  "))
	}
}
