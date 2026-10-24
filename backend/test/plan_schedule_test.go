//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/events"
)

// A plan's moves are saved together or not at all.
func TestAPlansMovesAreSavedAllOrNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	lead := api.client(t)
	lead.signIn(t, h.person(t, "lead", auth.RoleOwner))

	key, otherKey := h.projectKey(t, "MOV"), h.projectKey(t, "MOO")
	want(t, lead.post("/api/v1/projects", map[string]any{"name": h.name(t, "Moving"), "key": key}), http.StatusCreated, "the project")
	want(t, lead.post("/api/v1/projects", map[string]any{"name": h.name(t, "Elsewhere"), "key": otherKey}), http.StatusCreated, "another project")
	issue := func(projectKey, summary string, dates ...string) string {
		made := want(t, lead.post("/api/v1/projects/"+projectKey+"/issues", map[string]any{"summary": summary}), http.StatusCreated, summary)
		k := obj(t, made, "issue")["key"].(string)
		if len(dates) == 2 {
			want(t, lead.put("/api/v1/issues/"+k+"/schedule", map[string]any{"startDate": dates[0], "dueDate": dates[1]}), http.StatusOK, "schedule "+summary)
		}
		return k
	}
	first := issue(key, "first", "2027-11-01", "2027-11-02")
	second := issue(key, "second", "2027-11-03", "2027-11-04")
	unplaced := issue(key, "unplaced")
	elsewhere := issue(otherKey, "elsewhere", "2027-11-01", "2027-11-02")

	move := func(k string, expectedStart, expectedDue, start, due any) map[string]any {
		return map[string]any{"key": k, "expected": map[string]any{"startDate": expectedStart, "dueDate": expectedDue}, "startDate": start, "dueDate": due}
	}
	schedule := func(moves ...map[string]any) response {
		return lead.put("/api/v1/projects/"+key+"/plan/schedule", map[string]any{"moves": moves})
	}
	dates := func(k string) string {
		got := obj(t, want(t, lead.get("/api/v1/issues/"+k), http.StatusOK, k), "issue")
		day := func(field string) string {
			if v, ok := got[field].(string); ok {
				return v[:10]
			}
			return "unset"
		}
		return day("startDate") + ".." + day("dueDate")
	}
	refused := func(r response, status int, names string) {
		t.Helper()
		if r.Status != status || !strings.Contains(r.Error()["message"].(string), names) {
			t.Errorf("answer %d %s, want %d naming %s", r.Status, r.Raw, status, names)
		}
	}
	unchanged := func() {
		t.Helper()
		for k, was := range map[string]string{first: "2027-11-01..2027-11-02", second: "2027-11-03..2027-11-04", unplaced: "unset..unset"} {
			if got := dates(k); got != was {
				t.Errorf("%s = %s after a refused set, want %s", k, got, was)
			}
		}
	}

	t.Run("a stale expectation refuses every move", func(t *testing.T) {
		r := schedule(
			move(first, "2027-11-01", "2027-11-02", "2027-11-08", "2027-11-09"),
			move(second, "2027-11-01", "2027-11-04", "2027-11-10", "2027-11-11"),
		)
		refused(r, http.StatusConflict, second)
		if r.ErrorCode() != "moved_since" || r.Error()["fields"].(map[string]any)["issueKey"] != second {
			t.Errorf("the refusal %s does not name %s as a field a client can read", r.Raw, second)
		}
		unchanged()
	})

	t.Run("another project's issue refuses every move", func(t *testing.T) {
		r := schedule(
			move(first, "2027-11-01", "2027-11-02", "2027-11-08", "2027-11-09"),
			move(elsewhere, "2027-11-01", "2027-11-02", "2027-11-08", "2027-11-09"),
		)
		refused(r, http.StatusUnprocessableEntity, elsewhere)
		unchanged()
	})

	t.Run("a day out of range refuses every move", func(t *testing.T) {
		r := schedule(
			move(first, "2027-11-01", "2027-11-02", "2027-11-08", "2027-11-09"),
			move(second, "2027-11-03", "2027-11-04", "2027-11-10", "2300-01-01"),
		)
		refused(r, http.StatusUnprocessableEntity, second)
		unchanged()
	})

	t.Run("all moves are saved, each with its history and its event", func(t *testing.T) {
		r := want(t, schedule(
			move(first, "2027-11-01", "2027-11-02", "2027-11-08", "2027-11-09"),
			move(second, "2027-11-03", "2027-11-04", "2027-11-10", "2027-11-11"),
			move(unplaced, nil, nil, "2027-11-12", "2027-11-12"),
		), http.StatusOK, "the moves")
		if n := len(list(t, r, "issues")); n != 3 {
			t.Errorf("answered %d issues, want the 3 moved", n)
		}
		for k, wanted := range map[string]string{first: "2027-11-08..2027-11-09", second: "2027-11-10..2027-11-11", unplaced: "2027-11-12..2027-11-12"} {
			if got := dates(k); got != wanted {
				t.Errorf("%s = %s, want %s", k, got, wanted)
			}
			fields := ""
			for _, raw := range list(t, want(t, lead.get("/api/v1/issues/"+k+"/history"), http.StatusOK, k+" history"), "history") {
				for _, c := range raw.(map[string]any)["changes"].([]any) {
					fields += c.(map[string]any)["field"].(string) + " "
				}
			}
			if !strings.Contains(fields, "startDate") || !strings.Contains(fields, "dueDate") {
				t.Errorf("%s history holds %q, want both ends", k, fields)
			}
			var n int
			if err := h.super.QueryRow(context.Background(), `
				SELECT count(*) FROM outbox_event
				WHERE topic = $1 AND payload->>'key' = $2 AND payload::text LIKE '%' || $3 || '%'`,
				events.TopicIssueUpdated, k, wanted[:10]).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Errorf("%s sent %d issue.updated events for its move, want 1", k, n)
			}
		}
	})

	t.Run("someone who may only read is refused", func(t *testing.T) {
		email := h.outsider(t, "reader")
		var id string
		if err := h.super.QueryRow(context.Background(), `SELECT id FROM app_user WHERE email = $1`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		want(t, lead.post("/api/v1/role-assignments", map[string]any{"role": "reader", "projectKey": key, "userId": id}), http.StatusCreated, "a reader")
		reader := api.client(t)
		reader.signIn(t, email)
		r := reader.put("/api/v1/projects/"+key+"/plan/schedule", map[string]any{"moves": []any{move(first, "2027-11-08", "2027-11-09", "2027-11-15", "2027-11-16")}})
		if r.Status != http.StatusForbidden {
			t.Errorf("a reader's moves: %d %s", r.Status, r.Raw)
		}
	})
}
