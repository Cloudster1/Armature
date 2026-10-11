package git

import (
	"strconv"
	"testing"
	"time"
)

func checkAt(name string, status CIStatus, minute int) CIRun {
	return CIRun{
		ExternalID: name + "@" + strconv.Itoa(minute), Name: name, Status: status, SHA: "head0001",
		StartedAt: time.Date(2026, 9, 1, 10, minute, 0, 0, time.UTC),
	}
}

// The issue's case: GitHub sends a run per job, and the one started last says
// nothing about the others.
func TestAFailedCheckOutweighsALaterPassingOne(t *testing.T) {
	got := CombineChecks([]CIRun{checkAt("lint", CISuccess, 1), checkAt("test", CIFailure, 0)})
	if got == nil || got.Status != CIFailure {
		t.Fatalf("combined = %+v, want failure", got)
	}
	if len(got.Checks) != 2 {
		t.Errorf("checks = %+v, want both behind the answer", got.Checks)
	}
}

// A rerun of a check replaces the run it reran, so a fixed failure stops
// counting.
func TestOnlyTheLatestRunOfEachCheckCounts(t *testing.T) {
	got := CombineChecks([]CIRun{checkAt("test", CIFailure, 0), checkAt("lint", CISuccess, 1), checkAt("test", CISuccess, 5)})
	if got == nil || got.Status != CISuccess {
		t.Fatalf("combined = %+v, want success once the rerun passed", got)
	}
	if len(got.Checks) != 2 {
		t.Fatalf("checks = %+v, want one per name", got.Checks)
	}
	for _, c := range got.Checks {
		if c.Name == "test" && c.Status != CISuccess {
			t.Errorf("test = %+v, want the rerun", c)
		}
	}
}

func TestChecksCombineFailureThenPendingThenSuccess(t *testing.T) {
	cases := []struct {
		name string
		runs []CIRun
		want CIStatus
	}{
		{"all passed", []CIRun{checkAt("lint", CISuccess, 0), checkAt("test", CISuccess, 1)}, CISuccess},
		{"one still running", []CIRun{checkAt("lint", CISuccess, 0), checkAt("test", CIPending, 1)}, CIPending},
		{"a failure outweighs one running", []CIRun{checkAt("lint", CIFailure, 0), checkAt("test", CIPending, 1)}, CIFailure},
		{"a failure outweighs a cancellation", []CIRun{checkAt("lint", CIFailure, 0), checkAt("test", CICancelled, 1)}, CIFailure},
		{"one running outweighs a cancellation", []CIRun{checkAt("lint", CICancelled, 0), checkAt("test", CIPending, 1)}, CIPending},
		// A cancelled check never passed, so it must not read as green.
		{"a cancellation is not a pass", []CIRun{checkAt("lint", CISuccess, 0), checkAt("test", CICancelled, 1)}, CICancelled},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CombineChecks(c.runs)
			if got == nil || got.Status != c.want {
				t.Errorf("combined = %+v, want %s", got, c.want)
			}
		})
	}
}

// The database hands runs newest first, but the answer must not hang on that.
func TestTheCombinationDoesNotDependOnOrder(t *testing.T) {
	forward := CombineChecks([]CIRun{checkAt("test", CIFailure, 0), checkAt("test", CISuccess, 5)})
	backward := CombineChecks([]CIRun{checkAt("test", CISuccess, 5), checkAt("test", CIFailure, 0)})
	if forward.Status != CISuccess || backward.Status != CISuccess {
		t.Errorf("forward = %s, backward = %s, want both the rerun's success", forward.Status, backward.Status)
	}
}

func TestChecksAreListedByName(t *testing.T) {
	got := CombineChecks([]CIRun{checkAt("test", CISuccess, 2), checkAt("build", CISuccess, 0), checkAt("lint", CISuccess, 1)})
	var names []string
	for _, c := range got.Checks {
		names = append(names, c.Name)
	}
	if len(names) != 3 || names[0] != "build" || names[1] != "lint" || names[2] != "test" {
		t.Errorf("names = %v, want build, lint, test", names)
	}
}

func TestNoRunsIsNoAnswer(t *testing.T) {
	if got := CombineChecks(nil); got != nil {
		t.Errorf("combined = %+v, want nil", got)
	}
}
