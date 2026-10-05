package availability

import (
	"sort"
	"time"

	"github.com/google/uuid"
)

// DayOff says why a day holds less than the week does, for whoever draws it.
type DayOff string

const (
	OffNone    DayOff = ""
	OffHoliday DayOff = "holiday"
	OffAway    DayOff = "away"
)

// halvesPerDay counts a day in halves, the smallest part a holiday or an absence takes.
const halvesPerDay = 2

// PersonDay is one day of one person: what they can work, what their week
// alone would have said, and why the two differ.
type PersonDay struct {
	Day     time.Time
	Minutes int
	// Nominal is the week's minutes for the weekday, before holidays and absences.
	Nominal int
	// Off is a whole holiday before an absence, and an absence before a half holiday:
	// a person away on a day off anyway is not away from anything.
	Off     DayOff
	Holiday string
	HalfDay bool
}

// Person is somebody's week and their days over a stretch.
type Person struct {
	Week map[string]int
	Days []PersonDay
}

// Schedule is what one person's days are read from.
type Schedule struct {
	Week map[string]int
	// Off is the holidays of the calendar the person keeps.
	Off  Days
	Away []Absence
}

// Day combines the week, the calendar and the absences for one day. A half
// holiday and a half day away together take the whole day.
func (s Schedule) Day(day time.Time) PersonDay {
	key := day.Format(DateLayout)
	out := PersonDay{Day: day, Nominal: s.Week[weekdayKey[day.Weekday()]]}
	taken := 0
	if h, ok := s.Off[key]; ok {
		taken = halves(h.HalfDay)
		out.Off, out.Holiday, out.HalfDay = OffHoliday, h.Name, h.HalfDay
	}
	if a, ok := awayOn(s.Away, key); ok && taken < halvesPerDay {
		taken = min(halvesPerDay, taken+halves(a.HalfDay))
		out.Off, out.Holiday, out.HalfDay = OffAway, "", a.HalfDay
	}
	out.Minutes = out.Nominal * (halvesPerDay - taken) / halvesPerDay
	return out
}

// Days is every day from one to another, both included.
func (s Schedule) Days(from, to time.Time) []PersonDay {
	var out []PersonDay
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		out = append(out, s.Day(d))
	}
	return out
}

func halves(half bool) int {
	if half {
		return 1
	}
	return halvesPerDay
}

// awayOn finds the absence covering a day; written days compare as text.
func awayOn(away []Absence, key string) (Absence, bool) {
	for _, a := range away {
		if a.StartsOn <= key && key <= a.EndsOn {
			return a, true
		}
	}
	return Absence{}, false
}

// stored is a person's member_schedule row, or its absence.
type stored struct {
	Week       map[string]int
	CalendarID *uuid.UUID
}

// combine gives everybody asked about their days. calendars holds every
// calendar there is; one that is gone is the default, as the database makes it too.
func combine(ids []uuid.UUID, rows map[uuid.UUID]stored, calendars map[uuid.UUID]Days, defaultID *uuid.UUID, away map[uuid.UUID][]Absence, from, to time.Time) map[uuid.UUID]Person {
	out := make(map[uuid.UUID]Person, len(ids))
	for _, id := range ids {
		row, saved := rows[id]
		if !saved {
			row = stored{Week: StandardWeek()}
		}
		off, kept := Days(nil), false
		if row.CalendarID != nil {
			off, kept = calendars[*row.CalendarID]
		}
		if !kept && defaultID != nil {
			off = calendars[*defaultID]
		}
		s := Schedule{Week: row.Week, Off: off, Away: away[id]}
		out[id] = Person{Week: row.Week, Days: s.Days(from, to)}
	}
	return out
}

// ProjectPeople is who works on a project over a stretch: the members of its
// teams, and whoever is assigned work scheduled in it.
type ProjectPeople struct {
	Names map[uuid.UUID]string
	// Teams is each team's members.
	Teams map[uuid.UUID][]uuid.UUID
}

// IDs is everybody, in a stable order.
func (p *ProjectPeople) IDs() []uuid.UUID {
	out := make([]uuid.UUID, 0, len(p.Names))
	for id := range p.Names {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// CalendarDay is a holiday together with the calendar it is on.
type CalendarDay struct {
	ID       uuid.UUID
	Calendar CalendarRef
	Default  bool
	Holiday
}
