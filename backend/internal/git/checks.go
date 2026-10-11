package git

import (
	"slices"
	"sort"
)

// CIChecks is how CI came out on one commit: the latest run of every check
// against it, and the one answer they add up to.
type CIChecks struct {
	Status CIStatus `json:"status"`
	// Checks are the runs behind Status, one per check name, by name.
	Checks []CIRun `json:"checks"`
}

// outweighs lists the statuses from least to most telling. A failure decides
// it; a cancelled check never passed, so it must not read as green either.
var outweighs = []CIStatus{CISuccess, CICancelled, CIPending, CIFailure}

// CombineChecks reads the runs against one commit as a single answer. Hosts
// send a run per job, so the newest run alone says nothing about the others.
func CombineChecks(runs []CIRun) *CIChecks {
	if len(runs) == 0 {
		return nil
	}
	latest := map[string]CIRun{}
	for _, run := range runs {
		if seen, ok := latest[run.Name]; !ok || run.StartedAt.After(seen.StartedAt) {
			latest[run.Name] = run
		}
	}
	out := &CIChecks{Status: CISuccess, Checks: make([]CIRun, 0, len(latest))}
	for _, run := range latest {
		out.Checks = append(out.Checks, run)
		if slices.Index(outweighs, run.Status) > slices.Index(outweighs, out.Status) {
			out.Status = run.Status
		}
	}
	sort.Slice(out.Checks, func(i, j int) bool { return out.Checks[i].Name < out.Checks[j].Name })
	return out
}

// checksBySHA combines the runs on each commit, keyed by the commit's sha.
func checksBySHA(runs []CIRun) map[string]*CIChecks {
	grouped := map[string][]CIRun{}
	for _, run := range runs {
		grouped[run.SHA] = append(grouped[run.SHA], run)
	}
	out := make(map[string]*CIChecks, len(grouped))
	for sha, list := range grouped {
		out[sha] = CombineChecks(list)
	}
	return out
}
