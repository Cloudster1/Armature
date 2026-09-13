//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/workflow"
)

// upload sends one file as multipart form data, the way a browser does.
func (c *client) upload(path, fieldName, fileName, contentType string, data []byte) response {
	c.t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	header := make(map[string][]string)
	header["Content-Disposition"] = []string{`form-data; name="` + fieldName + `"; filename="` + fileName + `"`}
	if contentType != "" {
		header["Content-Type"] = []string{contentType}
	}
	part, err := form.CreatePart(header)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		c.t.Fatal(err)
	}
	_ = form.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, c.base+path, &body)
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("upload %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := response{Status: resp.StatusCode, Header: resp.Header, Raw: string(raw)}
	if len(bytes.TrimSpace(raw)) > 0 {
		_ = json.Unmarshal(raw, &out.Body)
	}
	apiContract.observe(c.t, http.MethodPost, path, resp.StatusCode, resp.Header.Get("Content-Type"), raw)
	return out
}

// download fetches raw bytes, which the JSON client refuses to do.
func (c *client) download(path string) (*http.Response, []byte) {
	c.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, c.base+path, nil)
	if err != nil {
		c.t.Fatal(err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("download %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	apiContract.observe(c.t, http.MethodGet, path, resp.StatusCode, resp.Header.Get("Content-Type"), data)
	return resp, data
}

func TestAnAttachmentRoundTripsThroughTheBucket(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "attacher")

	if made := owner.post("/api/v1/projects", map[string]any{"name": "Attached", "key": "ATT"}); made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}
	issued := owner.post("/api/v1/projects/ATT/issues", map[string]any{"summary": "has a screenshot"})
	if issued.Status != http.StatusCreated {
		t.Fatalf("create issue: %d %s", issued.Status, issued.Raw)
	}
	key := issued.Body["issue"].(map[string]any)["key"].(string)

	content := []byte("# Acceptance notes\n\nThe button is the wrong colour.\n")
	uploaded := owner.upload("/api/v1/issues/"+key+"/attachments", "file", "../notes.md", "text/markdown", content)
	if uploaded.Status != http.StatusCreated {
		t.Fatalf("upload: %d %s", uploaded.Status, uploaded.Raw)
	}
	record := uploaded.Body["attachment"].(map[string]any)
	id := record["id"].(string)
	if record["fileName"] != "notes.md" {
		t.Errorf("the path was not stripped from the name: %v", record["fileName"])
	}
	if record["size"] != float64(len(content)) || record["contentType"] != "text/markdown" {
		t.Errorf("record = %v", record)
	}

	t.Run("it is listed on the issue", func(t *testing.T) {
		listed := owner.get("/api/v1/issues/" + key + "/attachments")
		items := listed.Body["attachments"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["id"] != id {
			t.Fatalf("list = %s", listed.Raw)
		}
	})

	t.Run("the bytes come back exactly, as a download", func(t *testing.T) {
		resp, data := owner.download("/api/v1/attachments/" + id)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("download: %d %s", resp.StatusCode, data)
		}
		if !bytes.Equal(data, content) {
			t.Fatalf("bytes differ: %q", data)
		}
		if got := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment") || !strings.Contains(got, "notes.md") {
			t.Errorf("Content-Disposition = %q", got)
		}
		if resp.Header.Get("Content-Type") != "text/markdown" {
			t.Errorf("Content-Type = %q", resp.Header.Get("Content-Type"))
		}
	})

	t.Run("the changelog says it was attached", func(t *testing.T) {
		history := owner.get("/api/v1/issues/" + key + "/history")
		if !strings.Contains(history.Raw, `"field":"attachment"`) || !strings.Contains(history.Raw, `"to":"notes.md"`) {
			t.Fatalf("history = %s", history.Raw)
		}
	})

	t.Run("a stranger cannot reach it", func(t *testing.T) {
		stranger := api.client(t)
		stranger.signup(t, h, "attstranger")
		resp, _ := stranger.download("/api/v1/attachments/" + id)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("want 404, got %d", resp.StatusCode)
		}
		if del := stranger.delete("/api/v1/attachments/" + id); del.Status != http.StatusNotFound {
			t.Fatalf("want 404 on delete, got %d %s", del.Status, del.Raw)
		}
	})

	t.Run("deleting removes the row and the bytes", func(t *testing.T) {
		if del := owner.delete("/api/v1/attachments/" + id); del.Status != http.StatusNoContent {
			t.Fatalf("delete: %d %s", del.Status, del.Raw)
		}
		if resp, _ := owner.download("/api/v1/attachments/" + id); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("want 404 after delete, got %d", resp.StatusCode)
		}
		listed := owner.get("/api/v1/issues/" + key + "/attachments")
		if len(listed.Body["attachments"].([]any)) != 0 {
			t.Fatalf("still listed: %s", listed.Raw)
		}
		// The object itself is gone from the bucket, not just the row.
		store := h.attachmentStore(t)
		objectKey := "org/" + principalOrg(t, owner) + "/issue/" + issued.Body["issue"].(map[string]any)["id"].(string) + "/" + id + "/notes.md"
		if _, err := store.Get(context.Background(), objectKey); err != attachment.ErrNoObject {
			t.Fatalf("object should be gone, got %v", err)
		}
		var tombstones int
		_ = h.super.QueryRow(context.Background(), `SELECT count(*) FROM attachment_tombstone WHERE object_key = $1`, objectKey).Scan(&tombstones)
		if tombstones != 0 {
			t.Fatalf("a delete that removed the object should leave no tombstone, %d left", tombstones)
		}
	})
}

