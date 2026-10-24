package plan

import (
	"math"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/availability"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/workflow"
)

// Span is both ends of an issue's range, inclusive days.
type Span struct {
	Start time.Time `json:"startDate"`
	Due   time.Time `json:"dueDate"`
}

// Move is one issue the auto plan would shift so it starts after what blocks it.
type Move struct {
	IssueKey string `json:"issueKey"`
	// From is where the issue is now, absent for one that had no dates and is placed.
	From *Span `json:"from,omitempty"`
	To   Span  `json:"to"`
	// Placed says the issue had no dates and gets its first ones.
	Placed bool `json:"placed,omitempty"`
	// WorkingDays is the length the issue keeps, counted on its team's calendar.
	WorkingDays int `json:"workingDays"`
	// After is the blocker whose due date the new start follows, empty for a
	// placed issue nothing holds back.
	After string `json:"after"`
}

// Unplaced is an issue without dates the auto plan was asked to place and could not.
type Unplaced struct {
	IssueKey string `json:"issueKey"`
	Reason   string `json:"reason"`
}

// Proposal is the moves and what could not be placed, before anything is read
// about what the plan would then say.
type Proposal struct {
	Moves    []Move
	Unplaced []Unplaced
}

// Alignment is what the auto plan proposes. It changes nothing by itself.
type Alignment struct {
	ProjectKey string `json:"projectKey"`
	Moves      []Move `json:"moves"`
	// Unplaced is the work without dates that could not be placed, when placing was asked for.
	Unplaced []Unplaced `json:"unplaced"`
	// Warnings are the plan's own disagreements as they would stand after the moves.
	Warnings []Warning `json:"warnings"`
}

// AlignInput is everything the auto plan reads besides the rows themselves.
type AlignInput struct {
	Blockers []issue.Blocker
	Work     Workdays
	Today    time.Time
	// Matched limits the moves to these keys; nil moves any issue the rules allow.
	Matched map[string]bool
	// Outside holds the dates of blockers that are not rows of this plan, such as
	// one in another project; they constrain and never move.
	Outside map[string]Span
	// Place gives issues without dates their first ones, from their remaining or
	// estimated time, after what blocks them and no earlier than today.
	Place bool
}

// notPlacedReason is what an issue without dates and without an estimate is listed with.
const notPlacedReason = "Not placed: it has no estimate."

// Align proposes the moves that start every blocked issue after its blockers.
// Push only: an issue that already starts after them stays put, slack and all.
func Align(items []Item, in AlignInput) Proposal {
	type node struct {
		span    Span
		team    *uuid.UUID
		movable bool
		// days is the length of an issue to place, which has no span until it is placed.
		days int
	}
	today := midnight(in.Today)
	nodes := map[string]*node{}
	var unplaced []Unplaced
	for _, item := range Flatten(items) {
		if in.Place && placeable(item, in.Matched) {
			if hours := hoursOf(item.Issue); hours != nil && *hours > 0 {
				nodes[item.Issue.Key] = &node{team: item.Issue.TeamID, movable: true, days: daysOf(*hours)}
			} else {
				unplaced = append(unplaced, Unplaced{IssueKey: item.Issue.Key, Reason: notPlacedReason})
			}
			continue
		}
		if !item.Scheduled() {
			continue
		}
		nodes[item.Issue.Key] = &node{
			span:    Span{Start: midnight(*item.Start), Due: midnight(*item.Due)},
			team:    item.Issue.TeamID,
			movable: movable(item, today, in.Matched),
		}
	}
	for key, f := range in.Outside {
		if _, ok := nodes[key]; !ok {
			nodes[key] = &node{span: Span{Start: midnight(f.Start), Due: midnight(f.Due)}}
		}
	}

	blockedBy := map[string][]string{}
	blocks := map[string][]string{}
	indegree := map[string]int{}
	for _, c := range Constraints(items, in.Blockers) {
		if nodes[c.BlockerKey] == nil || nodes[c.BlockedKey] == nil {
			continue
		}
		blockedBy[c.BlockedKey] = append(blockedBy[c.BlockedKey], c.BlockerKey)
		blocks[c.BlockerKey] = append(blocks[c.BlockerKey], c.BlockedKey)
		indegree[c.BlockedKey]++
	}

	// Kahn's order, each batch sorted by key, so the same plan always proposes the same moves.
	var ready []string
	for key := range nodes {
		if indegree[key] == 0 {
			ready = append(ready, key)
		}
	}
	sort.Strings(ready)

	var moves []Move
	for len(ready) > 0 {
		key := ready[0]
		ready = ready[1:]
		n := nodes[key]

		latest, after := time.Time{}, ""
		for _, blocker := range blockedBy[key] {
			due := nodes[blocker].span.Due
			if due.After(latest) || (due.Equal(latest) && blocker < after) {
				latest, after = due, blocker
			}
		}
		switch {
		case n.days > 0:
			from := today
			if after != "" && latest.AddDate(0, 0, 1).After(today) {
				from = latest.AddDate(0, 0, 1)
			}
			n.span = in.Work.place(n.team, from, n.days, Span{})
			moves = append(moves, Move{IssueKey: key, To: n.span, Placed: true, WorkingDays: n.days, After: after})
		case n.movable && after != "" && !n.span.Start.After(latest):
			length := in.Work.workingDays(n.team, n.span)
			was := n.span
			n.span = in.Work.place(n.team, latest.AddDate(0, 0, 1), length, was)
			moves = append(moves, Move{IssueKey: key, From: &was, To: n.span, WorkingDays: length, After: after})
		}

		var next []string
		for _, blocked := range blocks[key] {
			indegree[blocked]--
			if indegree[blocked] == 0 {
				next = append(next, blocked)
			}
		}
		sort.Strings(next)
		ready = append(ready, next...)
	}
	return Proposal{Moves: moves, Unplaced: unplaced}
}

