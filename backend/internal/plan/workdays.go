package plan

import (
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/availability"
)

// Workdays is what the load knows of who can work when. The zero value is
// Monday to Friday for everybody, with no holidays and nobody away.
type Workdays struct {
	// holidays are the default calendar's, which every team keeps unless its people say otherwise.
	holidays availability.Days
	members  map[uuid.UUID][]uuid.UUID
	weeks    map[uuid.UUID]map[string]int
	days     map[uuid.UUID]map[string]availability.PersonDay
}

// NewWorkdays indexes the default calendar's holidays, each team's members, and
// their weeks and days.
func NewWorkdays(holidays []availability.Holiday, members map[uuid.UUID][]uuid.UUID, people map[uuid.UUID]availability.Person) Workdays {
	w := Workdays{
		holidays: availability.DaysOf(holidays),
		members:  members,
		weeks:    map[uuid.UUID]map[string]int{},
		days:     map[uuid.UUID]map[string]availability.PersonDay{},
	}
	for id, p := range people {
		w.weeks[id] = p.Week
		byDay := make(map[string]availability.PersonDay, len(p.Days))
		for _, d := range p.Days {
			byDay[d.Day.Format(availability.DateLayout)] = d
		}
		w.days[id] = byDay
	}
	return w
}

var standardWeek = availability.StandardWeek()

// personWorks says whether a person's work is spread onto a day: a day they have
// minutes on. A day not read falls back to their week and the default calendar.
func (w Workdays) personWorks(id uuid.UUID, d time.Time) bool {
	key := d.Format(availability.DateLayout)
	if day, ok := w.days[id][key]; ok {
		return day.Minutes > 0
	}
	if h, off := w.holidays[key]; off && !h.HalfDay {
		return false
	}
	week, ok := w.weeks[id]
	if !ok {
		week = standardWeek
	}
	return availability.MinutesOn(week, d, nil) > 0
}

// works says whether work is spread onto a day: a day some member's week holds
// and the default calendar does not take whole. A team of nobody works Monday to Friday.
func (w Workdays) works(teamID *uuid.UUID, d time.Time) bool {
	if h, off := w.holidays[d.Format(availability.DateLayout)]; off && !h.HalfDay {
		return false
	}
	var members []uuid.UUID
	if teamID != nil {
		members = w.members[*teamID]
	}
	if len(members) == 0 {
		return availability.MinutesOn(standardWeek, d, nil) > 0
	}
	for _, m := range members {
		if availability.MinutesOn(w.weeks[m], d, nil) > 0 {
			return true
		}
	}
	return false
}

// teamTime is what a team's members have in some days, against what their
// weeks alone would give, with the days away and the holidays that made the difference.
type teamTime struct {
	available, nominal int
	away               float64
	holidays           int
}

// halfDay is what half a day away counts for.
const halfDay = 0.5

func (w Workdays) teamTime(teamID uuid.UUID, from, to time.Time) teamTime {
	return w.peopleTime(w.members[teamID], from, to)
}

// peopleTime is teamTime for any set of people, one person being a set too.
func (w Workdays) peopleTime(people []uuid.UUID, from, to time.Time) teamTime {
	var out teamTime
	holidays := map[string]bool{}
	for _, m := range people {
		for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
			key := d.Format(availability.DateLayout)
			day, ok := w.days[m][key]
			if !ok {
				continue
			}
			out.available += day.Minutes
			out.nominal += day.Nominal
			if day.Nominal == 0 {
				continue
			}
			switch {
			case day.Off == availability.OffAway && day.HalfDay:
				out.away += halfDay
			case day.Off == availability.OffAway:
				out.away++
			case day.Off == availability.OffHoliday:
				holidays[key] = true
			}
		}
	}
	out.holidays = len(holidays)
	return out
}

// scaled is a capacity shrunk by the share of the team's time that is left.
// A team with nobody, or nobody due to work, keeps what it said.
func (t teamTime) scaled(capacity float64) float64 {
	if t.nominal == 0 {
		return capacity
	}
	return roundHundredths(capacity * float64(t.available) / float64(t.nominal))
}

func roundHundredths(v float64) float64 {
	return math.Round(v*hundredths) / hundredths
}

// hundredths is the precision the plan shows points in.
const hundredths = 100

// hours turns minutes into hours, to a tenth.
func hours(minutes int) float64 {
	return math.Round(float64(minutes)/minutesPerHour*tenths) / tenths
}

const (
	minutesPerHour = 60
	tenths         = 10
)
