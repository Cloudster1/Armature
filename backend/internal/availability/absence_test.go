package availability

import (
	"errors"
	"strings"
	"testing"
)

func TestAnAbsenceIsHeldToTheBoundsTheDatabaseHolds(t *testing.T) {
	cases := []struct {
		name            string
		starts, ends    string
		halfDay         bool
		refusedWith     string
		wantStartsOn    string
		wantEndsOn      string
		wantHalfDayKept bool
	}{
		{name: "one day", starts: "2026-11-02", ends: "2026-11-02", wantStartsOn: "2026-11-02", wantEndsOn: "2026-11-02"},
		{name: "a fortnight", starts: "2026-08-03", ends: "2026-08-14", wantStartsOn: "2026-08-03", wantEndsOn: "2026-08-14"},
		{name: "half of one day", starts: "2026-12-24", ends: "2026-12-24", halfDay: true, wantStartsOn: "2026-12-24", wantEndsOn: "2026-12-24", wantHalfDayKept: true},
		{name: "spaces around the days", starts: " 2026-11-02 ", ends: "2026-11-03 ", wantStartsOn: "2026-11-02", wantEndsOn: "2026-11-03"},
		{name: "the longest there is", starts: "2026-01-01", ends: "2027-01-01", wantStartsOn: "2026-01-01", wantEndsOn: "2027-01-01"},
		{name: "a day longer than that", starts: "2026-01-01", ends: "2027-01-02", refusedWith: "at most 366 days"},
		{name: "ending before it starts", starts: "2026-11-03", ends: "2026-11-02", refusedWith: "swap them"},
		{name: "a half day over two days", starts: "2026-11-02", ends: "2026-11-03", halfDay: true, refusedWith: "only a single day"},
		{name: "a day written another way", starts: "02.11.2026", ends: "2026-11-02", refusedWith: "YYYY-MM-DD"},
		{name: "a day that is not one", starts: "2026-02-30", ends: "2026-03-01", refusedWith: "YYYY-MM-DD"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := CheckAbsence(c.starts, c.ends, c.halfDay)
			if c.refusedWith != "" {
				if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), c.refusedWith) {
					t.Fatalf("err = %v, want a refusal saying %q", err, c.refusedWith)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.StartsOn != c.wantStartsOn || got.EndsOn != c.wantEndsOn || got.HalfDay != c.wantHalfDayKept {
				t.Errorf("got %+v", got)
			}
		})
	}
}

func TestAListingCoversAMonthBehindAndAYearAheadUnlessAsked(t *testing.T) {
	today := date(t, "2026-10-05")
	from, to, err := AbsenceWindow("", "", today)
	if err != nil {
		t.Fatal(err)
	}
	if from.Format(DateLayout) != "2026-09-04" || to.Format(DateLayout) != "2027-10-05" {
		t.Errorf("the default window = %s to %s", from.Format(DateLayout), to.Format(DateLayout))
	}

	from, to, err = AbsenceWindow("2026-12-01", "2026-12-31", today)
	if err != nil || from.Format(DateLayout) != "2026-12-01" || to.Format(DateLayout) != "2026-12-31" {
		t.Errorf("an asked window = %s to %s (%v)", from.Format(DateLayout), to.Format(DateLayout), err)
	}

	from, to, err = AbsenceWindow("2027-01-01", "", today)
	if err != nil || from.Format(DateLayout) != "2027-01-01" || daysBetween(from, to) != AbsenceDaysBehind+AbsenceDaysAhead {
		t.Errorf("a window from a day = %s to %s (%v), want as long as the default", from.Format(DateLayout), to.Format(DateLayout), err)
	}

	for name, window := range map[string][2]string{
		"backwards":        {"2026-12-31", "2026-12-01"},
		"too wide":         {"2026-01-01", "2028-01-03"},
		"not a day":        {"tomorrow", ""},
		"an end not a day": {"2026-01-01", "31/12/2026"},
	} {
		if _, _, err := AbsenceWindow(window[0], window[1], today); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want a refusal", name, err)
		}
	}
	if _, _, err := AbsenceWindow("2026-01-01", "2027-12-31", today); err != nil {
		t.Errorf("the widest window there is was refused: %v", err)
	}
}
