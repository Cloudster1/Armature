package arrange

// Default is the arrangement of an issue nobody has arranged: the page as it
// was drawn before a project could say otherwise.
func Default() []Placement {
	areas := []struct {
		area  Area
		slots []Slot
	}{
		{AreaMain, []Slot{SlotDescription}},
		{AreaPeople, []Slot{SlotAssignee, SlotReporter}},
		{AreaPlanning, []Slot{
			SlotParent, SlotSchedule, SlotSprint, SlotMilestone, SlotFixVersions,
			SlotAffectsVersions, SlotComponents, SlotEstimate, SlotTeam,
		}},
		{AreaTracking, []Slot{SlotRequest, SlotGoals, SlotPriority, SlotLabels, SlotTime}},
		{AreaMore, []Slot{SlotOtherFields, SlotCreated, SlotResolved}},
	}
	places := make([]Placement, 0, len(Slots()))
	for _, group := range areas {
		for _, slot := range group.slots {
			places = append(places, Placement{Area: group.area, Slot: slot})
		}
	}
	return places
}
