//go:build integration

package test

import (
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/component"
	"github.com/armature/armature/backend/internal/csvio"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/version"
)

// An issue is more than its own fields: the epic over it, the sprint and the
// version it is in, what it blocks, and what was said and spent on it.
func TestAnImportBringsTheWholeIssue(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "wholeissue")
	byrne := h.joinExisting(t, ws, h.email(t, "ada.byrne"), "member")
	files := csvio.NewService(h.cluster, ws.issues, label.NewService(h.cluster), field.NewService(h.cluster), h.authService()).
		WithPlanning(ws.sprints, version.NewService(h.cluster), component.NewService(h.cluster), ws.teams)

	data, err := os.ReadFile("testdata/jira-export.csv")
	if err != nil {
		t.Fatal(err)
	}
	_, mapping, err := files.Preview(ws.ctx, ws.project.Key, data, nil)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	for _, target := range []string{"epic", "sprint", "comments", "worklogs", "links:blocks", "fixVersions", "components"} {
		if len(mapping[target]) == 0 {
			t.Errorf("%s was not mapped; the file has a column for it", target)
		}
	}

	// The file's people are matched by the address that says who they are.
	people := csvio.People{Choices: map[string]csvio.PersonChoice{"Ada.Byrne": {Member: &byrne}}}
	ask := csvio.ImportRequest{
		Mapping: mapping,
		Values:  csvio.Values{"status": {"Requested": "To Do"}},
		People:  people,
		Source:  "jira-export.csv",
	}

	// What the dry run promises is what the real run does, parents included.
	dryAsk := ask
	dryAsk.DryRun = true
	dry, _, err := files.Import(ws.ctx, ws.project.Key, data, dryAsk, ws.actor)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}

	report, _, err := files.Import(ws.ctx, ws.project.Key, data, ask, ws.actor)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(report.Imported) != 7 {
		t.Fatalf("imported %d of 7: refused %+v", len(report.Imported), report.Refused)
	}
	if len(dry.Imported) != len(report.Imported) {
		t.Errorf("the dry run promised %d and the run made %d", len(dry.Imported), len(report.Imported))
	}

	epic, story := ws.project.Key+"-1", ws.project.Key+"-2"
	made, err := ws.issues.ByKey(ws.ctx, story)
	if err != nil {
		t.Fatal(err)
	}
	// The epic was written first, so the story under it had somewhere to hang.
	if made.ParentKey != epic {
		t.Errorf("parent = %q, want the epic the file named", made.ParentKey)
	}
	if made.Sprint == nil || made.Sprint.Name != "2026 - KW32/33" {
		t.Errorf("sprint = %+v, want the one the file named", made.Sprint)
	}
	if len(made.FixVersions) != 1 || made.FixVersions[0].Name != "1.0" {
		t.Errorf("fix versions = %+v, want the one the file named", made.FixVersions)
	}
	if len(made.Components) != 1 || made.Components[0].Name != "Cluster" {
		t.Errorf("components = %+v, want the one the file named", made.Components)
	}
	// The database stores a document as jsonb, which spaces itself out.
	if !strings.Contains(string(made.Description), "heading") || !strings.Contains(string(made.Description), "bulletList") {
		t.Errorf("the description kept none of its markup: %s", made.Description)
	}

	links, err := ws.issues.Links(ws.ctx, story)
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].TypeName != "Blocks" || links[0].Direction != "outward" || links[0].Issue.Key != ws.project.Key+"-3" {
		t.Errorf("links = %+v, want it blocking the issue the file named", links)
	}

	comments, err := ws.issues.Comments(ws.ctx, story, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || !strings.Contains(string(comments[0].Body), "Looks right") {
		t.Fatalf("comments = %+v, want the one the file holds", comments)
	}
	if comments[0].CreatedAt.Format("2006-01-02") != "2026-08-24" {
		t.Errorf("the comment is dated %s, want the day it was written", comments[0].CreatedAt)
	}

	// Two entries written in one minute are two entries, not one.
	worklogs, err := ws.issues.Worklogs(ws.ctx, story)
	if err != nil {
		t.Fatal(err)
	}
	spent := 0
	for _, each := range worklogs {
		spent += each.Minutes
	}
	if len(worklogs) != 2 || spent != 720 {
		t.Fatalf("worklogs = %+v, want both of the file's entries and twelve hours", worklogs)
	}
	if worklogs[0].Author == nil || worklogs[0].Author.ID != byrne {
		t.Errorf("worklog author = %+v, want the person the file named", worklogs[0].Author)
	}

	// An export is usually a filtered view, so it names epics it does not carry.
	// That costs the issue its parent, not its place in the tracker.
	orphan, err := ws.issues.ByKey(ws.ctx, ws.project.Key+"-7")
	if err != nil {
		t.Fatalf("the row whose epic is not in the file: %v", err)
	}
	if orphan.ParentKey != "" {
		t.Errorf("parent = %q, want none", orphan.ParentKey)
	}
	if !saidAbout(report.Notes, "IT-99") {
		t.Errorf("notes = %v, want the parent that is not in the file named", report.Notes)
	}

	// Running the file again writes none of it twice.
	again, _, err := files.Import(ws.ctx, ws.project.Key, data, ask, ws.actor)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if len(again.Imported) != 0 || len(again.Updated) != 7 {
		t.Errorf("second import = %d made, %d corrected", len(again.Imported), len(again.Updated))
	}
	after, _ := ws.issues.Comments(ws.ctx, story, true)
	logs, _ := ws.issues.Worklogs(ws.ctx, story)
	if len(after) != 1 || len(logs) != 2 {
		t.Errorf("a second run left %d comments and %d worklogs", len(after), len(logs))
	}
}

// Two projects of one organization may be told the same file. The second makes
// its own issues; the first one's are not touched, whatever the file calls them.
func TestTwoProjectsMayImportTheSameFile(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "twice")
	files := csvio.NewService(h.cluster, ws.issues, label.NewService(h.cluster), field.NewService(h.cluster), h.authService()).
		WithPlanning(ws.sprints, version.NewService(h.cluster), component.NewService(h.cluster), ws.teams)
	data, err := os.ReadFile("testdata/jira-export.csv")
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, _, err := ws.projects.Create(ws.ctx, project.CreateInput{
		Name: "The same work again", Key: "TW" + strings.ToUpper(uuid.New().String()[:4]),
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("the second project: %v", err)
	}

	ask := csvio.ImportRequest{Values: csvio.Values{"status": {"Requested": "To Do"}}, Source: "jira-export.csv"}
	here, _, err := files.Import(ws.ctx, ws.project.Key, data, ask, ws.actor)
	if err != nil {
		t.Fatalf("the first import: %v", err)
	}
	there, _, err := files.Import(ws.ctx, elsewhere.Key, data, ask, ws.actor)
	if err != nil {
		t.Fatalf("the second import: %v", err)
	}
	if len(here.Imported) != 7 || len(there.Imported) != 7 {
		t.Fatalf("made %d then %d, want seven each; refused %+v", len(here.Imported), len(there.Imported), there.Refused)
	}
	if len(there.Updated) != 0 {
		t.Errorf("the second import corrected %d issues of the first project", len(there.Updated))
	}
	for _, key := range []string{ws.project.Key, elsewhere.Key} {
		all, err := ws.issues.List(ws.ctx, issue.Filter{ProjectKey: key}, issue.Page{Limit: 50})
		if err != nil || all.Total != 7 {
			t.Errorf("%s holds %d issues, want seven (%v)", key, all.Total, err)
		}
	}
}
