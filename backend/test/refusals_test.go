//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/sprint"
)

// refusedWith fails unless the response is the refusal given, word for word.
func refusedWith(t *testing.T, r response, status int, code, message string) {
	t.Helper()
	if r.Status != status || r.ErrorCode() != code {
		t.Fatalf("got %d %s, want %d %s", r.Status, r.Raw, status, code)
	}
	if got, _ := r.Error()["message"].(string); got != message {
		t.Errorf("message = %q, want %q", got, message)
	}
}

// userIDOfEmail is the account an address belongs to.
func userIDOfEmail(t *testing.T, h *harness, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM app_user WHERE email = $1`, email).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// Naming the sprint being completed as where its work goes was a 500. It is a
// refusal the person can act on, and nothing moves.
func TestCompletingASprintIntoItselfIsRefusedWithASentence(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "selfcarry", auth.RoleOwner))

	key := h.projectKey(t, "SLF")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Self carry", "key": key, "template": "scrum"}), http.StatusCreated, "project")
	from, to := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"), time.Now().UTC().AddDate(0, 0, 13).Format("2006-01-02")
	sprintID := idOf(t, want(t, owner.post("/api/v1/projects/"+key+"/sprints", map[string]any{"name": "Sprint 1", "startsOn": from, "endsOn": to}), http.StatusCreated, "sprint"), "sprint")
	filed := obj(t, want(t, owner.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "unfinished", "sprintId": sprintID}), http.StatusCreated, "issue"), "issue")
	issueKey, issueID := filed["key"].(string), filed["id"].(string)
	want(t, owner.post("/api/v1/sprints/"+sprintID+"/start", nil), http.StatusOK, "start")

	refusedWith(t, owner.post("/api/v1/sprints/"+sprintID+"/complete", map[string]any{"moveTo": sprintID}),
		http.StatusUnprocessableEntity, "validation_failed",
		"A sprint cannot carry its work into itself. Pick another sprint or the backlog.")

	var state string
	if err := h.super.QueryRow(context.Background(), `SELECT state FROM sprint WHERE id = $1`, sprintID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != string(sprint.StateActive) {
		t.Errorf("the sprint is %s, want it still running", state)
	}
	var in *uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT sprint_id FROM issue WHERE id = $1`, issueID).Scan(&in); err != nil {
		t.Fatal(err)
	}
	if in == nil || in.String() != sprintID {
		t.Errorf("%s is now in sprint %v, want it still in %s", issueKey, in, sprintID)
	}
}

// A token whose expiry has passed would be dead the moment its secret is shown.
func TestATokenThatHasAlreadyExpiredIsNotMade(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	email := h.person(t, "pasttoken", auth.RoleOwner)
	owner.signIn(t, email)

	for name, expires := range map[string]time.Time{
		"yesterday": time.Now().Add(-24 * time.Hour),
		"just now":  time.Now().Add(-time.Second),
	} {
		t.Run(name, func(t *testing.T) {
			refusedWith(t, owner.post("/api/v1/tokens", map[string]any{"name": name, "expiresAt": expires}),
				http.StatusUnprocessableEntity, "validation_failed",
				"The expiry date has to be in the future. Pick a later date, or none for a token that never expires.")
			var n int
			if err := h.super.QueryRow(context.Background(),
				`SELECT count(*) FROM api_token WHERE user_id = $1 AND name = $2`, userIDOfEmail(t, h, email), name).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Errorf("%d token rows were written, want none", n)
			}
		})
	}
}

// Archiving and restoring a project that is already there said the project
// did not exist. They say which state it is in; only a missing key is a 404.
func TestArchivingAndRestoringSayWhereTheProjectIs(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "archivist", auth.RoleOwner))
	key := h.projectKey(t, "ARC")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Archive twice", "key": key}), http.StatusCreated, "project")

	t.Run("restoring a live project", func(t *testing.T) {
		refusedWith(t, owner.post("/api/v1/projects/"+key+"/restore", nil), http.StatusConflict, "conflict",
			"That project is not archived, so there is nothing to restore.")
	})

	t.Run("archiving an archived project", func(t *testing.T) {
		want(t, owner.delete("/api/v1/projects/"+key), http.StatusNoContent, "archive")
		refusedWith(t, owner.delete("/api/v1/projects/"+key), http.StatusConflict, "conflict",
			"That project is already archived. Restore it to bring it back.")
		want(t, owner.post("/api/v1/projects/"+key+"/restore", nil), http.StatusNoContent, "restore")
	})

	t.Run("a key that does not exist", func(t *testing.T) {
		missing := h.projectKey(t, "NOP")
		if r := owner.post("/api/v1/projects/"+missing+"/restore", nil); r.Status != http.StatusNotFound {
			t.Errorf("restore = %d %s, want 404", r.Status, r.Raw)
		}
		if r := owner.delete("/api/v1/projects/" + missing); r.Status != http.StatusNotFound {
			t.Errorf("archive = %d %s, want 404", r.Status, r.Raw)
		}
	})
}

// Team and group refusals reached people as lower-case fragments, and adding
// somebody who is not a member was a 400 rather than a refusal of the data.
func TestTeamAndGroupRefusalsAreSentences(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "teamwords", auth.RoleOwner))
	key := h.projectKey(t, "TMW")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Team words", "key": key}), http.StatusCreated, "project")
	teamID := idOf(t, want(t, owner.post("/api/v1/projects/"+key+"/teams", map[string]any{"name": "Platform"}), http.StatusCreated, "team"), "team")
	groupName := h.name(t, "Words")
	groupID := idOf(t, want(t, owner.post("/api/v1/groups", map[string]any{"name": groupName}), http.StatusCreated, "group"), "group")

	// Somebody who has left is an account with no standing here.
	gone := h.person(t, "teamwordsgone", auth.RoleMember)
	if _, err := h.super.Exec(context.Background(), `
		DELETE FROM role_assignment WHERE user_id = (SELECT id FROM app_user WHERE email = $1)`, gone); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET standing = NULL WHERE email = $1`, gone); err != nil {
		t.Fatal(err)
	}
	goneID := userIDOfEmail(t, h, gone)

	t.Run("adding a non-member to a team", func(t *testing.T) {
		refusedWith(t, owner.post("/api/v1/teams/"+teamID+"/members", map[string]any{"userId": goneID}),
			http.StatusUnprocessableEntity, "validation_failed",
			"That person is not a member here. Only people who work here join a team; invite them first.")
	})

	t.Run("adding a non-member to a group", func(t *testing.T) {
		refusedWith(t, owner.post("/api/v1/groups/"+groupID+"/members", map[string]any{"userId": goneID}),
			http.StatusUnprocessableEntity, "validation_failed",
			"That person is not a member here. Invite them first, then try again.")
	})

	t.Run("a second team by the same name", func(t *testing.T) {
		refusedWith(t, owner.post("/api/v1/projects/"+key+"/teams", map[string]any{"name": "Platform"}),
			http.StatusConflict, "conflict",
			"This project already has a team with that name. Choose another.")
	})

	t.Run("a second group by the same name", func(t *testing.T) {
		refusedWith(t, owner.post("/api/v1/groups", map[string]any{"name": groupName}),
			http.StatusConflict, "conflict",
			"There is already a group with that name. Choose another.")
	})
}
