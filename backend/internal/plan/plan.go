// Package plan is the timeline view of a project: what is scheduled when, what
// that implies for the levels above, and where the schedule disagrees with itself.
package plan

import (
	"fmt"
	"sort"
	"time"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/milestone"
)

// Item is one row of the plan.
type Item struct {
	Issue issue.Issue `json:"issue"`
	// Depth is how far to indent, so the client does not have to walk the tree
	// again to find out.
	Depth int        `json:"depth"`
	Start *time.Time `json:"start,omitempty"`
	Due   *time.Time `json:"due,omitempty"`
	// Derived says the range came from the children rather than from the issue,
	// which is the difference between a plan and a promise.
	Derived bool `json:"derived"`
	// Estimate is how much work this is, in the unit the team counts in, and
	// EstimateDerived says it was added up from the children rather than typed.
	Estimate        *float64       `json:"estimate,omitempty"`
	EstimateDerived bool           `json:"estimateDerived,omitempty"`
	Progress        issue.Progress `json:"progress"`
	Children        []Item         `json:"children"`
}

// Scheduled reports whether this row has a bar to draw.
func (i Item) Scheduled() bool { return i.Start != nil && i.Due != nil }

// Warning is a place the schedule disagrees with itself. Warnings never refuse
// a change: a plan is a draft, and a draft you cannot write down is useless.
type Warning struct {
	// Exactly one of these names what the warning is about. A sprint committed
	// beyond its capacity, or a team loaded beyond its week, is not any one
	// issue's fault.
	IssueKey string `json:"issueKey,omitempty"`
	Sprint   string `json:"sprint,omitempty"`
	Team     string `json:"team,omitempty"`
	Kind     string `json:"kind"`
	Message  string `json:"message"`
}

// The kinds of disagreement a plan can hold.
const (
	// WarnBlockedTooEarly is an issue starting before the thing blocking it ends.
	WarnBlockedTooEarly = "blocked-too-early"
	// WarnOutsideParent is a child scheduled outside its parent's own dates.
	WarnOutsideParent = "outside-parent"
	// WarnHalfScheduled is an issue with one end of its range and not the other.
	WarnHalfScheduled = "half-scheduled"
)

