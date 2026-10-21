package demo

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/issue"
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
			doc := Markdown(gh.Body, "https://github.com/"+s.Repo+"/blob/"+s.Commit+"/")
			if doc == nil {
				continue
			}
			if err := issue.ValidateDocument(doc, "description"); err != nil {
				t.Errorf("%s#%d: %v", s.Repo, gh.Number, err)
			}
		}
	}
}

func TestMarkdownKeepsWhatAnIssueBodyHolds(t *testing.T) {
	body := "## User story\n\nAs **somebody**, I want `code` and [docs](docs/api.md).\n\n" +
		"#### Deep heading\n\n- [x] done\n- [ ] open\n\n1. first\n2. second\n\n" +
		"| a | b |\n|---|---|\n| 1 | 2 |\n\n```go\nfmt.Println()\n```\n\n> quoted ~~gone~~\n\nSee https://example.com and [anchor](#here)."
	doc := Markdown(body, "https://github.com/o/r/blob/abc/")
	if err := issue.ValidateDocument(doc, "description"); err != nil {
		t.Fatalf("not a description: %v\n%s", err, doc)
	}
	text := string(doc)
	for _, want := range []string{
		`"type":"heading","attrs":{"level":2}`,
		`"type":"heading","attrs":{"level":3}`,
		`"type":"bold"`,
		`"type":"code"`,
		`"href":"https://github.com/o/r/blob/abc/docs/api.md"`,
		`"text":"[x] done"`,
		`"text":"[ ] open"`,
		`"type":"orderedList"`,
		`"text":"a | b"`,
		`"text":"1 | 2"`,
		`"type":"codeBlock","attrs":{"language":"go"}`,
		`"type":"blockquote"`,
		`"type":"strike"`,
		`"href":"https://example.com"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the description lacks %s:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"href":"#here"`) {
		t.Errorf("an anchor became a link the tracker would refuse:\n%s", text)
	}
}

func TestAnEmptyBodyIsNoDescription(t *testing.T) {
	if doc := Markdown("  \n\n", ""); doc != nil {
		t.Errorf("an empty body became %s", doc)
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
	if !slices.Equal(exc.Milestones, []string{"M2 Tracing"}) || !slices.Equal(arm.Milestones, []string{"M1"}) {
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
	if SuiteKeysText() != "ARM, EXC and STA" {
		t.Errorf("the keys read %q", SuiteKeysText())
	}
}
