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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/availability"
	"github.com/armature/armature/backend/internal/db"
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
	them := s.client(t)
	want(t, them.post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusOK, "sign "+name+" in")
	return them, obj(t, made, "user")["id"].(string)
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
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "absences")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Away", "key": "AWY", "template": "scrum"}), http.StatusCreated, "a project")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Elsewhere", "key": "ELW", "template": "scrum"}), http.StatusCreated, "another project")

	ada, adaID := api.namedMember(t, h, owner, "Ada Away")
	master, masterID := api.namedMember(t, h, owner, "Max Master")
	stranger, strangerID := api.namedMember(t, h, owner, "Stan Stranger")
	plain, plainID := api.namedMember(t, h, owner, "Pat Plain")

	// Max is scrum master where Ada is on a team; Stan is scrum master where she is not.
	want(t, owner.post("/api/v1/role-assignments", map[string]any{"role": "scrum_master", "projectKey": "AWY", "userId": masterID}), http.StatusCreated, "a scrum master of Ada's project")
	want(t, owner.post("/api/v1/role-assignments", map[string]any{"role": "scrum_master", "projectKey": "ELW", "userId": strangerID}), http.StatusCreated, "a scrum master elsewhere")
	crew := idOf(t, want(t, owner.post("/api/v1/projects/AWY/teams", map[string]any{"name": "Crew"}), http.StatusCreated, "a team"), "team")
	want(t, owner.post("/api/v1/teams/"+crew+"/members", map[string]any{"userId": adaID}), http.StatusOK, "Ada joins the team")
	other := idOf(t, want(t, owner.post("/api/v1/projects/ELW/teams", map[string]any{"name": "Others"}), http.StatusCreated, "another team"), "team")
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

		seen := want(t, plain.get("/api/v1/absences?from=2026-08-01&to=2026-12-31"), http.StatusOK, "a colleague lists absences")
		absences := list(t, seen, "absences")
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
		if outside := list(t, plain.get("/api/v1/absences?from=2026-09-01&to=2026-12-23"), "absences"); len(outside) != 0 {
			t.Errorf("a stretch with nobody away lists %v", outside)
		}
		if edge := list(t, plain.get("/api/v1/absences?from=2026-08-14&to=2026-08-14"), "absences"); len(edge) != 1 {
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

	t.Run("the person takes their absences with their data, and they go when the person does", func(t *testing.T) {
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
		want(t, owner.delete("/api/v1/members/"+adaID), http.StatusNoContent, "let Ada go")
		if left := count(adaID); left != 0 {
			t.Errorf("%d of Ada's absences stayed after she left", left)
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

func TestAbsencesPastTheirWindowAreSwept(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "absenceretention")
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO absence (org_id, user_id, starts_on, ends_on) VALUES ($1, $2, current_date - 500, current_date - 450)`,
		`INSERT INTO absence (org_id, user_id, starts_on, ends_on) VALUES ($1, $2, current_date - 30, current_date - 20)`,
		`INSERT INTO absence (org_id, user_id, starts_on, ends_on) VALUES ($1, $2, current_date + 20, current_date + 30)`,
	} {
		if _, err := h.super.Exec(ctx, q, ws.orgID, ws.actor.UserID); err != nil {
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
	if err := h.super.QueryRow(ctx, `SELECT count(*) FROM absence WHERE org_id = $1`, ws.orgID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 2 {
		t.Errorf("%d absences left, want the recent one and the coming one", left)
	}
}

// The service refusing is not the proof; the same things are tried straight through SQL.
func TestAbsencesHoldEvenThroughSQL(t *testing.T) {
	h := newHarness(t)
	home := h.newWorkspace(t, "absencehome")
	away := h.newWorkspace(t, "absenceaway")
	ctx := context.Background()
	if _, _, err := availability.NewService(h.cluster).RecordAbsence(home.ctx, availability.Recorder{ID: home.actor.UserID}, home.actor.UserID,
		availability.AbsenceInput{StartsOn: ptr("2026-08-03"), EndsOn: ptr("2026-08-14")}); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO absence (org_id, user_id, starts_on, ends_on, half_day) VALUES ($1, $2, $3::date, $4::date, $5)`
	refusedAs := func(t *testing.T, err error, code string) {
		t.Helper()
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != code {
			t.Errorf("got %v, want SQLSTATE %s", err, code)
		}
	}

	t.Run("one person is away on a day once", func(t *testing.T) {
		_, err := h.super.Exec(ctx, insert, home.orgID, home.actor.UserID, "2026-08-14", "2026-08-20", false)
		refusedAs(t, err, "23P01")
		_, err = h.super.Exec(ctx, insert, home.orgID, home.actor.UserID, "2026-08-15", "2026-08-20", false)
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
			_, err := h.super.Exec(ctx, insert, home.orgID, home.actor.UserID, days[0], days[1], name == "a half day over two days")
			refusedAs(t, err, "23514")
		}
	})

	t.Run("only somebody who works here is away from it", func(t *testing.T) {
		var customerID uuid.UUID
		err := h.super.QueryRow(ctx, `
			WITH made AS (INSERT INTO app_user (email, name) VALUES ($1, 'Sam Customer') RETURNING id)
			INSERT INTO org_member (org_id, user_id, org_role) SELECT $2, id, 'customer' FROM made RETURNING user_id`,
			h.email(t, "absencecustomer"), home.orgID).Scan(&customerID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = h.super.Exec(ctx, insert, home.orgID, customerID, "2026-11-02", "2026-11-02", false)
		refusedAs(t, err, "23514")
		_, err = h.super.Exec(ctx, insert, home.orgID, away.actor.UserID, "2026-11-02", "2026-11-02", false)
		refusedAs(t, err, "23514")
	})

	t.Run("another organization reads none of it and writes none into it", func(t *testing.T) {
		var seen int
		err := h.cluster.ReadPrimary(away.ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM absence WHERE org_id = $1`, home.orgID).Scan(&seen)
		})
		if err != nil || seen != 0 {
			t.Errorf("another organization sees %d absences (%v)", seen, err)
		}
		listed, err := availability.NewService(h.cluster).Absences(away.ctx, *mustDate(t, "2026-01-01"), *mustDate(t, "2026-12-31"), nil)
		if err != nil || len(listed) != 0 {
			t.Errorf("another organization lists %v (%v)", listed, err)
		}
		_, err = h.cluster.Write(away.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, insert, home.orgID, home.actor.UserID, "2026-12-01", "2026-12-01", false)
			return err
		})
		if err == nil {
			t.Error("an absence went into another organization")
		}
	})
}
