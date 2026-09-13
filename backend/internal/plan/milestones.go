package plan

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/milestone"
)

// MilestoneReader is what the plan needs to know about milestones.
type MilestoneReader interface {
	ForProject(ctx context.Context, projectKey string) ([]milestone.Milestone, error)
}

// WarnPastMilestone is an issue due after the milestone it counts towards.
const WarnPastMilestone = "past-milestone"

// CheckMilestones reports the issues that cannot finish in time for their
// milestone. Like every other warning on the plan, it refuses nothing: the
// date is the promise and the schedule is where it is being broken.
func CheckMilestones(items []Item, milestones []milestone.Milestone) []Warning {
	byID := make(map[uuid.UUID]milestone.Milestone, len(milestones))
	for _, m := range milestones {
		byID[m.ID] = m
	}

	var warnings []Warning
	for _, item := range Flatten(items) {
		if item.Issue.MilestoneID == nil || item.Due == nil {
			continue
		}
		m, ok := byID[*item.Issue.MilestoneID]
		if !ok || m.DueOn == nil || !item.Due.After(*m.DueOn) {
			continue
		}
		warnings = append(warnings, Warning{
			IssueKey: item.Issue.Key,
			Kind:     WarnPastMilestone,
			Message:  fmt.Sprintf("this is due after %s, which is due %s", m.Name, day(m.DueOn)),
		})
	}
	return warnings
}
