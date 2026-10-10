//go:build integration

package test

import (
	"context"
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/csvio"
)

// exportedRows reads an export back: the header, then every record after it.
func exportedRows(t *testing.T, body []byte) ([]string, [][]string) {
	t.Helper()
	reader := csv.NewReader(strings.NewReader(string(body)))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil || len(records) == 0 {
		t.Fatalf("the export is not CSV: %v %q", err, string(body))
	}
	return records[0], records[1:]
}

// exportProject makes a project over the API and answers its key and id.
func exportProject(t *testing.T, c *client, name, prefix string) (string, string) {
	t.Helper()
	made := want(t, c.post("/api/v1/projects", map[string]any{"name": name, "key": prefix + strings.ToUpper(uuid.New().String()[:3]), "template": "kanban"}), http.StatusCreated, "project")
	p := obj(t, made, "project")
	return p["key"].(string), p["id"].(string)
}

// An export holds every issue the query matches, not one page of the search.
func TestACSVExportHoldsEveryIssueItMatches(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "exportall")
	key, _ := exportProject(t, c, "Exported", "EXA")

	const made = 120
	for n := range made {
		want(t, c.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "Exported " + strconv.Itoa(n+1)}), http.StatusCreated, "issue")
	}

	resp, body := c.download("/api/v1/issues/export?project=" + key + "&columns=key,summary")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export answered %d: %s", resp.StatusCode, body)
	}
	header, rows := exportedRows(t, body)
	if strings.Join(header, ",") != "key,summary" {
		t.Errorf("header = %v", header)
	}
	if len(rows) != made {
		t.Fatalf("the export holds %d rows, want all %d issues", len(rows), made)
	}
	for n, row := range rows {
		if want := key + "-" + strconv.Itoa(n+1); row[0] != want {
			t.Fatalf("row %d is %v, want %s in key order", n+1, row, want)
		}
	}
	if resp.Header.Get("X-Export-Truncated") != "" {
		t.Errorf("an export that holds everything says it was cut: %q", resp.Header.Get("X-Export-Truncated"))
	}
}

// More than an export holds is cut at the cap, and the file and the response
// both say so, so nobody mistakes the first five thousand for all of them.
func TestACSVExportOverTheCapSaysItWasCut(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "exportcap")
	// Two projects, so key numbers repeat across the result and paging has to
	// break those ties the same way every page.
	first, firstID := exportProject(t, c, "Exported first", "EXF")
	second, secondID := exportProject(t, c, "Exported second", "EXS")
	perProject := map[string]int{first: csvio.ExportRows/2 + 1, second: csvio.ExportRows / 2}
	ids := map[string]string{first: firstID, second: secondID}

	ctx := context.Background()
	var orgID uuid.UUID
	for key, count := range perProject {
		want(t, c.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "The one made by hand"}), http.StatusCreated, "issue")
		// The rest are copies of it written straight to the database: making
		// five thousand issues one request at a time proves nothing more.
		if _, err := h.super.Exec(ctx, `
			INSERT INTO issue (org_id, project_id, key_num, issue_type_id, status_id, summary, priority, reporter_id, rank)
			SELECT i.org_id, i.project_id, n, i.issue_type_id, i.status_id, 'Copy ' || n, i.priority, i.reporter_id, lpad(to_hex(n), 16, '0')
			FROM issue i, generate_series(2, $2::int) n
			WHERE i.project_id = $1 AND i.key_num = 1`, ids[key], count); err != nil {
			t.Fatalf("copy issues into %s: %v", key, err)
		}
		if err := h.super.QueryRow(ctx, `UPDATE project SET issue_seq = $2 WHERE id = $1 RETURNING org_id`, ids[key], count).Scan(&orgID); err != nil {
			t.Fatalf("move %s's key counter on: %v", key, err)
		}
	}
	var matched int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM issue WHERE org_id = $1`, orgID).Scan(&matched); err != nil {
		t.Fatal(err)
	}
	if matched <= csvio.ExportRows {
		t.Fatalf("the organization holds %d issues, want more than the %d an export holds", matched, csvio.ExportRows)
	}

	resp, body := c.download("/api/v1/issues/export?columns=key,summary")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export answered %d: %s", resp.StatusCode, body)
	}
	_, rows := exportedRows(t, body)
	if len(rows) != csvio.ExportRows+1 {
		t.Fatalf("the export holds %d rows, want %d issues and a note", len(rows), csvio.ExportRows)
	}
	seen := map[string]bool{}
	for _, row := range rows[:csvio.ExportRows] {
		if seen[row[0]] {
			t.Fatalf("%s is in the export twice: paging is not stable", row[0])
		}
		seen[row[0]] = true
	}
	note := rows[csvio.ExportRows]
	if len(note) != 1 || seen[note[0]] || !strings.Contains(note[0], strconv.Itoa(csvio.ExportRows)) || !strings.Contains(note[0], strconv.Itoa(matched)) || !strings.Contains(note[0], "narrow") {
		t.Errorf("the last row is %q, want a note naming %d of %d and what to do", note, csvio.ExportRows, matched)
	}
	if got := resp.Header.Get("X-Export-Truncated"); got != "true" {
		t.Errorf("X-Export-Truncated = %q, want true on an export that was cut", got)
	}

	// Inside one project the same rule holds: every issue up to the cap.
	resp, body = c.download("/api/v1/issues/export?project=" + second + "&columns=key")
	_, rows = exportedRows(t, body)
	if len(rows) != perProject[second] || resp.Header.Get("X-Export-Truncated") != "" {
		t.Errorf("one project's export holds %d rows (cut: %q), want all %d", len(rows), resp.Header.Get("X-Export-Truncated"), perProject[second])
	}
}