// Plan is a project's whole timeline.
type Plan struct {
	ProjectKey   string          `json:"projectKey"`
	Items        []Item          `json:"items"`
	Dependencies []issue.Blocker `json:"dependencies"`
	// Sprints are the stretches of time the work is committed to, with what
	// each of them actually holds.
	Sprints []SprintPlan `json:"sprints"`
	// Milestones are the points the work is heading for, each with how far
	// along it is.
	Milestones []milestone.Milestone `json:"milestones"`
	// Load is the work per team per week against what each team can take.
	Load     Load      `json:"load"`
	Warnings []Warning `json:"warnings"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	// Unscheduled and Unestimated are the two kinds of row a plan cannot say
	// anything about, counted rather than hidden.
	Unscheduled int                 `json:"unscheduled"`
	Unestimated int                 `json:"unestimated"`
	LinkTypes   []issue.LinkTypeRef `json:"linkTypes"`
	// Matched is the keys the request's query selected, empty without one.
	// The rows are all still here; which to draw is the client's reading.
	Matched []string `json:"matched"`
}

// FromTree turns the issue hierarchy into plan rows, deriving the dates of
// everything that does not carry its own.
func FromTree(nodes []issue.Node) []Item {
	items := make([]Item, 0, len(nodes))
	for _, n := range nodes {
		items = append(items, itemOf(n, 0))
	}
	items = Rollup(items)
	rollupEstimates(items)
	return items
}

func itemOf(n issue.Node, depth int) Item {
	item := Item{
		Issue:    n.Issue,
		Depth:    depth,
		Start:    n.Issue.StartDate,
		Due:      n.Issue.DueDate,
		Estimate: n.Issue.Estimate,
		Progress: n.Progress,
		Children: make([]Item, 0, len(n.Children)),
	}
	for _, child := range n.Children {
		item.Children = append(item.Children, itemOf(child, depth+1))
	}
	return item
}

// Rollup gives every row without its own dates the span of everything beneath
// it, depth first so a grandparent sees its grandchildren through its children.
func Rollup(items []Item) []Item {
	for i := range items {
		items[i].Children = Rollup(items[i].Children)

		if items[i].Start != nil && items[i].Due != nil {
			continue
		}
		start, due := spanOf(items[i].Children)
		if start == nil || due == nil {
			continue
		}
		// A half-scheduled parent keeps the end it was given: an explicit date
		// is a decision, and the derived one is only filling a gap.
		if items[i].Start == nil {
			items[i].Start = start
		}
		if items[i].Due == nil {
			items[i].Due = due
		}
		items[i].Derived = true
	}
	return items
}

// spanOf is the earliest start and latest end across a set of rows.
func spanOf(items []Item) (*time.Time, *time.Time) {
	var start, due *time.Time
	for _, item := range items {
		if item.Start != nil && (start == nil || item.Start.Before(*start)) {
			start = item.Start
		}
		if item.Due != nil && (due == nil || item.Due.After(*due)) {
			due = item.Due
		}
	}
	return start, due
}

// Check reports where the schedule disagrees with itself.
func Check(items []Item, blockers []issue.Blocker) []Warning {
	flat := Flatten(items)
	byKey := make(map[string]Item, len(flat))
	for _, item := range flat {
		byKey[item.Issue.Key] = item
	}

	var warnings []Warning
	for _, item := range flat {
		if (item.Start == nil) != (item.Due == nil) {
			warnings = append(warnings, Warning{
				IssueKey: item.Issue.Key,
				Kind:     WarnHalfScheduled,
				Message:  "only one end of this is scheduled, so it has no place on the timeline",
			})
		}
	}

	// A derived parent cannot disagree with its children: it is made of them.
	for _, parent := range flat {
		if parent.Derived || !parent.Scheduled() {
			continue
		}
		for _, child := range parent.Children {
			if !child.Scheduled() {
				continue
			}
			if child.Start.Before(*parent.Start) || child.Due.After(*parent.Due) {
				warnings = append(warnings, Warning{
					IssueKey: child.Issue.Key,
					Kind:     WarnOutsideParent,
					Message: fmt.Sprintf("this runs outside %s, which is scheduled %s to %s",
						parent.Issue.Key, day(parent.Start), day(parent.Due)),
				})
			}
		}
	}

	for _, b := range blockers {
		blocker, blocked := byKey[b.BlockerKey], byKey[b.BlockedKey]
		if !blocker.Scheduled() || !blocked.Scheduled() {
			continue
		}
		if !blocked.Start.After(*blocker.Due) {
			warnings = append(warnings, Warning{
				IssueKey: blocked.Issue.Key,
				Kind:     WarnBlockedTooEarly,
				Message: fmt.Sprintf("starts %s, but %s does not finish until %s",
					day(blocked.Start), b.BlockerKey, day(blocker.Due)),
			})
		}
	}

	sort.SliceStable(warnings, func(a, b int) bool {
		return warnings[a].IssueKey < warnings[b].IssueKey
	})
	return warnings
}

// Flatten walks the plan in display order.
func Flatten(items []Item) []Item {
	out := make([]Item, 0, len(items))
	for _, item := range items {
		out = append(out, item)
		out = append(out, Flatten(item.Children)...)
	}
	return out
}

// How much room the timeline leaves around the work, in days, and the window it
// opens on a project where nothing is scheduled yet.
const (
	windowPaddingDays = 7
	emptyWindowBack   = 14
	emptyWindowAhead  = 45
)

// Window is the span the timeline should open on: the scheduled work with room
// on either side, or a stretch around today when nothing is scheduled at all.
func Window(items []Item, today time.Time) (time.Time, time.Time) {
	today = midnight(today)
	start, due := spanOf(Flatten(items))
	if start == nil || due == nil {
		return today.AddDate(0, 0, -emptyWindowBack), today.AddDate(0, 0, emptyWindowAhead)
	}

	from, to := midnight(*start).AddDate(0, 0, -windowPaddingDays), midnight(*due).AddDate(0, 0, windowPaddingDays)
	// Today is the one date a person always looks for, so it is always on screen.
	if today.Before(from) {
		from = today.AddDate(0, 0, -windowPaddingDays)
	}
	if today.After(to) {
		to = today.AddDate(0, 0, windowPaddingDays)
	}
	return from, to
}

// Widen stretches a window to take in dates that are not issues': sprint ends
// and milestone due days. A milestone drawn off the edge of the calendar is a
// milestone nobody sees, and a scrollbar is a poor way to say so.
func Widen(from, to time.Time, dates ...*time.Time) (time.Time, time.Time) {
	for _, date := range dates {
		if date == nil {
			continue
		}
		d := midnight(*date)
		if d.Before(from.AddDate(0, 0, windowPaddingDays)) {
			from = d.AddDate(0, 0, -windowPaddingDays)
		}
		if d.After(to.AddDate(0, 0, -windowPaddingDays)) {
			to = d.AddDate(0, 0, windowPaddingDays)
		}
	}
	return from, to
}

// Unscheduled counts the rows with no bar to draw.
func Unscheduled(items []Item) int {
	n := 0
	for _, item := range Flatten(items) {
		if !item.Scheduled() {
			n++
		}
	}
	return n
}

// Unestimated counts the rows nobody has sized, derived estimates included: a
// parent that only has a number because its children do is still an estimate.
func Unestimated(items []Item) int {
	n := 0
	for _, item := range Flatten(items) {
		if item.Estimate == nil {
			n++
		}
	}
	return n
}

func midnight(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func day(t *time.Time) string {
	if t == nil {
		return "an unset date"
	}
	return t.Format("2 Jan 2006")
}
