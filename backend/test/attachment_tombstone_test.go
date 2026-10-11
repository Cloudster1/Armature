//go:build integration

package test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/workflow"
)

// Deleting an epic takes its stories and their subtasks by cascade. Every file
// in that subtree leaves a tombstone, or its bytes stay in the bucket for good.
func TestDeletingAnEpicRemovesItsDescendantsFilesFromTheBucket(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	ctx := context.Background()
	owner := api.client(t)
	orgID := obj(t, owner.signup(t, h, "subtree"), "principal", "org")["id"].(string)
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Subtree", "key": "SBT"}), http.StatusCreated, "project")
	typeID := map[string]string{}
	for _, raw := range list(t, want(t, owner.get("/api/v1/issue-types"), http.StatusOK, "issue types"), "issueTypes") {
		typ := raw.(map[string]any)
		typeID[typ["name"].(string)] = typ["id"].(string)
	}
	create := func(summary, typeName, parentKey string) string {
		t.Helper()
		body := map[string]any{"summary": summary, "typeId": typeID[typeName]}
		if parentKey != "" {
			body["parentKey"] = parentKey
		}
		return obj(t, want(t, owner.post("/api/v1/projects/SBT/issues", body), http.StatusCreated, typeName), "issue")["key"].(string)
	}
	epic := create("the epic", bootstrap.TypeEpic, "")
	story := create("the story", bootstrap.TypeStory, epic)
	subtask := create("the step", bootstrap.TypeSubtask, story)

	objects := []string{
		uploadObject(t, h, owner, story, "story.txt"),
		uploadObject(t, h, owner, subtask, "step.txt"),
	}
	store := h.attachmentStore(t)
	for _, key := range objects {
		if _, err := store.Get(ctx, key); err != nil {
			t.Fatalf("%s should be in the bucket before the delete: %v", key, err)
		}
	}

	want(t, owner.delete("/api/v1/issues/"+epic), http.StatusNoContent, "delete the epic")
	for _, key := range objects {
		org, ok := tombstoneOrg(t, h, key)
		if !ok {
			t.Fatalf("no tombstone for %s, a file under the deleted epic", key)
		}
		if org == nil || *org != orgID {
			t.Errorf("the tombstone for %s names organization %v, want %s", key, org, orgID)
		}
	}

	reapUntilGone(t, h, store, objects...)
}

// The tombstone is the database's doing, not the service's: a row that leaves
// by any route, straight through SQL included, leaves one behind.
func TestAnAttachmentRowNeverLeavesWithoutATombstone(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	ctx := context.Background()
	store := h.attachmentStore(t)

	// withFile makes a project with one issue holding one file.
	withFile := func(c *client, project string) (issueID, objectKey string) {
		t.Helper()
		want(t, c.post("/api/v1/projects", map[string]any{"name": "Raw " + project, "key": project}), http.StatusCreated, "project")
		issued := obj(t, want(t, c.post("/api/v1/projects/"+project+"/issues", map[string]any{"summary": "goes by hand"}), http.StatusCreated, "issue"), "issue")
		return issued["id"].(string), uploadObject(t, h, c, issued["key"].(string), "raw.txt")
	}

	t.Run("an issue deleted in SQL", func(t *testing.T) {
		c := api.client(t)
		orgID := obj(t, c.signup(t, h, "rawissue"), "principal", "org")["id"].(string)
		issueID, objectKey := withFile(c, "RWI")
		if _, err := h.super.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID); err != nil {
			t.Fatal(err)
		}
		org, ok := tombstoneOrg(t, h, objectKey)
		if !ok {
			t.Fatal("deleting the issue in SQL left no tombstone for its file")
		}
		if org == nil || *org != orgID {
			t.Errorf("the tombstone names organization %v, want %s", org, orgID)
		}
		reapUntilGone(t, h, store, objectKey)
	})

	t.Run("an organization deleted in SQL", func(t *testing.T) {
		c := api.client(t)
		orgID := obj(t, c.signup(t, h, "raworg"), "principal", "org")["id"].(string)
		_, objectKey := withFile(c, "RWO")
		if _, err := h.super.Exec(ctx, `DELETE FROM org WHERE id = $1`, orgID); err != nil {
			t.Fatalf("deleting an organization with files in SQL: %v", err)
		}
		org, ok := tombstoneOrg(t, h, objectKey)
		if !ok {
			t.Fatal("deleting the organization in SQL left no tombstone for its file")
		}
		if org != nil {
			t.Errorf("the tombstone still names the deleted organization %s", *org)
		}
		reapUntilGone(t, h, store, objectKey)
	})
}

// uploadObject attaches a small file to an issue and returns its object key.
func uploadObject(t *testing.T, h *harness, c *client, issueKey, name string) string {
	t.Helper()
	uploaded := c.upload("/api/v1/issues/"+issueKey+"/attachments", "file", name, "text/plain", []byte(name))
	if uploaded.Status != http.StatusCreated {
		t.Fatalf("upload to %s: %d %s", issueKey, uploaded.Status, uploaded.Raw)
	}
	var objectKey string
	id := uploaded.Body["attachment"].(map[string]any)["id"]
	if err := h.super.QueryRow(context.Background(), `SELECT object_key FROM attachment WHERE id = $1`, id).Scan(&objectKey); err != nil {
		t.Fatal(err)
	}
	return objectKey
}

// tombstoneOrg is the organization an object's tombstone names, and whether
// there is a tombstone at all.
func tombstoneOrg(t *testing.T, h *harness, objectKey string) (*string, bool) {
	t.Helper()
	var org *string
	err := h.super.QueryRow(context.Background(), `SELECT org_id::text FROM attachment_tombstone WHERE object_key = $1`, objectKey).Scan(&org)
	return org, err == nil
}

// reapPasses bounds the reaper runs a test waits through; other tests leave
// tombstones too, and one pass takes a batch of them.
const reapPasses = 20

// reapUntilGone runs the reaper until the objects' tombstones are gone, then
// checks the bucket no longer holds their bytes.
func reapUntilGone(t *testing.T, h *harness, store attachment.Store, objectKeys ...string) {
	t.Helper()
	ctx := context.Background()
	engine := workflow.NewEngine(workflow.NewDefaultRegistry())
	service := attachment.NewService(h.cluster, store, issue.NewService(h.cluster, engine, workflow.NewStore()))
	reaper := attachment.NewReaper(service, slog.New(slog.NewTextHandler(io.Discard, nil)))
	left := len(objectKeys)
	for range reapPasses {
		if _, err := reaper.Once(ctx); err != nil {
			t.Fatal(err)
		}
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM attachment_tombstone WHERE object_key = ANY($1)`, objectKeys).Scan(&left); err != nil {
			t.Fatal(err)
		}
		if left == 0 {
			break
		}
	}
	if left != 0 {
		t.Fatalf("the reaper left %d of the tombstones behind", left)
	}
	for _, key := range objectKeys {
		if _, err := store.Get(ctx, key); err != attachment.ErrNoObject {
			t.Errorf("%s is still in the bucket: %v", key, err)
		}
	}
}
