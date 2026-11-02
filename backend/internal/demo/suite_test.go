package demo

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/markdown"
)

func TestEverySnapshotBodyIsADescriptionTheTrackerShows(t *testing.T) {
	snapshots, err := Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range snapshots {
		if len(s.Issues) == 0 {
			t.Errorf("the %s snapshot holds no issues; take it again with make demo-snapshot", s.Repo)
		}
		for _, gh := range s.Issues {
			doc := markdown.Document(gh.Body, "https://github.com/"+s.Repo+"/blob/"+s.Commit+"/")
			if doc == nil {
				continue
			}
			if err := issue.ValidateDocument(doc, "description"); err != nil {
				t.Errorf("%s#%d: %v", s.Repo, gh.Number, err)
			}
		}
	}
}

func TestDependenciesAreReadAsExciterReadsThem(t *testing.T) {
	body := "## User story\n\nAs somebody.\n\n## Depends on\n\n- #12 and Cloudster1/Exciter#59\n- #12 again\n\n## Notes\n\nSee #99, which is no dependency.\nDepends on: #7."
	got := Dependencies(body)
	want := []IssueRef{{Number: 12}, {Repo: "Cloudster1/Exciter", Number: 59}, {Number: 7}}
	if !slices.Equal(got, want) {
		t.Errorf("dependencies %v, want %v", got, want)
	}
}

func TestTheSnapshotMapsToIssuesUnderTheirGitHubNumbers(t *testing.T) {
	closed := time.Date(2026, 10, 12, 8, 14, 45, 0, time.UTC)
	opened := closed.Add(-time.Hour)
	snapshots := []Snapshot{
		{Repo: "Cloudster1/Armature", Key: "ARM", Name: "Armature", Commit: "abc", Taken: "2026-10-21", Issues: []GitHubIssue{
			{Number: 300, Title: "Add a suite demo", CreatedAt: opened, Labels: []string{"enhancement"}, URL: "https://github.com/Cloudster1/Armature/issues/300",
				Body: "## User story\n\nAs someone.\n\n## Depends on\n\nCloudster1/Exciter#59, praetorianer777/Dynamo#4 and #301."},
			{Number: 301, Title: "fix: the seed waits", CreatedAt: opened, Closed: true, ClosedAt: &closed, Milestone: "M1"},
		}},
		{Repo: "Cloudster1/Exciter", Key: "EXC", Name: "Exciter", Commit: "def", Taken: "2026-10-21", Issues: []GitHubIssue{
			{Number: 59, Title: "Trace links", CreatedAt: opened, Labels: []string{"type/bug", "area/web"}, Milestone: "M2 Tracing"},
			{Number: 261, Title: "Risk reporting", CreatedAt: opened, Milestone: "M2 Tracing", Body: "Depends on #59 and #400."},
		}},
	}
	plan := PlanSuite(snapshots, 3)

	if len(plan.Projects) != 2 || plan.Projects[0].Key != "ARM" || plan.Projects[1].Key != "EXC" {
		t.Fatalf("projects %+v", plan.Projects)
	}
	arm, exc := plan.Projects[0], plan.Projects[1]
	if got := arm.Issues[0].Key(arm.Key); got != "ARM-300" {
		t.Errorf("Armature issue #300 is %s", got)
	}
	if got := exc.Issues[1].Key(exc.Key); got != "EXC-261" {
		t.Errorf("Exciter issue #261 is %s", got)
	}
	story, fix, bug, task := arm.Issues[0], arm.Issues[1], exc.Issues[0], exc.Issues[1]
	if story.TypeName != bootstrap.TypeStory || fix.TypeName != bootstrap.TypeBug || bug.TypeName != bootstrap.TypeBug || task.TypeName != bootstrap.TypeTask {
		t.Errorf("types %s, %s, %s, %s", story.TypeName, fix.TypeName, bug.TypeName, task.TypeName)
	}
	if story.Done || !fix.Done || fix.ResolvedAt == nil || !fix.ResolvedAt.Equal(closed) {
		t.Errorf("states: open %v, closed %v at %v", story.Done, fix.Done, fix.ResolvedAt)
	}
	if !story.CreatedAt.Equal(opened) || story.URL != "https://github.com/Cloudster1/Armature/issues/300" {
		t.Errorf("the issue lost its date or address: %+v", story)
	}
	if !slices.Equal(bug.Labels, []string{"type/bug", "area/web"}) || bug.Milestone != "M2 Tracing" {
		t.Errorf("labels %v, milestone %q", bug.Labels, bug.Milestone)
	}
	if !slices.Equal(names(exc.Milestones), []string{"M2 Tracing"}) || !slices.Equal(names(arm.Milestones), []string{"M1"}) {
		t.Errorf("milestones %v and %v", arm.Milestones, exc.Milestones)
	}
	if story.Reporter != 0 || story.Assignee != 1 || fix.Reporter != 1 || fix.Assignee != 2 {
		t.Errorf("people: #300 by %d for %d, #301 by %d for %d", story.Reporter, story.Assignee, fix.Reporter, fix.Assignee)
	}
	var doc struct{ Type string }
	if err := json.Unmarshal(story.Description, &doc); err != nil || doc.Type != "doc" {
		t.Errorf("the body is no description: %s", story.Description)
	}

	// Dynamo is no project here and EXC-400 is no issue, so neither is linked.
	want := []SuiteLink{{Blocker: "EXC-59", Blocked: "ARM-300"}, {Blocker: "ARM-301", Blocked: "ARM-300"}, {Blocker: "EXC-59", Blocked: "EXC-261"}}
	if !slices.Equal(plan.Links, want) {
		t.Errorf("links %v, want %v", plan.Links, want)
	}
}

