package plan

import (
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/team"
)

// LoadWeek is one team's week: what is scheduled into it against what the team
// said it could take.
type LoadWeek struct {
	Start time.Time `json:"start"`
	// Load is the estimated work falling in the week, each scheduled issue's
	// estimate spread evenly over the days it covers.
	Load     float64  `json:"load"`
	Capacity *float64 `json:"capacity,omitempty"`
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

// WarnOverLoad is a team scheduled beyond what it said it could take in a week.
const WarnOverLoad = "over-load"

// Weeks returns the Monday of every week the window touches, oldest first.
func Weeks(from, to time.Time) []time.Time {
	var out []time.Time
	for w := mondayOf(from); !w.After(midnight(to)); w = w.AddDate(0, 0, 7) {
		out = append(out, w)
	}
	return out
}

// mondayOf is the Monday on or before a day, which is how the calendar's own
// week ticks are placed.
func mondayOf(t time.Time) time.Time {
	t = midnight(t)
	back := (int(t.Weekday()) + 6) % 7
	return t.AddDate(0, 0, -back)
}

// Loads reads the load per team over the window. Work is counted once: a row
// whose ancestor is on the same team contributes through that ancestor, as it
// does in a sprint. A child on another team counts for its own team.
func Loads(items []Item, teams []team.Team, from, to time.Time) Load {
	weeks := Weeks(from, to)
	byTeam := topmostByTeam(items)

	rowFor := func(id *uuid.UUID, name, kind string, capacity *float64) TeamLoad {
		row := TeamLoad{TeamID: id, Team: name, Kind: kind, WeeklyCapacity: capacity, Weeks: make([]LoadWeek, len(weeks))}
		for i, start := range weeks {
			row.Weeks[i] = LoadWeek{Start: start, Capacity: capacity}
		}
		key := ""
		if id != nil {
			key = id.String()
		}
		for _, item := range byTeam[key] {
			spread(&row, weeks, item)
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
	var capacity float64
	anyCapacity := false
	for _, row := range out.Rows {
		total.Unscheduled += row.Unscheduled
		for i := range row.Weeks {
			total.Weeks[i].Load += row.Weeks[i].Load
			total.Weeks[i].Issues += row.Weeks[i].Issues
			total.Weeks[i].Unestimated += row.Weeks[i].Unestimated
		}
		if row.WeeklyCapacity != nil {
			capacity += *row.WeeklyCapacity
			anyCapacity = true
		}
	}
	if anyCapacity {
		c := capacity
		total.WeeklyCapacity = &c
		for i := range total.Weeks {
			total.Weeks[i].Capacity = &c
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

// spread puts one row's work into the weeks it covers.
func spread(row *TeamLoad, weeks []time.Time, item Item) {
	if !item.Scheduled() {
		row.Unscheduled++
		return
	}
	start, due := midnight(*item.Start), midnight(*item.Due)
	days := int(due.Sub(start).Hours()/24) + 1
	if days < 1 {
		days = 1
	}
	var perDay float64
	if item.Estimate != nil {
		perDay = *item.Estimate / float64(days)
	}
	touched := map[int]bool{}
	for d := start; !d.After(due); d = d.AddDate(0, 0, 1) {
		i := weekIndex(weeks, d)
		if i < 0 {
			continue
		}
		row.Weeks[i].Load += perDay
		if !touched[i] {
			touched[i] = true
			row.Weeks[i].Issues++
			if item.Estimate == nil {
				row.Weeks[i].Unestimated++
			}
		}
	}
}

// weekIndex is which of the weeks a day falls in, or -1 outside the window.
func weekIndex(weeks []time.Time, d time.Time) int {
	if len(weeks) == 0 {
		return -1
	}
	i := int(d.Sub(weeks[0]).Hours() / 24 / 7)
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

// CheckLoad reports each week a team is loaded beyond what it said it could
// take. A warning, not a refusal: the plan is a draft.
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
			out = append(out, Warning{
				Team: row.Team, Kind: WarnOverLoad,
				Message: fmt.Sprintf("%s is loaded with %s in the week of %s against a capacity of %s",
					row.Team, points(week.Load), week.Start.Format("2 Jan"), points(*week.Capacity)),
			})
		}
	}
	return out
}
