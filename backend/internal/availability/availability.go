// Package availability says which days somebody can work: named holiday
// calendars, since a spread out team does not share its days off, and a week per person.
package availability

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	minutesPerHour   = 60
	hoursPerDay      = 24
	standardDayHours = 8
	// MinutesPerDay is the most a day can hold.
	MinutesPerDay = hoursPerDay * minutesPerHour
	// StandardDayMinutes is a working day in the standard week, Monday to Friday.
	StandardDayMinutes = standardDayHours * minutesPerHour
	// standardWorkdays is how many days, from Monday, the standard week works.
	standardWorkdays = 5
	// MaxNameLength bounds a calendar's name and a holiday's, as the database does.
	MaxNameLength = 100
	// MaxDays is the most days one calendar holds, and so the most one import brings.
	MaxDays = 1000
	// DateLayout is how a day is written, on the wire and in a calendar.
	DateLayout = "2006-01-02"
)

// Weekdays are the keys of a working week, Monday first as a week is written.
var Weekdays = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

var weekdayKey = map[time.Weekday]string{
	time.Monday: "mon", time.Tuesday: "tue", time.Wednesday: "wed", time.Thursday: "thu",
	time.Friday: "fri", time.Saturday: "sat", time.Sunday: "sun",
}

// HolidayCalendar is a named set of days off.
type HolidayCalendar struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// Default is the calendar of everybody who has not been given another.
	Default bool `json:"default"`
	// DayCount is always filled in; Days only when the calendar is read on its own.
	DayCount int       `json:"dayCount"`
	Days     []Holiday `json:"days,omitempty"`
	// PeopleCount is how many were given this calendar by name, not by default.
	PeopleCount int       `json:"peopleCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Holiday is one day off on a calendar.
type Holiday struct {
	// Day is written YYYY-MM-DD: a day off is a date, not an instant.
	Day     string `json:"day"`
	Name    string `json:"name"`
	HalfDay bool   `json:"halfDay"`
}

// CalendarRef is the shape a calendar takes when it hangs off a person.
type CalendarRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// WorkingWeek is how much one person works on each weekday, and which
// calendar's days they have off.
type WorkingWeek struct {
	UserID uuid.UUID `json:"userId"`
	// CalendarID is the calendar the person was given; null follows the default.
	CalendarID *uuid.UUID `json:"calendarId"`
	// Calendar is the one that answers for them, which for null is the default.
	Calendar *CalendarRef `json:"calendar"`
	// Minutes holds every weekday, mon to sun.
	Minutes map[string]int `json:"minutes"`
	// Saved is false for somebody nobody has set a week for, who works the standard one.
	Saved bool `json:"saved"`
}

var (
	// ErrNotFound is returned for a calendar that is not in the caller's organization.
	ErrNotFound = errors.New("holiday calendar not found")
	// ErrNameTaken is returned when the organization has a calendar by that name.
	ErrNameTaken = errors.New("this organization already has a holiday calendar by that name")
	// ErrDefaultCalendar is returned for deleting the default, or unmaking it
	// without naming another: somebody always needs a calendar to fall back to.
	ErrDefaultCalendar = errors.New("the default calendar stays until another one is made the default")
	// ErrInvalid wraps a refusal of what was sent; the words after it are for the person.
	ErrInvalid = errors.New("holiday calendar input refused")
	// ErrBadFile wraps a refusal of an uploaded calendar file, likewise.
	ErrBadFile = errors.New("calendar file refused")
	// ErrNotAMember is returned for a person who is not in the organization.
	ErrNotAMember = errors.New("that person is not a member of this organization")
	// ErrCustomer is returned for a portal customer, who has no working week here.
	ErrCustomer = errors.New("a portal customer has no working week")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, args...)...)
}

// StandardWeek is eight hours Monday to Friday and the weekend off.
func StandardWeek() map[string]int {
	week := map[string]int{}
	for i, day := range Weekdays {
		week[day] = 0
		if i < standardWorkdays {
			week[day] = StandardDayMinutes
		}
	}
	return week
}

// NormalizeWeek checks a week and fills in every weekday, a day left out
// being a day not worked. A nil week is the standard one.
func NormalizeWeek(in map[string]int) (map[string]int, error) {
	if in == nil {
		return StandardWeek(), nil
	}
	out := map[string]int{}
	for _, day := range Weekdays {
		out[day] = 0
	}
	for day, minutes := range in {
		if _, known := out[day]; !known {
			return nil, invalid("%q is not a weekday; use mon, tue, wed, thu, fri, sat or sun", day)
		}
		if minutes < 0 || minutes > MinutesPerDay {
			return nil, invalid("%s has %d minutes; a day holds from 0 to %d", day, minutes, MinutesPerDay)
		}
		out[day] = minutes
	}
	return out, nil
}

// Days is a calendar's holidays by the day they fall on.
type Days map[string]Holiday

// DaysOf indexes holidays by day.
func DaysOf(holidays []Holiday) Days {
	out := Days{}
	for _, h := range holidays {
		out[h.Day] = h
	}
	return out
}

// MinutesOn is how long somebody with this week can work on a day: what the
// weekday holds, nothing on a holiday, and half of it on a half day.
func MinutesOn(week map[string]int, day time.Time, off Days) int {
	minutes := week[weekdayKey[day.Weekday()]]
	h, isHoliday := off[day.Format(DateLayout)]
	switch {
	case !isHoliday:
		return minutes
	case h.HalfDay:
		return minutes / 2
	default:
		return 0
	}
}

// MinutesOn is how long this person can work on a day, given their calendar's days.
func (w WorkingWeek) MinutesOn(day time.Time, off Days) int {
	return MinutesOn(w.Minutes, day, off)
}

// NormalizeName trims a calendar's or a holiday's name and holds it to the bounds.
func NormalizeName(name, what string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", invalid("a %s needs a name", what)
	case utf8.RuneCountInString(name) > MaxNameLength:
		return "", invalid("a %s's name is at most %d characters; shorten it", what, MaxNameLength)
	}
	return name, nil
}

// NormalizeDays checks a calendar's whole list of days and puts it in order.
func NormalizeDays(in []Holiday) ([]Holiday, error) {
	if len(in) > MaxDays {
		return nil, invalid("a calendar holds at most %d days; remove some, or keep past years in a calendar of their own", MaxDays)
	}
	seen := map[string]bool{}
	out := make([]Holiday, 0, len(in))
	for _, h := range in {
		day, err := time.Parse(DateLayout, strings.TrimSpace(h.Day))
		if err != nil {
			return nil, invalid("%q is not a day; write it as YYYY-MM-DD", h.Day)
		}
		key := day.Format(DateLayout)
		if seen[key] {
			return nil, invalid("%s is listed twice; keep one of them", key)
		}
		seen[key] = true
		name, err := NormalizeName(h.Name, "holiday")
		if err != nil {
			return nil, err
		}
		out = append(out, Holiday{Day: key, Name: name, HalfDay: h.HalfDay})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Day < out[j].Day })
	return out, nil
}
