package plan

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/sprint"
)

// SprintPlan is a sprint together with what has actually been committed to it.
type SprintPlan struct {
	Sprint sprint.Sprint `json:"sprint"`
	// Committed is the work in the sprint, counted once. Completed is the part
	// of it that is done.
	Committed float64 `json:"committed"`
	Completed float64 `json:"completed"`
	// Issues counts the rows the totals were taken from, and Unestimated how
	// many of them nobody has sized. A sprint of eight points and five
	// unestimated issues is not a sprint of eight points.
	Issues      int `json:"issues"`
	Unestimated int `json:"unestimated"`
	// IssuesDone counts the rows that are done, estimated or not.
	IssuesDone int `json:"issuesDone"`
}

// OverBy is how far past its capacity the sprint is committed, or zero.
func (s SprintPlan) OverBy() float64 {
	if s.Sprint.Capacity == nil || s.Committed <= *s.Sprint.Capacity {
		return 0
	}
	return s.Committed - *s.Sprint.Capacity
}

// rollupEstimates gives every row without an estimate of its own the sum of
// everything beneath it, depth first.
//
// It is the same rule the dates follow: an estimate somebody typed is a
// decision, and one derived from the children is only filling a gap. A parent
// that carries its own estimate is taken at its word even when its children add
// up to something else.
func rollupEstimates(items []Item) {
	for i := range items {
		rollupEstimates(items[i].Children)
		if items[i].Estimate != nil {
			continue
		}
		total, any := sumOf(items[i].Children)
		if !any {
			continue
		}
		items[i].Estimate = &total
		items[i].EstimateDerived = true
	}
}

func sumOf(items []Item) (float64, bool) {
	total, any := 0.0, false
	for _, item := range items {
		if item.Estimate == nil {
			continue
		}
		total += *item.Estimate
		any = true
	}
	return total, any
}

// Sprints reports what each sprint holds.
//
// Work is counted once: an issue whose parent is in the same sprint contributes
// through that parent and not again on its own. Without that rule a story of
// five points broken into subtasks that add up to five would read as ten.
func Sprints(items []Item, sprints []sprint.Sprint) []SprintPlan {
	counted := topmostBySprint(items)

	out := make([]SprintPlan, 0, len(sprints))
	for _, s := range sprints {
		plan := SprintPlan{Sprint: s}
		for _, item := range counted[s.ID] {
			plan.Issues++
			if item.Issue.IsDone() {
				plan.IssuesDone++
			}
			if item.Estimate == nil {
				plan.Unestimated++
				continue
			}
			plan.Committed += *item.Estimate
			// A row contributes to completed when the row itself is done. A
			// half-finished parent is not half-completed work.
			if item.Issue.IsDone() {
				plan.Completed += *item.Estimate
			}
		}
		out = append(out, plan)
	}
	return out
}

// topmostBySprint groups the rows that carry each sprint's work: those in the
// sprint with no ancestor also in it.
func topmostBySprint(items []Item) map[uuid.UUID][]Item {
	out := map[uuid.UUID][]Item{}
	var walk func(items []Item, claimed map[uuid.UUID]bool)
	walk = func(items []Item, claimed map[uuid.UUID]bool) {
		for _, item := range items {
			mine := item.Issue.SprintID
			below := claimed
			if mine != nil && !claimed[*mine] {
				out[*mine] = append(out[*mine], item)
				below = map[uuid.UUID]bool{}
				for id := range claimed {
					below[id] = true
				}
				below[*mine] = true
			}
			walk(item.Children, below)
		}
	}
	walk(items, map[uuid.UUID]bool{})
	return out
}

// The disagreements a sprint can hold.
const (
	// WarnOverCapacity is a sprint committed beyond what the team said it could
	// take on.
	WarnOverCapacity = "over-capacity"
	// WarnOutsideSprint is an issue scheduled outside the sprint it belongs to.
	WarnOutsideSprint = "outside-sprint"
)

// CheckSprints reports where the commitments disagree with themselves. Like
// every other warning on the plan, none of these refuse anything.
func CheckSprints(items []Item, plans []SprintPlan) []Warning {
	byID := make(map[uuid.UUID]SprintPlan, len(plans))
	var warnings []Warning

	for _, plan := range plans {
		byID[plan.Sprint.ID] = plan
		if over := plan.OverBy(); over > 0 {
			warnings = append(warnings, Warning{
				Sprint: plan.Sprint.Name,
				Kind:   WarnOverCapacity,
				Message: fmt.Sprintf("%s of work is committed against a capacity of %s",
					points(plan.Committed), points(*plan.Sprint.Capacity)),
			})
		}
	}

	for _, item := range Flatten(items) {
		if item.Issue.SprintID == nil || !item.Scheduled() {
			continue
		}
		plan, ok := byID[*item.Issue.SprintID]
		if !ok || plan.Sprint.StartsOn == nil || plan.Sprint.EndsOn == nil {
			continue
		}
		if item.Start.Before(*plan.Sprint.StartsOn) || item.Due.After(*plan.Sprint.EndsOn) {
			warnings = append(warnings, Warning{
				IssueKey: item.Issue.Key,
				Kind:     WarnOutsideSprint,
				Message: fmt.Sprintf("this is scheduled outside %s, which runs %s to %s",
					plan.Sprint.Name, day(plan.Sprint.StartsOn), day(plan.Sprint.EndsOn)),
			})
		}
	}
	return warnings
}

// points formats an estimate the way a person writes one: 5 rather than 5.00,
// and 2.5 when it really is a half.
func points(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d points", int64(v))
	}
	return fmt.Sprintf("%g points", v)
}
