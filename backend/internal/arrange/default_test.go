package arrange

import "testing"

// The built-in arrangement is the page as it was drawn before any of this, so
// it is written down here as well as in the code that answers with it.
func TestTheBuiltInArrangementIsTodaysPage(t *testing.T) {
	want := map[Area][]Slot{
		AreaMain:     {SlotDescription},
		AreaPeople:   {SlotAssignee, SlotReporter},
		AreaPlanning: {SlotParent, SlotSchedule, SlotSprint, SlotMilestone, SlotFixVersions, SlotAffectsVersions, SlotComponents, SlotEstimate, SlotTeam},
		AreaTracking: {SlotRequest, SlotGoals, SlotPriority, SlotLabels, SlotTime},
		AreaMore:     {SlotOtherFields, SlotCreated, SlotResolved},
	}
	got := map[Area][]Slot{}
	for _, place := range Default() {
		got[place.Area] = append(got[place.Area], place.Slot)
	}
	for area, slots := range want {
		if len(got[area]) != len(slots) {
			t.Fatalf("%s holds %v, want %v", area, got[area], slots)
		}
		for n, slot := range slots {
			if got[area][n] != slot {
				t.Errorf("%s place %d is %s, want %s", area, n, got[area][n], slot)
			}
		}
	}
	if len(got[AreaHidden]) != 0 {
		t.Errorf("the built-in arrangement hides %v", got[AreaHidden])
	}
}

// A slot nobody placed would be a field no default page ever shows, which is a
// slot added to the vocabulary and forgotten.
func TestEverySlotIsPlacedOnceByTheDefault(t *testing.T) {
	seen := map[Slot]int{}
	for _, place := range Default() {
		seen[place.Slot]++
		if place.Slot == "" || !place.Slot.Known() {
			t.Errorf("the default places %q, which is not a slot", place.Slot)
		}
		if !place.Area.Known() {
			t.Errorf("the default uses area %q, which is not an area", place.Area)
		}
	}
	for _, slot := range Slots() {
		if seen[slot] != 1 {
			t.Errorf("%s is placed %d times by the default, want once", slot, seen[slot])
		}
	}
}
