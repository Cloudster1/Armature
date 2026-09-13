//go:build integration

package test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// The search bar finishes the sentence: fields, operators and values the
// query takes at the caret, names from the tables, and issues the words find.
func TestTheSearchBarSuggests(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	agent := api.client(t)
	agent.signup(t, h, "suggesting")
	want(t, agent.post("/api/v1/projects", map[string]any{"name": "Suggest", "key": "SUG"}), http.StatusCreated, "project")
	want(t, agent.post("/api/v1/projects/SUG/issues", map[string]any{"summary": "The printer is on fire"}), http.StatusCreated, "an issue")
	want(t, agent.post("/api/v1/projects/SUG/milestones", map[string]any{"name": "Winter release"}), http.StatusCreated, "a milestone")

	suggest := func(q string, extra ...string) response {
		params := url.Values{"q": {q}}
		for i := 0; i+1 < len(extra); i += 2 {
			params.Set(extra[i], extra[i+1])
		}
		return want(t, agent.get("/api/v1/issues/suggest?"+params.Encode()), http.StatusOK, "suggest "+q)
	}
	words := func(r response) []string {
		out := []string{}
		for _, w := range list(t, r, "words") {
			out = append(out, w.(map[string]any)["text"].(string))
		}
		return out
	}

	t.Run("a field, then its operators, then its values", func(t *testing.T) {
		if got := words(suggest("sta")); len(got) != 3 || got[0] != "start" || got[1] != "status" || got[2] != "statusCategory" {
			t.Fatalf("fields for sta = %v", got)
		}
		if got := strings.Join(words(suggest("status ")), " "); !strings.Contains(got, "IN") || strings.Contains(got, "<") {
			t.Fatalf("operators for status = %s", got)
		}
		got := words(suggest("status = ", "project", "SUG"))
		if !hasAll(got, `"To Do"`, "Done") {
			t.Fatalf("status names = %v", got)
		}
		r := suggest("status = ")
		if c := obj(t, r, "completion"); c["slot"] != "value" || c["field"] != "status" || c["from"].(float64) != 9 {
			t.Fatalf("completion = %v", c)
		}
	})

	t.Run("names come from the tables and are quoted when they need to be", func(t *testing.T) {
		if got := words(suggest("milestone = w", "project", "SUG")); len(got) != 1 || got[0] != `"Winter release"` {
			t.Fatalf("milestones = %v", got)
		}
		if got := words(suggest("assignee = ")); !hasAll(got, "currentUser()", "suggesting") {
			t.Fatalf("people = %v", got)
		}
		if got := words(suggest("statusCategory = d")); !hasAll(got, "done") || hasAll(got, "todo") {
			t.Fatalf("categories for d = %v", got)
		}
	})

	t.Run("words find issues, a key finds the issue, and a query finds what it matches", func(t *testing.T) {
		found := list(t, suggest("printer"), "issues")
		if len(found) != 1 || found[0].(map[string]any)["summary"] != "The printer is on fire" {
			t.Fatalf("issues for printer = %v", found)
		}
		key := found[0].(map[string]any)["key"].(string)
		if byKey := list(t, suggest(strings.ToLower(key)), "issues"); len(byKey) != 1 {
			t.Fatalf("issues for the key = %v", byKey)
		}
		if byQuery := list(t, suggest("summary ~ printer"), "issues"); len(byQuery) != 1 {
			t.Fatalf("issues for the query = %v", byQuery)
		}
		if half := list(t, suggest("summary ~ "), "issues"); len(half) != 0 {
			t.Fatalf("a half typed query finds %v", half)
		}
	})

	t.Run("the caret can sit anywhere, and a bad caret is refused", func(t *testing.T) {
		r := suggest("status = Done AND assignee = x", "at", "3")
		if c := obj(t, r, "completion"); c["slot"] != "field" || c["prefix"] != "sta" {
			t.Fatalf("mid-text completion = %v", c)
		}
		want(t, agent.get("/api/v1/issues/suggest?q=x&at=three"), http.StatusBadRequest, "a caret that is not a number")
	})
}

func hasAll(words []string, wanted ...string) bool {
	for _, w := range wanted {
		found := false
		for _, each := range words {
			if each == w {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}