func TestTheTwoDemosNeverWantTheSameKey(t *testing.T) {
	for _, key := range SuiteKeys {
		if slices.Contains(Keys, key) {
			t.Errorf("both demos want %s", key)
		}
	}
	if SuiteKeysText() != "ARM, EXC, STA and ROT" {
		t.Errorf("the keys read %q", SuiteKeysText())
	}
}

func names(milestones []SuiteMilestone) []string {
	var out []string
	for _, m := range milestones {
		out = append(out, m.Name)
	}
	return out
}

func TestMilestonesAndLabelsComeAsGitHubHasThem(t *testing.T) {
	due := time.Date(2026, 11, 30, 8, 0, 0, 0, time.UTC)
	snapshots := []Snapshot{
		{Repo: "Cloudster1/Armature", Key: "ARM", Taken: "2026-11-01",
			Milestones: []GitHubMilestone{
				{Title: "M0 Foundation", Description: "The skeleton.", Closed: true},
				{Title: "M1 Planning", DueOn: &due},
				{Title: "Backlog"},
			},
			Labels: []GitHubLabel{{Name: "bug", Color: "d73a4a"}, {Name: "good first issue", Color: "7057ff"}, {Name: "wontfix", Color: "ffffff"}},
			Issues: []GitHubIssue{
				{Number: 1, Title: "Start", Milestone: "M0 Foundation"},
				{Number: 2, Title: "Plan", Milestone: "Gone since"},
			}},
		{Repo: "Cloudster1/Rotor", Key: "ROT", Taken: "2026-11-01",
			Labels: []GitHubLabel{{Name: "Bug", Color: "0e8a16"}, {Name: "area/infra", Color: "c5def5"}, {Name: "odd", Color: "nope"}}},
	}
	plan := PlanSuite(snapshots, 1)

	arm := plan.Projects[0]
	want := []SuiteMilestone{
		{Name: "M0 Foundation", Description: "The skeleton.", Closed: true},
		{Name: "M1 Planning", DueOn: &due},
		{Name: "Backlog"},
		{Name: "Gone since"},
	}
	if len(arm.Milestones) != len(want) {
		t.Fatalf("milestones %+v", arm.Milestones)
	}
	for i, m := range arm.Milestones {
		w := want[i]
		if m.Name != w.Name || m.Description != w.Description || m.Closed != w.Closed || (m.DueOn == nil) != (w.DueOn == nil) || (m.DueOn != nil && !m.DueOn.Equal(*w.DueOn)) {
			t.Errorf("milestone %d is %+v, want %+v", i, m, w)
		}
	}

	// Labels are shared, so Rotor's green Bug is Armature's red bug; GitHub's spaces become hyphens.
	got := map[string]string{}
	for _, l := range plan.Labels {
		got[l.Name] = l.Color
	}
	wantLabels := map[string]string{"bug": "red", "good-first-issue": "purple", "wontfix": "gray", "area/infra": "blue", "odd": got["odd"]}
	if len(got) != len(wantLabels) {
		t.Fatalf("labels %v", got)
	}
	for name, color := range wantLabels {
		if got[name] != color {
			t.Errorf("label %s is %s, want %s", name, got[name], color)
		}
	}
	if got["odd"] == "" {
		t.Error("a label with an unreadable colour got none")
	}
}

func TestEverySnapshotListsTheMilestonesItsIssuesName(t *testing.T) {
	snapshots, err := Snapshots()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != len(SuiteKeys) || snapshots[len(snapshots)-1].Repo != "Cloudster1/Rotor" {
		t.Fatalf("the snapshot holds %d repositories", len(snapshots))
	}
	for _, s := range snapshots {
		if len(s.Milestones) == 0 || len(s.Labels) == 0 || s.Taken != snapshots[0].Taken {
			t.Errorf("%s was taken on %s with %d milestones and %d labels; take it again with make demo-snapshot", s.Repo, s.Taken, len(s.Milestones), len(s.Labels))
		}
		listed := map[string]bool{}
		for _, m := range s.Milestones {
			listed[m.Title] = true
		}
		for _, gh := range s.Issues {
			if gh.Milestone != "" && !listed[gh.Milestone] {
				t.Errorf("%s#%d names the milestone %q, which the snapshot does not list", s.Repo, gh.Number, gh.Milestone)
			}
		}
	}
}
