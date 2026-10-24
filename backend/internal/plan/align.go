package plan

import (
	"sort"
	"time"

	"github.com/google/uuid"

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
	From     Span   `json:"from"`
	To       Span   `json:"to"`
	// WorkingDays is the length the issue keeps, counted on its team's calendar.
	WorkingDays int `json:"workingDays"`
	// After is the blocker whose due date the new start follows.
	After string `json:"after"`
}

// Alignment is what the auto plan proposes. It changes nothing by itself.
type Alignment struct {
	ProjectKey string `json:"projectKey"`
	Moves      []Move `json:"moves"`
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
}

// Align proposes the moves that start every blocked issue after its blockers.
// Push only: an issue that already starts after them stays put, slack and all.
func Align(items []Item, in AlignInput) []Move {
	type node struct {
		span    Span
		team    *uuid.UUID
		movable bool
	}
	today := midnight(in.Today)
	nodes := map[string]*node{}
	for _, item := range Flatten(items) {
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
	for _, b := range in.Blockers {
		if nodes[b.BlockerKey] == nil || nodes[b.BlockedKey] == nil {
			continue
		}
		blockedBy[b.BlockedKey] = append(blockedBy[b.BlockedKey], b.BlockerKey)
		blocks[b.BlockerKey] = append(blocks[b.BlockerKey], b.BlockedKey)
		indegree[b.BlockedKey]++
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
		if n.movable && after != "" && !n.span.Start.After(latest) {
			length := in.Work.workingDays(n.team, n.span)
			to := in.Work.place(n.team, latest.AddDate(0, 0, 1), length, n.span)
			moves = append(moves, Move{IssueKey: key, From: n.span, To: to, WorkingDays: length, After: after})
			n.span = to
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
	return moves
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
