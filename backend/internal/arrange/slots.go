// Package arrange decides which of an issue's fields are shown, where, and in
// what order, per project and per issue type.
package arrange

// Slot is one thing an issue page can show. The key, the summary, the status
// and the type are not slots: the page is built around them.
type Slot string

const (
	SlotDescription     Slot = "description"
	SlotAssignee        Slot = "assignee"
	SlotReporter        Slot = "reporter"
	SlotParent          Slot = "parent"
	SlotSchedule        Slot = "schedule"
	SlotSprint          Slot = "sprint"
	SlotMilestone       Slot = "milestone"
	SlotFixVersions     Slot = "fixVersions"
	SlotAffectsVersions Slot = "affectsVersions"
	SlotComponents      Slot = "components"
	SlotEstimate        Slot = "estimate"
	SlotTeam            Slot = "team"
	SlotRequest         Slot = "request"
	SlotGoals           Slot = "goals"
	SlotPriority        Slot = "priority"
	SlotLabels          Slot = "labels"
	SlotTime            Slot = "time"
	// SlotOtherFields stands for every field of the project that the
	// arrangement does not name, so a field defined later needs no migration.
	SlotOtherFields Slot = "otherFields"
	SlotCreated     Slot = "created"
	SlotResolved    Slot = "resolved"
)

// Slots is the whole vocabulary, which the API document offers as an enum so a
// client that cannot draw one of them fails to compile.
func Slots() []Slot {
	return []Slot{
		SlotDescription, SlotAssignee, SlotReporter, SlotParent, SlotSchedule,
		SlotSprint, SlotMilestone, SlotFixVersions, SlotAffectsVersions, SlotComponents,
		SlotEstimate, SlotTeam, SlotRequest, SlotGoals, SlotPriority, SlotLabels,
		SlotTime, SlotOtherFields, SlotCreated, SlotResolved,
	}
}

func (s Slot) Known() bool {
	for _, known := range Slots() {
		if known == s {
			return true
		}
	}
	return false
}

// Area is a place on the page: the column under the description, the four
// groups of facts beside it, and the tray of what is not shown at all.
type Area string

const (
	AreaMain     Area = "main"
	AreaPeople   Area = "people"
	AreaPlanning Area = "planning"
	AreaTracking Area = "tracking"
	AreaMore     Area = "more"
	AreaHidden   Area = "hidden"
)

// Areas is every area, in the order a page reads.
func Areas() []Area {
	return []Area{AreaMain, AreaPeople, AreaPlanning, AreaTracking, AreaMore, AreaHidden}
}

func (a Area) Known() bool {
	for _, known := range Areas() {
		if known == a {
			return true
		}
	}
	return false
}
