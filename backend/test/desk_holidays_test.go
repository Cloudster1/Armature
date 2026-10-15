//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/availability"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/issue"
)

// aServiceDesk makes a service desk over the API and returns its key.
func aServiceDesk(t *testing.T, h *harness, c *client, name, prefix string) string {
	t.Helper()
	made := want(t, c.post("/api/v1/projects", map[string]any{"name": name, "key": h.projectKey(t, prefix), "template": "service-desk"}), http.StatusCreated, "a desk")
	return obj(t, made, "project")["key"].(string)
}

// hoursFrom is nine to five on weekdays, with the desk's own days and the holiday calendar named.
func hoursFrom(holidayCalendarID any, own ...string) map[string]any {
	day := []map[string]string{{"from": "09:00", "to": "17:00"}}
	return map[string]any{
		"timezone":          "UTC",
		"hours":             map[string]any{"mon": day, "tue": day, "wed": day, "thu": day, "fri": day},
		"holidays":          append([]string{}, own...),
		"holidayCalendarId": holidayCalendarID,
	}
}

func TestADeskNamesAHolidayCalendarForItsBusinessHours(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "deskdays", auth.RoleOwner))
	key := aServiceDesk(t, h, owner, "Days desk", "DDY")
	path := "/api/v1/projects/" + key + "/business-calendar"

	name := h.name(t, "Bank holidays")
	bank := idOf(t, want(t, owner.post("/api/v1/holiday-calendars", map[string]any{"name": name}), http.StatusCreated, "a holiday calendar"), "calendar")

	saved := obj(t, want(t, owner.put(path, hoursFrom(bank, "2026-12-31")), http.StatusOK, "hours that name the calendar"), "calendar")
	if saved["holidayCalendarId"] != bank || saved["holidayCalendarName"] != name {
		t.Errorf("saved = %v, want the calendar named by id and name", saved)
	}
	read := obj(t, want(t, owner.get(path), http.StatusOK, "the hours read back"), "calendar")
	if read["holidayCalendarId"] != bank || read["holidayCalendarName"] != name {
		t.Errorf("read = %v, want the calendar kept", read)
	}
	if own := read["holidays"].([]any); len(own) != 1 || own[0] != "2026-12-31" {
		t.Errorf("the desk's own days = %v, want them kept beside the calendar", own)
	}

	t.Run("a calendar that is not there is refused with a sentence", func(t *testing.T) {
		refused := want(t, owner.put(path, hoursFrom(uuid.NewString())), http.StatusUnprocessableEntity, "a calendar that is not there")
		if message, _ := refused.Error()["message"].(string); !strings.Contains(message, "holiday calendar") || !strings.HasSuffix(message, ".") {
			t.Errorf("an unknown calendar refused with %q", message)
		}
		if read := obj(t, owner.get(path), "calendar"); read["holidayCalendarId"] != bank {
			t.Errorf("a refused save changed the link: %v", read)
		}
	})

	t.Run("somebody who does not administer the desk sets nothing", func(t *testing.T) {
		member := api.client(t)
		member.signIn(t, h.person(t, "deskdaysmember", auth.RoleMember))
		want(t, member.put(path, hoursFrom(nil)), http.StatusForbidden, "a member takes the calendar away")
		if read := obj(t, owner.get(path), "calendar"); read["holidayCalendarId"] != bank {
			t.Errorf("a refused member changed the link: %v", read)
		}
	})

	t.Run("none takes the link away", func(t *testing.T) {
		cleared := obj(t, want(t, owner.put(path, hoursFrom(nil)), http.StatusOK, "hours without a calendar"), "calendar")
		if cleared["holidayCalendarId"] != nil || cleared["holidayCalendarName"] != nil {
			t.Errorf("cleared = %v, want no calendar", cleared)
		}
	})
}

