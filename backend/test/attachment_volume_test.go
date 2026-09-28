//go:build integration

package test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/armature/armature/backend/internal/attachment"
)

// A deployment without a bucket keeps its files on a volume, and moves them
// into a bucket later with one command. Tried the whole way: uploaded through
// the API onto the directory, copied into the real bucket, read back from it.
func TestAttachmentsLiveOnAVolumeAndMoveToTheBucket(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	dir := filepath.Join(t.TempDir(), "attachments")
	volume, err := attachment.NewFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The same service the api was built with, over the directory instead.
	_, issues, _, _, _ := h.services()
	api.api.Attachments = attachment.NewService(h.cluster, volume, issues)

	owner := api.client(t)
	owner.signup(t, h, "volume")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "On disk", "key": "DSK"}), http.StatusCreated, "create project")
	issued := want(t, owner.post("/api/v1/projects/DSK/issues", map[string]any{"summary": "has a file on disk"}), http.StatusCreated, "create issue")
	key := obj(t, issued, "issue")["key"].(string)

	content := []byte("%PDF-1.4\n%fake but sniffable\n")
	uploaded := owner.upload("/api/v1/issues/"+key+"/attachments", "file", "spec.pdf", "application/pdf", content)
	if uploaded.Status != http.StatusCreated {
		t.Fatalf("upload: %d %s", uploaded.Status, uploaded.Raw)
	}
	id := obj(t, uploaded, "attachment")["id"].(string)

	var onDisk []string
	if err := volume.Walk(context.Background(), func(o attachment.Object) error { onDisk = append(onDisk, o.Key); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(onDisk) != 1 {
		t.Fatalf("the directory holds %v, want the one file", onDisk)
	}
	if stat, err := os.Stat(filepath.Join(dir, filepath.FromSlash(onDisk[0]))); err != nil || stat.Size() != int64(len(content)) {
		t.Fatalf("the file on disk: %v %v", stat, err)
	}

	t.Run("the api serves it from the directory", func(t *testing.T) {
		resp, data := owner.download("/api/v1/attachments/" + id)
		if resp.StatusCode != http.StatusOK || !bytes.Equal(data, content) {
			t.Fatalf("download: %d %q", resp.StatusCode, data)
		}
	})

	t.Run("the move copies every file into the bucket, and again does no harm", func(t *testing.T) {
		bucket := h.attachmentStore(t)
		log := slog.New(slog.NewTextHandler(io.Discard, nil))
		report, err := attachment.Migrate(context.Background(), volume, bucket, log)
		if err != nil || report.Copied != 1 || report.Bytes != int64(len(content)) || len(report.Failed) != 0 {
			t.Fatalf("migrate: %+v %v", report, err)
		}
		body, err := bucket.Get(context.Background(), onDisk[0])
		if err != nil {
			t.Fatalf("the bucket does not have it: %v", err)
		}
		got, _ := io.ReadAll(body)
		_ = body.Close()
		if !bytes.Equal(got, content) {
			t.Fatalf("the bucket's copy differs: %q", got)
		}
		if again, err := attachment.Migrate(context.Background(), volume, bucket, log); err != nil || again.Copied != 1 {
			t.Fatalf("a second run is not a plain copy: %+v %v", again, err)
		}
		t.Cleanup(func() { _ = bucket.Delete(context.Background(), onDisk[0]) })

		// Pointed at the bucket, the api serves the same bytes.
		api.api.Attachments = attachment.NewService(h.cluster, bucket, issues)
		resp, data := owner.download("/api/v1/attachments/" + id)
		if resp.StatusCode != http.StatusOK || !bytes.Equal(data, content) {
			t.Fatalf("download from the bucket: %d %q", resp.StatusCode, data)
		}
	})

	t.Run("a file that vanished from the volume is reported, not hidden", func(t *testing.T) {
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		api.api.Attachments = attachment.NewService(h.cluster, volume, issues)
		resp, _ := owner.download("/api/v1/attachments/" + id)
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("a missing file was served")
		}
	})
}