func TestUploadsThatCannotBeTakenAreRefused(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "refused")
	if made := owner.post("/api/v1/projects", map[string]any{"name": "Refusing", "key": "RFS"}); made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}
	issued := owner.post("/api/v1/projects/RFS/issues", map[string]any{"summary": "nothing fits"})
	key := issued.Body["issue"].(map[string]any)["key"].(string)

	if resp := owner.upload("/api/v1/issues/"+key+"/attachments", "file", "big.bin", "application/octet-stream",
		bytes.Repeat([]byte{1}, int(attachment.MaxSize)+1)); resp.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized file should be refused with 413, got %d %s", resp.Status, resp.Raw)
	}
	if resp := owner.upload("/api/v1/issues/"+key+"/attachments", "file", "empty.txt", "text/plain", nil); resp.Status != http.StatusBadRequest {
		t.Fatalf("an empty file should be refused with 400, got %d %s", resp.Status, resp.Raw)
	}
	if resp := owner.upload("/api/v1/issues/"+key+"/attachments", "document", "a.txt", "text/plain", []byte("x")); resp.Status != http.StatusBadRequest {
		t.Fatalf("a part with the wrong name should be refused with 400, got %d %s", resp.Status, resp.Raw)
	}
	if resp := owner.upload("/api/v1/issues/RFS-999/attachments", "file", "a.txt", "text/plain", []byte("x")); resp.Status != http.StatusNotFound {
		t.Fatalf("an unknown issue should give 404, got %d %s", resp.Status, resp.Raw)
	}
	var rows int
	_ = h.super.QueryRow(context.Background(), `SELECT count(*) FROM attachment a JOIN issue i ON i.id = a.issue_id JOIN project p ON p.id = i.project_id WHERE p.key = 'RFS'`).Scan(&rows)
	if rows != 0 {
		t.Fatalf("%d attachment rows written for refused uploads", rows)
	}
}

// principalOrg is the signed-in client's organization id.
func principalOrg(t *testing.T, c *client) string {
	t.Helper()
	me := c.get("/api/v1/auth/me")
	return principalField(t, me, "principal", "org", "id").(string)
}

// Deleting an issue takes its files with it: the rows cascade, a tombstone
// is left per object, and the reaper removes the bytes from the bucket.
func TestDeletingAnIssueRemovesItsFilesFromTheBucket(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "reaped")
	if made := owner.post("/api/v1/projects", map[string]any{"name": "Reaped", "key": "RPD"}); made.Status != http.StatusCreated {
		t.Fatalf("create project: %d %s", made.Status, made.Raw)
	}
	issued := owner.post("/api/v1/projects/RPD/issues", map[string]any{"summary": "doomed"})
	key := issued.Body["issue"].(map[string]any)["key"].(string)
	issueID := issued.Body["issue"].(map[string]any)["id"].(string)

	uploaded := owner.upload("/api/v1/issues/"+key+"/attachments", "file", "one.txt", "text/plain", []byte("one"))
	if uploaded.Status != http.StatusCreated {
		t.Fatalf("upload: %d %s", uploaded.Status, uploaded.Raw)
	}
	id := uploaded.Body["attachment"].(map[string]any)["id"].(string)
	store := h.attachmentStore(t)
	objectKey := "org/" + principalOrg(t, owner) + "/issue/" + issueID + "/" + id + "/one.txt"
	if _, err := store.Get(context.Background(), objectKey); err != nil {
		t.Fatalf("the object should be in the bucket before the delete: %v", err)
	}

	if del := owner.delete("/api/v1/issues/" + key); del.Status != http.StatusNoContent {
		t.Fatalf("delete issue: %d %s", del.Status, del.Raw)
	}
	var tombstones int
	_ = h.super.QueryRow(context.Background(), `SELECT count(*) FROM attachment_tombstone WHERE object_key = $1`, objectKey).Scan(&tombstones)
	if tombstones != 1 {
		t.Fatalf("want one tombstone for the object, got %d", tombstones)
	}

	engine := workflow.NewEngine(workflow.NewDefaultRegistry())
	service := attachment.NewService(h.cluster, store, issue.NewService(h.cluster, engine, workflow.NewStore()))
	removed, err := attachment.NewReaper(service, slog.New(slog.NewTextHandler(io.Discard, nil))).Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if removed < 1 {
		t.Fatalf("the reaper removed %d objects, want at least the one", removed)
	}
	if _, err := store.Get(context.Background(), objectKey); err != attachment.ErrNoObject {
		t.Fatalf("the object should be gone from the bucket, got %v", err)
	}
	_ = h.super.QueryRow(context.Background(), `SELECT count(*) FROM attachment_tombstone WHERE object_key = $1`, objectKey).Scan(&tombstones)
	if tombstones != 0 {
		t.Fatalf("the tombstone should be gone with the object, %d left", tombstones)
	}
}
