package milestone

import "testing"

func TestMeasureCountsDoneOverEverythingAssigned(t *testing.T) {
	cases := []struct {
		done, inProgress, todo int
		percent                int
	}{
		{0, 0, 0, 0},
		{1, 1, 2, 25},
		{2, 0, 1, 66},
		{3, 0, 0, 100},
	}
	for _, tc := range cases {
		got := Measure(tc.done, tc.inProgress, tc.todo)
		if got.Percent != tc.percent {
			t.Errorf("Measure(%d, %d, %d).Percent = %d, want %d", tc.done, tc.inProgress, tc.todo, got.Percent, tc.percent)
		}
		if got.Issues != tc.done+tc.inProgress+tc.todo {
			t.Errorf("Issues = %d, want the sum", got.Issues)
		}
	}
}