// The desk's clocks read the named calendar's days each time, so a day added
// to it while a clock runs counts from then on, half days as half the hours.
func TestADesksClockSkipsTheDaysOfItsHolidayCalendar(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "deskclock")
	d, p := ws.aDesk(t, h, "Clock desk")
	calendars := availability.NewService(h.cluster)
	bank, _, err := calendars.CreateCalendar(ws.ctx, availability.CalendarInput{Name: ptr(h.name(t, "Bank holidays"))}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	labourDay := availability.Holiday{Day: "2026-09-07", Name: "Labour Day"}
	if _, _, err := calendars.ReplaceDays(ws.ctx, bank.ID, []availability.Holiday{labourDay}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	cal := desk.DefaultCalendar()
	cal.HolidayCalendarID = &bank.ID
	if _, _, err := d.SaveCalendar(ws.ctx, p.Key, cal); err != nil {
		t.Fatal(err)
	}
	policies, _ := d.Policies(ws.ctx, p.Key)
	var first *desk.Policy
	for i := range policies {
		if policies[i].Metric == "first_response" {
			first = &policies[i]
		}
	}
	if first == nil {
		t.Fatal("the desk template has no first response policy")
	}
	day := 8 * 60
	goals := map[issue.Priority]int{issue.PriorityLowest: day, issue.PriorityLow: day, issue.PriorityMedium: day, issue.PriorityHigh: day, issue.PriorityHighest: day}
	if _, _, err := d.UpdatePolicy(ws.ctx, first.ID, desk.PolicyInput{Goals: goals, UseCalendar: ptr(true)}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}

	customer := ws.customerOf(t, h, "holidaycustomer")
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "Filed before the long weekend"}, customer)
	if err != nil {
		t.Fatal(err)
	}
	friday := time.Date(2026, 9, 4, 16, 0, 0, 0, time.UTC)
	if _, err := h.super.Exec(context.Background(), `UPDATE sla_timer SET started_at = $2, running_since = $2 WHERE issue_id = $1`, raised.ID, friday); err != nil {
		t.Fatal(err)
	}
	d.WithClock(func() time.Time { return time.Date(2026, 9, 8, 14, 0, 0, 0, time.UTC) })
	left := func(t *testing.T) int64 {
		t.Helper()
		timers, err := d.TimersFor(ws.ctx, raised.Key)
		if err != nil {
			t.Fatal(err)
		}
		for _, tm := range timers {
			if tm.Metric == "first_response" {
				return tm.RemainingSeconds
			}
		}
		t.Fatal("the request has no first response clock")
		return 0
	}

	// Tuesday 14:00: an hour on Friday and five on Tuesday, Monday being a holiday.
	if got := left(t); got != 2*60*60 {
		t.Errorf("time left on Tuesday afternoon = %ds, want two hours with Monday skipped", got)
	}

	// Tuesday becomes a half day while the clock runs: open 09:00 to 13:00.
	if _, _, err := calendars.ReplaceDays(ws.ctx, bank.ID, []availability.Holiday{labourDay, {Day: "2026-09-08", Name: "Half day", HalfDay: true}}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	if got := left(t); got != 3*60*60 {
		t.Errorf("time left after Tuesday became a half day = %ds, want three hours", got)
	}

	// The desk's own list still counts beside the calendar.
	cal.Holidays = []string{"2026-09-04"}
	if _, _, err := d.SaveCalendar(ws.ctx, p.Key, cal); err != nil {
		t.Fatal(err)
	}
	if got := left(t); got != 4*60*60 {
		t.Errorf("time left with Friday on the desk's own list = %ds, want four hours", got)
	}

	// Without the calendar, Monday and all of Tuesday count again.
	if _, err := h.super.Exec(context.Background(), `UPDATE business_calendar SET holiday_calendar_id = NULL, holidays = '{}' WHERE project_id = $1`, p.ID); err != nil {
		t.Fatal(err)
	}
	if got := left(t); got != -6*60*60 {
		t.Errorf("time left without the calendar = %ds, want the goal six hours over", got)
	}
}

func TestAHolidayCalendarADeskUsesIsNotDeleted(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "deskdelete", auth.RoleOwner))
	front := aServiceDesk(t, h, owner, "Front", "FRD")
	people := aServiceDesk(t, h, owner, "People", "PPD")
	name := h.name(t, "Bank holidays")
	bank := idOf(t, want(t, owner.post("/api/v1/holiday-calendars", map[string]any{"name": name}), http.StatusCreated, "a holiday calendar"), "calendar")
	for _, key := range []string{people, front} {
		want(t, owner.put("/api/v1/projects/"+key+"/business-calendar", hoursFrom(bank)), http.StatusOK, "a desk takes the calendar")
	}

	for _, raw := range list(t, want(t, owner.get("/api/v1/holiday-calendars"), http.StatusOK, "the calendars"), "calendars") {
		if c := raw.(map[string]any); c["id"] == bank {
			if desks := c["desks"].([]any); len(desks) != 2 || desks[0] != "Front" || desks[1] != "People" {
				t.Errorf("desks = %v, want both by name", desks)
			}
		}
	}

	refused := want(t, owner.delete("/api/v1/holiday-calendars/"+bank), http.StatusConflict, "delete a calendar two desks use")
	if message, _ := refused.Error()["message"].(string); message != name+" is the holiday calendar of the Front and People desks; choose another one on their service desk pages first." {
		t.Errorf("the refusal reads %q", message)
	}

	// The database refuses it too, past the service.
	_, err := h.super.Exec(context.Background(), `DELETE FROM holiday_calendar WHERE id = $1`, bank)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Errorf("deleting through SQL = %v, want a foreign key violation", err)
	}

	want(t, owner.put("/api/v1/projects/"+people+"/business-calendar", hoursFrom(nil)), http.StatusOK, "one desk lets go")
	refused = want(t, owner.delete("/api/v1/holiday-calendars/"+bank), http.StatusConflict, "delete a calendar one desk uses")
	if message, _ := refused.Error()["message"].(string); message != name+" is the holiday calendar of the Front desk; choose another one on its service desk page first." {
		t.Errorf("the refusal reads %q", message)
	}

	want(t, owner.put("/api/v1/projects/"+front+"/business-calendar", hoursFrom(nil)), http.StatusOK, "the other lets go")
	want(t, owner.delete("/api/v1/holiday-calendars/"+bank), http.StatusNoContent, "delete a calendar no desk uses")
}
