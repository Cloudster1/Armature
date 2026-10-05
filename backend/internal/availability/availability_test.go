package availability

import (
	"errors"
	"testing"
	"time"
)

func date(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(DateLayout, s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestADayHoldsWhatItsWeekdaySaysLessItsHolidays(t *testing.T) {
	partTime := map[string]int{"mon": 480, "tue": 480, "wed": 240, "thu": 0, "fri": 450, "sat": 0, "sun": 0}
	off := DaysOf([]Holiday{
		{Day: "2026-12-25", Name: "Christmas Day"},
		{Day: "2026-12-24", Name: "Christmas Eve", HalfDay: true},
		{Day: "2026-12-26", Name: "Boxing Day"},
		{Day: "2026-12-31", Name: "New Year's Eve", HalfDay: true},
	})
	cases := []struct {
		name string
		day  string
		week map[string]int
		want int
	}{
		{"an ordinary Monday", "2026-12-21", partTime, 480},
		{"a short Wednesday", "2026-12-23", partTime, 240},
		{"a half day halves the day", "2026-12-24", StandardWeek(), 240},
		{"a half day on a day not worked stays nothing", "2026-12-24", partTime, 0},
		{"a half day of an odd length rounds down", "2026-12-31", map[string]int{"thu": 450}, 225},
		{"a holiday takes the whole day", "2026-12-25", partTime, 0},
		{"a holiday on a weekend takes nothing more", "2026-12-26", StandardWeek(), 0},
		{"a weekend in the standard week", "2026-12-27", StandardWeek(), 0},
		{"somebody with no week of their own works the standard one", "2026-12-22", StandardWeek(), StandardDayMinutes},
		{"a weekday left out of a week is not worked", "2026-12-22", map[string]int{"mon": 480}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MinutesOn(c.week, date(t, c.day), off); got != c.want {
				t.Errorf("MinutesOn(%s) = %d, want %d", c.day, got, c.want)
			}
		})
	}

	week := WorkingWeek{Minutes: partTime}
	if got := week.MinutesOn(date(t, "2026-12-25"), off); got != 0 {
		t.Errorf("a person on Christmas Day has %d minutes", got)
	}
	if got := week.MinutesOn(date(t, "2026-12-25"), nil); got != 450 {
		t.Errorf("with no calendar a Friday has %d minutes, want 450", got)
	}
}

func TestTheStandardWeekIsFiveEightHourDays(t *testing.T) {
	week := StandardWeek()
	total := 0
	for _, day := range Weekdays {
		total += week[day]
	}
	if total != 5*StandardDayMinutes || week["sat"] != 0 || week["sun"] != 0 {
		t.Errorf("standard week = %v", week)
	}
}

func TestAWeekIsHeldToItsBounds(t *testing.T) {
	filled, err := NormalizeWeek(map[string]int{"mon": 300})
	if err != nil {
		t.Fatal(err)
	}
	if len(filled) != len(Weekdays) || filled["mon"] != 300 || filled["tue"] != 0 {
		t.Errorf("a week with one day = %v, want every weekday and the rest at nothing", filled)
	}
	if standard, _ := NormalizeWeek(nil); standard["fri"] != StandardDayMinutes {
		t.Errorf("no week at all = %v, want the standard one", standard)
	}
	full, err := NormalizeWeek(map[string]int{"sun": MinutesPerDay})
	if err != nil || full["sun"] != MinutesPerDay {
		t.Errorf("a whole day refused: %v", err)
	}
	for name, week := range map[string]map[string]int{
		"a day that is not a weekday": {"funday": 60},
		"a capital weekday":           {"Mon": 60},
		"a negative day":              {"tue": -1},
		"more than a day holds":       {"wed": MinutesPerDay + 1},
	} {
		if _, err := NormalizeWeek(week); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: error = %v, want it refused", name, err)
		}
	}
}

func TestACalendarsDaysAreCheckedAndPutInOrder(t *testing.T) {
	got, err := NormalizeDays([]Holiday{
		{Day: "2026-12-26", Name: " Boxing Day "},
		{Day: "2026-01-01", Name: "New Year's Day", HalfDay: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Day != "2026-01-01" || !got[0].HalfDay || got[1].Name != "Boxing Day" {
		t.Errorf("days = %+v", got)
	}

	long := make([]byte, MaxNameLength+1)
	for i := range long {
		long[i] = 'x'
	}
	tooMany := make([]Holiday, MaxDays+1)
	for i := range tooMany {
		tooMany[i] = Holiday{Day: date(t, "2000-01-01").AddDate(0, 0, i).Format(DateLayout), Name: "Day"}
	}
	for name, days := range map[string][]Holiday{
		"a day that is not a date":        {{Day: "25.12.2026", Name: "Christmas"}},
		"a day that does not exist":       {{Day: "2026-02-30", Name: "Nope"}},
		"a day with no name":              {{Day: "2026-12-25", Name: "  "}},
		"a name that is too long":         {{Day: "2026-12-25", Name: string(long)}},
		"the same day twice":              {{Day: "2026-12-25", Name: "A"}, {Day: "2026-12-25", Name: "B"}},
		"more days than a calendar holds": tooMany,
	} {
		if _, err := NormalizeDays(days); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: error = %v, want it refused", name, err)
		}
	}
}
