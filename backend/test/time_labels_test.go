//go:build integration

package test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/auth"
)

// Time on an issue: an estimate, what remains, and the work logged against
// them, over the API as the issue page uses it.
func TestTimeIsEstimatedLoggedAndAccounted(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	tmdKey := h.projectKey(t, "TMD")
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "timekeeper", auth.RoleOwner))
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Timed", "key": tmdKey}), http.StatusCreated, "project")
	issued := want(t, owner.post("/api/v1/projects/"+tmdKey+"/issues", map[string]any{"summary": "takes a while"}), http.StatusCreated, "issue")
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
		colleagueID := principalField(t, colleague.get("/api/v1/auth/me"), "principal", "user", "id").(string)
		var other string
		for attempt := 0; attempt < 50 && other == ""; attempt++ {
			for _, raw := range list(t, owner.get("/api/v1/members?q="+url.QueryEscape(otherEmail)), "members") {
				if id := raw.(map[string]any)["id"].(string); id == colleagueID {
					other = id
				}
			}
			if other == "" {
				time.Sleep(50 * time.Millisecond)
			}
		}
		if other == "" {
			t.Fatalf("the colleague %s is not among the members to hand over to", colleagueID)
		}
		moved := want(t, owner.patch("/api/v1/issues/"+key, map[string]any{"reporterId": other}), http.StatusOK, "hand over")
		if obj(t, moved, "issue", "reporter")["id"] != other {
			t.Fatalf("reporter = %v", obj(t, moved, "issue", "reporter"))
		}
		want(t, owner.patch("/api/v1/issues/"+key, map[string]any{"reporterId": "00000000-0000-0000-0000-000000000001"}), http.StatusUnprocessableEntity, "a stranger as reporter")
	})
}

// Labels: coined by tagging, shared by everyone, filtered on, renamed
// everywhere, and gone everywhere when removed.
func TestLabelsAreWordsEveryoneShares(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	tagKey := h.projectKey(t, "TAG")
	otrKey := h.projectKey(t, "OTR")
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "labeller", auth.RoleOwner))
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Tagged", "key": tagKey}), http.StatusCreated, "project")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Other", "key": otrKey}), http.StatusCreated, "other project")
	first := obj(t, owner.post("/api/v1/projects/"+tagKey+"/issues", map[string]any{"summary": "one"}), "issue")["key"].(string)
	second := obj(t, owner.post("/api/v1/projects/"+tagKey+"/issues", map[string]any{"summary": "two"}), "issue")["key"].(string)
	elsewhere := obj(t, owner.post("/api/v1/projects/"+otrKey+"/issues", map[string]any{"summary": "three"}), "issue")["key"].(string)
	backend, urgent, server, design := h.name(t, "backend"), h.name(t, "Urgent"), h.name(t, "server"), h.name(t, "design")

	set := want(t, owner.put("/api/v1/issues/"+first+"/labels", map[string]any{"labels": []string{backend, urgent}}), http.StatusOK, "tag")
	if names := labelNames(t, set, "labels"); names != backend+", "+urgent {
		t.Fatalf("labels = %q", names)
	}
	// The same word again, in another case, is the same label.
	want(t, owner.put("/api/v1/issues/"+second+"/labels", map[string]any{"labels": []string{strings.ToUpper(backend)}}), http.StatusOK, "tag again")
	want(t, owner.put("/api/v1/issues/"+elsewhere+"/labels", map[string]any{"labels": []string{backend}}), http.StatusOK, "tag elsewhere")

	// Everybody's labels are on the list; these two are the test's own.
	var labels []any
	for _, word := range []string{backend, urgent} {
		for _, raw := range list(t, want(t, owner.get("/api/v1/labels?q="+url.QueryEscape(word)), http.StatusOK, "labels"), "labels") {
			if strings.EqualFold(raw.(map[string]any)["name"].(string), word) {
				labels = append(labels, raw)
			}
		}
	}
	if len(labels) != 2 {
		t.Fatalf("want 2 labels, got %d: %v", len(labels), labels)
	}
	var backendID, urgentID string
	for _, raw := range labels {
		l := raw.(map[string]any)
		switch l["name"] {
		case backend:
			backendID = l["id"].(string)
			if l["issueCount"] != float64(3) {
				t.Errorf("backend is on %v issues, want 3", l["issueCount"])
			}
		case urgent:
			urgentID = l["id"].(string)
		}
	}

	filtered := want(t, owner.get("/api/v1/projects/"+tagKey+"/issues?label="+urgentID), http.StatusOK, "filter")
	if filtered.Body["total"] != float64(1) {
		t.Fatalf("filtering by Urgent should find one issue, got %v", filtered.Body["total"])
	}
	shown := obj(t, owner.get("/api/v1/issues/"+first), "issue")
	if len(shown["labels"].([]any)) != 2 {
		t.Fatalf("the issue should carry two labels: %v", shown["labels"])
	}

	renamed := want(t, owner.patch("/api/v1/labels/"+backendID, map[string]any{"name": server, "color": "blue"}), http.StatusOK, "rename")
	if obj(t, renamed, "label")["name"] != server {
		t.Fatalf("rename: %s", renamed.Raw)
	}
	if names := labelNames(t, owner.get("/api/v1/issues/"+second), "issue", "labels"); names != server {
		t.Fatalf("the rename should show on the issue, got %q", names)
	}

	want(t, owner.put("/api/v1/issues/"+first+"/labels", map[string]any{"labels": []string{}}), http.StatusOK, "untag")
	if names := labelNames(t, owner.get("/api/v1/issues/"+first), "issue", "labels"); names != "" {
		t.Fatalf("untagged issue still has %q", names)
	}
	history := owner.get("/api/v1/issues/" + first + "/history")
	if !strings.Contains(history.Raw, `"field":"labels"`) || !strings.Contains(history.Raw, `"from":"`+urgent+`, `+server+`"`) {
		var entries []string
		for _, raw := range list(t, history, "history") {
			for _, c := range raw.(map[string]any)["changes"].([]any) {
				ch := c.(map[string]any)
				entries = append(entries, fmt.Sprintf("%v: %q -> %q", ch["field"], ch["from"], ch["to"]))
			}
		}
		t.Errorf("the changelog should record the labels that came and went:\n%s", strings.Join(entries, "\n"))
	}

	coined := want(t, owner.post("/api/v1/labels", map[string]any{"name": design, "color": "pink"}), http.StatusCreated, "coin a label directly")
	if obj(t, coined, "label")["color"] != "pink" {
		t.Fatalf("coined: %s", coined.Raw)
	}

	t.Run("bad words are refused", func(t *testing.T) {
		want(t, owner.put("/api/v1/issues/"+first+"/labels", map[string]any{"labels": []string{"two words"}}), http.StatusBadRequest, "spaces")
		want(t, owner.post("/api/v1/labels", map[string]any{"name": urgent}), http.StatusConflict, "duplicate")
		want(t, owner.post("/api/v1/labels", map[string]any{"name": h.name(t, "fine"), "color": "mauve"}), http.StatusBadRequest, "colour outside the palette")
	})

	t.Run("deleting a label takes it off every issue", func(t *testing.T) {
		want(t, owner.delete("/api/v1/labels/"+backendID), http.StatusNoContent, "delete")
		if names := labelNames(t, owner.get("/api/v1/issues/"+elsewhere), "issue", "labels"); names != "" {
			t.Fatalf("the deleted label is still on an issue: %q", names)
		}
	})
}

