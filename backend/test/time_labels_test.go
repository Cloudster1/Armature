//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Time on an issue: an estimate, what remains, and the work logged against
// them, over the API as the issue page uses it.
func TestTimeIsEstimatedLoggedAndAccounted(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "timekeeper")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Timed", "key": "TMD"}), http.StatusCreated, "project")
	issued := want(t, owner.post("/api/v1/projects/TMD/issues", map[string]any{"summary": "takes a while"}), http.StatusCreated, "issue")
	key := obj(t, issued, "issue")["key"].(string)

	estimated := want(t, owner.patch("/api/v1/issues/"+key, map[string]any{"timeEstimateMinutes": 480}), http.StatusOK, "estimate")
	if got := obj(t, estimated, "issue")["timeEstimateMinutes"]; got != float64(480) {
		t.Fatalf("estimate = %v", got)
	}
	if got := obj(t, estimated, "issue")["timeSpentMinutes"]; got != float64(0) {
		t.Fatalf("nothing is spent yet, got %v", got)
	}

	// Logging takes time off what remains, derived from the estimate first.
	logged := want(t, owner.post("/api/v1/issues/"+key+"/worklogs", map[string]any{"minutes": 90, "note": "read the spec", "startedOn": "2026-09-01"}), http.StatusCreated, "log work")
	worklogID := idOf(t, logged, "worklog")
	after := obj(t, want(t, owner.get("/api/v1/issues/"+key), http.StatusOK, "issue"), "issue")
	if after["timeSpentMinutes"] != float64(90) || after["timeRemainingMinutes"] != float64(390) {
		t.Fatalf("spent/remaining = %v/%v, want 90/390", after["timeSpentMinutes"], after["timeRemainingMinutes"])
	}
	// A second entry comes off the explicit remaining figure.
	want(t, owner.post("/api/v1/issues/"+key+"/worklogs", map[string]any{"minutes": 60}), http.StatusCreated, "log more")
	after = obj(t, owner.get("/api/v1/issues/"+key), "issue")
	if after["timeSpentMinutes"] != float64(150) || after["timeRemainingMinutes"] != float64(330) {
		t.Fatalf("spent/remaining = %v/%v, want 150/330", after["timeSpentMinutes"], after["timeRemainingMinutes"])
	}
	// Remaining can be set by hand and never goes below zero from logging.
	want(t, owner.patch("/api/v1/issues/"+key, map[string]any{"timeRemainingMinutes": 30}), http.StatusOK, "remaining by hand")
	want(t, owner.post("/api/v1/issues/"+key+"/worklogs", map[string]any{"minutes": 45}), http.StatusCreated, "overrun")
	after = obj(t, owner.get("/api/v1/issues/"+key), "issue")
	if after["timeRemainingMinutes"] != float64(0) {
		t.Fatalf("remaining should floor at zero, got %v", after["timeRemainingMinutes"])
	}

	listed := list(t, want(t, owner.get("/api/v1/issues/"+key+"/worklogs"), http.StatusOK, "worklogs"), "worklogs")
	if len(listed) != 3 {
		t.Fatalf("want 3 worklogs, got %d", len(listed))
	}
	want(t, owner.patch("/api/v1/worklogs/"+worklogID, map[string]any{"minutes": 120, "note": "read the spec twice"}), http.StatusOK, "correct an entry")
	history := owner.get("/api/v1/issues/" + key + "/history")
	for _, phrase := range []string{`"field":"timeEstimate"`, `"to":"1d"`, `"field":"timeSpent"`, `"field":"timeRemaining"`, `"field":"worklog"`} {
		if !strings.Contains(history.Raw, phrase) {
			t.Errorf("the changelog lacks %s", phrase)
		}
	}

	t.Run("time that is not time is refused", func(t *testing.T) {
		want(t, owner.post("/api/v1/issues/"+key+"/worklogs", map[string]any{"minutes": 0}), http.StatusBadRequest, "zero")
		want(t, owner.post("/api/v1/issues/"+key+"/worklogs", map[string]any{"minutes": 100000}), http.StatusBadRequest, "a fortnight in one go")
		want(t, owner.patch("/api/v1/issues/"+key, map[string]any{"timeEstimateMinutes": -5}), http.StatusBadRequest, "negative estimate")
		want(t, owner.patch("/api/v1/issues/"+key, map[string]any{"timeEstimateMinutes": 2.5}), http.StatusBadRequest, "fractional minutes")
	})

	otherEmail := h.email(t, "colleague")
	invited := want(t, owner.post("/api/v1/invites", map[string]any{"email": otherEmail, "role": "member"}), http.StatusCreated, "invite")
	colleague := api.client(t)
	t.Run("somebody else cannot change my entry, but I can remove it", func(t *testing.T) {
		want(t, colleague.post("/api/v1/auth/invites/accept", map[string]any{"token": invited.Body["token"], "name": "Colleague", "password": testPassword}), http.StatusOK, "join")
		want(t, colleague.patch("/api/v1/worklogs/"+worklogID, map[string]any{"minutes": 5}), http.StatusForbidden, "edit another's entry")
		want(t, colleague.delete("/api/v1/worklogs/"+worklogID), http.StatusForbidden, "delete another's entry")
		want(t, owner.delete("/api/v1/worklogs/"+worklogID), http.StatusNoContent, "delete my own")
		after := obj(t, owner.get("/api/v1/issues/"+key), "issue")
		if after["timeSpentMinutes"] != float64(105) {
			t.Fatalf("spent after the delete = %v, want 105", after["timeSpentMinutes"])
		}
	})

	t.Run("the reporter can be handed to another member and to nobody else", func(t *testing.T) {
		// The colleague joined a moment ago on the primary; the members list
		// is a replica read, so it may take a beat to see them.
		me := principalField(t, owner.get("/api/v1/auth/me"), "principal", "user", "id").(string)
		var other string
		for attempt := 0; attempt < 50 && other == ""; attempt++ {
			for _, raw := range list(t, owner.get("/api/v1/members"), "members") {
				if id := raw.(map[string]any)["id"].(string); id != me {
					other = id
				}
			}
			if other == "" {
				time.Sleep(50 * time.Millisecond)
			}
		}
		if other == "" {
			t.Fatalf("no second member to hand over to; owner sees %s; colleague is %s", owner.get("/api/v1/members").Raw, colleague.get("/api/v1/auth/me").Raw)
		}
		moved := want(t, owner.patch("/api/v1/issues/"+key, map[string]any{"reporterId": other}), http.StatusOK, "hand over")
		if obj(t, moved, "issue", "reporter")["id"] != other {
			t.Fatalf("reporter = %v", obj(t, moved, "issue", "reporter"))
		}
		want(t, owner.patch("/api/v1/issues/"+key, map[string]any{"reporterId": "00000000-0000-0000-0000-000000000001"}), http.StatusUnprocessableEntity, "a stranger as reporter")
	})
}

