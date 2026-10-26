package availability

import (
	"reflect"
	"testing"
)

func TestTheLatestYearIsCarriedToTheNextOnTheSameDates(t *testing.T) {
	days := []Holiday{
		{Day: "2026-01-01", Name: "New Year's Day"},
		{Day: "2027-02-28", Name: "End of February"},
		{Day: "2027-05-01", Name: "Labour Day"},
		{Day: "2027-12-24", Name: "Christmas Eve", HalfDay: true},
		{Day: "2028-01-01", Name: "Already there"},
	}
	from, ok := LatestYear(days)
	if !ok || from != 2028 {
		t.Fatalf("latest year = %d, %v; want 2028", from, ok)
	}

	got := NextYear(days, 2027)
	want := []Holiday{
		{Day: "2028-02-28", Name: "End of February"},
		{Day: "2028-05-01", Name: "Labour Day"},
		{Day: "2028-12-24", Name: "Christmas Eve", HalfDay: true},
	}
	if got.From != 2027 || got.To != 2028 || !reflect.DeepEqual(got.Days, want) {
		t.Errorf("proposal = %+v, want 2027 to 2028 with %+v", got, want)
	}
	if len(got.Skipped) != 0 {
		t.Errorf("skipped = %+v, want none", got.Skipped)
	}
}

func TestADayTheNextYearHoldsIsLeftAsItIs(t *testing.T) {
	got := NextYear([]Holiday{
		{Day: "2026-12-25", Name: "Christmas Day"},
		{Day: "2026-12-26", Name: "Boxing Day"},
		{Day: "2027-12-25", Name: "Weihnachten", HalfDay: true},
	}, 2026)
	if want := []Holiday{{Day: "2027-12-26", Name: "Boxing Day"}}; !reflect.DeepEqual(got.Days, want) {
		t.Errorf("days = %+v, want only %+v: 2027-12-25 is already held", got.Days, want)
	}
}

func TestTwentyNinthFebruaryIsSkippedInACommonYear(t *testing.T) {
	leap := []Holiday{{Day: "2028-02-28", Name: "Eve"}, {Day: "2028-02-29", Name: "Leap Day"}, {Day: "2028-03-01", Name: "March"}}
	got := NextYear(leap, 2028)
	if want := []Holiday{{Day: "2029-02-28", Name: "Eve"}, {Day: "2029-03-01", Name: "March"}}; !reflect.DeepEqual(got.Days, want) {
		t.Errorf("days = %+v, want %+v", got.Days, want)
	}
	if want := []Holiday{{Day: "2028-02-29", Name: "Leap Day"}}; !reflect.DeepEqual(got.Skipped, want) {
		t.Errorf("skipped = %+v, want %+v", got.Skipped, want)
	}

	// Into a leap year nothing is lost, and nothing is invented for the 29th.
	into := NextYear([]Holiday{{Day: "2027-02-28", Name: "Eve"}}, 2027)
	if want := []Holiday{{Day: "2028-02-28", Name: "Eve"}}; !reflect.DeepEqual(into.Days, want) || len(into.Skipped) != 0 {
		t.Errorf("into a leap year = %+v", into)
	}
}

func TestAnEmptyCalendarHasNoYearToCopy(t *testing.T) {
	if _, ok := LatestYear(nil); ok {
		t.Error("an empty calendar has a latest year")
	}
	got := NextYear(nil, 2026)
	if got.Days == nil || got.Skipped == nil || len(got.Days) != 0 {
		t.Errorf("proposal = %+v, want empty lists rather than null", got)
	}
}
