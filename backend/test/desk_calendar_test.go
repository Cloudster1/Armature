//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Overlapping hours on one day are refused over the API, and a calendar saved
// with them before that rule still counts each open minute once.
func TestADeskCalendarRefusesOverlappingHours(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "overlaps")
	made := want(t, c.post("/api/v1/projects", map[string]any{"name": "Overlap", "key": "OVL" + strings.ToUpper(uuid.New().String()[:3]), "template": "service-desk"}), http.StatusCreated, "desk")
	key := obj(t, made, "project")["key"].(string)
	path := "/api/v1/projects/" + key + "/business-calendar"

	overlapping := map[string]any{"timezone": "UTC", "hours": map[string]any{"mon": []map[string]string{{"from": "09:00", "to": "13:00"}, {"from": "12:00", "to": "17:00"}}}}
	refused := want(t, c.put(path, overlapping), http.StatusUnprocessableEntity, "overlapping hours")
	if message, _ := refused.Error()["message"].(string); message != "Monday: 12:00 to 17:00 overlaps 09:00 to 13:00; join them into one span" {
		t.Errorf("overlapping hours refused with %q", message)
	}
	want(t, c.get(path), http.StatusConflict, "nothing saved after the refusal")

	split := map[string]any{"timezone": "UTC", "hours": map[string]any{"mon": []map[string]string{{"from": "09:00", "to": "12:00"}, {"from": "13:00", "to": "17:00"}}}}
	want(t, c.put(path, split), http.StatusOK, "a day with a lunch break")

	// Written the way a calendar saved before the rule looks, past the service.
	if _, err := h.super.Exec(context.Background(), `
		UPDATE business_calendar bc SET hours = '{"mon":[{"from":"09:00","to":"13:00"},{"from":"12:00","to":"17:00"}]}'
		FROM project p WHERE p.id = bc.project_id AND p.key = $1`, key); err != nil {
		t.Fatal(err)
	}
	read := obj(t, want(t, c.get(path), http.StatusOK, "a saved calendar with overlaps"), "calendar")
	if days := read["hours"].(map[string]any)["mon"].([]any); len(days) != 2 {
		t.Errorf("the saved hours came back as %v, want them as written", days)
	}
}

// The clock of a goal on a desk whose saved hours overlap counts the overlap
// once: eight open hours on Monday, not nine.
func TestAGoalOnOverlappingSavedHoursCountsThemOnce(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "overlap")
	d, p := ws.aDesk(t, h, "Overlap desk")
	if _, err := h.super.Exec(context.Background(), `
		INSERT INTO business_calendar (org_id, project_id, timezone, hours, holidays)
		SELECT org_id, id, 'UTC', '{"mon":[{"from":"09:00","to":"13:00"},{"from":"12:00","to":"17:00"}]}', '{}' FROM project WHERE id = $1`, p.ID); err != nil {
		t.Fatal(err)
	}
	cal, err := d.Calendar(ws.ctx, p.Key)
	if err != nil {
		t.Fatalf("a saved calendar with overlaps could not be read: %v", err)
	}
	monday := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	if got := cal.WorkingDuration(monday, monday.AddDate(0, 0, 1)); got != 8*time.Hour {
		t.Errorf("Monday = %s, want 8h", got)
	}
}
