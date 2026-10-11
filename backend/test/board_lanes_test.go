//go:build integration

package test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/perm"
)

// openingBoard is the id of the board the workspace's project opens on, which
// is the one its lanes are configured through in the service level tests.
func (ws *workspace) openingBoard(t *testing.T) uuid.UUID {
	t.Helper()
	b, err := ws.boards.ForProject(ws.ctx, ws.project.Key)
	if err != nil {
		t.Fatalf("load board: %v", err)
	}
	return b.ID
}

// grouped is a board edit that changes nothing but the grouping.
func grouped(g board.Grouping) board.UpdateBoardInput {
	return board.UpdateBoardInput{GroupBy: &g}
}

// Another organization's board is not there, whichever lane is named with it.
func TestAnotherOrganizationsBoardCannotBeConfigured(t *testing.T) {
	h := newHarness(t)
	alpha := h.newWorkspace(t, "lanesalpha")
	beta := h.newWorkspace(t, "lanesbeta")
	betaBoard := beta.openingBoard(t)
	betaLane := beta.swimlane(t, bootstrap.StatusToDo)

	if _, _, err := alpha.boards.AddSwimlane(alpha.ctx, betaBoard, board.AddSwimlaneInput{Name: "Planted"}); !errors.Is(err, board.ErrNotFound) {
		t.Errorf("adding a lane to beta's board: error = %v, want ErrNotFound", err)
	}
	if _, err := alpha.boards.DeleteSwimlane(alpha.ctx, betaBoard, betaLane.ID); !errors.Is(err, board.ErrNotFound) {
		t.Errorf("deleting beta's lane: error = %v, want ErrNotFound", err)
	}
	if lanes := laneRows(t, h, betaBoard.String()); slices.ContainsFunc(lanes, func(l string) bool { return strings.HasPrefix(l, "Planted/") }) || len(lanes) == 0 {
		t.Errorf("beta's board was changed by alpha: %v", lanes)
	}
}

