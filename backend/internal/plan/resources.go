package plan

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/availability"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/team"
)

// ResourcePlan is the work set against the hours there are, per team or person
// and per week. Hours, not points: availability is measured in hours.
type ResourcePlan struct {
	ProjectKey string                   `json:"projectKey"`
	Method     project.PlanningMethod   `json:"method"`
	Grouping   project.ResourceGrouping `json:"grouping"`
	From       time.Time                `json:"from"`
	To         time.Time                `json:"to"`
	Weeks      []time.Time              `json:"weeks"`
	Rows       []ResourceRow            `json:"rows"`
	// Unscheduled is open work with hours but not both dates; Unestimated is
	// open work nobody has put hours on. Listed, since a guess would be a lie.
	Unscheduled []ResourceIssue `json:"unscheduled"`
	Unestimated []ResourceIssue `json:"unestimated"`
	Warnings    []Warning       `json:"warnings"`
}

// RowPerson is a resource row for one person; teams and the unassigned work
// share the load's kinds.
const RowPerson = "person"

// ResourceRow is one team or person, or the work nobody carries, across the weeks.
type ResourceRow struct {
	Kind string     `json:"kind"`
	ID   *uuid.UUID `json:"id,omitempty"`
	Name string     `json:"name"`
	// SharePercent is the part of a person's week the project has, when that
	// is less than all of it; their hours are already counted at it.
	SharePercent *int           `json:"sharePercent,omitempty"`
	Weeks        []ResourceWeek `json:"weeks"`
}

// ResourceWeek is one row's week: the hours scheduled into it against the
// hours there are, and the days off that made the difference.
type ResourceWeek struct {
	Start time.Time `json:"start"`
	// CapacityHours is left out for the unassigned row, which has nobody's time.
	CapacityHours *float64    `json:"capacityHours,omitempty"`
	NominalHours  *float64    `json:"nominalHours,omitempty"`
	LoadHours     float64     `json:"loadHours"`
	DaysAway      float64     `json:"daysAway,omitempty"`
	Holidays      int         `json:"holidays,omitempty"`
	Issues        []WeekIssue `json:"issues"`
}

// WeekIssue is one issue's share of a week.
type WeekIssue struct {
	Key     string  `json:"key"`
	Summary string  `json:"summary"`
	Hours   float64 `json:"hours"`
}

// ResourceIssue is open work the view cannot place, with its hours when it has any.
type ResourceIssue struct {
	Key     string   `json:"key"`
	Summary string   `json:"summary"`
	Hours   *float64 `json:"hours,omitempty"`
}

// WarnOverAllocated is a team or person scheduled beyond their hours in a week.
const WarnOverAllocated = "over-allocated"

// The resource view's window in weeks: what it opens on, and the most one
// reading covers. reachDays bounds how far around it an issue's days are read.
const (
	ResourceDefaultWeeks = 8
	ResourceMaxWeeks     = 26
	reachDays            = 366
)

// ErrBadWindow is returned for a window that is not one.
var ErrBadWindow = errors.New("that is not a window to plan in")

// ResourceWindow reads the window asked for as whole Monday weeks, opening on
// this week when nothing is asked.
func ResourceWindow(fromText, toText string, today time.Time) (time.Time, time.Time, error) {
	span := ResourceDefaultWeeks*daysPerWeek - 1
	from := mondayOf(today)
	if fromText != "" {
		day, err := parseWindowDay(fromText, "from")
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		from = mondayOf(day)
	}
	to := from.AddDate(0, 0, span)
	if toText != "" {
		day, err := parseWindowDay(toText, "to")
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		to = mondayOf(day).AddDate(0, 0, daysPerWeek-1)
		if fromText == "" {
			from = to.AddDate(0, 0, -span)
		}
	}
	switch {
	case to.Before(from):
		return time.Time{}, time.Time{}, fmt.Errorf("%w: to is before from; swap them", ErrBadWindow)
	case len(Weeks(from, to)) > ResourceMaxWeeks:
		return time.Time{}, time.Time{}, fmt.Errorf("%w: one reading covers at most %d weeks; ask for a shorter stretch", ErrBadWindow, ResourceMaxWeeks)
	}
	return from, to, nil
}

func parseWindowDay(text, what string) (time.Time, error) {
	day, err := time.Parse(availability.DateLayout, strings.TrimSpace(text))
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %s %q is not a day; write it as YYYY-MM-DD", ErrBadWindow, what, text)
	}
	return day, nil
}

