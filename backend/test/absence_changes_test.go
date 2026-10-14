//go:build integration

package test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/notify"
)

// absenceCrew is a project where Max is scrum master of the team Ada is
// on: the people a manager's reach over absences is tried with.
type absenceCrew struct {
	api                *apiServer
	owner, ada, master *client
	ownerID, ownerName string
	adaID, masterID    string
	crew               string
}

func newAbsenceCrew(t *testing.T, h *harness, slug string) *absenceCrew {
	t.Helper()
	api := newAPIServer(t, h)
	c := &absenceCrew{api: api, owner: api.client(t), ownerName: slug}
	signedUp := c.owner.signIn(t, h.person(t, slug, auth.RoleOwner))
	c.ownerID = principalField(t, signedUp, "principal", "user", "id").(string)
	key := h.projectKey(t, "AWY")
	want(t, c.owner.post("/api/v1/projects", map[string]any{"name": "Away", "key": key, "template": "scrum"}), http.StatusCreated, "a project")
	c.ada, c.adaID = api.namedMember(t, h, c.owner, "Ada Away")
	c.master, c.masterID = api.namedMember(t, h, c.owner, "Max Master")
	want(t, c.owner.post("/api/v1/role-assignments", map[string]any{"role": "scrum_master", "projectKey": key, "userId": c.masterID}), http.StatusCreated, "Max is scrum master")
	c.crew = idOf(t, want(t, c.owner.post("/api/v1/projects/"+key+"/teams", map[string]any{"name": "Crew"}), http.StatusCreated, "a team"), "team")
	want(t, c.owner.post("/api/v1/teams/"+c.crew+"/members", map[string]any{"userId": c.adaID}), http.StatusOK, "Ada joins the team")
	return c
}

// record writes an absence down and returns its id.
func (c *absenceCrew) record(t *testing.T, by *client, userID, startsOn, endsOn, what string) string {
	t.Helper()
	return idOf(t, want(t, by.post("/api/v1/absences", map[string]any{"userId": userID, "startsOn": startsOn, "endsOn": endsOn}), http.StatusCreated, what), "absence")
}

// daysOf reads an absence's days straight from the database.
func daysOf(t *testing.T, h *harness, id string) string {
	t.Helper()
	var days string
	err := h.super.QueryRow(context.Background(), `SELECT to_char(starts_on, 'YYYY-MM-DD') || ' ' || to_char(ends_on, 'YYYY-MM-DD') FROM absence WHERE id = $1`, id).Scan(&days)
	if err != nil {
		t.Fatalf("read absence %s: %v", id, err)
	}
	return days
}

// A manager may add days for somebody on their team but never rewrite what the
// person or an administrator wrote down, even after putting them on the team.
func TestAManagerChangesOnlyTheAbsencesTheyRecorded(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	c := newAbsenceCrew(t, h, "absencereach")

	adasOwn := c.record(t, c.ada, c.adaID, "2026-08-03", "2026-08-14", "Ada records her own")
	byTheAdmin := c.record(t, c.owner, c.adaID, "2026-09-07", "2026-09-08", "an administrator records one for Ada")
	byTheMaster := c.record(t, c.master, c.adaID, "2026-10-19", "2026-10-20", "Max records one for Ada")

	t.Run("the scrum master cannot change or remove what Ada or an administrator recorded", func(t *testing.T) {
		for name, id := range map[string]string{"Ada's own": adasOwn, "the administrator's": byTheAdmin} {
			before := daysOf(t, h, id)
			refused := want(t, c.master.patch("/api/v1/absences/"+id, map[string]any{"endsOn": "2026-10-30"}), http.StatusForbidden, "Max moves "+name)
			refusedWithASentence(t, refused, "Only Ada Away or an administrator can change this absence")
			refusedWithASentence(t, want(t, c.master.delete("/api/v1/absences/"+id), http.StatusForbidden, "Max removes "+name), "Only Ada Away or an administrator")
			if after := daysOf(t, h, id); after != before {
				t.Errorf("%s reads %s after the refusals, want %s", name, after, before)
			}
		}
	})

	t.Run("the scrum master still changes and removes one they recorded", func(t *testing.T) {
		moved := obj(t, want(t, c.master.patch("/api/v1/absences/"+byTheMaster, map[string]any{"startsOn": "2026-10-21", "endsOn": "2026-10-22"}), http.StatusOK, "Max moves his"), "absence")
		if moved["startsOn"] != "2026-10-21" || moved["endsOn"] != "2026-10-22" {
			t.Errorf("moved = %v", moved)
		}
		want(t, c.master.delete("/api/v1/absences/"+byTheMaster), http.StatusNoContent, "Max removes his")
	})

	t.Run("the person and an administrator keep full control, whoever recorded it", func(t *testing.T) {
		again := c.record(t, c.master, c.adaID, "2026-11-02", "2026-11-03", "Max records another for Ada")
		want(t, c.ada.patch("/api/v1/absences/"+again, map[string]any{"endsOn": "2026-11-04"}), http.StatusOK, "Ada moves Max's")
		want(t, c.ada.patch("/api/v1/absences/"+byTheAdmin, map[string]any{"endsOn": "2026-09-09"}), http.StatusOK, "Ada moves the administrator's")
		want(t, c.ada.delete("/api/v1/absences/"+again), http.StatusNoContent, "Ada removes Max's")
		yetAgain := c.record(t, c.master, c.adaID, "2026-11-16", "2026-11-16", "Max records a third for Ada")
		want(t, c.owner.patch("/api/v1/absences/"+yetAgain, map[string]any{"halfDay": true}), http.StatusOK, "the administrator changes Max's")
		want(t, c.owner.patch("/api/v1/absences/"+adasOwn, map[string]any{"endsOn": "2026-08-13"}), http.StatusOK, "the administrator changes Ada's own")
		want(t, c.owner.delete("/api/v1/absences/"+yetAgain), http.StatusNoContent, "the administrator removes Max's")
	})

	t.Run("adding an administrator to his team lets the scrum master add days, never rewrite theirs", func(t *testing.T) {
		theirs := c.record(t, c.owner, c.ownerID, "2026-12-21", "2026-12-23", "the administrator records their own")
		want(t, c.master.post("/api/v1/teams/"+c.crew+"/members", map[string]any{"userId": c.ownerID}), http.StatusOK, "Max puts the administrator on his team")
		refusedWithASentence(t, want(t, c.master.patch("/api/v1/absences/"+theirs, map[string]any{"endsOn": "2026-12-31"}), http.StatusForbidden, "Max moves the administrator's"), "Only "+c.ownerName+" or an administrator")
		want(t, c.master.delete("/api/v1/absences/"+theirs), http.StatusForbidden, "Max removes the administrator's")
		if days := daysOf(t, h, theirs); days != "2026-12-21 2026-12-23" {
			t.Errorf("the administrator's absence reads %s", days)
		}
		want(t, c.master.post("/api/v1/absences", map[string]any{"userId": c.ownerID, "startsOn": "2027-01-04"}), http.StatusCreated, "Max records a day for the administrator")
	})
}

