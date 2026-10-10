//go:build integration

package test

import (
	"net/http"
	"testing"
)

// The page draws six weeks, padded from the neighbouring months, so their days
// off come along and an absence is not cut where the month ends.
func TestTheCalendarMonthCarriesTheDaysOffOfTheSixWeeksItDraws(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "calgrid")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Grid", "key": "GRD", "template": "scrum"}), http.StatusCreated, "a project")

	_, adaID := api.namedMember(t, h, owner, "Ada Lovelace")
	_, bobID := api.namedMember(t, h, owner, "Bob Away")
	_, carlID := api.namedMember(t, h, owner, "Carl Later")
	crew := idOf(t, want(t, owner.post("/api/v1/projects/GRD/teams", map[string]any{"name": "Crew"}), http.StatusCreated, "a team"), "team")
	for _, id := range []string{adaID, bobID, carlID} {
		want(t, owner.post("/api/v1/teams/"+crew+"/members", map[string]any{"userId": id}), http.StatusOK, "join the team")
	}

	// September 2032 begins on a Wednesday: its grid runs from Monday 30 August
	// to Sunday 10 October.
	calendars := list(t, want(t, owner.get("/api/v1/holiday-calendars"), http.StatusOK, "the calendars"), "calendars")
	standard := calendars[0].(map[string]any)["id"].(string)
	want(t, owner.put("/api/v1/holiday-calendars/"+standard+"/days", map[string]any{"days": []map[string]any{
		{"day": "2032-08-30", "name": "Late Summer Day"},
		{"day": "2032-10-01", "name": "Harvest Day"},
		{"day": "2032-10-11", "name": "Past the Grid Day"},
	}}), http.StatusOK, "the default holidays")

	away := func(person, from, to string) {
		t.Helper()
		want(t, owner.post("/api/v1/absences", map[string]any{"userId": person, "startsOn": from, "endsOn": to}), http.StatusCreated, "an absence")
	}
	away(bobID, "2032-09-27", "2032-10-08")
	away(adaID, "2032-08-16", "2032-10-22")
	away(carlID, "2032-10-11", "2032-10-12")

	items := list(t, want(t, owner.get("/api/v1/projects/GRD/calendar?month=2032-09"), http.StatusOK, "September"), "month", "items")
	byTitle := map[string]map[string]any{}
	for _, raw := range items {
		it := raw.(map[string]any)
		if it["kind"] == "holiday" || it["kind"] == "absence" {
			byTitle[it["title"].(string)] = it
		}
	}

	if it := byTitle["Harvest Day"]; it == nil || it["kind"] != "holiday" || it["from"] != "2032-10-01" {
		t.Errorf("the October holiday on the grid's last week = %v", it)
	}
	if it := byTitle["Late Summer Day"]; it == nil || it["from"] != "2032-08-30" {
		t.Errorf("the August holiday on the grid's first day = %v", it)
	}
	if it := byTitle["Bob Away"]; it == nil || it["kind"] != "absence" || it["from"] != "2032-09-27" || it["to"] != "2032-10-08" {
		t.Errorf("Bob's absence into October = %v, want all of it, 2032-09-27 to 2032-10-08", it)
	}
	// Running past both ends of the grid, the absence keeps its own days so
	// the page draws it as continuing rather than ending on the last cell.
	if it := byTitle["Ada Lovelace"]; it == nil || it["from"] != "2032-08-16" || it["to"] != "2032-10-22" {
		t.Errorf("Ada's absence around the whole grid = %v, want 2032-08-16 to 2032-10-22", it)
	}
	if byTitle["Past the Grid Day"] != nil || byTitle["Carl Later"] != nil {
		t.Errorf("a day off after the grid's last day reached the month: %v", byTitle)
	}
	if len(byTitle) != 4 {
		t.Errorf("holidays and absences = %v, want the four above", byTitle)
	}
}
