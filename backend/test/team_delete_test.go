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

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/team"
)

// teamSprint plans a sprint for one team, with the dates starting one needs.
func (ws *workspace) teamSprint(t *testing.T, name string, teamID uuid.UUID) *sprint.Sprint {
	t.Helper()
	created, _, err := ws.sprints.Create(ws.ctx, ws.project.Key, sprint.CreateInput{
		Name: name, StartsOn: on("2026-03-02"), EndsOn: on("2026-03-13"), TeamID: &teamID,
	}, ws.actor.UserID)
	if err != nil {
		t.Fatalf("create sprint %q: %v", name, err)
	}
	return created
}

// sprintsOwnedBy counts a project's sprints in one stream; a nil team is the
// project's own.
func sprintsOwnedBy(t *testing.T, h *harness, projectID uuid.UUID, teamID *uuid.UUID) int {
	t.Helper()
	var n int
	if err := h.super.QueryRow(context.Background(),
		`SELECT count(*) FROM sprint WHERE project_id = $1 AND team_id IS NOT DISTINCT FROM $2`,
		projectID, teamID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func teamExists(t *testing.T, h *harness, id uuid.UUID) bool {
	t.Helper()
	var n int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM team WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

// When the project's own stream is running a sprint, a team's running sprint
// had nowhere to go and the delete failed with a 500. It is refused instead.
func TestATeamRunningASprintBesideTheProjectIsRefusedWithASentence(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "twostreams")

	key := "TWO" + strings.ToUpper(uuid.New().String()[:3])
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Two streams", "key": key, "template": "scrum"}), http.StatusCreated, "project")
	alpha := idOf(t, want(t, owner.post("/api/v1/projects/"+key+"/teams", map[string]any{"name": "Alpha"}), http.StatusCreated, "team"), "team")

	from, to := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"), time.Now().UTC().AddDate(0, 0, 13).Format("2006-01-02")
	own := idOf(t, want(t, owner.post("/api/v1/projects/"+key+"/sprints", map[string]any{"name": "Project 1", "startsOn": from, "endsOn": to}), http.StatusCreated, "project sprint"), "sprint")
	want(t, owner.post("/api/v1/sprints/"+own+"/start", nil), http.StatusOK, "start the project's sprint")
	theirs := idOf(t, want(t, owner.post("/api/v1/projects/"+key+"/sprints", map[string]any{"name": "Alpha 1", "teamId": alpha, "startsOn": from, "endsOn": to}), http.StatusCreated, "team sprint"), "sprint")
	want(t, owner.post("/api/v1/sprints/"+theirs+"/start", nil), http.StatusOK, "start the team's sprint")

	refused := owner.delete("/api/v1/teams/" + alpha)
	if refused.Status != http.StatusConflict || refused.ErrorCode() != "conflict" {
		t.Fatalf("delete = %d %s, want a 409 conflict", refused.Status, refused.Raw)
	}
	message, _ := refused.Error()["message"].(string)
	if !strings.HasPrefix(message, "Alpha still has 1 sprint, and it is running.") || !strings.Contains(message, "rename it instead") {
		t.Errorf("message = %q, want it to name the running sprint and say what to do", message)
	}

	var teamID *uuid.UUID
	var state string
	if err := h.super.QueryRow(context.Background(), `SELECT team_id, state FROM sprint WHERE id = $1`, theirs).Scan(&teamID, &state); err != nil {
		t.Fatal(err)
	}
	if teamID == nil || teamID.String() != alpha || state != string(sprint.StateActive) {
		t.Errorf("the team's sprint is now team %v, %s; want it still Alpha's and running", teamID, state)
	}
}

// Otherwise the team's sprints quietly became the project's, and its closed
// ones joined the project's velocity and history. They stay the team's.
func TestATeamsSprintsNeverBecomeTheProjects(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "keepsprints")
	alpha := ws.newTeam(t, "Alpha")

	for _, name := range []string{"Alpha 1", "Alpha 2", "Alpha 3"} {
		s := ws.teamSprint(t, name, alpha.ID)
		if _, _, err := ws.sprints.Start(ws.ctx, s.ID, ws.actor.UserID); err != nil {
			t.Fatalf("start %s: %v", name, err)
		}
		if _, _, err := ws.sprints.Complete(ws.ctx, s.ID, sprint.CompleteInput{}, ws.actor.UserID); err != nil {
			t.Fatalf("complete %s: %v", name, err)
		}
	}
	running := ws.teamSprint(t, "Alpha 4", alpha.ID)
	if _, _, err := ws.sprints.Start(ws.ctx, running.ID, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	projectsOwn := sprintsOwnedBy(t, h, ws.project.ID, nil)

	_, err := ws.teams.Delete(ws.ctx, alpha.ID)
	if !errors.Is(err, team.ErrInUse) {
		t.Fatalf("error = %v, want the delete refused", err)
	}
	if !strings.Contains(err.Error(), "Alpha still has 4 sprints, one of them running.") {
		t.Errorf("error = %q, want it to name the sprints", err)
	}

	if !teamExists(t, h, alpha.ID) {
		t.Fatal("the team went anyway")
	}
	if got := sprintsOwnedBy(t, h, ws.project.ID, &alpha.ID); got != 4 {
		t.Errorf("Alpha has %d sprints after the refusal, want all 4", got)
	}
	if got := sprintsOwnedBy(t, h, ws.project.ID, nil); got != projectsOwn {
		t.Errorf("the project's own stream has %d sprints, want the %d it had", got, projectsOwn)
	}
}

// Sprints that never started are plans, not record: once they are deleted the
// team can go.
func TestATeamGoesOnceItsUnstartedSprintsDo(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "unstarted")
	alpha := ws.newTeam(t, "Alpha")
	first := ws.teamSprint(t, "Alpha 1", alpha.ID)
	second := ws.teamSprint(t, "Alpha 2", alpha.ID)

	_, err := ws.teams.Delete(ws.ctx, alpha.ID)
	if !errors.Is(err, team.ErrInUse) {
		t.Fatalf("error = %v, want the delete refused", err)
	}
	if !strings.Contains(err.Error(), "Alpha still has 2 sprints that have not started. Delete them first.") {
		t.Errorf("error = %q, want it to name the sprints and say what to do", err)
	}

	for _, s := range []*sprint.Sprint{first, second} {
		if _, err := ws.sprints.Delete(ws.ctx, s.ID); err != nil {
			t.Fatalf("delete %s: %v", s.Name, err)
		}
	}
	if _, err := ws.teams.Delete(ws.ctx, alpha.ID); err != nil {
		t.Errorf("deleting a team whose sprints are gone: %v", err)
	}
}

// A request type routed to the team used to stop routing without anybody being
// told. The refusal names it, and the routing is left alone.
func TestATeamRequestsAreRoutedToIsNotDeletedUnderThem(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "routedteam")
	d, p := ws.aDesk(t, h, "Routed desk")
	field, _, err := ws.teams.Create(ws.ctx, p.Key, team.CreateInput{Name: "Field support"}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	laptop, _, err := d.CreateRequestType(ws.ctx, p.Key, desk.RequestTypeInput{Name: "Laptop replacement", TeamID: &field.ID}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ws.teams.Delete(ws.ctx, field.ID)
	if !errors.Is(err, team.ErrInUse) {
		t.Fatalf("error = %v, want the delete refused", err)
	}
	if !strings.Contains(err.Error(), "Field support still has 1 request type routed to it. Route it to another team or to the project first.") {
		t.Errorf("error = %q, want it to name the request type and say what to do", err)
	}
	types, err := d.RequestTypes(ws.ctx, p.Key)
	if err != nil {
		t.Fatal(err)
	}
	if kept := requestTypeNamed(t, types, laptop.Name); kept.TeamID == nil || *kept.TeamID != field.ID {
		t.Errorf("the request type now routes to %v, want Field support still", kept.TeamID)
	}

	if _, _, err := d.UpdateRequestType(ws.ctx, laptop.ID, desk.RequestTypeUpdate{ClearTeam: true}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.teams.Delete(ws.ctx, field.ID); err != nil {
		t.Errorf("deleting a team nothing is routed to: %v", err)
	}
}

// The service refusing is not enough: a team deleted straight through SQL must
// not hand its sprints or its requests to the project either.
func TestTheDatabaseRefusesToDeleteATeamThatStillHasSprints(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "rawteam")
	alpha := ws.newTeam(t, "Alpha")
	ws.teamSprint(t, "Alpha 1", alpha.ID)

	isForeignKey := func(err error) bool {
		var pgErr *pgconn.PgError
		return errors.As(err, &pgErr) && pgErr.Code == "23503"
	}

	t.Run("as the application", func(t *testing.T) {
		_, err := h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `DELETE FROM team WHERE id = $1`, alpha.ID)
			return err
		})
		if !isForeignKey(err) {
			t.Errorf("error = %v, want a foreign key refusing it", err)
		}
	})

	t.Run("as a superuser", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(), `DELETE FROM team WHERE id = $1`, alpha.ID)
		if !isForeignKey(err) {
			t.Errorf("error = %v, want a foreign key refusing it", err)
		}
		if !teamExists(t, h, alpha.ID) {
			t.Fatal("SQL deleted a team that still has sprints")
		}
	})

	t.Run("nor one that requests are routed to", func(t *testing.T) {
		d, p := ws.aDesk(t, h, "Raw desk")
		field, _, err := ws.teams.Create(ws.ctx, p.Key, team.CreateInput{Name: "Field support"}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := d.CreateRequestType(ws.ctx, p.Key, desk.RequestTypeInput{Name: "Laptop replacement", TeamID: &field.ID}, ws.actor.UserID); err != nil {
			t.Fatal(err)
		}
		_, err = h.super.Exec(context.Background(), `DELETE FROM team WHERE id = $1`, field.ID)
		if !isForeignKey(err) {
			t.Errorf("error = %v, want a foreign key refusing it", err)
		}
	})

	// The guard is checked once the statement is done, so an organization
	// going with everything in it still takes its teams and their sprints.
	t.Run("but the whole organization still goes", func(t *testing.T) {
		if _, err := h.super.Exec(context.Background(), `DELETE FROM org WHERE id = $1`, ws.orgID); err != nil {
			t.Fatalf("deleting the organization: %v", err)
		}
		if teamExists(t, h, alpha.ID) {
			t.Error("the team outlived its organization")
		}
	})
}
