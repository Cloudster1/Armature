package plan

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/milestone"
)

func TestCheckMilestonesFlagsWorkDueAfterItsMilestone(t *testing.T) {
	release := milestone.Milestone{ID: uuid.New(), Name: "Release 1", DueOn: at("2026-10-01")}
	undated := milestone.Milestone{ID: uuid.New(), Name: "Someday"}

	items := []Item{
		{Issue: issue.Issue{Key: "P-1", MilestoneID: &release.ID}, Start: at("2026-09-20"), Due: at("2026-10-05")},
		{Issue: issue.Issue{Key: "P-2", MilestoneID: &release.ID}, Start: at("2026-09-20"), Due: at("2026-10-01")},
		{Issue: issue.Issue{Key: "P-3", MilestoneID: &undated.ID}, Start: at("2026-09-20"), Due: at("2027-01-01")},
		{Issue: issue.Issue{Key: "P-4", MilestoneID: &release.ID}},
		{Issue: issue.Issue{Key: "P-5"}, Due: at("2027-01-01")},
	}

	got := CheckMilestones(items, []milestone.Milestone{release, undated})
	if len(got) != 1 {
		t.Fatalf("warnings = %+v, want just P-1", got)
	}
	if got[0].IssueKey != "P-1" || got[0].Kind != WarnPastMilestone {
		t.Errorf("warning = %+v, want P-1 past its milestone", got[0])
	}
	if got[0].Message != "this is due after Release 1, which is due 1 Oct 2026" {
		t.Errorf("message = %q", got[0].Message)
	}
}

func at(s string) *time.Time {
	v, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return &v
}

// A milestone due after the last issue would otherwise be drawn off the edge
// of the calendar, where a flag is only a scrollbar.
func TestWidenReachesTheDatesTheCalendarDraws(t *testing.T) {
	from, to := *at("2026-09-01"), *at("2026-09-30")

	gotFrom, gotTo := Widen(from, to, at("2026-10-20"), nil, at("2026-09-15"))
	if !gotFrom.Equal(from) {
		t.Errorf("from moved to %s for a date inside the window", gotFrom.Format("2006-01-02"))
	}
	if want := at("2026-10-27"); !gotTo.Equal(*want) {
		t.Errorf("to = %s, want the milestone plus the usual room, %s", gotTo.Format("2006-01-02"), want.Format("2006-01-02"))
	}

	gotFrom, _ = Widen(from, to, at("2026-08-01"))
	if want := at("2026-07-25"); !gotFrom.Equal(*want) {
		t.Errorf("from = %s, want %s", gotFrom.Format("2006-01-02"), want.Format("2006-01-02"))
	}
}
