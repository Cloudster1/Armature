//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/availability"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
)

// holidayFile is an .ics as a calendar application exports it: folded lines,
// an event over several days, and one without an end.
const holidayFile = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Test//Holidays//EN\r\n" +
	"BEGIN:VEVENT\r\nUID:a\r\nDTSTART;VALUE=DATE:20261224\r\nDTEND;VALUE=DATE:20261227\r\nSUMMARY:Christmas\r\n  break\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:b\r\nDTSTART;VALUE=DATE:20270101\r\nSUMMARY:New Year's Day\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

// localMember makes a password account in the owner's organization and signs it in.
func (s *apiServer) localMember(t *testing.T, h *harness, owner *client, role string) (*client, string) {
	t.Helper()
	email := h.email(t, "worker")
	made := want(t, owner.post("/api/v1/users", map[string]string{
		"email": email, "name": "Working Person", "role": role, "password": testPassword,
	}), http.StatusCreated, "make a member")
	them := s.client(t)
	want(t, them.post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusOK, "sign the member in")
	return them, obj(t, made, "user")["id"].(string)
}

func TestHolidayCalendarsAreTheAdministratorsToKeep(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "holidays")
	member, _ := api.localMember(t, h, owner, "member")

	var standard map[string]any
	t.Run("a new organization has an empty default calendar", func(t *testing.T) {
		calendars := list(t, want(t, owner.get("/api/v1/holiday-calendars"), http.StatusOK, "list calendars"), "calendars")
		if len(calendars) != 1 {
			t.Fatalf("calendars = %v, want the one default", calendars)
		}
		standard = calendars[0].(map[string]any)
		if standard["name"] != bootstrap.DefaultHolidayCalendarName || standard["default"] != true || standard["dayCount"] != float64(0) {
			t.Errorf("the new organization's calendar = %v", standard)
		}
		want(t, api.client(t).get("/api/v1/holiday-calendars"), http.StatusUnauthorized, "nobody signed in")
	})

	made := obj(t, want(t, owner.post("/api/v1/holiday-calendars", map[string]any{"name": " Germany "}), http.StatusCreated, "make a calendar"), "calendar")
	germany := made["id"].(string)
	if made["name"] != "Germany" || made["default"] != false {
		t.Fatalf("made = %v, want a trimmed calendar that is not the default", made)
	}

	t.Run("names are the organization's own and must be there", func(t *testing.T) {
		want(t, owner.post("/api/v1/holiday-calendars", map[string]any{"name": "germany"}), http.StatusConflict, "the same name again")
		refused := want(t, owner.post("/api/v1/holiday-calendars", map[string]any{"name": "  "}), http.StatusUnprocessableEntity, "a blank name")
		if message, _ := refused.Error()["message"].(string); !strings.HasSuffix(message, ".") || !strings.Contains(message, "needs a name") {
			t.Errorf("a blank name refused with %q", message)
		}
		want(t, owner.post("/api/v1/holiday-calendars", map[string]any{"name": strings.Repeat("x", availability.MaxNameLength+1)}), http.StatusUnprocessableEntity, "a long name")
	})

	t.Run("one calendar is read with its days, and a stranger's is not there", func(t *testing.T) {
		read := obj(t, want(t, owner.get("/api/v1/holiday-calendars/"+germany), http.StatusOK, "read a calendar"), "calendar")
		if read["name"] != "Germany" {
			t.Errorf("read = %v", read)
		}
		want(t, owner.get("/api/v1/holiday-calendars/"+uuid.NewString()), http.StatusNotFound, "a calendar that is not there")
		want(t, owner.get("/api/v1/holiday-calendars/not-an-id"), http.StatusBadRequest, "an id that is not one")
	})

	t.Run("the days are set, refused when they cannot be read, and imported from a file", func(t *testing.T) {
		set := obj(t, want(t, owner.put("/api/v1/holiday-calendars/"+germany+"/days", map[string]any{"days": []map[string]any{
			{"day": "2026-12-25", "name": "Erster Weihnachtstag"},
			{"day": "2026-12-24", "name": "Heiligabend", "halfDay": true},
		}}), http.StatusOK, "set the days"), "calendar")
		if days := set["days"].([]any); len(days) != 2 || days[0].(map[string]any)["day"] != "2026-12-24" || days[0].(map[string]any)["halfDay"] != true {
			t.Errorf("days = %v, want two in order with the half day kept", days)
		}
		refused := want(t, owner.put("/api/v1/holiday-calendars/"+germany+"/days", map[string]any{"days": []map[string]any{{"day": "25.12.2026", "name": "Weihnachten"}}}), http.StatusUnprocessableEntity, "a day that is not a date")
		if message, _ := refused.Error()["message"].(string); !strings.Contains(message, "YYYY-MM-DD") {
			t.Errorf("a bad day refused with %q", message)
		}

		imported := want(t, owner.upload("/api/v1/holiday-calendars/"+germany+"/import", "file", "holidays.ics", "text/calendar", []byte(holidayFile)), http.StatusOK, "import a file")
		if imported.Body["imported"] != float64(4) {
			t.Errorf("imported = %v, want the four days the file covers", imported.Body["imported"])
		}
		days := map[string]string{}
		for _, row := range list(t, imported, "calendar", "days") {
			d := row.(map[string]any)
			days[d["day"].(string)] = d["name"].(string)
		}
		// The file is newer than the calendar, so a day in both takes the file's name.
		if len(days) != 4 || days["2026-12-25"] != "Christmas break" || days["2027-01-01"] != "New Year's Day" {
			t.Errorf("days after the import = %v", days)
		}

		recurring := strings.Replace(holidayFile, "SUMMARY:New Year's Day", "SUMMARY:New Year's Day\r\nRRULE:FREQ=YEARLY", 1)
		refused = want(t, owner.upload("/api/v1/holiday-calendars/"+germany+"/import", "file", "recurring.ics", "text/calendar", []byte(recurring)), http.StatusUnprocessableEntity, "a recurring event")
		if message, _ := refused.Error()["message"].(string); !strings.Contains(message, "written out one by one") || !strings.HasSuffix(message, ".") {
			t.Errorf("a recurring event refused with %q", message)
		}
		if read := obj(t, owner.get("/api/v1/holiday-calendars/"+germany), "calendar"); read["dayCount"] != float64(4) {
			t.Errorf("a refused import changed the calendar: %v", read)
		}
	})

	t.Run("the default moves to another calendar and is never left without one", func(t *testing.T) {
		moved := obj(t, want(t, owner.patch("/api/v1/holiday-calendars/"+germany, map[string]any{"name": "Deutschland", "default": true}), http.StatusOK, "rename and make the default"), "calendar")
		if moved["name"] != "Deutschland" || moved["default"] != true {
			t.Errorf("moved = %v", moved)
		}
		calendars := list(t, owner.get("/api/v1/holiday-calendars"), "calendars")
		if first := calendars[0].(map[string]any); first["id"] != germany || calendars[1].(map[string]any)["default"] != false {
			t.Errorf("calendars = %v, want the new default first and the old one no longer it", calendars)
		}
		want(t, owner.patch("/api/v1/holiday-calendars/"+germany, map[string]any{"default": false}), http.StatusConflict, "unmake the default")
		want(t, owner.delete("/api/v1/holiday-calendars/"+germany), http.StatusConflict, "delete the default")
		want(t, owner.patch("/api/v1/holiday-calendars/"+standard["id"].(string), map[string]any{"default": true}), http.StatusOK, "hand the default back")
	})

	t.Run("a member reads the calendars and changes none of them", func(t *testing.T) {
		want(t, member.get("/api/v1/holiday-calendars"), http.StatusOK, "a member lists")
		want(t, member.get("/api/v1/holiday-calendars/"+germany), http.StatusOK, "a member reads one")
		want(t, member.post("/api/v1/holiday-calendars", map[string]any{"name": "Mine"}), http.StatusForbidden, "a member makes one")
		want(t, member.patch("/api/v1/holiday-calendars/"+germany, map[string]any{"name": "Mine"}), http.StatusForbidden, "a member renames one")
		want(t, member.put("/api/v1/holiday-calendars/"+germany+"/days", map[string]any{"days": []any{}}), http.StatusForbidden, "a member clears one")
		want(t, member.upload("/api/v1/holiday-calendars/"+germany+"/import", "file", "h.ics", "text/calendar", []byte(holidayFile)), http.StatusForbidden, "a member imports")
		want(t, member.delete("/api/v1/holiday-calendars/"+germany), http.StatusForbidden, "a member deletes one")
	})

	t.Run("a calendar that is not the default is deleted", func(t *testing.T) {
		want(t, owner.delete("/api/v1/holiday-calendars/"+germany), http.StatusNoContent, "delete a calendar")
		want(t, owner.get("/api/v1/holiday-calendars/"+germany), http.StatusNotFound, "read it after")
	})
}

