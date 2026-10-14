//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/perm"
)

// Putting the same address again retitles the page, so a sync can resend
// everything and the issue carries each page once, without an edit of its own.
func TestAPageLinkIsKeptInStepByItsAddress(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	pgsKey := h.projectKey(t, "PGS")
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "pages", auth.RoleOwner))
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Pages", "key": pgsKey}), http.StatusCreated, "create project")
	key := obj(t, want(t, owner.post("/api/v1/projects/"+pgsKey+"/issues", map[string]any{"summary": "checkout flow"}), http.StatusCreated, "file issue"), "issue")["key"].(string)
	path := "/api/v1/issues/" + key + "/remote-links"

	countTopic := func(topic string) int {
		t.Helper()
		var n int
		if err := h.super.QueryRow(context.Background(), `
			SELECT count(*) FROM outbox_event WHERE topic = $1 AND payload->>'key' = $2`, topic, key).Scan(&n); err != nil {
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
		want(t, owner.post("/api/v1/issues/"+pgsKey+"-999/remote-links", map[string]any{"url": spec, "title": "Spec", "source": "Stator"}), http.StatusNotFound, "no such issue")
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
		other := obj(t, want(t, owner.post("/api/v1/projects/"+pgsKey+"/issues", map[string]any{"summary": "another"}), http.StatusCreated, "file another"), "issue")["key"].(string)
		want(t, owner.delete("/api/v1/issues/"+other+"/remote-links/"+first["id"].(string)), http.StatusNotFound, "removing through the wrong issue")
		if pages := list(t, owner.get(path), "remoteLinks"); len(pages) != 1 {
			t.Errorf("the page went through the wrong issue: %d left", len(pages))
		}
	})

	t.Run("a reader sees the pages and changes none of them", func(t *testing.T) {
		reader := api.asRole(t, h, owner, pgsKey, perm.Reader)
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
		// A new key is not pinned to its owner's writes; let the replica catch up.
		h.waitForPrimary(t)
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

// A page belongs to its issue: another issue cannot take it away, and what the
// service refuses to write the table refuses too, straight through SQL.
func TestAPageLinkKeepsToItsIssueEvenThroughSQL(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	home := h.newProject(t, "pagehome")
	away := h.newProject(t, "pageaway")
	mine := home.newIssue(t, "has a spec")
	theirs := away.newIssue(t, "has a spec elsewhere")

	link, created, _, err := home.issues.PutRemoteLink(home.ctx, mine.Key, issue.RemoteLinkInput{
		URL: "https://wiki.example.com/pages/home", Title: "Home spec", Source: "Stator",
	}, home.actor)
	if err != nil || !created {
		t.Fatalf("put a page: %v (created %v)", err, created)
	}

	t.Run("another issue does not remove it", func(t *testing.T) {
		if _, err := away.issues.RemoveRemoteLink(away.ctx, theirs.Key, link.ID, away.actor); !errors.Is(err, issue.ErrRemoteLinkNotFound) {
			t.Errorf("removing it through another issue: %v", err)
		}
	})

	t.Run("the table holds a row written by hand to the same bounds", func(t *testing.T) {
		for what, address := range map[string]string{
			"a script address": "javascript:alert(1)",
			"a long address":   "https://wiki.example.com/" + strings.Repeat("a", issue.MaxRemoteLinkURL),
		} {
			_, err := h.cluster.Write(home.ctx, func(ctx context.Context, tx db.DBTX) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO issue_remote_link (issue_id, url, title, source)
					VALUES ($1, $2, 'By hand', 'Stator')`, mine.ID, address)
				return err
			})
			if err == nil || !strings.Contains(err.Error(), "issue_remote_link_url_shape") {
				t.Errorf("%s was written: %v", what, err)
			}
		}
		_, err := h.cluster.Write(home.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO issue_remote_link (issue_id, url, title, source)
				VALUES ($1, $2, 'Twice', 'Stator')`, mine.ID, link.URL)
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

// A requirement traced to an issue is told apart from a page about it, and a
// sync that says nothing of the relation leaves the one it has.
func TestALinkSaysWhatItIsToTheIssue(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	relKey := h.projectKey(t, "REL")
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "relations", auth.RoleOwner))
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Relations", "key": relKey}), http.StatusCreated, "create project")
	filed := obj(t, want(t, owner.post("/api/v1/projects/"+relKey+"/issues", map[string]any{"summary": "checkout flow"}), http.StatusCreated, "file issue"), "issue")
	key, issueID := filed["key"].(string), filed["id"].(string)
	path := "/api/v1/issues/" + key + "/remote-links"
	requirement := "https://req.example.com/requirements/R-7"

	payloadOf := func(topic, url string) map[string]any {
		t.Helper()
		var payload map[string]any
		if err := h.super.QueryRow(context.Background(), `
			SELECT payload FROM outbox_event WHERE topic = $1 AND payload->>'key' = $2 AND payload->>'url' = $3`,
			topic, key, url).Scan(&payload); err != nil {
			t.Fatalf("the %s event for %s: %v", topic, url, err)
		}
		return payload
	}

	made := obj(t, want(t, owner.post(path, map[string]any{
		"url": requirement, "title": "R-7 Pay by card", "source": "Exciter", "relation": "requirement",
	}), http.StatusCreated, "trace a requirement to the issue"), "remoteLink")
	if made["relation"] != "requirement" {
		t.Errorf("the requirement came back as %v", made)
	}
	if got := payloadOf(events.TopicRemoteLinkAdded, requirement)["relation"]; got != "requirement" {
		t.Errorf("the addition announced relation %v, want requirement", got)
	}

	t.Run("a link sent without a relation is a page", func(t *testing.T) {
		page := obj(t, want(t, owner.post(path, map[string]any{
			"url": "https://wiki.example.com/pages/checkout", "title": "Checkout spec", "source": "Stator",
		}), http.StatusCreated, "put a page as before relations"), "remoteLink")
		if page["relation"] != "page" {
			t.Errorf("a link without a relation came back as %v", page)
		}
	})

	t.Run("a sync that leaves the relation out keeps it, and one that says it changes it", func(t *testing.T) {
		again := obj(t, want(t, owner.post(path, map[string]any{
			"url": requirement, "title": "R-7 Pay by card or wallet", "source": "Exciter",
		}), http.StatusOK, "retitle the requirement"), "remoteLink")
		if again["relation"] != "requirement" || again["title"] != "R-7 Pay by card or wallet" {
			t.Errorf("a retitle without a relation came back as %v", again)
		}
		read := map[any]any{}
		for _, link := range list(t, owner.get(path), "remoteLinks") {
			read[link.(map[string]any)["url"]] = link.(map[string]any)["relation"]
		}
		if read[requirement] != "requirement" {
			t.Errorf("the requirement reads back as %v", read[requirement])
		}
		changed := obj(t, want(t, owner.post(path, map[string]any{
			"url": requirement, "title": "R-7 Pay by card or wallet", "source": "Exciter", "relation": "page",
		}), http.StatusOK, "say it is a page after all"), "remoteLink")
		if changed["relation"] != "page" {
			t.Errorf("a relation sent again was not taken: %v", changed)
		}
		want(t, owner.post(path, map[string]any{
			"url": requirement, "title": "R-7 Pay by card or wallet", "source": "Exciter", "relation": "requirement",
		}), http.StatusOK, "and a requirement again")
	})

	t.Run("a relation the server does not know is refused with a sentence", func(t *testing.T) {
		refused := want(t, owner.post(path, map[string]any{
			"url": "https://req.example.com/requirements/R-8", "title": "R-8", "source": "Exciter", "relation": "spec",
		}), http.StatusUnprocessableEntity, "an unknown relation")
		message, _ := refused.Error()["message"].(string)
		if !strings.Contains(message, "page or requirement") || !strings.HasSuffix(message, ".") {
			t.Errorf("the refusal reads %q", message)
		}
	})

	t.Run("the removal announces what the link was", func(t *testing.T) {
		want(t, owner.delete(path+"/"+made["id"].(string)), http.StatusNoContent, "take the requirement off")
		if got := payloadOf(events.TopicRemoteLinkRemoved, requirement)["relation"]; got != "requirement" {
			t.Errorf("the removal announced relation %v, want requirement", got)
		}
	})

	t.Run("the table refuses a relation written by hand that it does not know", func(t *testing.T) {
		_, err := h.cluster.Write(context.Background(), func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO issue_remote_link (issue_id, url, title, source, relation)
				VALUES ($1, 'https://req.example.com/requirements/R-9', 'By hand', 'Exciter', 'other')`, issueID)
			return err
		})
		if err == nil || !strings.Contains(err.Error(), "issue_remote_link_relation_known") {
			t.Errorf("a relation of other was written: %v", err)
		}
		var relation string
		if _, err := h.cluster.Write(context.Background(), func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `
				INSERT INTO issue_remote_link (issue_id, url, title, source)
				VALUES ($1, 'https://wiki.example.com/pages/by-hand', 'By hand', 'Stator')
				RETURNING relation`, issueID).Scan(&relation)
		}); err != nil {
			t.Fatal(err)
		}
		if relation != "page" {
			t.Errorf("a row written by hand without a relation is %q, want page", relation)
		}
	})
}
