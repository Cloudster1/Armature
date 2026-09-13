package issue

import (
	"errors"
	"testing"
)

func TestFormatMinutes(t *testing.T) {
	cases := map[int]string{0: "0m", 45: "45m", 60: "1h", 150: "2h 30m", 480: "1d", 8*60 + 60 + 5: "1d 1h 5m", 16 * 60: "2d"}
	for minutes, want := range cases {
		if got := FormatMinutes(minutes); got != want {
			t.Errorf("FormatMinutes(%d) = %q, want %q", minutes, got, want)
		}
	}
}

func TestWorklogsAreCheckedBeforeTheyAreWritten(t *testing.T) {
	if _, err := checkWorklog(WorklogInput{Minutes: 0}); !errors.Is(err, ErrBadDuration) {
		t.Errorf("zero minutes should be refused, got %v", err)
	}
	if _, err := checkWorklog(WorklogInput{Minutes: MaxWorklogMinutes + 1}); !errors.Is(err, ErrBadDuration) {
		t.Errorf("more than a week in one go should be refused, got %v", err)
	}
	in, err := checkWorklog(WorklogInput{Minutes: 30, Note: "  looked into it  "})
	if err != nil {
		t.Fatal(err)
	}
	if in.StartedOn == nil {
		t.Error("an entry with no day is today")
	}
	if in.Note != "looked into it" {
		t.Errorf("the note should be trimmed, got %q", in.Note)
	}
}
