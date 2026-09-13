package desk

import (
	"testing"
	"time"
)

func TestWorkingDurationSkipsNightsWeekendsAndHolidays(t *testing.T) {
	c := DefaultCalendar()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	// Friday 16:00 to Monday 10:00: one hour Friday, one hour Monday.
	friday := time.Date(2026, 9, 4, 16, 0, 0, 0, time.UTC)
	monday := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	if got := c.WorkingDuration(friday, monday); got != 2*time.Hour {
		t.Errorf("Friday 16:00 to Monday 10:00 = %s, want 2h", got)
	}
	c.Holidays = []string{"2026-09-07"}
	if got := c.WorkingDuration(friday, monday); got != time.Hour {
		t.Errorf("with Monday a holiday = %s, want 1h", got)
	}
	// Inside one day, plain.
	if got := c.WorkingDuration(friday, friday.Add(30*time.Minute)); got != 30*time.Minute {
		t.Errorf("half an hour inside a day = %s", got)
	}
	// Nothing before the start.
	if got := c.WorkingDuration(monday, friday); got != 0 {
		t.Errorf("a span that ends before it starts = %s", got)
	}
}

func TestAddWorkingLandsOnTheNextOpenMinute(t *testing.T) {
	c := DefaultCalendar()
	_ = c.Validate()
	friday := time.Date(2026, 9, 4, 16, 0, 0, 0, time.UTC)
	// Two working hours from Friday 16:00: one until 17:00, one on Monday from 09:00.
	if got := c.AddWorking(friday, 2*time.Hour); !got.Equal(time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("deadline = %s, want Monday 10:00", got)
	}
	// A start outside the hours counts from the next opening.
	saturday := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	if got := c.AddWorking(saturday, 30*time.Minute); !got.Equal(time.Date(2026, 9, 7, 9, 30, 0, 0, time.UTC)) {
		t.Errorf("from Saturday = %s, want Monday 09:30", got)
	}
}

func TestCalendarHonoursItsZoneAcrossDaylightSaving(t *testing.T) {
	c := DefaultCalendar()
	c.Timezone = "Europe/Berlin"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	// The night the clocks go back (25 Oct 2026): the open day is still eight hours.
	sunday := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	tuesday := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	if got := c.WorkingDuration(sunday, tuesday); got != 8*time.Hour {
		t.Errorf("Monday after the change = %s, want 8h", got)
	}
}

func TestCalendarValidation(t *testing.T) {
	bad := []Calendar{
		{Timezone: "Mars/Olympus"},
		{Timezone: "UTC", Hours: map[string][]Span{"funday": {{From: "09:00", To: "17:00"}}}},
		{Timezone: "UTC", Hours: map[string][]Span{"mon": {{From: "17:00", To: "09:00"}}}},
		{Timezone: "UTC", Hours: map[string][]Span{"mon": {{From: "9am", To: "5pm"}}}},
		{Timezone: "UTC", Holidays: []string{"25.12.2026"}},
	}
	for i := range bad {
		if err := bad[i].Validate(); err == nil {
			t.Errorf("case %d was accepted", i)
		}
	}
	def := DefaultCalendar()
	if s := def.String(); s == "" || s == "never open" {
		t.Errorf("default calendar reads %q", s)
	}
}