func TestAWorkingWeekIsAPersonsOwnToReadAndAnAdministratorsToSet(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	signedUp := owner.signup(t, h, "weeks")
	ownerID := principalField(t, signedUp, "principal", "user", "id").(string)
	member, memberID := api.localMember(t, h, owner, "member")
	standardID := list(t, owner.get("/api/v1/holiday-calendars"), "calendars")[0].(map[string]any)["id"].(string)
	abroad := idOf(t, want(t, owner.post("/api/v1/holiday-calendars", map[string]any{"name": "Portugal"}), http.StatusCreated, "make a calendar"), "calendar")

	t.Run("nobody has set it, so it is the standard week on the default calendar", func(t *testing.T) {
		week := obj(t, want(t, member.get("/api/v1/users/"+memberID+"/schedule"), http.StatusOK, "read my own week"), "week")
		minutes := week["minutes"].(map[string]any)
		if week["saved"] != false || week["calendarId"] != nil || minutes["mon"] != float64(availability.StandardDayMinutes) || minutes["sun"] != float64(0) {
			t.Errorf("an unset week = %v", week)
		}
		if calendar, _ := week["calendar"].(map[string]any); calendar["id"] != standardID {
			t.Errorf("an unset week keeps %v, want the default", week["calendar"])
		}
	})

	t.Run("somebody else's week is an administrator's to read", func(t *testing.T) {
		refused := want(t, member.get("/api/v1/users/"+ownerID+"/schedule"), http.StatusForbidden, "a member reads the owner's week")
		if message, _ := refused.Error()["message"].(string); !strings.HasSuffix(message, ".") {
			t.Errorf("refused with %q", message)
		}
		want(t, owner.get("/api/v1/users/"+memberID+"/schedule"), http.StatusOK, "the owner reads the member's week")
		want(t, owner.get("/api/v1/users/"+uuid.NewString()+"/schedule"), http.StatusNotFound, "somebody who is not here")
	})

	t.Run("an administrator sets it, and it is held to a week's bounds", func(t *testing.T) {
		saved := obj(t, want(t, owner.put("/api/v1/users/"+memberID+"/schedule", map[string]any{
			"calendarId": abroad, "minutes": map[string]int{"mon": 480, "tue": 480, "wed": 240},
		}), http.StatusOK, "set the member's week"), "week")
		minutes := saved["minutes"].(map[string]any)
		if saved["saved"] != true || saved["calendarId"] != abroad || minutes["wed"] != float64(240) || minutes["thu"] != float64(0) || len(minutes) != len(availability.Weekdays) {
			t.Errorf("saved = %v", saved)
		}
		if mine := obj(t, member.get("/api/v1/users/"+memberID+"/schedule"), "week"); mine["calendar"].(map[string]any)["name"] != "Portugal" {
			t.Errorf("the member reads %v", mine)
		}

		for name, minutes := range map[string]map[string]int{
			"more than a day":   {"mon": availability.MinutesPerDay + 1},
			"less than nothing": {"tue": -30},
			"not a weekday":     {"funday": 60},
		} {
			refused := want(t, owner.put("/api/v1/users/"+memberID+"/schedule", map[string]any{"minutes": minutes}), http.StatusUnprocessableEntity, name)
			if message, _ := refused.Error()["message"].(string); !strings.HasSuffix(message, ".") {
				t.Errorf("%s refused with %q", name, message)
			}
		}
		want(t, owner.put("/api/v1/users/"+memberID+"/schedule", map[string]any{"calendarId": uuid.NewString()}), http.StatusNotFound, "a calendar that is not there")
		want(t, member.put("/api/v1/users/"+memberID+"/schedule", map[string]any{"minutes": map[string]int{"mon": 60}}), http.StatusForbidden, "a member sets their own")
	})

	t.Run("a deleted calendar hands its people back to the default", func(t *testing.T) {
		want(t, owner.delete("/api/v1/holiday-calendars/"+abroad), http.StatusNoContent, "delete the calendar")
		week := obj(t, owner.get("/api/v1/users/"+memberID+"/schedule"), "week")
		if week["calendarId"] != nil || week["calendar"].(map[string]any)["id"] != standardID || week["minutes"].(map[string]any)["wed"] != float64(240) {
			t.Errorf("after the calendar went: %v", week)
		}
	})

	t.Run("a portal customer has no working week", func(t *testing.T) {
		invited := want(t, owner.post("/api/v1/invites", map[string]any{"email": h.email(t, "weekcustomer"), "role": "customer"}), http.StatusCreated, "invite a customer")
		customer := api.client(t)
		accepted := want(t, customer.post("/api/v1/auth/invites/accept", map[string]any{"token": invited.Body["token"], "name": "Sam Customer", "password": testPassword}), http.StatusOK, "the customer joins")
		customerID := principalField(t, accepted, "principal", "user", "id").(string)
		h.waitForPrimary(t)
		refused := want(t, owner.put("/api/v1/users/"+customerID+"/schedule", map[string]any{"minutes": map[string]int{"mon": 60}}), http.StatusUnprocessableEntity, "a week for a customer")
		if message, _ := refused.Error()["message"].(string); !strings.Contains(message, "customer") {
			t.Errorf("refused with %q", message)
		}
	})

	t.Run("the person takes their week with their data, and it goes when they do", func(t *testing.T) {
		exported := want(t, member.get("/api/v1/auth/me/export"), http.StatusOK, "export my data")
		if hours := list(t, exported, "workingHours"); len(hours) != 1 {
			t.Errorf("the export holds %v, want the one week", hours)
		}
		want(t, owner.delete("/api/v1/members/"+memberID), http.StatusNoContent, "let the member go")
		var left int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM member_schedule WHERE user_id = $1`, memberID).Scan(&left); err != nil {
			t.Fatal(err)
		}
		if left != 0 {
			t.Errorf("%d weeks stayed behind after the member left", left)
		}
	})
}

// The service holding calendars and weeks to their organization is not the
// proof; the same things are tried straight through SQL.
func TestCalendarsAndWeeksHoldEvenThroughSQL(t *testing.T) {
	h := newHarness(t)
	home := h.newWorkspace(t, "calhome")
	away := h.newWorkspace(t, "calaway")
	calendars := availability.NewService(h.cluster)
	ctx := context.Background()

	defaultOf := func(t *testing.T, ws *workspace) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := h.super.QueryRow(ctx, `SELECT id FROM holiday_calendar WHERE org_id = $1 AND is_default`, ws.orgID).Scan(&id); err != nil {
			t.Fatalf("the default calendar: %v", err)
		}
		return id
	}
	homeDefault, awayDefault := defaultOf(t, home), defaultOf(t, away)
	if _, _, err := calendars.ReplaceDays(home.ctx, homeDefault, []availability.Holiday{{Day: "2026-12-25", Name: "Christmas Day"}}, home.actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := calendars.SaveWorkingWeek(home.ctx, home.actor.UserID, availability.WeekInput{}, home.actor.UserID); err != nil {
		t.Fatal(err)
	}

	refusedAs := func(t *testing.T, err error, code string) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != code {
			t.Errorf("got %v, want SQLSTATE %s", err, code)
		}
	}

	t.Run("another organization reads none of it", func(t *testing.T) {
		for _, query := range []string{
			`SELECT count(*) FROM holiday_calendar WHERE org_id = $1`,
			`SELECT count(*) FROM holiday WHERE org_id = $1`,
			`SELECT count(*) FROM member_schedule WHERE org_id = $1`,
		} {
			var seen int
			err := h.cluster.ReadPrimary(away.ctx, func(ctx context.Context, tx db.DBTX) error {
				return tx.QueryRow(ctx, query, home.orgID).Scan(&seen)
			})
			if err != nil || seen != 0 {
				t.Errorf("%s: another organization sees %d rows (%v)", query, seen, err)
			}
		}
		if _, err := calendars.Calendar(away.ctx, homeDefault); !errors.Is(err, availability.ErrNotFound) {
			t.Errorf("another organization reading the calendar: %v", err)
		}
	})

	t.Run("a row planted in another organization is refused", func(t *testing.T) {
		_, err := h.cluster.Write(away.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `INSERT INTO holiday (org_id, calendar_id, day, name) VALUES ($1, $2, '2026-01-01', 'Planted')`, home.orgID, homeDefault)
			return err
		})
		if err == nil {
			t.Error("a holiday went into another organization")
		}
		_, err = h.super.Exec(ctx, `INSERT INTO holiday (org_id, calendar_id, day, name) VALUES ($1, $2, '2026-01-01', 'Crossed')`, away.orgID, homeDefault)
		refusedAs(t, err, "23514")
		_, err = h.super.Exec(ctx, `UPDATE member_schedule SET calendar_id = $2 WHERE org_id = $1`, home.orgID, awayDefault)
		refusedAs(t, err, "23514")
	})

	t.Run("only somebody who works here has a week", func(t *testing.T) {
		var customerID uuid.UUID
		err := h.super.QueryRow(ctx, `
			WITH made AS (INSERT INTO app_user (email, name) VALUES ($1, 'Sam Customer') RETURNING id)
			INSERT INTO org_member (org_id, user_id, org_role) SELECT $2, id, 'customer' FROM made RETURNING user_id`,
			h.email(t, "sqlcustomer"), home.orgID).Scan(&customerID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.super.Exec(ctx, `INSERT INTO member_schedule (org_id, user_id) VALUES ($1, $2)`, home.orgID, customerID)
		refusedAs(t, err, "23514")
		if err != nil && !strings.Contains(err.Error(), "only a member") {
			t.Errorf("error = %v, want the guard's own words", err)
		}
		_, err = h.super.Exec(ctx, `INSERT INTO member_schedule (org_id, user_id) VALUES ($1, $2)`, home.orgID, away.actor.UserID)
		refusedAs(t, err, "23514")
	})

	t.Run("a week the service would refuse is refused by the database", func(t *testing.T) {
		for name, minutes := range map[string]string{
			"more than a day":    `{"mon": 1441}`,
			"less than nothing":  `{"tue": -1}`,
			"not a weekday":      `{"funday": 60}`,
			"part of a minute":   `{"wed": 60.5}`,
			"words for a number": `{"thu": "eight hours"}`,
			"a list, not a week": `[480, 480]`,
		} {
			_, err := h.super.Exec(ctx, `UPDATE member_schedule SET minutes = $2::jsonb WHERE org_id = $1`, home.orgID, minutes)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Errorf("%s: got %v, want a check violation", name, err)
			}
		}
	})

	t.Run("an organization has one default and its names once", func(t *testing.T) {
		if _, _, err := calendars.CreateCalendar(home.ctx, availability.CalendarInput{Name: ptr("Second")}, home.actor.UserID); err != nil {
			t.Fatal(err)
		}
		_, err := h.super.Exec(ctx, `UPDATE holiday_calendar SET is_default = true WHERE org_id = $1`, home.orgID)
		refusedAs(t, err, "23505")
		_, err = h.super.Exec(ctx, `INSERT INTO holiday_calendar (org_id, name) VALUES ($1, 'SECOND')`, home.orgID)
		refusedAs(t, err, "23505")
		_, err = h.super.Exec(ctx, `INSERT INTO holiday_calendar (org_id, name) VALUES ($1, '   ')`, home.orgID)
		refusedAs(t, err, "23514")
		_, err = h.super.Exec(ctx, `INSERT INTO holiday (org_id, calendar_id, day, name) VALUES ($1, $2, '2026-12-25', 'Twice')`, home.orgID, homeDefault)
		refusedAs(t, err, "23505")
	})
}