// Labels: coined by tagging, shared by the organization, filtered on, renamed
// everywhere, and gone everywhere when removed.
func TestLabelsAreWordsTheOrganizationShares(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "labeller")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Tagged", "key": "TAG"}), http.StatusCreated, "project")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Other", "key": "OTR"}), http.StatusCreated, "other project")
	first := obj(t, owner.post("/api/v1/projects/TAG/issues", map[string]any{"summary": "one"}), "issue")["key"].(string)
	second := obj(t, owner.post("/api/v1/projects/TAG/issues", map[string]any{"summary": "two"}), "issue")["key"].(string)
	elsewhere := obj(t, owner.post("/api/v1/projects/OTR/issues", map[string]any{"summary": "three"}), "issue")["key"].(string)

	set := want(t, owner.put("/api/v1/issues/"+first+"/labels", map[string]any{"labels": []string{"backend", "Urgent"}}), http.StatusOK, "tag")
	if names := labelNames(t, set, "labels"); names != "backend, Urgent" {
		t.Fatalf("labels = %q", names)
	}
	// The same word again, in another case, is the same label.
	want(t, owner.put("/api/v1/issues/"+second+"/labels", map[string]any{"labels": []string{"BACKEND"}}), http.StatusOK, "tag again")
	want(t, owner.put("/api/v1/issues/"+elsewhere+"/labels", map[string]any{"labels": []string{"backend"}}), http.StatusOK, "tag elsewhere")

	labels := list(t, want(t, owner.get("/api/v1/labels"), http.StatusOK, "labels"), "labels")
	if len(labels) != 2 {
		t.Fatalf("want 2 labels, got %d: %v", len(labels), labels)
	}
	var backendID, urgentID string
	for _, raw := range labels {
		l := raw.(map[string]any)
		switch l["name"] {
		case "backend":
			backendID = l["id"].(string)
			if l["issueCount"] != float64(3) {
				t.Errorf("backend is on %v issues, want 3", l["issueCount"])
			}
		case "Urgent":
			urgentID = l["id"].(string)
		}
	}

	filtered := want(t, owner.get("/api/v1/projects/TAG/issues?label="+urgentID), http.StatusOK, "filter")
	if filtered.Body["total"] != float64(1) {
		t.Fatalf("filtering by Urgent should find one issue, got %v", filtered.Body["total"])
	}
	shown := obj(t, owner.get("/api/v1/issues/"+first), "issue")
	if len(shown["labels"].([]any)) != 2 {
		t.Fatalf("the issue should carry two labels: %v", shown["labels"])
	}

	renamed := want(t, owner.patch("/api/v1/labels/"+backendID, map[string]any{"name": "server", "color": "blue"}), http.StatusOK, "rename")
	if obj(t, renamed, "label")["name"] != "server" {
		t.Fatalf("rename: %s", renamed.Raw)
	}
	if names := labelNames(t, owner.get("/api/v1/issues/"+second), "issue", "labels"); names != "server" {
		t.Fatalf("the rename should show on the issue, got %q", names)
	}

	want(t, owner.put("/api/v1/issues/"+first+"/labels", map[string]any{"labels": []string{}}), http.StatusOK, "untag")
	if names := labelNames(t, owner.get("/api/v1/issues/"+first), "issue", "labels"); names != "" {
		t.Fatalf("untagged issue still has %q", names)
	}
	history := owner.get("/api/v1/issues/" + first + "/history")
	if !strings.Contains(history.Raw, `"field":"labels"`) || !strings.Contains(history.Raw, `"from":"Urgent, server"`) {
		var entries []string
		for _, raw := range list(t, history, "history") {
			for _, c := range raw.(map[string]any)["changes"].([]any) {
				ch := c.(map[string]any)
				entries = append(entries, fmt.Sprintf("%v: %q -> %q", ch["field"], ch["from"], ch["to"]))
			}
		}
		t.Errorf("the changelog should record the labels that came and went:\n%s", strings.Join(entries, "\n"))
	}

	coined := want(t, owner.post("/api/v1/labels", map[string]any{"name": "design", "color": "pink"}), http.StatusCreated, "coin a label directly")
	if obj(t, coined, "label")["color"] != "pink" {
		t.Fatalf("coined: %s", coined.Raw)
	}

	t.Run("bad words are refused", func(t *testing.T) {
		want(t, owner.put("/api/v1/issues/"+first+"/labels", map[string]any{"labels": []string{"two words"}}), http.StatusBadRequest, "spaces")
		want(t, owner.post("/api/v1/labels", map[string]any{"name": "Urgent"}), http.StatusConflict, "duplicate")
		want(t, owner.post("/api/v1/labels", map[string]any{"name": "fine", "color": "mauve"}), http.StatusBadRequest, "colour outside the palette")
	})

	t.Run("deleting a label takes it off every issue", func(t *testing.T) {
		want(t, owner.delete("/api/v1/labels/"+backendID), http.StatusNoContent, "delete")
		if names := labelNames(t, owner.get("/api/v1/issues/"+elsewhere), "issue", "labels"); names != "" {
			t.Fatalf("the deleted label is still on an issue: %q", names)
		}
	})

	t.Run("another tenant has words of its own", func(t *testing.T) {
		stranger := api.client(t)
		stranger.signup(t, h, "labelstranger")
		if got := list(t, stranger.get("/api/v1/labels"), "labels"); len(got) != 0 {
			t.Fatalf("a new organization should see no labels, got %v", got)
		}
		want(t, stranger.patch("/api/v1/labels/"+urgentID, map[string]any{"name": "mine"}), http.StatusNotFound, "rename another tenant's label")
		var seen int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM label WHERE id = $1`, urgentID).Scan(&seen); err != nil || seen != 1 {
			t.Fatalf("the label should still exist for its owner: %d %v", seen, err)
		}
	})
}

func labelNames(t *testing.T, r response, keys ...string) string {
	t.Helper()
	items := list(t, r, keys...)
	names := make([]string, 0, len(items))
	for _, raw := range items {
		names = append(names, raw.(map[string]any)["name"].(string))
	}
	return strings.Join(names, ", ")
}