// absenceNotice is the kind of notification that tells a person about their days.
const absenceNotice = "absence"

// fanOutAbsences hands every absence event about one person to the inbox's
// fan-out, as the worker would, each twice to show a repeat tells nobody twice.
func fanOutAbsences(t *testing.T, h *harness, fan *notify.FanOut, userID string) int {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT id, topic, payload FROM outbox_event
		WHERE topic LIKE 'absence.%' AND payload->>'userId' = $1
		ORDER BY created_at, id`, userID)
	if err != nil {
		t.Fatal(err)
	}
	var found []events.Event
	for rows.Next() {
		e := events.Event{}
		if err := rows.Scan(&e.ID, &e.Topic, &e.Payload); err != nil {
			t.Fatal(err)
		}
		found = append(found, e)
	}
	rows.Close()
	for _, e := range found {
		for range 2 {
			if err := fan.Handle(context.Background(), e); err != nil {
				t.Fatalf("fan out %s: %v", e.Topic, err)
			}
		}
	}
	return len(found)
}

// The person hears, in the inbox and by mail, each time somebody else touches
// their days; never which days, and never about what they did themselves.
func TestThePersonIsToldWhenSomebodyElseTouchesTheirAbsences(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	c := newAbsenceCrew(t, h, "absencenotice")
	mailer := &fakeMailer{}
	fan := notify.NewFanOut(h.cluster, nil, mailer, "http://app.test", slog.New(slog.NewTextHandler(os.Stderr, nil)))

	c.record(t, c.ada, c.adaID, "2026-08-03", "2026-08-14", "Ada records her own")
	byTheMaster := c.record(t, c.master, c.adaID, "2026-10-19", "2026-10-20", "Max records one for Ada")
	want(t, c.master.patch("/api/v1/absences/"+byTheMaster, map[string]any{"endsOn": "2026-10-21"}), http.StatusOK, "Max moves it")
	want(t, c.master.delete("/api/v1/absences/"+byTheMaster), http.StatusNoContent, "Max removes it")
	c.record(t, c.owner, c.adaID, "2026-11-02", "2026-11-02", "an administrator records one for Ada")

	if n := fanOutAbsences(t, h, fan, c.adaID); n != 5 {
		t.Fatalf("%d absence events about Ada, want five", n)
	}
	h.waitForPrimary(t)
	inbox := list(t, want(t, c.ada.get("/api/v1/notifications"), http.StatusOK, "Ada reads her inbox"), "notifications")
	var titles []string
	for _, item := range inbox {
		n := item.(map[string]any)
		title, _ := n["title"].(string)
		body, _ := n["body"].(string)
		titles = append(titles, title)
		if n["kind"] != absenceNotice || !strings.HasSuffix(n["link"].(string), "/settings/profile") {
			t.Errorf("notice %v, want an absence notice that opens her profile", n)
		}
		if strings.ContainsAny(title+body, "0123456789") {
			t.Errorf("notice %q / %q reads like a day; the notice never carries the days", title, body)
		}
	}
	wantTitles := []string{
		c.ownerName + " recorded an absence for you",
		"Max Master removed one of your absences",
		"Max Master changed one of your absences",
		"Max Master recorded an absence for you",
	}
	if strings.Join(titles, "|") != strings.Join(wantTitles, "|") {
		t.Errorf("Ada's inbox reads %q, want %q, newest first and nothing about her own", titles, wantTitles)
	}

	var adas []struct{ To, Subject, Body string }
	for _, m := range mailer.sent {
		if strings.HasPrefix(m.To, "ada") {
			adas = append(adas, m)
		}
	}
	if len(adas) != len(wantTitles) {
		t.Fatalf("Ada was mailed %+v, want one mail per notice", adas)
	}
	for _, m := range adas {
		if strings.Contains(m.Subject+m.Body, "2026") || !strings.Contains(m.Body, "http://app.test/settings/profile") {
			t.Errorf("mail %+v, want it to point at her profile without the days", m)
		}
	}

	t.Run("mail is hers to turn off, and the inbox still tells her", func(t *testing.T) {
		want(t, c.ada.put("/api/v1/notification-preferences", map[string]any{"digest": "off", "watchOwn": true, "mail": map[string]bool{absenceNotice: false}, "inapp": map[string]bool{}}), http.StatusOK, "Ada turns absence mail off")
		before := len(mailer.sent)
		c.record(t, c.master, c.adaID, "2026-12-07", "2026-12-07", "Max records another for Ada")
		fanOutAbsences(t, h, fan, c.adaID)
		if len(mailer.sent) != before {
			t.Errorf("mailed %+v with absence mail off", mailer.sent[before:])
		}
		h.waitForPrimary(t)
		if got := list(t, c.ada.get("/api/v1/notifications"), "notifications"); len(got) != len(wantTitles)+1 {
			t.Errorf("Ada's inbox holds %d notices, want the new one too", len(got))
		}
	})
}

// The rule rests on who recorded an absence, so the database keeps that
// honest whatever the service does: it is never rewritten to somebody else.
func TestWhoRecordedAnAbsenceHoldsThroughSQL(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "absencerecorder")
	ctx := context.Background()
	recorder := h.joinExisting(t, h.email(t, "absencerecorder"), "member")
	other := h.joinExisting(t, h.email(t, "absencenotrecorder"), "member")
	var id uuid.UUID
	err := h.super.QueryRow(ctx, `
		INSERT INTO absence (user_id, starts_on, ends_on, created_by)
		VALUES ($1, '2026-08-03', '2026-08-14', $2) RETURNING id`, ws.actor.UserID, recorder).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	refusedAs := func(t *testing.T, err error, code string) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != code {
			t.Errorf("got %v, want SQLSTATE %s", err, code)
		}
	}

	t.Run("who recorded it is never rewritten", func(t *testing.T) {
		_, err := h.super.Exec(ctx, `UPDATE absence SET created_by = $2 WHERE id = $1`, id, other)
		refusedAs(t, err, "23514")
		_, err = h.super.Exec(ctx, `UPDATE absence SET created_by = $2 WHERE id = $1`, id, ws.actor.UserID)
		refusedAs(t, err, "23514")
		if _, err := h.super.Exec(ctx, `UPDATE absence SET ends_on = '2026-08-13' WHERE id = $1`, id); err != nil {
			t.Errorf("moving the days was refused: %v", err)
		}
	})

	t.Run("a recorder who is gone leaves it recorded by nobody", func(t *testing.T) {
		if _, err := h.super.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, recorder); err != nil {
			t.Fatal(err)
		}
		var by *uuid.UUID
		if err := h.super.QueryRow(ctx, `SELECT created_by FROM absence WHERE id = $1`, id).Scan(&by); err != nil {
			t.Fatal(err)
		}
		if by != nil {
			t.Errorf("recorded by %v after the recorder went, want nobody", *by)
		}
		_, err := h.super.Exec(ctx, `UPDATE absence SET created_by = $2 WHERE id = $1`, id, other)
		refusedAs(t, err, "23514")
	})

	t.Run("an absence notice is a kind the inbox knows, and nothing else gets in", func(t *testing.T) {
		insert := `INSERT INTO notification (user_id, kind, title, event_id) VALUES ($1, $2, 'x', gen_random_uuid())`
		if _, err := h.super.Exec(ctx, insert, ws.actor.UserID, absenceNotice); err != nil {
			t.Errorf("an absence notice was refused: %v", err)
		}
		_, err := h.super.Exec(ctx, insert, ws.actor.UserID, "shouting")
		refusedAs(t, err, "23514")
	})
}
