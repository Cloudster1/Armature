package issue

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAnIssueIsScheduledWithinTheYearsAPlanCanRead(t *testing.T) {
	day := func(s string) *time.Time {
		d, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return &d
	}
	for _, ok := range [][2]*time.Time{
		{nil, nil},
		{day("1900-01-01"), day("2199-12-31")},
		{nil, day("2026-10-09")},
		{day("2026-10-09"), day("2026-10-09")},
	} {
		if err := CheckDates(ok[0], ok[1]); err != nil {
			t.Errorf("CheckDates(%s, %s) = %v, want it taken", formatDate(ok[0]), formatDate(ok[1]), err)
		}
	}
	for _, out := range []string{"1899-12-31", "2200-01-01", "9999-12-31"} {
		err := CheckDates(nil, day(out))
		if !errors.Is(err, ErrDateOutOfRange) || !strings.Contains(err.Error(), out) || !strings.HasSuffix(err.Error(), ".") {
			t.Errorf("a due date of %s: %v, want a sentence refusing it", out, err)
		}
		if err := CheckDates(day(out), nil); !errors.Is(err, ErrDateOutOfRange) {
			t.Errorf("a start date of %s: %v, want it refused", out, err)
		}
	}
	if err := CheckDates(day("2026-10-09"), day("2026-10-01")); !errors.Is(err, ErrBackwardsRange) {
		t.Errorf("a backwards range: %v, want ErrBackwardsRange", err)
	}
}
