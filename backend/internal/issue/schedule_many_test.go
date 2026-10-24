package issue

import (
	"errors"
	"strings"
	"testing"
)

// These are refused before the database is reached, so a nil one is enough.
func TestScheduleManyRefusesMovesItCannotMake(t *testing.T) {
	cases := []struct {
		name  string
		moves []ScheduleMove
		want  error
		names string
	}{
		{"another project's issue", []ScheduleMove{{Key: "PR-1"}, {Key: "OT-2"}}, ErrNotInProject, "OT-2"},
		{"one issue twice", []ScheduleMove{{Key: "PR-1"}, {Key: "pr-1"}}, ErrMovedTwice, "pr-1"},
		{"not a key", []ScheduleMove{{Key: "nonsense"}}, ErrInvalidKey, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := (&Service{}).ScheduleMany(t.Context(), "pr", c.moves, Actor{})
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if !strings.Contains(err.Error(), c.names) || !strings.HasSuffix(err.Error(), ".") {
				t.Errorf("err = %q, want a sentence naming %s", err, c.names)
			}
		})
	}
}
