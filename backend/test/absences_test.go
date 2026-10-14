//go:build integration

package test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/availability"
	"github.com/armature/armature/backend/internal/privacy"
)

// namedMember makes a password account in the owner's organization under a
// name of its own and signs it in.
func (s *apiServer) namedMember(t *testing.T, h *harness, owner *client, name string) (*client, string) {
	t.Helper()
	email := h.email(t, strings.ToLower(strings.Fields(name)[0]))
	made := want(t, owner.post("/api/v1/users", map[string]string{
		"email": email, "name": name, "role": "member", "password": testPassword,
	}), http.StatusCreated, "make "+name)
	id := obj(t, made, "user")["id"].(string)
	h.erasable(id)
	them := s.client(t)
	want(t, them.post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusOK, "sign "+name+" in")
	return them, id
}

// refusedWithASentence checks a refusal reads as one, naming what it is about.
func refusedWithASentence(t *testing.T, got response, about string) {
	t.Helper()
	message, _ := got.Error()["message"].(string)
	if !strings.HasSuffix(message, ".") || !strings.Contains(message, about) {
		t.Errorf("refused with %q, want a sentence about %q", message, about)
	}
}

func TestAnAbsenceIsWrittenByThePersonAndByThoseWhoPlanWithThem(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	awyKey := h.projectKey(t, "AWY")
	elwKey := h.projectKey(t, "ELW")
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "absences", auth.RoleOwner))
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Away", "key": awyKey, "template": "scrum"}), http.StatusCreated, "a project")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Elsewhere", "key": elwKey, "template": "scrum"}), http.StatusCreated, "another project")

	ada, adaID := api.namedMember(t, h, owner, "Ada Away")
	master, masterID := api.namedMember(t, h, owner, "Max Master")
	stranger, strangerID := api.namedMember(t, h, owner, "Stan Stranger")
	plain, plainID := api.namedMember(t, h, owner, "Pat Plain")

	// Max is scrum master where Ada is on a team; Stan is scrum master where she is not.
	want(t, owner.post("/api/v1/role-assignments", map[string]any{"role": "scrum_master", "projectKey": awyKey, "userId": masterID}), http.StatusCreated, "a scrum master of Ada's project")
	want(t, owner.post("/api/v1/role-assignments", map[string]any{"role": "scrum_master", "projectKey": elwKey, "userId": strangerID}), http.StatusCreated, "a scrum master elsewhere")
	crew := idOf(t, want(t, owner.post("/api/v1/projects/"+awyKey+"/teams", map[string]any{"name": "Crew"}), http.StatusCreated, "a team"), "team")
	want(t, owner.post("/api/v1/teams/"+crew+"/members", map[string]any{"userId": adaID}), http.StatusOK, "Ada joins the team")
	other := idOf(t, want(t, owner.post("/api/v1/projects/"+elwKey+"/teams", map[string]any{"name": "Others"}), http.StatusCreated, "another team"), "team")
	want(t, owner.post("/api/v1/teams/"+other+"/members", map[string]any{"userId": plainID}), http.StatusOK, "Pat joins the other team")

	var mine string
	t.Run("a person records their own absence and every colleague sees only the days", func(t *testing.T) {
		made := obj(t, want(t, ada.post("/api/v1/absences", map[string]any{"startsOn": "2026-08-03", "endsOn": "2026-08-14"}), http.StatusCreated, "Ada records a fortnight"), "absence")
		mine = made["id"].(string)
		if made["userId"] != adaID || made["userName"] != "Ada Away" || made["startsOn"] != "2026-08-03" || made["endsOn"] != "2026-08-14" || made["halfDay"] != false {
			t.Errorf("made = %v", made)
		}
		half := obj(t, want(t, ada.post("/api/v1/absences", map[string]any{"startsOn": "2026-12-24", "halfDay": true}), http.StatusCreated, "Ada records half a day"), "absence")
		if half["endsOn"] != "2026-12-24" || half["halfDay"] != true {
			t.Errorf("a day left without an end = %v, want one half day", half)
		}

		// Everybody in the installation is listed; Ada's and Pat's are this test's.
		ours := func(listed []any) []any {
			kept := []any{}
			for _, a := range listed {
				if who := a.(map[string]any)["userId"]; who == adaID || who == plainID {
					kept = append(kept, a)
				}
			}
			return kept
		}
		seen := want(t, plain.get("/api/v1/absences?from=2026-08-01&to=2026-12-31"), http.StatusOK, "a colleague lists absences")
		absences := ours(list(t, seen, "absences"))
		if len(absences) != 2 || seen.Body["from"] != "2026-08-01" || seen.Body["to"] != "2026-12-31" {
			t.Fatalf("a colleague sees %v", seen.Body)
		}
		keys := []string{}
		for key := range absences[0].(map[string]any) {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		if strings.Join(keys, ",") != "endsOn,halfDay,id,startsOn,userId,userName" {
			t.Errorf("a colleague reads %v of an absence, want the days and the person only", keys)
		}
		if outside := ours(list(t, plain.get("/api/v1/absences?from=2026-09-01&to=2026-12-23"), "absences")); len(outside) != 0 {
			t.Errorf("a stretch with nobody away lists %v", outside)
		}
		if edge := ours(list(t, plain.get("/api/v1/absences?from=2026-08-14&to=2026-08-14"), "absences")); len(edge) != 1 {
			t.Errorf("the last day away lists %v, want it counted", edge)
		}
		if hers := list(t, owner.get("/api/v1/absences?from=2026-01-01&to=2026-12-31&userId="+adaID), "absences"); len(hers) != 2 {
			t.Errorf("one person's absences = %v", hers)
		}
		if theirs := list(t, owner.get("/api/v1/absences?from=2026-01-01&to=2026-12-31&userId="+plainID), "absences"); len(theirs) != 0 {
			t.Errorf("somebody never away = %v", theirs)
		}
	})

	t.Run("the days are held to their bounds, and one day is away once", func(t *testing.T) {
		overlap := want(t, ada.post("/api/v1/absences", map[string]any{"startsOn": "2026-08-10", "endsOn": "2026-08-20"}), http.StatusConflict, "an absence over one already there")
		refusedWithASentence(t, overlap, "already away")
		for name, body := range map[string]map[string]any{
			"a half day over two days": {"startsOn": "2026-11-02", "endsOn": "2026-11-03", "halfDay": true},
			"an end before the start":  {"startsOn": "2026-11-03", "endsOn": "2026-11-02"},
			"longer than a year":       {"startsOn": "2026-01-01", "endsOn": "2027-06-01"},
			"a day that is not one":    {"startsOn": "3 November"},
			"no first day":             {"endsOn": "2026-11-03"},
		} {
			refusedWithASentence(t, want(t, ada.post("/api/v1/absences", body), http.StatusUnprocessableEntity, name), "")
		}
		refusedWithASentence(t, want(t, ada.get("/api/v1/absences?from=2026-01-01&to=2029-01-01"), http.StatusUnprocessableEntity, "a window too wide"), "shorter")
		want(t, ada.get("/api/v1/absences?userId=somebody"), http.StatusBadRequest, "a person who is not an id")
		want(t, api.client(t).get("/api/v1/absences"), http.StatusUnauthorized, "nobody signed in")
	})

	t.Run("the scrum master of the person's team records, moves and removes one for them", func(t *testing.T) {
		made := obj(t, want(t, master.post("/api/v1/absences", map[string]any{"userId": adaID, "startsOn": "2026-10-19", "endsOn": "2026-10-20"}), http.StatusCreated, "the scrum master records one"), "absence")
		id := made["id"].(string)
		if made["userId"] != adaID {
			t.Errorf("made = %v, want Ada's", made)
		}
		moved := obj(t, want(t, master.patch("/api/v1/absences/"+id, map[string]any{"startsOn": "2026-10-21", "endsOn": "2026-10-21", "halfDay": true}), http.StatusOK, "the scrum master moves it"), "absence")
		if moved["startsOn"] != "2026-10-21" || moved["endsOn"] != "2026-10-21" || moved["halfDay"] != true {
			t.Errorf("moved = %v", moved)
		}
		want(t, master.patch("/api/v1/absences/"+id, map[string]any{"endsOn": "2026-10-22"}), http.StatusUnprocessableEntity, "a half day stretched over two days")
		want(t, master.patch("/api/v1/absences/"+id, map[string]any{"startsOn": "2026-08-05", "endsOn": "2026-08-05", "halfDay": false}), http.StatusConflict, "moved onto days already away")
		want(t, master.patch("/api/v1/absences/"+id, map[string]any{"userId": plainID}), http.StatusBadRequest, "handed to somebody else")
		want(t, master.delete("/api/v1/absences/"+id), http.StatusNoContent, "the scrum master removes it")
		want(t, master.delete("/api/v1/absences/"+id), http.StatusNotFound, "and it is gone")
		want(t, master.patch("/api/v1/absences/"+uuid.NewString(), map[string]any{"halfDay": false}), http.StatusNotFound, "an absence that is not there")
		want(t, master.patch("/api/v1/absences/not-an-id", map[string]any{"halfDay": false}), http.StatusBadRequest, "an id that is not one")
	})

	t.Run("anybody else is refused, a scrum master of another team included", func(t *testing.T) {
		refusedWithASentence(t, want(t, stranger.post("/api/v1/absences", map[string]any{"userId": adaID, "startsOn": "2026-11-02"}), http.StatusForbidden, "a scrum master of a team Ada is not on"), "Ask one of them")
		want(t, stranger.post("/api/v1/absences", map[string]any{"userId": plainID, "startsOn": "2026-11-02"}), http.StatusCreated, "the same scrum master for a member of their team")
		want(t, plain.post("/api/v1/absences", map[string]any{"userId": adaID, "startsOn": "2026-11-02"}), http.StatusForbidden, "a colleague records for Ada")
		want(t, plain.patch("/api/v1/absences/"+mine, map[string]any{"endsOn": "2026-08-31"}), http.StatusForbidden, "a colleague moves Ada's")
		want(t, plain.delete("/api/v1/absences/"+mine), http.StatusForbidden, "a colleague removes Ada's")
		want(t, master.post("/api/v1/absences", map[string]any{"userId": plainID, "startsOn": "2026-11-09"}), http.StatusForbidden, "a scrum master for somebody on no team of theirs")
		if hers := list(t, owner.get("/api/v1/absences?from=2026-08-01&to=2026-08-31&userId="+adaID), "absences"); len(hers) != 1 || hers[0].(map[string]any)["endsOn"] != "2026-08-14" {
			t.Errorf("after the refusals Ada's absence is %v", hers)
		}
	})

	t.Run("an administrator records for anybody who works here, and for nobody else", func(t *testing.T) {
		want(t, owner.post("/api/v1/absences", map[string]any{"userId": strangerID, "startsOn": "2026-11-16", "endsOn": "2026-11-20"}), http.StatusCreated, "the owner records for Stan")
		want(t, owner.patch("/api/v1/absences/"+mine, map[string]any{"endsOn": "2026-08-13"}), http.StatusOK, "the owner shortens Ada's")
		want(t, owner.post("/api/v1/absences", map[string]any{"userId": uuid.NewString(), "startsOn": "2026-11-16"}), http.StatusNotFound, "somebody who is not here")

		invited := want(t, owner.post("/api/v1/invites", map[string]any{"email": h.email(t, "awaycustomer"), "role": "customer"}), http.StatusCreated, "invite a customer")
		customer := api.client(t)
		accepted := want(t, customer.post("/api/v1/auth/invites/accept", map[string]any{"token": invited.Body["token"], "name": "Sam Customer", "password": testPassword}), http.StatusOK, "the customer joins")
		customerID := principalField(t, accepted, "principal", "user", "id").(string)
		h.waitForPrimary(t)
		refusedWithASentence(t, want(t, owner.post("/api/v1/absences", map[string]any{"userId": customerID, "startsOn": "2026-11-16"}), http.StatusUnprocessableEntity, "an absence for a customer"), "customer")
		want(t, customer.get("/api/v1/absences"), http.StatusForbidden, "a customer reads absences")
		want(t, customer.post("/api/v1/absences", map[string]any{"startsOn": "2026-11-16"}), http.StatusForbidden, "a customer records one")
	})

	t.Run("the person takes their absences with their data, and they go when the person is erased", func(t *testing.T) {
		exported := want(t, ada.get("/api/v1/auth/me/export"), http.StatusOK, "Ada exports her data")
		absences := list(t, exported, "absences")
		if len(absences) != 2 || absences[0].(map[string]any)["recordedBy"] != "Ada Away" {
			t.Errorf("the export holds %v, want both of Ada's with who recorded them", absences)
		}
		count := func(userID string) int {
			var n int
			if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM absence WHERE user_id = $1`, userID).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		want(t, stranger.delete("/api/v1/auth/me"), http.StatusNoContent, "Stan erases his account")
		if left := count(strangerID); left != 0 {
			t.Errorf("%d of Stan's absences stayed after he was erased", left)
		}
		if kept := count(plainID); kept != 1 {
			t.Errorf("Pat has %d absences, want the one Stan recorded for him to stay", kept)
		}
	})
}

// An editor lists from a month back. An absence saved further ahead than a
// year has to be in that list, or nobody can see, change or remove it.
func TestAnAbsenceListFromADayRunsOnAndSaysWhenItIsCut(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "absencelist", auth.RoleOwner))
	ada, adaID := api.namedMember(t, h, owner, "Ada Ahead")

	today := time.Now().UTC()
	monthBack := today.AddDate(0, 0, -availability.AbsenceDaysBehind).Format(availability.DateLayout)
	farAhead := today.AddDate(0, 14, 0).Format(availability.DateLayout)
	want(t, ada.post("/api/v1/absences", map[string]any{"startsOn": farAhead}), http.StatusCreated, "Ada records a day fourteen months ahead")
	editorList := "/api/v1/absences?userId=" + adaID + "&from=" + monthBack
	startsOf := func(listed []any) []string {
		days := []string{}
		for _, a := range listed {
			days = append(days, a.(map[string]any)["startsOn"].(string))
		}
		return days
	}

	t.Run("a list from a month back holds a day fourteen months ahead", func(t *testing.T) {
		seen := want(t, ada.get(editorList), http.StatusOK, "Ada lists her absences from a month back")
		if days := startsOf(list(t, seen, "absences")); !slices.Equal(days, []string{farAhead}) {
			t.Errorf("listed %v, want the day fourteen months ahead", days)
		}
		if seen.Body["from"] != monthBack || seen.Body["to"] != nil || seen.Body["truncated"] != false {
			t.Errorf("the list says from %v to %v, truncated %v; want no last day and nothing cut", seen.Body["from"], seen.Body["to"], seen.Body["truncated"])
		}
		bounded := want(t, ada.get(editorList+"&to="+today.Format(availability.DateLayout)), http.StatusOK, "a list with a last day")
		if len(list(t, bounded, "absences")) != 0 || bounded.Body["to"] != today.Format(availability.DateLayout) || bounded.Body["truncated"] != false {
			t.Errorf("a list up to today = %v", bounded.Body)
		}
	})

	t.Run("a list with no last day stops at its bound and says it was cut", func(t *testing.T) {
		// One more absence than a list holds, each a single day, the far one among them.
		if _, err := h.super.Exec(context.Background(), `
			INSERT INTO absence (user_id, starts_on, ends_on)
			SELECT m.user_id, d::date, d::date
			FROM org_member m, generate_series(current_date + 1, current_date + $2::int + 1, interval '1 day') d
			WHERE m.user_id = $1 AND d::date <> $3::date
			LIMIT $2`, adaID, availability.MaxOpenAbsences, farAhead); err != nil {
			t.Fatal(err)
		}
		// Written on the primary; Ada's reads may come from the replica.
		h.waitForPrimary(t)
		lastDay := today.AddDate(0, 0, availability.MaxAbsenceWindowDays-availability.AbsenceDaysBehind-1).Format(availability.DateLayout)
		full := want(t, ada.get(editorList+"&to="+lastDay), http.StatusOK, "every one of them, with a last day")
		every := startsOf(list(t, full, "absences"))
		if len(every) != availability.MaxOpenAbsences+1 || full.Body["truncated"] != false {
			t.Fatalf("a list with a last day = %d absences, truncated %v; want all %d", len(every), full.Body["truncated"], availability.MaxOpenAbsences+1)
		}
		seen := want(t, ada.get(editorList), http.StatusOK, "Ada lists more absences than one list holds")
		days := startsOf(list(t, seen, "absences"))
		if !slices.Equal(days, every[:availability.MaxOpenAbsences]) || seen.Body["truncated"] != true {
			t.Errorf("listed %d, truncated %v; want the earliest %d and the list to say it was cut", len(days), seen.Body["truncated"], availability.MaxOpenAbsences)
		}
	})
}

func TestAbsencesPastTheirWindowAreSwept(t *testing.T) {
	h := newHarness(t)
	ws := h.newProject(t, "absenceretention")
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO absence (user_id, starts_on, ends_on) VALUES ($1, current_date - 500, current_date - 450)`,
		`INSERT INTO absence (user_id, starts_on, ends_on) VALUES ($1, current_date - 30, current_date - 20)`,
		`INSERT INTO absence (user_id, starts_on, ends_on) VALUES ($1, current_date + 20, current_date + 30)`,
	} {
		if _, err := h.super.Exec(ctx, q, ws.actor.UserID); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	counts, err := privacy.NewRetention(h.cluster, privacy.DefaultPolicy(), slog.New(slog.DiscardHandler)).Once(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts["absences"] < 1 {
		t.Fatalf("pruned = %v, want the absence that ended long ago", counts)
	}
	var left int
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM absence WHERE user_id = $1`, ws.actor.UserID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 2 {
		t.Errorf("%d absences left, want the recent one and the coming one", left)
	}
}

// The service refusing is not the proof; the same things are tried straight through SQL.
func TestAbsencesHoldEvenThroughSQL(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	home := h.newProject(t, "absencehome")
	ctx := context.Background()
	if _, _, err := availability.NewService(h.cluster).RecordAbsence(home.ctx, availability.Recorder{ID: home.actor.UserID}, home.actor.UserID,
		availability.AbsenceInput{StartsOn: ptr("2026-08-03"), EndsOn: ptr("2026-08-14")}); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO absence (user_id, starts_on, ends_on, half_day) VALUES ($1, $2::date, $3::date, $4)`
	refusedAs := func(t *testing.T, err error, code string) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != code {
			t.Errorf("got %v, want SQLSTATE %s", err, code)
		}
	}

	t.Run("one person is away on a day once", func(t *testing.T) {
		_, err := h.super.Exec(ctx, insert, home.actor.UserID, "2026-08-14", "2026-08-20", false)
		refusedAs(t, err, "23P01")
		_, err = h.super.Exec(ctx, insert, home.actor.UserID, "2026-08-15", "2026-08-20", false)
		if err != nil {
			t.Errorf("the day after the last day away was refused: %v", err)
		}
	})

	t.Run("the days are held to the bounds the service holds them to", func(t *testing.T) {
		for name, days := range map[string][2]string{
			"a half day over two days": {"2026-11-02", "2026-11-03"},
			"an end before the start":  {"2026-11-03", "2026-11-02"},
			"longer than a year":       {"2026-01-01", "2027-01-02"},
		} {
			_, err := h.super.Exec(ctx, insert, home.actor.UserID, days[0], days[1], name == "a half day over two days")
			refusedAs(t, err, "23514")
		}
	})

	t.Run("only somebody who works here is away from it", func(t *testing.T) {
		var customerID uuid.UUID
		err := h.super.QueryRow(ctx, `
			WITH made AS (INSERT INTO app_user (email, name) VALUES ($1, 'Sam Customer') RETURNING id)
			INSERT INTO org_member (user_id, org_role) SELECT id, 'customer' FROM made RETURNING user_id`,
			h.email(t, "absencecustomer")).Scan(&customerID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.super.Exec(ctx, insert, customerID, "2026-11-02", "2026-11-02", false)
		refusedAs(t, err, "23514")
	})
}
