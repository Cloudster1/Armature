package issue

import (
	"errors"
	"testing"
	"time"
)

func TestAnEditStartedFromAnOlderCopyOfTheIssueIsStale(t *testing.T) {
	current := time.Date(2026, 10, 10, 9, 30, 0, 123456000, time.UTC)

	if err := CheckFresh(current, nil); err != nil {
		t.Fatalf("an edit that names no starting point is taken as before, got %v", err)
	}
	sameInstant := current.In(time.FixedZone("CEST", 2*60*60))
	if err := CheckFresh(current, &sameInstant); err != nil {
		t.Fatalf("the same instant written in another zone is not stale, got %v", err)
	}
	older := current.Add(-time.Microsecond)
	if err := CheckFresh(current, &older); !errors.Is(err, ErrStale) {
		t.Fatalf("an edit started a microsecond earlier must be stale, got %v", err)
	}
	newer := current.Add(time.Second)
	if err := CheckFresh(current, &newer); !errors.Is(err, ErrStale) {
		t.Fatalf("an edit claiming a copy the server never had must be stale, got %v", err)
	}
}
