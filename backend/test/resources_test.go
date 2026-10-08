//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/perm"
)

// resourceRows reads the rows of a resource view by name, with their weeks by Monday.
func resourceRows(t *testing.T, got response) map[string]map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]map[string]any{}
	for _, raw := range list(t, got, "rows") {
		row := raw.(map[string]any)
		weeks := map[string]map[string]any{}
		for _, w := range row["weeks"].([]any) {
			week := w.(map[string]any)
			weeks[week["start"].(string)[:10]] = week
		}
		out[row["name"].(string)] = weeks
	}
	return out
}

// A kanban project sets its work against its teams or its people, a scrum
// project against its teams only, and the database holds the second rule too.
func TestTheResourceViewSetsHoursAgainstTeamsOrPeople(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	orgID := obj(t, owner.signup(t, h, "resources"), "principal", "org")["id"].(string)
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Flow", "key": "FLOW", "template": "kanban"}), http.StatusCreated, "a kanban project")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Sprinting", "key": "SPRT", "template": "scrum"}), http.StatusCreated, "a scrum project")
	_, adaID := api.namedMember(t, h, owner, "Ada Lovelace")
	_, beaID := api.namedMember(t, h, owner, "Bea Lisboa")
	crew := idOf(t, want(t, owner.post("/api/v1/projects/FLOW/teams", map[string]any{"name": "Crew"}), http.StatusCreated, "a team"), "team")
	for _, id := range []string{adaID, beaID} {
		want(t, owner.post("/api/v1/teams/"+crew+"/members", map[string]any{"userId": id}), http.StatusOK, "join the team")
	}
	// Ada is away on the Tuesday of the week of 3 March 2031.
	want(t, owner.post("/api/v1/absences", map[string]any{"userId": adaID, "startsOn": "2031-03-04", "endsOn": "2031-03-04"}), http.StatusCreated, "an absence")

	file := func(body map[string]any, minutes int) string {
		t.Helper()
		key := obj(t, want(t, owner.post("/api/v1/projects/FLOW/issues", body), http.StatusCreated, "an issue"), "issue")["key"].(string)
		if minutes > 0 {
			want(t, owner.patch("/api/v1/issues/"+key, map[string]any{"timeRemainingMinutes": minutes}), http.StatusOK, "its hours")
		}
		return key
	}
	adas := file(map[string]any{"summary": "Ada's work", "assigneeId": adaID, "teamId": crew, "startDate": "2031-03-03", "dueDate": "2031-03-07"}, 16*60)
	beas := file(map[string]any{"summary": "Bea's long day", "assigneeId": beaID, "startDate": "2031-03-03", "dueDate": "2031-03-03"}, 50*60)
	unsized := file(map[string]any{"summary": "nobody sized this"}, 0)
	undated := file(map[string]any{"summary": "due but not started", "dueDate": "2031-03-05"}, 3*60)
	const window = "?from=2031-03-03&to=2031-03-09"

	t.Run("new projects take their method from the template and have the page", func(t *testing.T) {
		flow := obj(t, owner.get("/api/v1/projects/FLOW"), "project")
		sprinting := obj(t, owner.get("/api/v1/projects/SPRT"), "project")
		if flow["planningMethod"] != "kanban" || sprinting["planningMethod"] != "scrum" || flow["resourceGrouping"] != "team" || sprinting["resourceGrouping"] != "team" {
			t.Errorf("flow = %v %v, sprinting = %v %v", flow["planningMethod"], flow["resourceGrouping"], sprinting["planningMethod"], sprinting["resourceGrouping"])
		}
		if !strings.Contains(strings.Join(strs(flow["features"]), ","), "resources") {
			t.Errorf("features = %v, want the resource page", flow["features"])
		}
	})

	t.Run("by team, the crew has its members' hours and the rest is unassigned", func(t *testing.T) {
		got := want(t, owner.get("/api/v1/projects/FLOW/resources"+window), http.StatusOK, "the resources")
		if body := got.Body; body["grouping"] != "team" || body["method"] != "kanban" || len(body["weeks"].([]any)) != 1 {
			t.Fatalf("got %v", body)
		}
		rows := resourceRows(t, got)
		week := rows["Crew"]["2031-03-03"]
		// Two people's forty hours, less the day Ada is away.
		if week["capacityHours"] != 72.0 || week["nominalHours"] != 80.0 || week["daysAway"] != 1.0 || week["loadHours"] != 16.0 {
			t.Errorf("the crew's week = %v, want 16 h against 72 of 80", week)
		}
		issues := week["issues"].([]any)
		if len(issues) != 1 || issues[0].(map[string]any)["key"] != adas || issues[0].(map[string]any)["hours"] != 16.0 {
			t.Errorf("the crew's issues = %v", issues)
		}
		if u := rows["Unassigned"]["2031-03-03"]; u["loadHours"] != 50.0 || u["capacityHours"] != nil {
			t.Errorf("the unassigned week = %v, want Bea's 50 h and no capacity", u)
		}
		if len(rows) != 2 || len(list(t, got, "warnings")) != 0 {
			t.Errorf("rows = %v, warnings = %v", rows, got.Body["warnings"])
		}
		unestimated, unscheduled := list(t, got, "unestimated"), list(t, got, "unscheduled")
		if len(unestimated) != 1 || unestimated[0].(map[string]any)["key"] != unsized {
			t.Errorf("unestimated = %v", unestimated)
		}
		if len(unscheduled) != 1 || unscheduled[0].(map[string]any)["key"] != undated || unscheduled[0].(map[string]any)["hours"] != 3.0 {
			t.Errorf("unscheduled = %v", unscheduled)
		}
	})

	t.Run("by person, Ada's week is shorter and Bea is over", func(t *testing.T) {
		switched := obj(t, want(t, owner.patch("/api/v1/projects/FLOW", map[string]any{"resourceGrouping": "person"}), http.StatusOK, "plan by person"), "project")
		if switched["resourceGrouping"] != "person" {
			t.Fatalf("switched = %v", switched)
		}
		got := want(t, owner.get("/api/v1/projects/FLOW/resources"+window), http.StatusOK, "the resources")
		rows := resourceRows(t, got)
		ada, bea := rows["Ada Lovelace"]["2031-03-03"], rows["Bea Lisboa"]["2031-03-03"]
		if ada["capacityHours"] != 32.0 || ada["nominalHours"] != 40.0 || ada["daysAway"] != 1.0 || ada["loadHours"] != 16.0 {
			t.Errorf("Ada's week = %v, want 16 h against 32 of 40", ada)
		}
		if bea["capacityHours"] != 40.0 || bea["loadHours"] != 50.0 || bea["issues"].([]any)[0].(map[string]any)["key"] != beas {
			t.Errorf("Bea's week = %v, want 50 h against 40", bea)
		}
		order := []string{}
		for _, raw := range list(t, got, "rows") {
			order = append(order, raw.(map[string]any)["name"].(string))
		}
		if strings.Join(order, ",") != "Ada Lovelace,Bea Lisboa,Unassigned" {
			t.Errorf("rows = %v, want people by name and the unassigned last", order)
		}
		warnings := list(t, got, "warnings")
		if len(warnings) != 1 || warnings[0].(map[string]any)["kind"] != "over-allocated" || warnings[0].(map[string]any)["person"] != "Bea Lisboa" {
			t.Errorf("warnings = %v, want Bea over-allocated", warnings)
		}
	})

	t.Run("a scrum project plans by team, and turning to scrum takes the grouping back", func(t *testing.T) {
		got := want(t, owner.get("/api/v1/projects/SPRT/resources"), http.StatusOK, "the scrum resources")
		if got.Body["method"] != "scrum" || got.Body["grouping"] != "team" || len(got.Body["weeks"].([]any)) != 8 {
			t.Errorf("got %v", got.Body)
		}
		refused := want(t, owner.patch("/api/v1/projects/SPRT", map[string]any{"resourceGrouping": "person"}), http.StatusUnprocessableEntity, "scrum by person")
		refusedWithASentence(t, refused, "Switch the project to kanban")
		turned := obj(t, want(t, owner.patch("/api/v1/projects/FLOW", map[string]any{"planningMethod": "scrum"}), http.StatusOK, "turn to scrum"), "project")
		if turned["planningMethod"] != "scrum" || turned["resourceGrouping"] != "team" {
			t.Errorf("turned = %v %v, want scrum by team", turned["planningMethod"], turned["resourceGrouping"])
		}
		back := obj(t, want(t, owner.patch("/api/v1/projects/FLOW", map[string]any{"planningMethod": "kanban", "resourceGrouping": "person"}), http.StatusOK, "back to kanban by person"), "project")
		if back["planningMethod"] != "kanban" || back["resourceGrouping"] != "person" {
			t.Errorf("back = %v %v", back["planningMethod"], back["resourceGrouping"])
		}
		want(t, owner.patch("/api/v1/projects/FLOW", map[string]any{"planningMethod": "waterfall"}), http.StatusUnprocessableEntity, "not a method")
	})

	t.Run("the database refuses a scrum project by person", func(t *testing.T) {
		for name, query := range map[string]string{
			"scrum by person":                      `UPDATE project SET resource_grouping = 'person' WHERE key = 'SPRT' AND org_id = $1`,
			"a method":                             `UPDATE project SET planning_method = 'waterfall' WHERE key = 'FLOW' AND org_id = $1`,
			"a grouping":                           `UPDATE project SET resource_grouping = 'everyone' WHERE key = 'FLOW' AND org_id = $1`,
			"turning to scrum without the service": `UPDATE project SET planning_method = 'scrum' WHERE key = 'FLOW' AND org_id = $1`,
		} {
			_, err := h.super.Exec(context.Background(), query, orgID)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Errorf("%s: got %v, want a check violation", name, err)
			}
		}
	})

	t.Run("a window that is not one is refused with a sentence", func(t *testing.T) {
		refused := want(t, owner.get("/api/v1/projects/FLOW/resources?from=2031-03-10&to=2031-03-01"), http.StatusUnprocessableEntity, "a backwards window")
		refusedWithASentence(t, refused, "swap them")
		want(t, owner.get("/api/v1/projects/FLOW/resources?from=2031-01-06&to=2031-12-31"), http.StatusUnprocessableEntity, "a long window")
	})

	t.Run("without the feature the view is refused", func(t *testing.T) {
		features := strs(obj(t, owner.get("/api/v1/projects/FLOW"), "project")["features"])
		kept := []string{}
		for _, f := range features {
			if f != "resources" {
				kept = append(kept, f)
			}
		}
		want(t, owner.patch("/api/v1/projects/FLOW", map[string]any{"features": kept}), http.StatusOK, "turn the page off")
		refused := want(t, owner.get("/api/v1/projects/FLOW/resources"), http.StatusConflict, "the page is off")
		refusedWithASentence(t, refused, "resource planning")
		want(t, owner.patch("/api/v1/projects/FLOW", map[string]any{"features": features}), http.StatusOK, "turn it back on")
	})

	t.Run("a story and its subtasks are counted once, on the level that carries the hours", func(t *testing.T) {
		typeID := map[string]string{}
		for _, raw := range list(t, owner.get("/api/v1/issue-types"), "issueTypes") {
			typ := raw.(map[string]any)
			typeID[typ["name"].(string)] = typ["id"].(string)
		}
		squad := idOf(t, want(t, owner.post("/api/v1/projects/SPRT/teams", map[string]any{"name": "Squad"}), http.StatusCreated, "a team"), "team")
		filed := func(body map[string]any, minutes int) string {
			t.Helper()
			body["teamId"], body["startDate"], body["dueDate"] = squad, "2031-04-07", "2031-04-11"
			key := obj(t, want(t, owner.post("/api/v1/projects/SPRT/issues", body), http.StatusCreated, "an issue"), "issue")["key"].(string)
			want(t, owner.patch("/api/v1/issues/"+key, map[string]any{"timeRemainingMinutes": minutes}), http.StatusOK, "its hours")
			return key
		}
		story := filed(map[string]any{"summary": "the story", "typeId": typeID[bootstrap.TypeStory]}, 16*60)
		for _, summary := range []string{"one half", "the other half"} {
			filed(map[string]any{"summary": summary, "typeId": typeID[bootstrap.TypeSubtask], "parentKey": story}, 8*60)
		}
		got := want(t, owner.get("/api/v1/projects/SPRT/resources?from=2031-04-07&to=2031-04-13"), http.StatusOK, "the resources")
		week := resourceRows(t, got)["Squad"]["2031-04-07"]
		if issues := week["issues"].([]any); week["loadHours"] != 16.0 || len(issues) != 1 || issues[0].(map[string]any)["key"] != story {
			t.Errorf("the squad's week = %v, want the story's 16 h once", week)
		}
	})

	t.Run("somebody who cannot see the project does not find it", func(t *testing.T) {
		member := api.asRole(t, h, owner, "SPRT", perm.User)
		want(t, member.get("/api/v1/projects/FLOW/resources"), http.StatusNotFound, "another project's resources")
		want(t, member.get("/api/v1/projects/SPRT/resources"), http.StatusOK, "their own project's resources")
	})
}

func strs(v any) []string {
	out := []string{}
	for _, s := range v.([]any) {
		out = append(out, s.(string))
	}
	return out
}
