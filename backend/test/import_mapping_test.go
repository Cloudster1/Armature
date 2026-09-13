//go:build integration

package test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/csvio"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
)

// A Jira export is the file this has to read: columns repeated under one name,
// its own words for types and statuses, and usernames where addresses go.
func TestAnImportMapsWhatItFinds(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "jiraimport")
	files := csvio.NewService(h.cluster, ws.issues, label.NewService(h.cluster), field.NewService(h.cluster), h.authService())
	byrne := h.joinExisting(t, ws, "ada.byrne@example.test", "member")

	data, err := os.ReadFile("testdata/jira-export.csv")
	if err != nil {
		t.Fatal(err)
	}

	// The preview reads the file as it is: by position, and with the words and
	// the people it holds.
	preview, mapping, err := files.Preview(ws.ctx, ws.project.Key, data, nil)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.Total != 7 || len(preview.Columns) != 27 {
		t.Fatalf("preview = %d rows over %d columns, want 7 over 27", preview.Total, len(preview.Columns))
	}
	if len(mapping["labels"]) != 3 {
		t.Errorf("labels mapped to %v, want the three columns of that name", mapping["labels"])
	}
	if len(mapping["timeEstimate"]) != 1 || len(mapping["key"]) != 1 {
		t.Errorf("the file's own key and estimate were not mapped: %v", mapping)
	}
	if means := wordMeans(preview.Words, "status", "Requested"); means != "" {
		t.Errorf("Requested was taken to mean %q; nothing here is called that", means)
	}
	if means := wordMeans(preview.Words, "status", "Done"); means != "Done" {
		t.Errorf("Done was taken to mean %q", means)
	}
	if who := personNamed(preview.People, "Ada.Byrne"); who == nil || who.Member == nil || *who.Member != byrne {
		t.Errorf("Ada.Byrne = %+v, want the member whose address says so", who)
	}
	if who := personNamed(preview.People, "Sam.Okonkwo"); who == nil || who.Member != nil {
		t.Errorf("Sam.Okonkwo = %+v, want nobody here", who)
	}

	// A refusal names the line the record starts on, not the record's number:
	// one of these rows is six lines long.
	broken := csvio.ImportRequest{Mapping: mapping, Values: csvio.Values{"status": {"Done": "Nowhere"}}, DryRun: true}
	dry, _, err := files.Import(ws.ctx, ws.project.Key, data, broken, ws.actor)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !refusedOnLine(dry.Refused, 3) || refusedOnLine(dry.Refused, 4) {
		t.Errorf("refusals = %+v, want the one starting on line 3 and nothing on line 4", dry.Refused)
	}

	h.importedAsTheFileSaysIt(t, ws, files, data, mapping, byrne)
}

func (h *harness) importedAsTheFileSaysIt(t *testing.T, ws *workspace, files *csvio.Service, data []byte, mapping csvio.Mapping, byrne uuid.UUID) {
	t.Helper()
	// The integration database outlives the run, so the account this makes is
	// given a domain of its own; an address held elsewhere is refused, rightly.
	domain := "made-" + uuid.New().String()[:8] + ".test"
	address := "sam.okonkwo@" + domain
	req := csvio.ImportRequest{
		Mapping: mapping,
		Values:  csvio.Values{"status": {"Requested": "To Do"}},
		People: csvio.People{
			Domain:  domain,
			Choices: map[string]csvio.PersonChoice{"Sam.Okonkwo": {Create: true}},
		},
		Source: "jira-export.csv",
	}
	report, _, err := files.Import(ws.ctx, ws.project.Key, data, req, ws.actor)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(report.Imported) != 7 || len(report.Refused) != 0 {
		t.Fatalf("import = %d made, %+v refused", len(report.Imported), report.Refused)
	}

	// The row that named a person nobody answers to still became an issue.
	if !saidAbout(report.Notes, "Grace.Hoff") {
		t.Errorf("notes = %v, want the name nobody answers to named", report.Notes)
	}
	if !saidAbout(report.Notes, address) {
		t.Errorf("notes = %v, want the account that was made named", report.Notes)
	}

	cluster, err := ws.issues.ByKey(ws.ctx, ws.project.Key+"-2")
	if err != nil {
		t.Fatalf("the issue that had key IT-2: %v", err)
	}
	if cluster.Summary != "Stand up the cluster" || cluster.Status.Name != "Done" || cluster.Priority != issue.PriorityHighest {
		t.Errorf("imported = %+v, want the file's summary, status and priority", cluster)
	}
	if cluster.CreatedAt.Format("2006-01-02") != "2026-08-04" || cluster.ResolvedAt == nil {
		t.Errorf("created %s resolved %v, want the days the file gave", cluster.CreatedAt, cluster.ResolvedAt)
	}
	if cluster.TimeEstimateMinutes == nil || *cluster.TimeEstimateMinutes != 960 {
		t.Errorf("estimate = %v, want the 57600 seconds the file wrote as minutes", cluster.TimeEstimateMinutes)
	}
	if len(cluster.Labels) != 2 {
		t.Errorf("labels = %+v, want both of the columns that held one", cluster.Labels)
	}
	if cluster.Reporter == nil || cluster.Assignee == nil || cluster.Assignee.ID != byrne {
		t.Errorf("people = %+v %+v, want the member the file's username matched", cluster.Assignee, cluster.Reporter)
	}

	runbook, err := ws.issues.ByKey(ws.ctx, ws.project.Key+"-4")
	if err != nil {
		t.Fatal(err)
	}
	if runbook.Status.Name != "To Do" {
		t.Errorf("status = %s, want what Requested was said to mean", runbook.Status.Name)
	}

	// The same file again corrects what it made; it does not make it twice.
	second, _, err := files.Import(ws.ctx, ws.project.Key, data, req, ws.actor)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if len(second.Updated) != 7 || len(second.Imported) != 0 {
		t.Fatalf("second import = %d made and %d corrected, want 0 and 7", len(second.Imported), len(second.Updated))
	}
	all, err := ws.issues.List(ws.ctx, issue.Filter{ProjectKey: ws.project.Key}, issue.Page{Limit: 50})
	if err != nil || all.Total != 7 {
		t.Fatalf("issues = %d, want the file's seven (%v)", all.Total, err)
	}

	// The account made for a name has no way in until somebody sets it up.
	var active bool
	if err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT is_active FROM app_user WHERE email = $1`, address).Scan(&active)
	}); err != nil {
		t.Fatalf("the account that was made: %v", err)
	}
	if active {
		t.Error("the account made for a name in the file can be signed into")
	}
}

func wordMeans(words []csvio.Word, target, value string) string {
	for _, w := range words {
		if w.Target == target && w.Value == value {
			return w.Means
		}
	}
	return "missing"
}

func personNamed(people []csvio.PersonFound, name string) *csvio.PersonFound {
	for n := range people {
		if people[n].Name == name {
			return &people[n]
		}
	}
	return nil
}

func refusedOnLine(refusals []csvio.Refusal, line int) bool {
	for _, r := range refusals {
		if r.Row == line {
			return true
		}
	}
	return false
}

func saidAbout(notes []string, about string) bool {
	for _, note := range notes {
		if strings.Contains(note, about) {
			return true
		}
	}
	return false
}
