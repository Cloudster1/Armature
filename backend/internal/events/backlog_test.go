package events

import "testing"

// Stream ids are a millisecond and a sequence: "10-0" comes after "9-5",
// which comparing them as strings gets wrong.
func TestStreamIDsCompareAsNumbers(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"9-5", "10-0", -1},
		{"10-0", "9-5", 1},
		{"1700000000000-2", "1700000000000-10", -1},
		{"1700000000000-3", "1700000000000-3", 0},
		{"0-0", "1-0", -1},
	} {
		if got := compareIDs(c.a, c.b); got != c.want {
			t.Errorf("compareIDs(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// The stream is trimmed below the oldest entry some group has not
// acknowledged, never by a length that ignores what a group has read.
func TestTheTrimStopsAtWhatTheSlowestGroupStillNeeds(t *testing.T) {
	t.Run("a group with deliveries outstanding needs the oldest of them", func(t *testing.T) {
		got, ok := oldestNeeded([]groupState{
			{lastDelivered: "500-0"},
			{lastDelivered: "480-0", pending: 3, oldestPending: "90-1"},
			{lastDelivered: "100-0"},
		})
		if !ok || got != "90-1" {
			t.Errorf("oldestNeeded = %q, %v, want 90-1", got, ok)
		}
	})

	t.Run("a group with nothing outstanding needs what follows its last delivery", func(t *testing.T) {
		got, ok := oldestNeeded([]groupState{
			{lastDelivered: "1000-0"},
			{lastDelivered: "999-7"},
		})
		if !ok || got != "999-7" {
			t.Errorf("oldestNeeded = %q, %v, want 999-7", got, ok)
		}
	})

	t.Run("a group that has read nothing keeps the whole stream", func(t *testing.T) {
		got, ok := oldestNeeded([]groupState{{lastDelivered: "1000-0"}, {lastDelivered: "0-0"}})
		if !ok || got != "0-0" {
			t.Errorf("oldestNeeded = %q, %v, want 0-0", got, ok)
		}
	})

	t.Run("ids are ordered as numbers across groups", func(t *testing.T) {
		got, ok := oldestNeeded([]groupState{{lastDelivered: "10-0"}, {lastDelivered: "9-0"}})
		if !ok || got != "9-0" {
			t.Errorf("oldestNeeded = %q, %v, want 9-0", got, ok)
		}
	})

	t.Run("with no group reading there is nobody to wait for", func(t *testing.T) {
		if _, ok := oldestNeeded(nil); ok {
			t.Error("oldestNeeded found a floor with no groups")
		}
	})
}

// The relay sends no more than fits under the cap, and nothing at all while
// the slowest group is that far behind.
func TestTheRelaySendsOnlyWhatFitsUnderTheCap(t *testing.T) {
	for _, c := range []struct {
		backlog, cap int64
		batch, want  int
	}{
		{backlog: 0, cap: 10_000, batch: 100, want: 100},
		{backlog: 9_950, cap: 10_000, batch: 100, want: 50},
		{backlog: 10_000, cap: 10_000, batch: 100, want: 0},
		{backlog: 12_000, cap: 10_000, batch: 100, want: 0},
	} {
		if got := room(c.backlog, c.cap, c.batch); got != c.want {
			t.Errorf("room(%d, %d, %d) = %d, want %d", c.backlog, c.cap, c.batch, got, c.want)
		}
	}
}
