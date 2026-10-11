package calendar

import "testing"

// The days off are read over the six weeks the page draws, so the padding days
// of the neighbouring months carry their holidays and absences too.
func TestAMonthIsReadOverTheSixWeeksItsGridDraws(t *testing.T) {
	cases := []struct {
		name        string
		year, month int
		first, last string
	}{
		{name: "a month starting on a Tuesday", year: 2026, month: 9, first: "2026-08-31", last: "2026-10-11"},
		{name: "a month starting on a Monday", year: 2026, month: 6, first: "2026-06-01", last: "2026-07-12"},
		{name: "a month starting on a Sunday", year: 2026, month: 3, first: "2026-02-23", last: "2026-04-05"},
		{name: "across a year's end", year: 2027, month: 1, first: "2026-12-28", last: "2027-02-07"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			first, last := gridSpan(c.year, c.month)
			if got := first.Format(dateLayout); got != c.first {
				t.Errorf("the grid's first day = %s, want %s", got, c.first)
			}
			if got := last.Format(dateLayout); got != c.last {
				t.Errorf("the grid's last day = %s, want %s", got, c.last)
			}
			if days := int(last.Sub(first).Hours()/24) + 1; days != GridDays {
				t.Errorf("the grid spans %d days, want %d", days, GridDays)
			}
		})
	}
}
