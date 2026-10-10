package team

import "testing"

// The refusal is what a person reads when a team cannot go, so it names what
// the team still carries and what to do about each.
func TestTheRefusalNamesWhatATeamStillCarries(t *testing.T) {
	cases := []struct {
		name  string
		holds carrying
		want  string
	}{
		{"nothing at all", carrying{}, ""},
		{
			"sprints, one of them running",
			carrying{Sprints: 4, Running: 1, Finished: 3},
			"Alpha still has 4 sprints, one of them running. A team keeps the sprints it has run as the record of what it delivered, so it cannot be deleted; rename it instead.",
		},
		{
			"a single sprint that is running",
			carrying{Sprints: 1, Running: 1},
			"Alpha still has 1 sprint, and it is running. A team keeps the sprints it has run as the record of what it delivered, so it cannot be deleted; rename it instead.",
		},
		{
			"finished sprints only",
			carrying{Sprints: 2, Finished: 2},
			"Alpha still has 2 sprints. A team keeps the sprints it has run as the record of what it delivered, so it cannot be deleted; rename it instead.",
		},
		{
			"sprints that have not started",
			carrying{Sprints: 2},
			"Alpha still has 2 sprints that have not started. Delete them first.",
		},
		{
			"one sprint that has not started",
			carrying{Sprints: 1},
			"Alpha still has 1 sprint that has not started. Delete it first.",
		},
		{
			"request types routed to it",
			carrying{RequestTypes: 2},
			"Alpha still has 2 request types routed to it. Route them to another team or to the project first.",
		},
		{
			"one request type",
			carrying{RequestTypes: 1},
			"Alpha still has 1 request type routed to it. Route it to another team or to the project first.",
		},
		{
			"issues and boards",
			carrying{Issues: 3, Boards: 1},
			"Alpha still has 3 issues. Hand them to another team or back to the project first. " +
				"Alpha still has 1 board. Delete it first.",
		},
		{
			"everything that can still be moved, named at once",
			carrying{Issues: 1, Boards: 2, Sprints: 1, RequestTypes: 3},
			"Alpha still has 1 issue. Hand it to another team or back to the project first. " +
				"Alpha still has 2 boards. Delete them first. " +
				"Alpha still has 1 sprint that has not started. Delete it first. " +
				"Alpha still has 3 request types routed to it. Route them to another team or to the project first.",
		},
		// Moving the rest would not help: a sprint that has run never leaves.
		{
			"a sprint that has run outweighs the rest",
			carrying{Issues: 5, RequestTypes: 2, Sprints: 3, Finished: 3},
			"Alpha still has 3 sprints. A team keeps the sprints it has run as the record of what it delivered, so it cannot be deleted; rename it instead.",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := refusal("Alpha", c.holds); got != c.want {
				t.Errorf("refusal =\n  %q\nwant\n  %q", got, c.want)
			}
		})
	}
}
