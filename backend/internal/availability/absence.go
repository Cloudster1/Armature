package availability

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// MaxAbsenceDays is the longest one absence runs, both ends counted, as the database holds it.
	MaxAbsenceDays = 366
	// MaxAbsenceWindowDays is the widest stretch one listing of absences covers.
	MaxAbsenceWindowDays = 2 * MaxAbsenceDays
	// AbsenceDaysBehind and AbsenceDaysAhead are what a listing covers when it names no days.
	AbsenceDaysBehind = 31
	AbsenceDaysAhead  = 365
)

// Absence is the days one person is away. It says when and never why: a
// reason is nobody else's business, and it is not asked for.
type Absence struct {
	ID       uuid.UUID `json:"id"`
	UserID   uuid.UUID `json:"userId"`
	UserName string    `json:"userName"`
	// StartsOn and EndsOn are written YYYY-MM-DD, and both days are away.
	StartsOn string `json:"startsOn"`
	EndsOn   string `json:"endsOn"`
	// HalfDay is half of a single day away.
	HalfDay bool `json:"halfDay"`
}

// AbsenceInput is a new absence, or what an edit changes. A nil EndsOn on a
// new absence is the day it starts; on an edit, nil leaves a field alone.
type AbsenceInput struct {
	StartsOn *string
	EndsOn   *string
	HalfDay  *bool
}

var (
	// ErrAbsenceNotFound is returned for an absence that is not in the caller's organization.
	ErrAbsenceNotFound = errors.New("absence not found")
	// ErrOverlap is returned when the person is already away on one of the days.
	ErrOverlap = errors.New("the person is already away on some of those days")
	// ErrMayNotRecord is returned to somebody who neither is the person nor
	// administers them nor manages one of their teams.
	ErrMayNotRecord = errors.New("only the person, an administrator or a manager of one of their teams records their absences")
)

// ParseDay reads a YYYY-MM-DD day; what names it goes into the refusal.
func ParseDay(text, what string) (time.Time, error) {
	day, err := time.Parse(DateLayout, strings.TrimSpace(text))
	if err != nil {
		return time.Time{}, invalid("%s %q is not a day; write it as YYYY-MM-DD", what, text)
	}
	return day, nil
}

// daysBetween counts the days from one date to another, the first not counted.
func daysBetween(from, to time.Time) int {
	return int(to.Sub(from).Hours() / hoursPerDay)
}

// CheckAbsence holds an absence to the bounds the database holds it to.
func CheckAbsence(startsOn, endsOn string, halfDay bool) (Absence, error) {
	start, err := ParseDay(startsOn, "the first day")
	if err != nil {
		return Absence{}, err
	}
	end, err := ParseDay(endsOn, "the last day")
	if err != nil {
		return Absence{}, err
	}
	switch {
	case end.Before(start):
		return Absence{}, invalid("the last day is before the first; swap them")
	case daysBetween(start, end) >= MaxAbsenceDays:
		return Absence{}, invalid("one absence runs at most %d days; record a longer one in parts", MaxAbsenceDays)
	case halfDay && !end.Equal(start):
		return Absence{}, invalid("only a single day can be a half day; make the last day the first, or take the half day off")
	}
	return Absence{StartsOn: start.Format(DateLayout), EndsOn: end.Format(DateLayout), HalfDay: halfDay}, nil
}

// AbsenceWindow is the stretch of days a listing covers: from and to as
// asked, or a month behind and a year ahead of today when not.
func AbsenceWindow(from, to string, today time.Time) (time.Time, time.Time, error) {
	start := today.AddDate(0, 0, -AbsenceDaysBehind)
	end := today.AddDate(0, 0, AbsenceDaysAhead)
	var err error
	if from != "" {
		if start, err = ParseDay(from, "from"); err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if to != "" {
		if end, err = ParseDay(to, "to"); err != nil {
			return time.Time{}, time.Time{}, err
		}
	} else if from != "" {
		end = start.AddDate(0, 0, AbsenceDaysBehind+AbsenceDaysAhead)
	}
	switch {
	case end.Before(start):
		return time.Time{}, time.Time{}, invalid("to is before from; swap them")
	case daysBetween(start, end) >= MaxAbsenceWindowDays:
		return time.Time{}, time.Time{}, invalid("one listing covers at most %d days; ask for a shorter stretch", MaxAbsenceWindowDays)
	}
	return start, end, nil
}
