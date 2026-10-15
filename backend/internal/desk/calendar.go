package desk

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Calendar is when a desk is open: hours per weekday, days off, and the zone
// the hours are read in. A goal that counts working time asks it how much of
// a span was open.
type Calendar struct {
	Timezone string            `json:"timezone"`
	Hours    map[string][]Span `json:"hours"`
	// Holidays are the desk's own days off, closed beside the holiday calendar's.
	Holidays []string `json:"holidays"`
	// HolidayCalendarID is the holiday calendar whose days close the desk too,
	// null for none; its name is sent back and never read.
	HolidayCalendarID   *uuid.UUID `json:"holidayCalendarId"`
	HolidayCalendarName *string    `json:"holidayCalendarName"`
	// linked are that calendar's days as they stand when the clock is read,
	// true for a half day, so a change to it counts from then on.
	linked map[string]bool
	loc    *time.Location
}

// Span is one open stretch of a day, written as HH:MM.
type Span struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// dayLayout is how a day off is written.
const dayLayout = "2006-01-02"

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

// Validate refuses hours that cannot be read, and a day whose spans overlap,
// so what is saved says plainly when the desk is open.
func (c *Calendar) Validate() error {
	if err := c.readable(); err != nil {
		return err
	}
	for _, day := range weekdays {
		if err := noOverlaps(day, c.Hours[day]); err != nil {
			return err
		}
	}
	return nil
}

// readable is what a saved calendar must pass to be read back; one saved with
// overlaps before they were refused is still read, and counted merged.
func (c *Calendar) readable() error {
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
				return fmt.Errorf("%s: %s to %s ends before it starts", dayName(day), s.From, s.To)
			}
		}
	}
	if c.Holidays == nil {
		c.Holidays = []string{}
	}
	for _, h := range c.Holidays {
		if _, err := time.Parse(dayLayout, h); err != nil {
			return fmt.Errorf("%q is not a day written as YYYY-MM-DD", h)
		}
	}
	return nil
}

// noOverlaps names the first span of a day that starts before an earlier one
// has ended. Spans that only touch are fine: the day reads as one stretch.
func noOverlaps(day string, spans []Span) error {
	sorted := sortedSpans(spans)
	var held Span
	heldTo := -1
	for _, s := range sorted {
		from, _ := minuteOf(s.From)
		to, _ := minuteOf(s.To)
		if from < heldTo {
			return fmt.Errorf("%s: %s to %s overlaps %s to %s; join them into one span", dayName(day), s.From, s.To, held.From, held.To)
		}
		if to > heldTo {
			held, heldTo = s, to
		}
	}
	return nil
}

// sortedSpans is a copy in the order the day runs, leaving the saved hours as written.
func sortedSpans(spans []Span) []Span {
	out := append([]Span(nil), spans...)
	sort.SliceStable(out, func(i, j int) bool {
		a, _ := minuteOf(out[i].From)
		b, _ := minuteOf(out[j].From)
		return a < b
	})
	return out
}

func dayName(key string) string {
	for wd, k := range weekdayKey {
		if k == key {
			return wd.String()
		}
	}
	return key
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

// link takes the named holiday calendar's whole and half days.
func (c *Calendar) link(whole, half []string) {
	c.linked = map[string]bool{}
	for _, d := range whole {
		c.linked[d] = false
	}
	for _, d := range half {
		c.linked[d] = true
	}
}

// closedOn says whether a day is closed, and whether only its second half is.
func (c *Calendar) closedOn(day time.Time) (closed, halfDay bool) {
	key := day.Format(dayLayout)
	for _, h := range c.Holidays {
		if h == key {
			return true, false
		}
	}
	halfDay, linked := c.linked[key]
	return linked && !halfDay, halfDay
}

// openSpans are the open stretches of one calendar day, as instants: none on
// a day off, and the first half of the day's open time on a half day.
func (c *Calendar) openSpans(day time.Time) []openSpan {
	closed, halfDay := c.closedOn(day)
	if closed {
		return nil
	}
	spans := c.mergedSpans(day)
	if halfDay {
		return firstHalf(spans)
	}
	return spans
}

// openSpan is one open stretch of a particular day.
type openSpan struct{ from, to time.Time }

// firstHalf keeps the first half of a day's open time, as a person on a half
// day off works half of what their week holds.
func firstHalf(spans []openSpan) []openSpan {
	var open time.Duration
	for _, s := range spans {
		open += s.to.Sub(s.from)
	}
	left := open / 2
	var out []openSpan
	for _, s := range spans {
		if left <= 0 {
			break
		}
		if s.to.Sub(s.from) > left {
			s.to = s.from.Add(left)
		}
		left -= s.to.Sub(s.from)
		out = append(out, s)
	}
	return out
}

// mergedSpans are the day's hours as instants. Spans that overlap are merged,
// so a minute saved twice is still open only once.
func (c *Calendar) mergedSpans(day time.Time) []openSpan {
	var out []openSpan
	for _, s := range sortedSpans(c.Hours[weekdayKey[day.Weekday()]]) {
		from, err1 := minuteOf(s.From)
		to, err2 := minuteOf(s.To)
		if err1 != nil || err2 != nil {
			continue
		}
		start := time.Date(day.Year(), day.Month(), day.Day(), from/60, from%60, 0, 0, c.location())
		end := time.Date(day.Year(), day.Month(), day.Day(), to/60, to%60, 0, 0, c.location())
		if n := len(out); n > 0 && !start.After(out[n-1].to) {
			if end.After(out[n-1].to) {
				out[n-1].to = end
			}
			continue
		}
		out = append(out, openSpan{start, end})
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

// ErrNoHolidayCalendar is returned for hours that name a holiday calendar that is not there.
var ErrNoHolidayCalendar = errors.New("that holiday calendar is not there")

// parseCalendar reads a saved calendar with the named holiday calendar's whole and half days.
func parseCalendar(timezone string, hours []byte, holidays, wholeDays, halfDays []string) (*Calendar, error) {
	c := &Calendar{Timezone: timezone, Holidays: holidays}
	c.link(wholeDays, halfDays)
	if err := json.Unmarshal(hours, &c.Hours); err != nil {
		return nil, err
	}
	if c.Hours == nil {
		c.Hours = map[string][]Span{}
	}
	if c.Holidays == nil {
		c.Holidays = []string{}
	}
	if err := c.readable(); err != nil {
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