// laneRows reads a board's swimlanes straight from the database, as
// "name/position/limit" in order, so no service read path can hide a stray write.
func laneRows(t *testing.T, h *harness, boardID string) []string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT name, position, wip_limit FROM board_swimlane
		WHERE board_id = $1 ORDER BY position, name`, boardID)
	if err != nil {
		t.Fatalf("read the lanes of %s: %v", boardID, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var (
			name            string
			position, limit int
		)
		if err := rows.Scan(&name, &position, &limit); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%s/%d/%d", name, position, limit))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// laneIDs lists the ids of a board's swimlanes as the API shows them.
func laneIDs(t *testing.T, board map[string]any) []string {
	t.Helper()
	var ids []string
	for _, raw := range board["swimlanes"].([]any) {
		ids = append(ids, raw.(map[string]any)["id"].(string))
	}
	return ids
}

func shownLaneNames(board map[string]any) []string {
	var names []string
	for _, raw := range board["swimlanes"].([]any) {
		names = append(names, raw.(map[string]any)["name"].(string))
	}
	return names
}

// A project with a team board has two sets of swimlanes. Configuring the one on
// screen used to configure the project's first board instead.
func TestEachBoardKeepsItsOwnSwimlanes(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "lanes")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Laned", "key": "LANE"}), http.StatusCreated, "the project")

	first := obj(t, want(t, owner.get("/api/v1/projects/LANE/board"), http.StatusOK, "the first board"), "board")
	firstID := first["id"].(string)
	firstLanes := laneIDs(t, first)
	before := laneRows(t, h, firstID)

	secondID := idOf(t, want(t, owner.post("/api/v1/projects/LANE/boards", map[string]any{"name": "Team flow", "type": "kanban"}), http.StatusCreated, "a second board"), "board")
	second := func() map[string]any {
		return obj(t, want(t, owner.get("/api/v1/boards/"+secondID), http.StatusOK, "the second board"), "board")
	}
	secondLanes := laneIDs(t, second())

	var parked string
	t.Run("a lane is added to the board it was asked of", func(t *testing.T) {
		added := want(t, owner.post("/api/v1/boards/"+secondID+"/swimlanes", map[string]any{"name": "Parked", "wipLimit": 2}), http.StatusCreated, "add a lane to the second board")
		parked = idOf(t, added, "swimlane")
		if names := shownLaneNames(second()); !slices.Contains(names, "Parked") {
			t.Errorf("the second board has %v, want Parked among them", names)
		}
		opening := obj(t, owner.get("/api/v1/projects/LANE/board"), "board")
		if names := shownLaneNames(opening); slices.Contains(names, "Parked") {
			t.Errorf("the first board gained the lane: %v", names)
		}
	})

	t.Run("a lane is renamed, limited and reordered on its own board", func(t *testing.T) {
		want(t, owner.patch("/api/v1/boards/"+secondID+"/swimlanes/"+secondLanes[0], map[string]any{"name": "Queued", "wipLimit": 3}), http.StatusNoContent, "edit a lane of the second board")
		order := laneIDs(t, second())
		slices.Reverse(order)
		want(t, owner.put("/api/v1/boards/"+secondID+"/swimlanes", map[string]any{"order": order}), http.StatusNoContent, "reorder the second board")

		got := second()["swimlanes"].([]any)
		last := got[len(got)-1].(map[string]any)
		if last["name"] != "Queued" || last["wipLimit"].(float64) != 3 {
			t.Errorf("the edited lane ended as %v, want Queued with a limit of 3 at the end", last)
		}
	})

	t.Run("a lane of one board is not reached through another", func(t *testing.T) {
		want(t, owner.patch("/api/v1/boards/"+secondID+"/swimlanes/"+firstLanes[0], map[string]any{"name": "Taken"}), http.StatusNotFound, "edit the first board's lane through the second")
		want(t, owner.delete("/api/v1/boards/"+secondID+"/swimlanes/"+firstLanes[0]), http.StatusNotFound, "delete the first board's lane through the second")
		// The right number of lanes, but one of them the first board's.
		order := laneIDs(t, second())
		order[0] = firstLanes[0]
		want(t, owner.put("/api/v1/boards/"+secondID+"/swimlanes", map[string]any{"order": order}), http.StatusNotFound, "order the second board with the first's lane")
	})

	t.Run("grouping is the board's own", func(t *testing.T) {
		regrouped := want(t, owner.patch("/api/v1/boards/"+secondID, map[string]any{"groupBy": "assignee"}), http.StatusOK, "group the second board")
		if got := obj(t, regrouped, "board")["groupBy"]; got != "assignee" {
			t.Errorf("the second board groups by %v, want assignee", got)
		}
		if got := obj(t, owner.get("/api/v1/projects/LANE/board"), "board")["groupBy"]; got != "none" {
			t.Errorf("the first board groups by %v, want it left alone", got)
		}
	})

	t.Run("a lane is deleted from its own board", func(t *testing.T) {
		want(t, owner.delete("/api/v1/boards/"+secondID+"/swimlanes/"+parked), http.StatusNoContent, "delete the parked lane")
		if names := shownLaneNames(second()); slices.Contains(names, "Parked") {
			t.Errorf("the parked lane is still there: %v", names)
		}
	})

	t.Run("a reader cannot configure a board", func(t *testing.T) {
		reader := api.asRole(t, h, owner, "LANE", perm.Reader)
		want(t, reader.get("/api/v1/boards/"+secondID), http.StatusOK, "a reader reads the board")
		lanes := laneRows(t, h, secondID)
		for _, attempt := range []struct {
			what string
			do   func() response
		}{
			{"add a lane", func() response {
				return reader.post("/api/v1/boards/"+secondID+"/swimlanes", map[string]any{"name": "Mine"})
			}},
			{"rename a lane", func() response {
				return reader.patch("/api/v1/boards/"+secondID+"/swimlanes/"+secondLanes[0], map[string]any{"name": "Mine"})
			}},
			{"reorder the lanes", func() response {
				return reader.put("/api/v1/boards/"+secondID+"/swimlanes", map[string]any{"order": secondLanes})
			}},
			{"delete a lane", func() response {
				return reader.delete("/api/v1/boards/" + secondID + "/swimlanes/" + secondLanes[0])
			}},
			{"regroup the board", func() response {
				return reader.patch("/api/v1/boards/"+secondID, map[string]any{"groupBy": "priority"})
			}},
		} {
			if got := attempt.do(); got.Status != http.StatusForbidden {
				t.Errorf("a reader may %s: got %d, want 403: %s", attempt.what, got.Status, got.Raw)
			}
		}
		if after := laneRows(t, h, secondID); !slices.Equal(after, lanes) {
			t.Errorf("a refused reader changed the lanes: %v, then %v", lanes, after)
		}
	})

	t.Run("the first board's lanes stay with it in the database", func(t *testing.T) {
		if after := laneRows(t, h, firstID); !slices.Equal(after, before) {
			t.Errorf("the first board's lanes were %v and are now %v", before, after)
		}
		var group string
		if err := h.super.QueryRow(context.Background(), `SELECT group_by::text FROM board WHERE id = $1`, firstID).Scan(&group); err != nil {
			t.Fatal(err)
		}
		if group != "none" {
			t.Errorf("the first board groups by %q in the database, want none", group)
		}
		// Every state a lane claims is claimed on that lane's own board.
		var strays int
		if err := h.super.QueryRow(context.Background(), `
			SELECT count(*) FROM board_swimlane_status s
			JOIN board_swimlane l ON l.id = s.swimlane_id
			WHERE l.board_id IN ($1, $2) AND s.board_id <> l.board_id`, firstID, secondID).Scan(&strays); err != nil {
			t.Fatal(err)
		}
		if strays != 0 {
			t.Errorf("%d states are claimed on a board their lane is not on", strays)
		}
	})
}