// ResourceInput is what a resource reading is made of, read beforehand so
// that the reading itself touches nothing.
type ResourceInput struct {
	Grouping project.ResourceGrouping
	From, To time.Time
	// Issues are the project's open issues.
	Issues []issue.Issue
	Teams  []team.Team
	// People are who the person rows are for, by name.
	People map[uuid.UUID]string
	Work   Workdays
	// Shares are the percent of a person's week the project has; anybody
	// missing gives it all of their week.
	Shares map[uuid.UUID]int
}

// resourceRow is a row being filled, with the days its work is spread onto.
type resourceRow struct {
	ResourceRow
	works func(time.Time) bool
}

// ReadResources sets each open issue's hours, its remaining time or else its
// estimate, over its row's working days between its start and its due day.
func ReadResources(in ResourceInput) ResourcePlan {
	weeks := Weeks(in.From, in.To)
	out := ResourcePlan{
		Grouping: in.Grouping, From: in.From, To: in.To, Weeks: weeks,
		Unscheduled: []ResourceIssue{}, Unestimated: []ResourceIssue{}, Warnings: []Warning{},
	}
	issues := append([]issue.Issue(nil), in.Issues...)
	sort.SliceStable(issues, func(a, b int) bool { return keyBefore(issues[a].Key, issues[b].Key) })
	rows, rowOf := resourceRows(in, issues, weeks)

	parents := map[uuid.UUID]bool{}
	for _, i := range issues {
		if i.ParentID != nil {
			parents[*i.ParentID] = true
		}
	}
	for _, i := range issues {
		hours := hoursOf(i)
		scheduled := i.StartDate != nil && i.DueDate != nil
		switch {
		case hours == nil:
			// A parent is sized by its children; it is not missing an estimate.
			if !parents[i.ID] && (!scheduled || touches(i, in.From, in.To)) {
				out.Unestimated = append(out.Unestimated, ResourceIssue{Key: i.Key, Summary: i.Summary})
			}
		case !scheduled:
			out.Unscheduled = append(out.Unscheduled, ResourceIssue{Key: i.Key, Summary: i.Summary, Hours: ptrTo(roundHundredths(*hours))})
		case *hours > 0:
			row := rows[rowOf(i)]
			for w, h := range spreadHours(weeks, *i.StartDate, *i.DueDate, *hours, row.works) {
				week := &row.Weeks[w]
				week.LoadHours += h
				week.Issues = append(week.Issues, WeekIssue{Key: i.Key, Summary: i.Summary, Hours: roundHundredths(h)})
			}
		}
	}

	out.Rows = make([]ResourceRow, len(rows))
	for r, row := range rows {
		for w := range row.Weeks {
			row.Weeks[w].LoadHours = roundHundredths(row.Weeks[w].LoadHours)
		}
		out.Rows[r] = row.ResourceRow
	}
	out.Warnings = append(out.Warnings, CheckAllocation(out.Rows)...)
	return out
}

// resourceRows makes a row per team or person, and the unassigned row last,
// and says which row an issue's work goes to.
func resourceRows(in ResourceInput, issues []issue.Issue, weeks []time.Time) ([]*resourceRow, func(issue.Issue) int) {
	var rows []*resourceRow
	index := map[uuid.UUID]int{}
	add := func(id *uuid.UUID, kind, name string, people []uuid.UUID, works func(time.Time) bool) {
		row := &resourceRow{ResourceRow: ResourceRow{Kind: kind, ID: id, Name: name, Weeks: make([]ResourceWeek, len(weeks))}, works: works}
		for w, start := range weeks {
			row.Weeks[w] = ResourceWeek{Start: start, Issues: []WeekIssue{}}
			if id != nil {
				hoursOfWeek(&row.Weeks[w], in.Work.sharedTime(people, in.Shares, start, start.AddDate(0, 0, daysPerWeek-1)))
			}
		}
		if id != nil {
			index[*id] = len(rows)
		}
		rows = append(rows, row)
	}

	if in.Grouping == project.GroupByPerson {
		names := map[uuid.UUID]string{}
		for id, name := range in.People {
			names[id] = name
		}
		for _, i := range issues {
			if i.Assignee != nil && names[i.Assignee.ID] == "" {
				names[i.Assignee.ID] = i.Assignee.Name
			}
		}
		ids := make([]uuid.UUID, 0, len(names))
		for id := range names {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(a, b int) bool {
			x, y := strings.ToLower(names[ids[a]]), strings.ToLower(names[ids[b]])
			return x < y || (x == y && ids[a].String() < ids[b].String())
		})
		for _, id := range ids {
			add(&id, RowPerson, names[id], []uuid.UUID{id}, func(d time.Time) bool { return in.Work.personWorks(id, d) })
			if share, set := in.Shares[id]; set && share < availability.FullShare {
				rows[len(rows)-1].SharePercent = &share
			}
		}
	} else {
		for _, t := range in.Teams {
			id := t.ID
			add(&id, LoadTeam, t.Name, in.Work.members[id], func(d time.Time) bool { return in.Work.works(&id, d) })
		}
	}
	unassigned := len(rows)
	add(nil, LoadUnassigned, "Unassigned", nil, func(d time.Time) bool { return in.Work.works(nil, d) })

	rowOf := func(i issue.Issue) int {
		var id *uuid.UUID
		if in.Grouping == project.GroupByPerson && i.Assignee != nil {
			id = &i.Assignee.ID
		} else if in.Grouping != project.GroupByPerson {
			id = i.TeamID
		}
		if id == nil {
			return unassigned
		}
		if r, ok := index[*id]; ok {
			return r
		}
		return unassigned
	}
	return rows, rowOf
}

