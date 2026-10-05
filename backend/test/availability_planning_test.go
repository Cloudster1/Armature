//go:build integration

package test

import (
	"net/http"
	"testing"
)

// The project calendar and the plan read holidays and absences: of the default
// calendar and of the project's people, and nobody else's.
func TestHolidaysAndAbsencesReachTheCalendarAndThePlan(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "availplan")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Available", "key": "AVL", "template": "scrum"}), http.StatusCreated, "a project")

	_, adaID := api.namedMember(t, h, owner, "Ada Lovelace")
	_, beaID := api.namedMember(t, h, owner, "Bea Lisboa")
	_, carlID := api.namedMember(t, h, owner, "Carl Assigned")
	_, stanID := api.namedMember(t, h, owner, "Stan Stranger")

	crew := idOf(t, want(t, owner.post("/api/v1/projects/AVL/teams", map[string]any{"name": "Crew"}), http.StatusCreated, "a team"), "team")
	want(t, owner.patch("/api/v1/teams/"+crew, map[string]any{"weeklyCapacity": 20}), http.StatusOK, "the team's capacity")
	for _, id := range []string{adaID, beaID} {
		want(t, owner.post("/api/v1/teams/"+crew+"/members", map[string]any{"userId": id}), http.StatusOK, "join the team")
	}

	// The default calendar has Wednesday the 12th off; Bea keeps Lisbon's, and
	// Stan, who is not on the project, keeps one of his own.
	calendars := list(t, want(t, owner.get("/api/v1/holiday-calendars"), http.StatusOK, "the calendars"), "calendars")
	standard := calendars[0].(map[string]any)["id"].(string)
	want(t, owner.put("/api/v1/holiday-calendars/"+standard+"/days", map[string]any{"days": []map[string]any{{"day": "2031-03-12", "name": "Founders' Day"}}}), http.StatusOK, "a default holiday")
	keep := func(person, name, day, holiday string) {
		t.Helper()
		id := idOf(t, want(t, owner.post("/api/v1/holiday-calendars", map[string]any{"name": name}), http.StatusCreated, "a calendar"), "calendar")
		want(t, owner.put("/api/v1/holiday-calendars/"+id+"/days", map[string]any{"days": []map[string]any{{"day": day, "name": holiday}}}), http.StatusOK, "its holiday")
		want(t, owner.put("/api/v1/users/"+person+"/schedule", map[string]any{"calendarId": id}), http.StatusOK, "give it to somebody")
	}
	keep(beaID, "Lisbon", "2031-03-14", "Lisbon day")
	keep(stanID, "Faraway", "2031-03-13", "Far day")

	away := func(person, from, to string) {
		t.Helper()
		want(t, owner.post("/api/v1/absences", map[string]any{"userId": person, "startsOn": from, "endsOn": to}), http.StatusCreated, "an absence")
	}
	away(adaID, "2031-02-26", "2031-03-04")
	away(carlID, "2031-03-24", "2031-03-24")
	away(stanID, "2031-03-05", "2031-03-06")

	// Ten points for the crew over a week with the default holiday in it, and
	// work assigned to Carl, which makes him one of the project's people.
	crewed := obj(t, want(t, owner.post("/api/v1/projects/AVL/issues", map[string]any{"summary": "crew work", "estimate": 10}), http.StatusCreated, "crew work"), "issue")["key"].(string)
	want(t, owner.put("/api/v1/issues/"+crewed+"/team", map[string]any{"teamId": crew}), http.StatusOK, "hand it to the crew")
	want(t, owner.put("/api/v1/issues/"+crewed+"/schedule", map[string]any{"startDate": "2031-03-10", "dueDate": "2031-03-14"}), http.StatusOK, "schedule it")
	carls := obj(t, want(t, owner.post("/api/v1/projects/AVL/issues", map[string]any{"summary": "Carl's work", "assigneeId": carlID}), http.StatusCreated, "Carl's work"), "issue")["key"].(string)
	want(t, owner.put("/api/v1/issues/"+carls+"/schedule", map[string]any{"startDate": "2031-03-17", "dueDate": "2031-03-20"}), http.StatusOK, "schedule Carl's work")
	want(t, owner.post("/api/v1/projects/AVL/sprints", map[string]any{"name": "Crew sprint", "teamId": crew, "startsOn": "2031-03-03", "endsOn": "2031-03-14"}), http.StatusCreated, "a sprint")

	t.Run("the month shows the holidays and who of the project is away", func(t *testing.T) {
		items := list(t, want(t, owner.get("/api/v1/projects/AVL/calendar?month=2031-03"), http.StatusOK, "the month"), "month", "items")
		byTitle := map[string]map[string]any{}
		for _, raw := range items {
			it := raw.(map[string]any)
			if it["kind"] == "holiday" || it["kind"] == "absence" {
				byTitle[it["title"].(string)] = it
			}
		}
		if it := byTitle["Founders' Day"]; it == nil || it["kind"] != "holiday" || it["from"] != "2031-03-12" || it["calendar"] != nil {
			t.Errorf("the default holiday = %v", it)
		}
		if it := byTitle["Lisbon day (Lisbon)"]; it == nil || it["calendar"] != "Lisbon" || it["from"] != "2031-03-14" {
			t.Errorf("the holiday of a calendar somebody on the project keeps = %v", it)
		}
		if it := byTitle["Ada Lovelace"]; it == nil || it["kind"] != "absence" || it["from"] != "2031-03-01" || it["to"] != "2031-03-04" {
			t.Errorf("Ada's absence, cut to the month = %v", it)
		}
		if it := byTitle["Carl Assigned"]; it == nil || it["from"] != "2031-03-24" {
			t.Errorf("the absence of somebody assigned work in the month = %v", it)
		}
		if byTitle["Stan Stranger"] != nil || byTitle["Far day (Faraway)"] != nil {
			t.Errorf("somebody outside the project leaked onto its calendar: %v", byTitle)
		}
		if len(byTitle) != 4 {
			t.Errorf("holidays and absences = %v, want the four above", byTitle)
		}
	})

	t.Run("the plan shades the default holidays and shrinks the crew's weeks", func(t *testing.T) {
		got := want(t, owner.get("/api/v1/projects/AVL/plan"), http.StatusOK, "the plan")
		holidays := list(t, got, "holidays")
		if len(holidays) != 1 || holidays[0].(map[string]any)["name"] != "Founders' Day" {
			t.Errorf("the plan's holidays = %v, want the default calendar's one", holidays)
		}
		weeks := map[string]map[string]any{}
		for _, raw := range list(t, got, "load", "rows") {
			row := raw.(map[string]any)
			if row["team"] != "Crew" {
				continue
			}
			for _, w := range row["weeks"].([]any) {
				week := w.(map[string]any)
				weeks[week["start"].(string)[:10]] = week
			}
		}
		// Ada is away Monday and Tuesday: eight of ten days are left.
		if w := weeks["2031-03-03"]; w == nil || w["capacity"] != 16.0 || w["nominalCapacity"] != 20.0 || w["daysAway"] != 2.0 {
			t.Errorf("the week Ada is away = %v, want 16 of 20 after two days", w)
		}
		// Ada has Wednesday off, Bea Friday: the ten points fall on the four days the crew works.
		if w := weeks["2031-03-10"]; w == nil || w["capacity"] != 16.0 || w["holidays"] != 2.0 || w["load"] != 10.0 {
			t.Errorf("the week of the holidays = %v, want 10 points against 16 after two holidays", w)
		}

		var sprint map[string]any
		for _, raw := range list(t, got, "sprints") {
			if s := raw.(map[string]any); s["sprint"].(map[string]any)["name"] == "Crew sprint" {
				sprint = s
			}
		}
		// Twenty days of eight hours, less Ada's two away and the two holidays.
		if sprint == nil || sprint["availableHours"] != 128.0 || sprint["nominalHours"] != 160.0 {
			t.Errorf("the crew's sprint = %v, want 128 of 160 hours", sprint)
		}
	})
}
