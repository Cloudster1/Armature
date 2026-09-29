//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/perm"
)

// Putting the same address again retitles the page, so a sync can resend
// everything and the issue carries each page once, without an edit of its own.
func TestAPageLinkIsKeptInStepByItsAddress(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	signedUp := owner.signup(t, h, "pages")
	orgID := obj(t, signedUp, "principal", "org")["id"].(string)
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Pages", "key": "PGS"}), http.StatusCreated, "create project")
	key := obj(t, want(t, owner.post("/api/v1/projects/PGS/issues", map[string]any{"summary": "checkout flow"}), http.StatusCreated, "file issue"), "issue")["key"].(string)
	path := "/api/v1/issues/" + key + "/remote-links"

	countTopic := func(topic string) int {
		t.Helper()
		var n int
		if err := h.super.QueryRow(context.Background(), `
			SELECT count(*) FROM outbox_event WHERE org_id = $1 AND topic = $2 AND payload->>'key' = $3`,
			orgID, topic, key).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	editsBefore := countTopic(events.TopicIssueUpdated)

	want(t, owner.get(path), http.StatusOK, "no pages yet")
	if got := list(t, owner.get(path), "remoteLinks"); len(got) != 0 {
		t.Fatalf("a new issue carries %d pages, want none", len(got))
	}

	spec := "https://wiki.example.com/pages/checkout"
	first := obj(t, want(t, owner.post(path, map[string]any{
		"url": spec, "title": "Checkout spec", "source": "Stator", "iconUrl": "https://wiki.example.com/favicon.png",
	}), http.StatusCreated, "put a page on the issue"), "remoteLink")
	if first["title"] != "Checkout spec" || first["source"] != "Stator" || first["iconUrl"] != "https://wiki.example.com/favicon.png" {
		t.Errorf("the page came back as %v", first)
	}

	t.Run("the same address again retitles the page instead of adding a second", func(t *testing.T) {
		again := obj(t, want(t, owner.post(path, map[string]any{
			"url": spec, "title": "Checkout spec, second draft", "source": "Stator",
		}), http.StatusOK, "put the same page again"), "remoteLink")
		if again["id"] != first["id"] {
			t.Errorf("the page got a new id: %v, was %v", again["id"], first["id"])
		}
		if again["title"] != "Checkout spec, second draft" || again["iconUrl"] != nil {
			t.Errorf("the page was not brought in step: %v", again)
		}
		pages := list(t, owner.get(path), "remoteLinks")
		if len(pages) != 1 {
			t.Fatalf("the issue carries %d pages, want the one", len(pages))
		}
	})

	t.Run("an address that is not a web page is refused with a sentence", func(t *testing.T) {
		refused := want(t, owner.post(path, map[string]any{"url": "javascript:alert(1)", "title": "Sneaky", "source": "Stator"}), http.StatusUnprocessableEntity, "a script address")
		message, _ := refused.Error()["message"].(string)
		if !strings.Contains(message, "http:// or https://") || !strings.HasSuffix(message, ".") {
			t.Errorf("the refusal reads %q", message)
		}
		want(t, owner.post(path, map[string]any{"url": "https://wiki.example.com/x", "title": "", "source": "Stator"}), http.StatusUnprocessableEntity, "no title")
		want(t, owner.post("/api/v1/issues/PGS-999/remote-links", map[string]any{"url": spec, "title": "Spec", "source": "Stator"}), http.StatusNotFound, "no such issue")
	})

	t.Run("adding and removing are announced on their own topics, never as an edit", func(t *testing.T) {
		second := idOf(t, want(t, owner.post(path, map[string]any{"url": spec + "/appendix", "title": "Appendix", "source": "Stator"}), http.StatusCreated, "a second page"), "remoteLink")
		want(t, owner.delete(path+"/"+second), http.StatusNoContent, "take the second page off")
		want(t, owner.delete(path+"/"+second), http.StatusNotFound, "it is gone already")
		want(t, owner.delete(path+"/not-an-id"), http.StatusBadRequest, "not an id")

		if got := countTopic(events.TopicRemoteLinkAdded); got != 2 {
			t.Errorf("%d pages announced as added, want 2: a retitle is not an addition", got)
		}
		if got := countTopic(events.TopicRemoteLinkRemoved); got != 1 {
			t.Errorf("%d pages announced as removed, want 1", got)
		}
		if got := countTopic(events.TopicIssueUpdated); got != editsBefore {
			t.Errorf("page links wrote %d issue edits", got-editsBefore)
		}
	})

	t.Run("a page on one issue cannot be removed through another", func(t *testing.T) {
		other := obj(t, want(t, owner.post("/api/v1/projects/PGS/issues", map[string]any{"summary": "another"}), http.StatusCreated, "file another"), "issue")["key"].(string)
		want(t, owner.delete("/api/v1/issues/"+other+"/remote-links/"+first["id"].(string)), http.StatusNotFound, "removing through the wrong issue")
		if pages := list(t, owner.get(path), "remoteLinks"); len(pages) != 1 {
			t.Errorf("the page went through the wrong issue: %d left", len(pages))
		}
	})

	t.Run("a reader sees the pages and changes none of them", func(t *testing.T) {
		reader := api.asRole(t, h, owner, "PGS", perm.Reader)
		if pages := list(t, want(t, reader.get(path), http.StatusOK, "reader lists"), "remoteLinks"); len(pages) != 1 {
			t.Errorf("the reader sees %d pages, want 1", len(pages))
		}
		want(t, reader.post(path, map[string]any{"url": spec + "/reader", "title": "Mine", "source": "Stator"}), http.StatusForbidden, "reader adds")
		want(t, reader.delete(path+"/"+first["id"].(string)), http.StatusForbidden, "reader removes")
	})

	t.Run("a read token lists the pages and is refused a change", func(t *testing.T) {
		made := want(t, owner.post("/api/v1/tokens", map[string]any{"name": "sync", "scopes": []string{"read"}}), http.StatusCreated, "read token")
		token := api.client(t)
		token.bearer = principalField(t, made, "token", "secret").(string)
		want(t, token.get(path), http.StatusOK, "token lists")
		refused := token.post(path, map[string]any{"url": spec + "/token", "title": "From a token", "source": "Stator"})
		if refused.Status != http.StatusForbidden || refused.ErrorCode() != "read_only_token" {
			t.Errorf("a read token adding a page: %d %s", refused.Status, refused.Raw)
		}
		if refused := token.delete(path + "/" + first["id"].(string)); refused.Status != http.StatusForbidden {
			t.Errorf("a read token removing a page: %d %s", refused.Status, refused.Raw)
		}
	})
}

// The service keeping pages to their organization is not the proof. The same
// reads and writes are tried straight through SQL and the database refuses them.
func TestAPageLinkStaysInItsOrganizationEvenThroughSQL(t *testing.T) {
	h := newHarness(t)
	home := h.newWorkspace(t, "pagehome")
	away := h.newWorkspace(t, "pageaway")
	mine := home.newIssue(t, "has a spec")
	theirs := away.newIssue(t, "has a spec elsewhere")

	link, created, _, err := home.issues.PutRemoteLink(home.ctx, mine.Key, issue.RemoteLinkInput{
		URL: "https://wiki.example.com/pages/home", Title: "Home spec", Source: "Stator",
	}, home.actor)
	if err != nil || !created {
		t.Fatalf("put a page: %v (created %v)", err, created)
	}

	t.Run("another organization reads none of it", func(t *testing.T) {
		var seen int
		_, err := h.cluster.Write(away.ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM issue_remote_link WHERE id = $1`, link.ID).Scan(&seen)
		})
		if err != nil {
			t.Fatal(err)
		}
		if seen != 0 {
			t.Errorf("another organization sees the page")
		}
		if _, err := away.issues.RemoveRemoteLink(away.ctx, theirs.Key, link.ID, away.actor); !errors.Is(err, issue.ErrRemoteLinkNotFound) {
			t.Errorf("another organization removing it: %v", err)
		}
	})

	t.Run("another organization changes none of it", func(t *testing.T) {
		var touched int64
		_, err := h.cluster.Write(away.ctx, func(ctx context.Context, tx db.DBTX) error {
			tag, err := tx.Exec(ctx, `UPDATE issue_remote_link SET title = 'taken' WHERE id = $1`, link.ID)
			if err != nil {
				return err
			}
			touched = tag.RowsAffected()
			tag, err = tx.Exec(ctx, `DELETE FROM issue_remote_link WHERE id = $1`, link.ID)
			touched += tag.RowsAffected()
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if touched != 0 {
			t.Errorf("another organization changed %d rows", touched)
		}
	})

	// The trigger runs before the policy's check, and refuses first: from here
	// the other organization's issue cannot be seen at all.
	t.Run("a row planted in another organization is refused", func(t *testing.T) {
		_, err := h.cluster.Write(away.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO issue_remote_link (org_id, issue_id, url, title, source)
				VALUES ($1, $2, 'https://wiki.example.com/planted', 'Planted', 'Stator')`, home.orgID, mine.ID)
			return err
		})
		if err == nil {
			t.Error("a row went into another organization")
		}
		var planted int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM issue_remote_link WHERE issue_id = $1`, mine.ID).Scan(&planted); err != nil {
			t.Fatal(err)
		}
		if planted != 1 {
			t.Errorf("the issue carries %d pages, want only its own", planted)
		}
	})

	t.Run("a row pointing at another organization's issue is refused by the trigger", func(t *testing.T) {
		_, err := h.cluster.Write(away.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO issue_remote_link (org_id, issue_id, url, title, source)
				VALUES (current_org_id(), $1, 'https://wiki.example.com/planted', 'Planted', 'Stator')`, mine.ID)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "same organization as its issue") {
			t.Errorf("a page reached another organization's issue: %v", err)
		}
	})

	t.Run("the table holds a row written by hand to the same bounds", func(t *testing.T) {
		for what, address := range map[string]string{
			"a script address": "javascript:alert(1)",
			"a long address":   "https://wiki.example.com/" + strings.Repeat("a", issue.MaxRemoteLinkURL),
		} {
			_, err := h.cluster.Write(home.ctx, func(ctx context.Context, tx db.DBTX) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO issue_remote_link (org_id, issue_id, url, title, source)
					VALUES (current_org_id(), $1, $2, 'By hand', 'Stator')`, mine.ID, address)
				return err
			})
			if err == nil || !strings.Contains(err.Error(), "issue_remote_link_url_shape") {
				t.Errorf("%s was written: %v", what, err)
			}
		}
		_, err := h.cluster.Write(home.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO issue_remote_link (org_id, issue_id, url, title, source)
				VALUES (current_org_id(), $1, $2, 'Twice', 'Stator')`, mine.ID, link.URL)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "issue_remote_link_url_idx") {
			t.Errorf("the same address went on the issue twice: %v", err)
		}
	})

	t.Run("the page goes with its issue", func(t *testing.T) {
		if _, err := home.issues.Delete(home.ctx, mine.Key, home.actor); err != nil {
			t.Fatal(err)
		}
		var left int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM issue_remote_link WHERE id = $1`, link.ID).Scan(&left); err != nil {
			t.Fatal(err)
		}
		if left != 0 {
			t.Error("the page outlived its issue")
		}
	})
}
