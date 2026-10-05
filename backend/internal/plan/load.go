package plan

import (
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/team"
)

// LoadWeek is one team's week: what is scheduled into it against what the team
// can take in it.
type LoadWeek struct {
	Start time.Time `json:"start"`
	// Load is the estimated work falling in the week, each scheduled issue's
	// estimate spread evenly over the working days it covers.
	Load float64 `json:"load"`
	// Capacity is what the team said, shrunk by the share of its members' time
	// that holidays and absences take; NominalCapacity is what it said.
	Capacity        *float64 `json:"capacity,omitempty"`
	NominalCapacity *float64 `json:"nominalCapacity,omitempty"`
	// DaysAway counts the members' working days away, a half day as a half,
	// and Holidays the days some member has a holiday on.
	DaysAway float64 `json:"daysAway,omitempty"`
	Holidays int     `json:"holidays,omitempty"`
	// Issues counts the rows touching the week, Unestimated those of them that
	// nobody has sized: work that is there but not in the number.
	Issues      int `json:"issues"`
	Unestimated int `json:"unestimated"`
}

// The kinds of row the load has.
const (
	LoadTeam       = "team"
	LoadUnassigned = "unassigned"
	LoadTotal      = "total"
)

// TeamLoad is one row of the load: a team, the work no team carries, or the
// whole project.
type TeamLoad struct {
	TeamID         *uuid.UUID `json:"teamId,omitempty"`
	Team           string     `json:"team"`
	Kind           string     `json:"kind"`
	WeeklyCapacity *float64   `json:"weeklyCapacity,omitempty"`
	Weeks          []LoadWeek `json:"weeks"`
	// Unscheduled counts the row's work that has no dates and so falls in no
	// week; it is counted rather than spread, since a guess would be a lie.
	Unscheduled int `json:"unscheduled"`
}

// OverBy is how far past its capacity the week is loaded, or zero.
func (w LoadWeek) OverBy() float64 {
	if w.Capacity == nil || w.Load <= *w.Capacity+loadTolerance {
		return 0
	}
	return w.Load - *w.Capacity
}

// loadTolerance keeps a week spread into thirds from reading as over its
// capacity by a rounding error.
const loadTolerance = 1e-6

// Load is the plan's resource reading: the weeks of the window, and a row per
// team, one for the unassigned work, and the total.
type Load struct {
	Weeks []time.Time `json:"weeks"`
	Rows  []TeamLoad  `json:"rows"`
}

// WarnOverLoad is a team scheduled beyond what it can take in a week.
const WarnOverLoad = "over-load"

const (
	daysPerWeek = 7
	hoursPerDay = 24
)

// Weeks returns the Monday of every week the window touches, oldest first.
func Weeks(from, to time.Time) []time.Time {
	var out []time.Time
	for w := mondayOf(from); !w.After(midnight(to)); w = w.AddDate(0, 0, daysPerWeek) {
		out = append(out, w)
	}
	return out
}

// mondayOf is the Monday on or before a day, which is how the calendar's own
// week ticks are placed.
func mondayOf(t time.Time) time.Time {
	t = midnight(t)
	back := (int(t.Weekday()) + daysPerWeek - 1) % daysPerWeek
	return t.AddDate(0, 0, -back)
}

// Loads reads the load per team over the window. Work is counted once: a row
// whose ancestor is on the same team contributes through that ancestor, as it
// does in a sprint. A child on another team counts for its own team.
func Loads(items []Item, teams []team.Team, from, to time.Time, work Workdays) Load {
	weeks := Weeks(from, to)
	byTeam := topmostByTeam(items)

	rowFor := func(id *uuid.UUID, name, kind string, capacity *float64) TeamLoad {
		row := TeamLoad{TeamID: id, Team: name, Kind: kind, WeeklyCapacity: capacity, Weeks: make([]LoadWeek, len(weeks))}
		for i, start := range weeks {
			row.Weeks[i] = LoadWeek{Start: start}
			if id != nil {
				weekOf(&row.Weeks[i], work.teamTime(*id, start, start.AddDate(0, 0, daysPerWeek-1)), capacity)
			}
		}
		key := ""
		if id != nil {
			key = id.String()
		}
		works := func(d time.Time) bool { return work.works(id, d) }
		for _, item := range byTeam[key] {
			spread(&row, weeks, item, works)
		}
		return row
	}

	out := Load{Weeks: weeks, Rows: make([]TeamLoad, 0, len(teams)+2)}
	for _, t := range teams {
		id := t.ID
		out.Rows = append(out.Rows, rowFor(&id, t.Name, LoadTeam, t.WeeklyCapacity))
	}
	out.Rows = append(out.Rows, rowFor(nil, "Unassigned", LoadUnassigned, nil))

	total := TeamLoad{Team: "Total", Kind: LoadTotal, Weeks: make([]LoadWeek, len(weeks))}
	for i, start := range weeks {
		total.Weeks[i] = LoadWeek{Start: start}
	}
	for _, row := range out.Rows {
		total.Unscheduled += row.Unscheduled
		for i, w := range row.Weeks {
			sum := &total.Weeks[i]
			sum.Load += w.Load
			sum.Issues += w.Issues
			sum.Unestimated += w.Unestimated
			sum.DaysAway += w.DaysAway
			sum.Holidays = max(sum.Holidays, w.Holidays)
			if w.Capacity != nil {
				sum.Capacity = ptrTo(valueOr(sum.Capacity) + *w.Capacity)
				sum.NominalCapacity = ptrTo(valueOr(sum.NominalCapacity) + *w.NominalCapacity)
			}
		}
		if row.WeeklyCapacity != nil {
			total.WeeklyCapacity = ptrTo(valueOr(total.WeeklyCapacity) + *row.WeeklyCapacity)
		}
	}
	for i := range total.Weeks {
		if c := total.Weeks[i].Capacity; c != nil {
			*c = roundHundredths(*c)
		}
	}
	out.Rows = append(out.Rows, total)
	// Spreading an estimate over days leaves float dust; the plan shows
	// hundredths, so the load is what it shows.
	for r := range out.Rows {
		for w := range out.Rows[r].Weeks {
			out.Rows[r].Weeks[w].Load = math.Round(out.Rows[r].Weeks[w].Load*100) / 100
		}
	}
	return out
}