// placeable says whether an issue is work without dates the auto plan may give
// some: a leaf, since a parent takes its span from its children, not done, and
// matched by the query when there is one.
func placeable(item Item, matched map[string]bool) bool {
	if item.Start != nil || item.Due != nil || len(item.Children) > 0 || item.Issue.IsDone() {
		return false
	}
	return matched == nil || matched[item.Issue.Key]
}

// daysOf is how many working days some hours of work take one person on a standard day.
func daysOf(hours float64) int {
	return int(math.Ceil(hours * minutesPerHour / availability.StandardDayMinutes))
}

// movable says whether the auto plan may shift an issue. Done work, work under
// way and dates rolled up from children are facts the rest is planned around.
func movable(item Item, today time.Time, matched map[string]bool) bool {
	if item.Derived || item.Issue.IsDone() {
		return false
	}
	if matched != nil && !matched[item.Issue.Key] {
		return false
	}
	underWay := item.Issue.Status.Category != workflow.CategoryTodo && midnight(*item.Start).Before(today)
	return !underWay
}

// workingDays counts the days of a span the team works.
func (w Workdays) workingDays(team *uuid.UUID, s Span) int {
	n := 0
	for d := s.Start; !d.After(s.Due); d = d.AddDate(0, 0, 1) {
		if w.works(team, d) {
			n++
		}
	}
	return n
}

// nextWorkday is the first day from d the team works, or d when none is found.
func (w Workdays) nextWorkday(team *uuid.UUID, d time.Time) time.Time {
	for i := 0; i < WorkdaySearchDays; i++ {
		day := d.AddDate(0, 0, i)
		if w.works(team, day) {
			return day
		}
	}
	return d
}

// place lays out an issue of length working days from the first working day on
// or after from. A span with no working days in it keeps its length in calendar days.
func (w Workdays) place(team *uuid.UUID, from time.Time, length int, was Span) Span {
	start := w.nextWorkday(team, from)
	if length == 0 {
		return Span{Start: start, Due: start.Add(was.Due.Sub(was.Start))}
	}
	due := start
	for counted := 1; counted < length; {
		due = w.nextWorkday(team, due.AddDate(0, 0, 1))
		counted++
	}
	return Span{Start: start, Due: due}
}

// Moved is the plan as it would stand after the moves, its derived spans rolled up again.
func Moved(items []Item, moves []Move) []Item {
	to := make(map[string]Span, len(moves))
	for _, m := range moves {
		to[m.IssueKey] = m.To
	}
	return Rollup(moved(items, to))
}

func moved(items []Item, to map[string]Span) []Item {
	out := make([]Item, len(items))
	for i, item := range items {
		item.Children = moved(item.Children, to)
		if span, ok := to[item.Issue.Key]; ok {
			start, due := span.Start, span.Due
			item.Start, item.Due = &start, &due
		}
		if item.Derived {
			item.Start, item.Due, item.Derived = item.Issue.StartDate, item.Issue.DueDate, false
		}
		out[i] = item
	}
	return out
}
