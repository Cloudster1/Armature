//go:build integration

package test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
)

// An import reproduces a record: the key, the day, the person and the status
// are the file's. Every other writer still stamps those for itself.
func TestAnImportKeepsItsHistory(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "import")
	story := h.issueTypeID(t, ws, "Story")
	done := h.statusID(t, ws, "Done")
	reporter := h.joinExisting(t, ws, h.email(t, "reporter"), "member")

	filed := time.Date(2026, time.August, 24, 8, 51, 0, 0, time.UTC)
	touched := time.Date(2026, time.September, 8, 9, 39, 0, 0, time.UTC)
	resolved := time.Date(2026, time.August, 27, 8, 58, 0, 0, time.UTC)

	in := issue.ImportInput{
		CreateInput: issue.CreateInput{
			ProjectKey: ws.project.Key,
			TypeID:     story,
			Summary:    "Landscape T2 not syncing from T1",
		},
		ExternalKey:          "jira:ITRD-512",
		KeyNum:               ptr(int64(512)),
		StatusID:             done,
		ReporterID:           &reporter,
		CreatedAt:            filed,
		UpdatedAt:            touched,
		ResolvedAt:           &resolved,
		TimeRemainingMinutes: ptr(480),
		Source:               "a Jira export",
	}

	// The dating is the importer's alone. An ordinary actor asking for it is
	// asking to file an issue as somebody else, last month.
	if _, _, err := ws.issues.Import(ws.ctx, in, ws.actor); !errors.Is(err, issue.ErrNotAnImport) {
		t.Fatalf("import as an ordinary actor = %v, want it refused", err)
	}

	importer := ws.actor
	importer.Import = true
	first, _, err := ws.issues.Import(ws.ctx, in, importer)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if first.Updated {
		t.Error("the first import corrected something instead of making it")
	}
	made := first.Issue
	if made.Key != ws.project.Key+"-512" {
		t.Errorf("key = %s, want the number the issue had", made.Key)
	}
	if !made.CreatedAt.Equal(filed) || !made.UpdatedAt.Equal(touched) {
		t.Errorf("created %s updated %s, want %s and %s", made.CreatedAt, made.UpdatedAt, filed, touched)
	}
	if made.ResolvedAt == nil || !made.ResolvedAt.Equal(resolved) {
		t.Errorf("resolved = %v, want %s", made.ResolvedAt, resolved)
	}
	if made.Status.Name != "Done" {
		t.Errorf("status = %s, want the status it actually stands in", made.Status.Name)
	}
	if made.Reporter == nil || made.Reporter.ID != reporter {
		t.Errorf("reporter = %v, want the person who filed it", made.Reporter)
	}

	// The same file again corrects what it made rather than making it twice.
	in.Summary = "Landscape T2 not syncing from T1 (corrected)"
	again, _, err := ws.issues.Import(ws.ctx, in, importer)
	if err != nil {
		t.Fatalf("import again: %v", err)
	}
	if !again.Updated || again.Issue.Key != made.Key || again.Issue.Summary != in.Summary {
		t.Errorf("second import = %+v, want the same issue corrected", again.Issue)
	}
	if all, err := ws.issues.List(ws.ctx, issue.Filter{ProjectKey: ws.project.Key}, issue.Page{Limit: 50}); err != nil || all.Total != 1 {
		t.Fatalf("issues after two imports of one row = %d, want 1 (%v)", all.Total, err)
	}

	// A number already taken is not fought over, and the issues made here after
	// the import do not collide with what it placed.
	in.ExternalKey, in.Summary = "jira:ITRD-511", "Renew the certificates"
	taken, _, err := ws.issues.Import(ws.ctx, in, importer)
	if err != nil {
		t.Fatalf("import onto a taken number: %v", err)
	}
	if taken.Issue.Key == made.Key {
		t.Fatalf("two issues were given %s", made.Key)
	}
	ordinary, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{
		ProjectKey: ws.project.Key, TypeID: story, Summary: "Filed here, afterwards",
	}, ws.actor)
	if err != nil {
		t.Fatalf("create after import: %v", err)
	}
	if keyNum(t, ws.project.Key, ordinary.Key) <= 512 {
		t.Errorf("the next issue got %s, which the import had already used", ordinary.Key)
	}

	h.importedCommentAndWorklog(t, ws, made.Key, reporter, importer)
	h.datesAreTheImportersAlone(t, ws, made.ID, taken.Issue.ID)
}