// weekOf fills in a team's week from its members' time: the capacity they
// said, shrunk by what holidays and absences take, and why.
func weekOf(week *LoadWeek, t teamTime, capacity *float64) {
	week.DaysAway, week.Holidays = t.away, t.holidays
	if capacity == nil {
		return
	}
	nominal, scaled := *capacity, t.scaled(*capacity)
	week.NominalCapacity, week.Capacity = &nominal, &scaled
}

// spread puts a row's work on the days its team works, and work scheduled on
// no working day at all on its calendar days, so that no point disappears.
func spread(row *TeamLoad, weeks []time.Time, item Item, works func(time.Time) bool) {
	if !item.Scheduled() {
		row.Unscheduled++
		return
	}
	start, due := midnight(*item.Start), midnight(*item.Due)
	var all, working []time.Time
	for d := start; !d.After(due); d = d.AddDate(0, 0, 1) {
		all = append(all, d)
		if works(d) {
			working = append(working, d)
		}
	}
	if len(all) == 0 {
		all = []time.Time{start}
	}
	if len(working) == 0 {
		working = all
	}
	if item.Estimate != nil {
		perDay := *item.Estimate / float64(len(working))
		for _, d := range working {
			if i := weekIndex(weeks, d); i >= 0 {
				row.Weeks[i].Load += perDay
			}
		}
	}
	touched := map[int]bool{}
	for _, d := range all {
		i := weekIndex(weeks, d)
		if i < 0 || touched[i] {
			continue
		}
		touched[i] = true
		row.Weeks[i].Issues++
		if item.Estimate == nil {
			row.Weeks[i].Unestimated++
		}
	}
}

func ptrTo(v float64) *float64 { return &v }

func valueOr(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// weekIndex is which of the weeks a day falls in, or -1 outside the window.
func weekIndex(weeks []time.Time, d time.Time) int {
	if len(weeks) == 0 {
		return -1
	}
	i := int(d.Sub(weeks[0]).Hours() / hoursPerDay / daysPerWeek)
	if i < 0 || i >= len(weeks) {
		return -1
	}
	return i
}

// topmostByTeam groups the rows that carry each team's work: those on the
// team with no ancestor also on it. The empty key is work no team carries.
func topmostByTeam(items []Item) map[string][]Item {
	out := map[string][]Item{}
	var walk func(items []Item, claimed map[string]bool)
	walk = func(items []Item, claimed map[string]bool) {
		for _, item := range items {
			key := ""
			if item.Issue.TeamID != nil {
				key = item.Issue.TeamID.String()
			}
			below := claimed
			if !claimed[key] {
				out[key] = append(out[key], item)
				below = map[string]bool{}
				for k := range claimed {
					below[k] = true
				}
				below[key] = true
			}
			walk(item.Children, below)
		}
	}
	walk(items, map[string]bool{})
	return out
}

// CheckLoad reports each week a team is loaded beyond what it can take that
// week. A warning, not a refusal: the plan is a draft.
func CheckLoad(load Load) []Warning {
	var out []Warning
	for _, row := range load.Rows {
		if row.Kind != LoadTeam || row.WeeklyCapacity == nil {
			continue
		}
		for _, week := range row.Weeks {
			if week.OverBy() <= 0 {
				continue
			}
			message := fmt.Sprintf("%s is loaded with %s in the week of %s against a capacity of %s",
				row.Team, points(week.Load), week.Start.Format("2 Jan"), points(*week.Capacity))
			if week.NominalCapacity != nil && *week.NominalCapacity != *week.Capacity {
				message += fmt.Sprintf(", %s before holidays and absences", points(*week.NominalCapacity))
			}
			out = append(out, Warning{Team: row.Team, Kind: WarnOverLoad, Message: message})
		}
	}
	return out
}
