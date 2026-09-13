package label

import (
	"errors"
	"testing"
)

func TestCleanName(t *testing.T) {
	if got, err := CleanName("  backend "); err != nil || got != "backend" {
		t.Fatalf("got %q %v", got, err)
	}
	for _, bad := range []string{"", "   ", "two words", "tab\tbed", string(make([]rune, MaxNameLength+1))} {
		if _, err := CleanName(bad); !errors.Is(err, ErrBadName) {
			t.Errorf("%q should be refused, got %v", bad, err)
		}
	}
}

// The colour comes from the word, so it is stable and case does not matter.
func TestColorForIsStableAndInThePalette(t *testing.T) {
	a, b := ColorFor("Backend"), ColorFor("backend")
	if a != b {
		t.Fatalf("case should not change the colour: %s vs %s", a, b)
	}
	if !validColor(a) {
		t.Fatalf("%s is not in the palette", a)
	}
	seen := map[string]bool{}
	for _, name := range []string{"backend", "frontend", "urgent", "design", "infra", "docs", "security", "billing", "mobile"} {
		seen[ColorFor(name)] = true
	}
	if len(seen) < 3 {
		t.Fatalf("nine words landed on %d colours; the spread is too poor", len(seen))
	}
}