// A conversation that already happened is filed as it was: its author, its day,
// and once however many times the file is imported.
func (h *harness) importedCommentAndWorklog(t *testing.T, ws *workspace, key string, author uuid.UUID, importer issue.Actor) {
	t.Helper()
	said := time.Date(2026, time.July, 28, 9, 19, 0, 0, time.UTC)
	body := issue.TextDocument("We would have to make RabbitMQ outside of the cluster available.")
	for range 2 {
		if _, err := ws.issues.AddImportedComment(ws.ctx, key, body, &author, said, "jira:comment:10001", importer); err != nil {
			t.Fatalf("import comment: %v", err)
		}
	}
	comments, err := ws.issues.Comments(ws.ctx, key, true)
	if err != nil {
		t.Fatalf("read comments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("comments = %d, want the one comment the file has", len(comments))
	}
	if !comments[0].CreatedAt.Equal(said) {
		t.Errorf("comment dated %s, want %s", comments[0].CreatedAt, said)
	}
	if comments[0].Author == nil || comments[0].Author.ID != author {
		t.Errorf("comment author = %v, want the person who wrote it", comments[0].Author)
	}

	spent := time.Date(2026, time.August, 28, 0, 0, 0, 0, time.UTC)
	for range 2 {
		if _, err := ws.issues.LogImportedWork(ws.ctx, key,
			issue.WorklogInput{Minutes: 960, StartedOn: &spent}, &author, "jira:worklog:20001", importer); err != nil {
			t.Fatalf("import worklog: %v", err)
		}
	}
	worklogs, err := ws.issues.Worklogs(ws.ctx, key)
	if err != nil {
		t.Fatalf("read worklogs: %v", err)
	}
	if len(worklogs) != 1 || worklogs[0].Minutes != 960 {
		t.Fatalf("worklogs = %+v, want the one entry the file has", worklogs)
	}
	if worklogs[0].Author == nil || worklogs[0].Author.ID != author {
		t.Errorf("worklog author = %v, want the person who did the work", worklogs[0].Author)
	}
	// The file said what remained; logging its own hours must not say otherwise.
	after, err := ws.issues.ByKey(ws.ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if after.TimeRemainingMinutes == nil || *after.TimeRemainingMinutes != 480 {
		t.Errorf("remaining = %v, want the 480 the file gave", after.TimeRemainingMinutes)
	}
}

// The database keeps both halves of the rule: an edit is stamped, an explicit
// time is kept, and one organization cannot hold two rows of the same name.
func (h *harness) datesAreTheImportersAlone(t *testing.T, ws *workspace, imported, other uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	if _, err := h.super.Exec(ctx, `UPDATE issue SET summary = 'edited straight through SQL' WHERE id = $1`, imported); err != nil {
		t.Fatalf("edit through SQL: %v", err)
	}
	var stamped time.Time
	if err := h.super.QueryRow(ctx, `SELECT updated_at FROM issue WHERE id = $1`, imported).Scan(&stamped); err != nil {
		t.Fatal(err)
	}
	if time.Since(stamped) > time.Minute {
		t.Errorf("an ordinary edit left updated_at at %s; the trigger stopped stamping", stamped)
	}

	past := time.Date(2026, time.September, 8, 9, 39, 0, 0, time.UTC)
	if _, err := h.super.Exec(ctx, `UPDATE issue SET updated_at = $2 WHERE id = $1`, imported, past); err != nil {
		t.Fatalf("date a row: %v", err)
	}
	var kept time.Time
	if err := h.super.QueryRow(ctx, `SELECT updated_at FROM issue WHERE id = $1`, imported).Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if !kept.UTC().Equal(past) {
		t.Errorf("updated_at = %s, want the %s the import gave", kept, past)
	}

	_, err := h.super.Exec(ctx, `UPDATE issue SET external_key = 'jira:ITRD-512' WHERE id = $1`, other)
	if err == nil {
		t.Fatal("two issues took the same external key")
	}
	if !strings.Contains(err.Error(), "issue_external_key_idx") {
		t.Errorf("error = %v, want the unique index to refuse it", err)
	}
}

func keyNum(t *testing.T, projectKey, key string) int {
	t.Helper()
	n, err := strconv.Atoi(strings.TrimPrefix(key, projectKey+"-"))
	if err != nil {
		t.Fatalf("read the number out of %q: %v", key, err)
	}
	return n
}
