//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/perm"
)

// allocationsOf reads a project's shares by person name.
func allocationsOf(t *testing.T, c *client, projectKey string) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, raw := range list(t, want(t, c.get("/api/v1/projects/"+projectKey+"/allocations"), http.StatusOK, "the shares"), "allocations") {
		a := raw.(map[string]any)
		out[a["name"].(string)] = a
	}
	return out
}

func TestAShareOfThePersonsWeekIsSetPerProject(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	orgID := obj(t, owner.signup(t, h, "shares"), "principal", "org")["id"].(string)
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Half", "key": "HALF", "template": "kanban"}), http.StatusCreated, "a project")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Other", "key": "OTHR", "template": "kanban"}), http.StatusCreated, "another project")
	_, adaID := api.namedMember(t, h, owner, "Ada Lovelace")
	_, beaID := api.namedMember(t, h, owner, "Bea Lisboa")
	crew := idOf(t, want(t, owner.post("/api/v1/projects/HALF/teams", map[string]any{"name": "Crew"}), http.StatusCreated, "a team"), "team")
	for _, id := range []string{adaID, beaID} {
		want(t, owner.post("/api/v1/teams/"+crew+"/members", map[string]any{"userId": id}), http.StatusOK, "join the team")
	}
	const window = "?from=2031-03-03&to=2031-03-09"

	t.Run("everybody gives a project their whole week until somebody says otherwise", func(t *testing.T) {
		shares := allocationsOf(t, owner, "HALF")
		if ada := shares["Ada Lovelace"]; ada == nil || ada["percent"] != 100.0 || ada["elsewherePercent"] != 0.0 || ada["userId"] != adaID {
			t.Errorf("Ada = %v, want her whole week here and none elsewhere", ada)
		}
		if shares["Bea Lisboa"] == nil {
			t.Errorf("shares = %v, want the team's people listed", shares)
		}
	})

	t.Run("a share is set, and the person's other projects are counted beside it", func(t *testing.T) {
		set := obj(t, want(t, owner.put("/api/v1/projects/HALF/allocations/"+adaID, map[string]any{"percent": 50}), http.StatusOK, "half of Ada's week"), "allocation")
		if set["percent"] != 50.0 || set["name"] != "Ada Lovelace" {
			t.Errorf("set = %v", set)
		}
		want(t, owner.put("/api/v1/projects/OTHR/allocations/"+adaID, map[string]any{"percent": 70}), http.StatusOK, "most of it elsewhere")
		ada := allocationsOf(t, owner, "HALF")["Ada Lovelace"]
		if ada["percent"] != 50.0 || ada["elsewherePercent"] != 70.0 {
			t.Errorf("Ada = %v, want 50 here and 70 elsewhere", ada)
		}
		// Somebody named in another project only is listed where they have a share.
		if other := allocationsOf(t, owner, "OTHR")["Ada Lovelace"]; other["percent"] != 70.0 || other["elsewherePercent"] != 50.0 {
			t.Errorf("Ada in the other project = %v", other)
		}
	})

	t.Run("the resources page reads the week at the share", func(t *testing.T) {
		want(t, owner.patch("/api/v1/projects/HALF", map[string]any{"resourceGrouping": "person"}), http.StatusOK, "plan by person")
		got := want(t, owner.get("/api/v1/projects/HALF/resources"+window), http.StatusOK, "the resources")
		rows := resourceRows(t, got)
		if ada := rows["Ada Lovelace"]["2031-03-03"]; ada["capacityHours"] != 20.0 || ada["nominalHours"] != 20.0 {
			t.Errorf("Ada's week = %v, want 20 of 20 h", ada)
		}
		if bea := rows["Bea Lisboa"]["2031-03-03"]; bea["capacityHours"] != 40.0 {
			t.Errorf("Bea's week = %v, want her whole 40 h", bea)
		}
		for _, raw := range list(t, got, "rows") {
			row := raw.(map[string]any)
			if row["name"] == "Ada Lovelace" && row["sharePercent"] != 50.0 {
				t.Errorf("Ada's row = %v, want it to say 50%%", row)
			}
			if row["name"] == "Bea Lisboa" && row["sharePercent"] != nil {
				t.Errorf("Bea's row = %v, want no share shown for a whole week", row)
			}
		}
		want(t, owner.patch("/api/v1/projects/HALF", map[string]any{"resourceGrouping": "team"}), http.StatusOK, "plan by team")
		crewWeek := resourceRows(t, want(t, owner.get("/api/v1/projects/HALF/resources"+window), http.StatusOK, "the resources"))["Crew"]["2031-03-03"]
		if crewWeek["capacityHours"] != 60.0 {
			t.Errorf("the crew's week = %v, want half of Ada's 40 and all of Bea's", crewWeek)
		}
	})

	t.Run("a share that is not one, or for somebody who does not work here, is refused", func(t *testing.T) {
		refused := want(t, owner.put("/api/v1/projects/HALF/allocations/"+adaID, map[string]any{"percent": 150}), http.StatusUnprocessableEntity, "more than a whole week")
		refusedWithASentence(t, refused, "0 and 100")
		want(t, owner.put("/api/v1/projects/HALF/allocations/"+adaID, map[string]any{"percent": -5}), http.StatusUnprocessableEntity, "less than nothing")
		refused = want(t, owner.put("/api/v1/projects/HALF/allocations/00000000-0000-4000-8000-000000000001", map[string]any{"percent": 50}), http.StatusUnprocessableEntity, "a stranger")
		refusedWithASentence(t, refused, "works here")
	})

	t.Run("a whole week again leaves no share behind", func(t *testing.T) {
		want(t, owner.put("/api/v1/projects/OTHR/allocations/"+adaID, map[string]any{"percent": 100}), http.StatusOK, "all of it")
		if ada := allocationsOf(t, owner, "HALF")["Ada Lovelace"]; ada["elsewherePercent"] != 0.0 {
			t.Errorf("Ada = %v, want nothing counted elsewhere once the other share is gone", ada)
		}
		var rows int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM project_allocation WHERE user_id = $1`, adaID).Scan(&rows); err != nil || rows != 1 {
			t.Errorf("rows = %d (%v), want only the half share kept", rows, err)
		}
	})

	t.Run("a project's people read the shares and only its administrators change them", func(t *testing.T) {
		member := api.asRole(t, h, owner, "HALF", perm.User)
		allocationsOf(t, member, "HALF")
		want(t, member.put("/api/v1/projects/HALF/allocations/"+beaID, map[string]any{"percent": 10}), http.StatusForbidden, "a member sets a share")
		stranger := api.asRole(t, h, owner, "OTHR", perm.User)
		want(t, stranger.get("/api/v1/projects/HALF/allocations"), http.StatusNotFound, "another project's shares")
	})

	t.Run("the database holds a share to the same rules", func(t *testing.T) {
		refusedAs := func(name string, err error) {
			t.Helper()
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
				t.Errorf("%s: got %v, want a check violation", name, err)
			}
		}
		ctx := context.Background()
		_, err := h.super.Exec(ctx, `UPDATE project_allocation SET percent = 150 WHERE user_id = $1`, adaID)
		refusedAs("more than a whole week", err)

		var customerID, halfID string
		if err := h.super.QueryRow(ctx, `
			WITH made AS (INSERT INTO app_user (email, name) VALUES ($1, 'Sam Customer') RETURNING id)
			INSERT INTO org_member (org_id, user_id, org_role) SELECT $2, id, 'customer' FROM made RETURNING user_id`,
			h.email(t, "sharecustomer"), orgID).Scan(&customerID); err != nil {
			t.Fatal(err)
		}
		if err := h.super.QueryRow(ctx, `SELECT id FROM project WHERE org_id = $1 AND key = 'HALF'`, orgID).Scan(&halfID); err != nil {
			t.Fatal(err)
		}
		_, err = h.super.Exec(ctx, `INSERT INTO project_allocation (org_id, project_id, user_id, percent) VALUES ($1, $2, $3, 50)`, orgID, halfID, customerID)
		refusedAs("a customer", err)

		elsewhere := h.newWorkspace(t, "shareaway")
		_, err = h.super.Exec(ctx, `INSERT INTO project_allocation (org_id, project_id, user_id, percent) VALUES ($1, $2, $3, 50)`, elsewhere.orgID, halfID, elsewhere.actor.UserID)
		refusedAs("another organization's project", err)

		var seen int
		if err := h.cluster.ReadPrimary(elsewhere.ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM project_allocation WHERE org_id = $1`, orgID).Scan(&seen)
		}); err != nil || seen != 0 {
			t.Errorf("another organization sees %d shares (%v)", seen, err)
		}
	})
}