// A label field asks for the few labels that hold what was typed, those
// starting with it first, rather than every label everybody has coined.
func TestLabelsAreFoundByWhatIsTyped(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "finder", auth.RoleOwner))
	word := strings.ToLower(unique("word"))
	inside, starts, alsoInside, alsoStarts := h.name(t, "b"+word), h.name(t, word), h.name(t, "a"+word), h.name(t, word)
	for _, name := range []string{inside, starts, alsoInside, alsoStarts} {
		want(t, owner.post("/api/v1/labels", map[string]any{"name": name}), http.StatusCreated, "coin "+name)
	}
	first, second := starts, alsoStarts
	if strings.ToLower(second) < strings.ToLower(first) {
		first, second = second, first
	}

	found := func(t *testing.T, query string) string {
		t.Helper()
		return labelNames(t, want(t, owner.get("/api/v1/labels?"+query), http.StatusOK, "search "+query), "labels")
	}
	if got, wanted := found(t, "q="+word), strings.Join([]string{first, second, alsoInside, inside}, ", "); got != wanted {
		t.Fatalf("labels holding %s = %q, want those starting with it first: %q", word, got, wanted)
	}
	if got, wanted := found(t, "q="+strings.ToUpper(word)), strings.Join([]string{first, second, alsoInside, inside}, ", "); got != wanted {
		t.Errorf("the search should ignore case: %q, want %q", got, wanted)
	}
	if got, wanted := found(t, "q="+word+"&limit=3"), strings.Join([]string{first, second, alsoInside}, ", "); got != wanted {
		t.Errorf("a limit of 3 = %q, want %q", got, wanted)
	}
	if got := found(t, "limit=1"); strings.Contains(got, ", ") || got == "" {
		t.Errorf("a limit alone should give one label, got %q", got)
	}
	if got := found(t, "q="+word+"&limit=100000"); got == "" {
		t.Errorf("a limit past the cap should be held to the cap, not refused")
	}

	// A search pages like the plain list, and says how many it holds in all.
	paged := want(t, owner.get("/api/v1/labels?q="+word+"&limit=2&offset=2"), http.StatusOK, "the second page")
	if got, wanted := labelNames(t, paged, "labels"), strings.Join([]string{alsoInside, inside}, ", "); got != wanted {
		t.Errorf("the second page of two = %q, want %q", got, wanted)
	}
	if paged.Body["total"] != float64(4) || paged.Body["offset"] != float64(2) {
		t.Errorf("the second page says total %v and offset %v, want 4 and 2", paged.Body["total"], paged.Body["offset"])
	}
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
