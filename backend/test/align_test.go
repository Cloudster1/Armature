//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
)

// 1 November 2027 is a Monday, and far enough ahead that nothing is under way.
func TestAutoPlanProposesMovesAndSavesNothing(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	lead := api.client(t)
	lead.signIn(t, h.person(t, "lead", auth.RoleOwner))

	key, otherKey := h.projectKey(t, "ALN"), h.projectKey(t, "ALO")
	want(t, lead.post("/api/v1/projects", map[string]any{"name": h.name(t, "Aligned"), "key": key}), http.StatusCreated, "the project")
	want(t, lead.post("/api/v1/projects", map[string]any{"name": h.name(t, "Elsewhere"), "key": otherKey}), http.StatusCreated, "another project")

	issue := func(projectKey, summary, start, due string) string {
		made := want(t, lead.post("/api/v1/projects/"+projectKey+"/issues", map[string]any{"summary": summary}), http.StatusCreated, summary)
		k := obj(t, made, "issue")["key"].(string)
		want(t, lead.put("/api/v1/issues/"+k+"/schedule", map[string]any{"startDate": start, "dueDate": due}), http.StatusOK, "schedule "+summary)
		return k
	}
	link := func(blocker, blocked string) {
		want(t, lead.post("/api/v1/issues/"+blocker+"/links", map[string]any{"type": "blocks", "targetKey": blocked}), http.StatusCreated, blocker+" blocks "+blocked)
	}
	first := issue(key, "first", "2027-11-01", "2027-11-05")
	second := issue(key, "second", "2027-11-03", "2027-11-04")
	third := issue(key, "third", "2027-11-08", "2027-11-08")
	waiting := issue(key, "waits on another project", "2027-11-10", "2027-11-10")
	outside := issue(otherKey, "the other project's", "2027-11-01", "2027-11-12")
	link(first, second)
	link(second, third)
	link(outside, waiting)

	moves := func(r response) map[string]string {
		out := map[string]string{}
		for _, raw := range list(t, r, "moves") {
			m := raw.(map[string]any)
			to := m["to"].(map[string]any)
			out[m["issueKey"].(string)] = to["startDate"].(string)[:10] + ".." + to["dueDate"].(string)[:10] + " after " + m["after"].(string)
		}
		return out
	}

	t.Run("each blocked issue starts after its blockers, across projects too", func(t *testing.T) {
		got := moves(want(t, lead.post("/api/v1/projects/"+key+"/plan/align", map[string]any{}), http.StatusOK, "the proposal"))
		expect := map[string]string{
			second:  "2027-11-08..2027-11-09 after " + first,
			third:   "2027-11-10..2027-11-10 after " + second,
			waiting: "2027-11-15..2027-11-15 after " + outside,
		}
		if len(got) != len(expect) {
			t.Fatalf("moves = %v, want %v", got, expect)
		}
		for k, v := range expect {
			if got[k] != v {
				t.Errorf("%s = %q, want %q", k, got[k], v)
			}
		}
	})

	t.Run("the proposal writes nothing", func(t *testing.T) {
		reread := obj(t, want(t, lead.get("/api/v1/issues/"+second), http.StatusOK, "the second issue"), "issue")
		if !strings.HasPrefix(reread["startDate"].(string), "2027-11-03") {
			t.Errorf("the second issue starts %v after a proposal, want it where it was", reread["startDate"])
		}
		plan := want(t, lead.get("/api/v1/projects/"+key+"/plan"), http.StatusOK, "the plan")
		tooEarly := 0
		for _, raw := range list(t, plan, "warnings") {
			if raw.(map[string]any)["kind"] == "blocked-too-early" {
				tooEarly++
			}
		}
		if tooEarly == 0 {
			t.Error("the plan lost its warnings, as if the proposal had been saved")
		}
	})

	t.Run("a query limits what may move", func(t *testing.T) {
		got := moves(want(t, lead.post("/api/v1/projects/"+key+"/plan/align", map[string]any{"q": "key = " + second}), http.StatusOK, "a proposal by query"))
		if len(got) != 1 || got[second] == "" {
			t.Errorf("moves = %v, want only %s", got, second)
		}
		want(t, lead.post("/api/v1/projects/"+key+"/plan/align", map[string]any{"q": "nonsense"}), http.StatusBadRequest, "a bad query")
	})

	grant := func(name, role string) *client {
		email := h.outsider(t, name)
		var id string
		if err := h.super.QueryRow(context.Background(), `SELECT id FROM app_user WHERE email = $1`, email).Scan(&id); err != nil {
			t.Fatal(err)
		}
		want(t, lead.post("/api/v1/role-assignments", map[string]any{"role": role, "projectKey": key, "userId": id}), http.StatusCreated, "a "+role+" in the project")
		c := api.client(t)
		c.signIn(t, email)
		return c
	}

	t.Run("someone who may only read is refused", func(t *testing.T) {
		reader := grant("reader", "reader")
		resp := reader.post("/api/v1/projects/"+key+"/plan/align", map[string]any{})
		if resp.Status != http.StatusForbidden || resp.ErrorCode() != "forbidden" {
			t.Errorf("a reader's proposal: %d %s", resp.Status, resp.Raw)
		}
	})

	t.Run("a blocker in a project the person cannot read holds nothing back", func(t *testing.T) {
		user := grant("user", "user")
		got := moves(want(t, user.post("/api/v1/projects/"+key+"/plan/align", map[string]any{}), http.StatusOK, "a proposal without the other project"))
		if _, moved := got[waiting]; moved {
			t.Errorf("%s moved after a blocker its person cannot see: %v", waiting, got)
		}
		if got[second] == "" {
			t.Errorf("moves = %v, want the project's own still proposed", got)
		}
	})
}
