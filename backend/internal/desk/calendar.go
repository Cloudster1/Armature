package desk

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Calendar is when a desk is open: hours per weekday, days off, and the zone
// the hours are read in. A goal that counts working time asks it how much of
// a span was open.
type Calendar struct {
	Timezone string            `json:"timezone"`
	Hours    map[string][]Span `json:"hours"`
	Holidays []string          `json:"holidays"`
	loc      *time.Location
}

// Span is one open stretch of a day, written as HH:MM.
type Span struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// weekdays are the keys of Hours, Monday first as a week is written.
var weekdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

var weekdayKey = map[time.Weekday]string{
	time.Monday: "mon", time.Tuesday: "tue", time.Wednesday: "wed", time.Thursday: "thu",
	time.Friday: "fri", time.Saturday: "sat", time.Sunday: "sun",
}

// DefaultCalendar is nine to five, Monday to Friday, in UTC.
func DefaultCalendar() Calendar {
	hours := map[string][]Span{}
	for _, d := range weekdays[:5] {
		hours[d] = []Span{{From: "09:00", To: "17:00"}}
	}
	return Calendar{Timezone: "UTC", Hours: hours, Holidays: []string{}}
}

// Validate refuses hours that cannot be read.
func (c *Calendar) Validate() error {
	if c.Timezone == "" {
		c.Timezone = "UTC"
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return fmt.Errorf("%q is not a time zone", c.Timezone)
	}
	c.loc = loc
	if c.Hours == nil {
		c.Hours = map[string][]Span{}
	}
	for day, spans := range c.Hours {
		known := false
		for _, w := range weekdays {
			if w == day {
				known = true
			}
		}
		if !known {
			return fmt.Errorf("%q is not a weekday; use mon to sun", day)
		}
		for _, s := range spans {
			from, err := minuteOf(s.From)
			if err != nil {
				return err
			}
			to, err := minuteOf(s.To)
			if err != nil {
				return err
			}
			if to <= from {
				return fmt.Errorf("%s: %s to %s ends before it starts", day, s.From, s.To)
			}
		}
	}
	if c.Holidays == nil {
		c.Holidays = []string{}
	}
	for _, h := range c.Holidays {
		if _, err := time.Parse("2006-01-02", h); err != nil {
			return fmt.Errorf("%q is not a day written as YYYY-MM-DD", h)
		}
	}
	return nil
}

func minuteOf(hhmm string) (int, error) {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return 0, fmt.Errorf("%q is not a time written as HH:MM", hhmm)
	}
	return t.Hour()*60 + t.Minute(), nil
}

func (c *Calendar) location() *time.Location {
	if c.loc == nil {
		if loc, err := time.LoadLocation(c.Timezone); err == nil {
			c.loc = loc
		} else {
			c.loc = time.UTC
		}
	}
	return c.loc
}

func (c *Calendar) isHoliday(day time.Time) bool {
	key := day.Format("2006-01-02")
	for _, h := range c.Holidays {
		if h == key {
			return true
		}
	}
	return false
}

// openSpans are the open stretches of one calendar day, as instants.
func (c *Calendar) openSpans(day time.Time) []struct{ from, to time.Time } {
	if c.isHoliday(day) {
		return nil
	}
	var out []struct{ from, to time.Time }
	spans := c.Hours[weekdayKey[day.Weekday()]]
	sort.Slice(spans, func(i, j int) bool { return spans[i].From < spans[j].From })
	for _, s := range spans {
		from, err1 := minuteOf(s.From)
		to, err2 := minuteOf(s.To)
		if err1 != nil || err2 != nil {
			continue
		}
		start := time.Date(day.Year(), day.Month(), day.Day(), from/60, from%60, 0, 0, c.location())
		end := time.Date(day.Year(), day.Month(), day.Day(), to/60, to%60, 0, 0, c.location())
		out = append(out, struct{ from, to time.Time }{start, end})
	}
	return out
}

// WorkingDuration is how much of the span from a to b the desk was open.
func (c *Calendar) WorkingDuration(a, b time.Time) time.Duration {
	if !b.After(a) {
		return 0
	}
	loc := c.location()
	a, b = a.In(loc), b.In(loc)
	var total time.Duration
	for day := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, loc); day.Before(b); day = day.AddDate(0, 0, 1) {
		for _, s := range c.openSpans(day) {
			from, to := s.from, s.to
			if from.Before(a) {
				from = a
			}
			if to.After(b) {
				to = b
			}
			if to.After(from) {
				total += to.Sub(from)
			}
		}
	}
	return total
}

// AddWorking is the instant at which the desk will have been open for the
// given time after start: the deadline a goal turns into.
func (c *Calendar) AddWorking(start time.Time, d time.Duration) time.Time {
	loc := c.location()
	at := start.In(loc)
	if d <= 0 {
		return at
	}
	// A year of days is the most anyone waits; past that, the calendar is closed.
	for i := 0; i < 366; i++ {
		day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, i)
		for _, s := range c.openSpans(day) {
			from, to := s.from, s.to
			if from.Before(at) {
				from = at
			}
			if !to.After(from) {
				continue
			}
			if open := to.Sub(from); open >= d {
				return from.Add(d)
			} else {
				d -= open
			}
		}
	}
	return at.AddDate(1, 0, 0)
}

// ErrNoCalendar is returned when a project has not set its hours.
var ErrNoCalendar = errors.New("this project has no business hours yet")

func parseCalendar(timezone string, hours []byte, holidays []string) (*Calendar, error) {
	c := &Calendar{Timezone: timezone, Holidays: holidays}
	if err := json.Unmarshal(hours, &c.Hours); err != nil {
		return nil, err
	}
	if c.Hours == nil {
		c.Hours = map[string][]Span{}
	}
	if c.Holidays == nil {
		c.Holidays = []string{}
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// String is the hours in a sentence, for the goals page.
func (c *Calendar) String() string {
	var parts []string
	for _, d := range weekdays {
		if spans := c.Hours[d]; len(spans) > 0 {
			var words []string
			for _, s := range spans {
				words = append(words, s.From+" to "+s.To)
			}
			parts = append(parts, strings.ToUpper(d[:1])+d[1:]+" "+strings.Join(words, ", "))
		}
	}
	if len(parts) == 0 {
		return "never open"
	}
	return strings.Join(parts, "; ") + " (" + c.Timezone + ")"
}