// hoursOfWeek fills a week's hours from the time of the people in its row.
func hoursOfWeek(week *ResourceWeek, t teamTime) {
	week.CapacityHours = ptrTo(roundHundredths(float64(t.available) / minutesPerHour))
	week.NominalHours = ptrTo(roundHundredths(float64(t.nominal) / minutesPerHour))
	week.DaysAway, week.Holidays = t.away, t.holidays
}

// hoursOf is what is left of an issue in hours: its remaining time, else its
// estimate, else nil for work nobody has sized.
func hoursOf(i issue.Issue) *float64 {
	minutes := i.TimeRemainingMinutes
	if minutes == nil {
		minutes = i.TimeEstimateMinutes
	}
	if minutes == nil {
		return nil
	}
	return ptrTo(float64(*minutes) / minutesPerHour)
}

// spreadHours puts hours evenly on the working days between two days, both
// included, or on all of them when none is a working day, and sums them per week.
func spreadHours(weeks []time.Time, start, due time.Time, hours float64, works func(time.Time) bool) map[int]float64 {
	start, due = midnight(start), midnight(due)
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
	out := map[int]float64{}
	perDay := hours / float64(len(working))
	for _, d := range working {
		if w := weekIndex(weeks, d); w >= 0 {
			out[w] += perDay
		}
	}
	return out
}

// touches reports whether a scheduled issue's range meets the window.
func touches(i issue.Issue, from, to time.Time) bool {
	return !midnight(*i.DueDate).Before(from) && !midnight(*i.StartDate).After(to)
}

// keyBefore orders issue keys as people read them: PRJ-9 before PRJ-10.
func keyBefore(a, b string) bool {
	ap, an := splitKey(a)
	bp, bn := splitKey(b)
	if ap != bp {
		return ap < bp
	}
	return an < bn
}

func splitKey(key string) (string, int) {
	cut := strings.LastIndex(key, "-")
	if cut < 0 {
		return key, 0
	}
	n, _ := strconv.Atoi(key[cut+1:])
	return key[:cut], n
}

// CheckAllocation reports each week a team or person is scheduled beyond the
// hours they have. A warning, never a refusal: the plan is a draft.
func CheckAllocation(rows []ResourceRow) []Warning {
	var out []Warning
	for _, row := range rows {
		if row.Kind == LoadUnassigned {
			continue
		}
		for _, week := range row.Weeks {
			if week.CapacityHours == nil || week.LoadHours <= *week.CapacityHours+loadTolerance {
				continue
			}
			message := fmt.Sprintf("%s is allocated %s in the week of %s against %s available",
				row.Name, hoursText(week.LoadHours), week.Start.Format("2 Jan"), hoursText(*week.CapacityHours))
			if week.NominalHours != nil && *week.NominalHours != *week.CapacityHours {
				message += fmt.Sprintf(", %s before holidays and absences", hoursText(*week.NominalHours))
			}
			warning := Warning{Kind: WarnOverAllocated, Message: message}
			if row.Kind == RowPerson {
				warning.Person = row.Name
			} else {
				warning.Team = row.Name
			}
			out = append(out, warning)
		}
	}
	return out
}

func hoursText(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64) + " h"
}
