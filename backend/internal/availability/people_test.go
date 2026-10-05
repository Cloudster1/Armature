package availability

import (
	"testing"

	"github.com/google/uuid"
)

func TestADayCombinesTheWeekTheCalendarAndTheAbsences(t *testing.T) {
	off := DaysOf([]Holiday{
		{Day: "2026-12-24", Name: "Christmas Eve", HalfDay: true},
		{Day: "2026-12-25", Name: "Christmas Day"},
		{Day: "2026-12-26", Name: "Boxing Day"},
	})
	away := []Absence{
		{StartsOn: "2026-12-24", EndsOn: "2026-12-24", HalfDay: true},
		{StartsOn: "2026-12-25", EndsOn: "2026-12-29"},
	}
	s := Schedule{Week: StandardWeek(), Off: off, Away: away}
	cases := []struct {
		name    string
		day     string
		minutes int
		nominal int
		off     DayOff
		half    bool
	}{
		{"an ordinary day", "2026-12-23", 480, 480, OffNone, false},
		{"a half holiday and a half day away take the whole day", "2026-12-24", 0, 480, OffAway, true},
		{"away over a holiday is the holiday", "2026-12-25", 0, 480, OffHoliday, false},
		{"a holiday on a weekend takes nothing the week gave", "2026-12-26", 0, 0, OffHoliday, false},
		{"away on a weekend is away from nothing", "2026-12-27", 0, 0, OffAway, false},
		{"away on a working day", "2026-12-28", 0, 480, OffAway, false},
		{"back after the absence", "2026-12-30", 480, 480, OffNone, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := s.Day(date(t, c.day))
			if got.Minutes != c.minutes || got.Nominal != c.nominal || got.Off != c.off || got.HalfDay != c.half {
				t.Errorf("%s = %+v, want %d of %d minutes, off %q, half %v", c.day, got, c.minutes, c.nominal, c.off, c.half)
			}
		})
	}
	if got := s.Day(date(t, "2026-12-25")); got.Holiday != "Christmas Day" {
		t.Errorf("the holiday is called %q", got.Holiday)
	}
	alone := Schedule{Week: StandardWeek(), Off: off}
	if got := alone.Day(date(t, "2026-12-24")); got.Minutes != 240 || got.Off != OffHoliday || !got.HalfDay {
		t.Errorf("a half holiday alone = %+v, want half the day", got)
	}
	halfAway := Schedule{Week: StandardWeek(), Away: away[:1]}
	if got := halfAway.Day(date(t, "2026-12-24")); got.Minutes != 240 || got.Off != OffAway {
		t.Errorf("a half day away alone = %+v, want half the day", got)
	}
}

func TestEverybodyAskedAboutGetsTheirDays(t *testing.T) {
	saved, unsaved, orphaned, berliner := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	defaultID, berlin, gone := uuid.New(), uuid.New(), uuid.New()
	calendars := map[uuid.UUID]Days{
		defaultID: DaysOf([]Holiday{{Day: "2026-10-05", Name: "Company day"}}),
		berlin:    DaysOf([]Holiday{{Day: "2026-10-06", Name: "Berlin day"}}),
	}
	partTime := map[string]int{"mon": 240, "tue": 240, "wed": 240, "thu": 240, "fri": 240, "sat": 0, "sun": 0}
	rows := map[uuid.UUID]stored{
		saved:    {Week: partTime},
		orphaned: {Week: StandardWeek(), CalendarID: &gone},
		berliner: {Week: StandardWeek(), CalendarID: &berlin},
	}
	away := map[uuid.UUID][]Absence{saved: {{StartsOn: "2026-10-07", EndsOn: "2026-10-07"}}}
	got := combine([]uuid.UUID{saved, unsaved, orphaned, berliner}, rows, calendars, &defaultID, away, date(t, "2026-10-05"), date(t, "2026-10-11"))

	minutes := func(id uuid.UUID) []int {
		var out []int
		for _, d := range got[id].Days {
			out = append(out, d.Minutes)
		}
		return out
	}
	expect := map[string]struct {
		id   uuid.UUID
		want []int
	}{
		"their own week on the default calendar, away on Wednesday":   {saved, []int{0, 240, 0, 240, 240, 0, 0}},
		"nobody set a week: the standard one on the default calendar": {unsaved, []int{0, 480, 480, 480, 480, 0, 0}},
		"a calendar that is gone falls back to the default":           {orphaned, []int{0, 480, 480, 480, 480, 0, 0}},
		"a calendar of their own, and not the default's days":         {berliner, []int{480, 0, 480, 480, 480, 0, 0}},
	}
	for name, c := range expect {
		t.Run(name, func(t *testing.T) {
			have := minutes(c.id)
			if len(have) != len(c.want) {
				t.Fatalf("days = %v, want %v", have, c.want)
			}
			for i := range have {
				if have[i] != c.want[i] {
					t.Fatalf("days = %v, want %v", have, c.want)
				}
			}
		})
	}
	if got[unsaved].Week["mon"] != StandardDayMinutes {
		t.Errorf("the week of somebody with none set = %v", got[unsaved].Week)
	}
}

func TestProjectPeopleAreListedInAStableOrder(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	p := &ProjectPeople{Names: map[uuid.UUID]string{a: "A", b: "B"}}
	first, second := p.IDs(), p.IDs()
	if len(first) != 2 || first[0] != second[0] || first[1] != second[1] {
		t.Errorf("IDs = %v then %v", first, second)
	}
}
