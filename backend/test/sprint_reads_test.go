//go:build integration

package test

import (
	"fmt"
	"net/http"
	"testing"
)

// newerThanAPage is how many issues are filed after the old ones: more than
// the largest page, so a read that only looks at the newest misses them.
const newerThanAPage = 205

// keysIn lists the issue keys of one page of the issue list.
func keysIn(t *testing.T, r response) []string {
	t.Helper()
	var keys []string
	for _, row := range list(t, r, "issues") {
		keys = append(keys, row.(map[string]any)["key"].(string))
	}
	return keys
}

func holds(keys []string, key string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

// The sprints page reads each sprint and the backlog for themselves. Reading
// the project's newest page and filtering it lost every older committed issue.
func TestTheSprintsPageReadsEachSprintAndTheBacklogWhole(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "sprintreads")

	want(t, owner.post("/api/v1/projects", map[string]string{"name": "Long history", "key": "LONG"}), http.StatusCreated, "create project")
	task := find(t, owner.get("/api/v1/issue-types").Body["issueTypes"], "name", "Task")["id"].(string)
	file := func(summary string) string {
		t.Helper()
		created := want(t, owner.post("/api/v1/projects/LONG/issues", map[string]any{"summary": summary, "typeId": task}), http.StatusCreated, "file "+summary)
		return obj(t, created, "issue")["key"].(string)
	}

	sprintID := idOf(t, want(t, owner.post("/api/v1/projects/LONG/sprints", map[string]any{"name": "Sprint 3"}), http.StatusCreated, "plan a sprint"), "sprint")
	teamID := idOf(t, want(t, owner.post("/api/v1/projects/LONG/teams", map[string]any{"name": "Platform"}), http.StatusCreated, "form a team"), "team")

	committed := file("old committed work")
	want(t, owner.put("/api/v1/issues/"+committed+"/sprint", map[string]any{"sprintId": sprintID}), http.StatusOK, "commit the old work")

	finished := file("old finished work")
	transitions := want(t, owner.get("/api/v1/issues/"+finished+"/transitions"), http.StatusOK, "transitions")
	closing := find(t, transitions.Body["transitions"], "name", "Close")["id"]
	want(t, owner.post("/api/v1/issues/"+finished+"/transitions", map[string]any{"transitionId": closing}), http.StatusOK, "close the old work")

	waiting := file("old work nobody has taken on")
	theirs := file("old work the platform team carries")
	want(t, owner.put("/api/v1/issues/"+theirs+"/team", map[string]any{"teamId": teamID}), http.StatusOK, "hand the work to the team")

	for i := range newerThanAPage {
		file(fmt.Sprintf("newer work %d", i))
	}

	t.Run("the newest page of the project does not hold the old work", func(t *testing.T) {
		newest := keysIn(t, want(t, owner.get("/api/v1/projects/LONG/issues?limit=200"), http.StatusOK, "the newest page"))
		if holds(newest, committed) {
			t.Fatalf("%s is on the newest page, so this test proves nothing", committed)
		}
	})

	t.Run("a sprint's read finds its work however old", func(t *testing.T) {
		read := want(t, owner.get("/api/v1/projects/LONG/issues?limit=200&sprint="+sprintID), http.StatusOK, "the sprint's read")
		if keys := keysIn(t, read); len(keys) != 1 || keys[0] != committed {
			t.Errorf("sprint holds %v, want just %s", keys, committed)
		}
		if read.Body["total"] != float64(1) {
			t.Errorf("total = %v, want 1, which is what says the sprint is not empty", read.Body["total"])
		}
	})

	t.Run("an empty sprint reads as empty", func(t *testing.T) {
		empty := idOf(t, want(t, owner.post("/api/v1/projects/LONG/sprints", map[string]any{"name": "Sprint 4"}), http.StatusCreated, "plan another sprint"), "sprint")
		read := want(t, owner.get("/api/v1/projects/LONG/issues?sprint="+empty), http.StatusOK, "the empty sprint's read")
		if read.Body["total"] != float64(0) {
			t.Errorf("total = %v, want 0", read.Body["total"])
		}
	})

	backlog := "/api/v1/projects/LONG/issues?sprint=none&team=none&category=todo&category=in_progress&orderBy=rank&order=asc&limit=200"

	t.Run("the backlog leaves out done work and what a sprint or a team holds", func(t *testing.T) {
		first := want(t, owner.get(backlog), http.StatusOK, "the backlog")
		if first.Body["total"] != float64(newerThanAPage+1) {
			t.Errorf("total = %v, want the %d newer issues and the one old one waiting", first.Body["total"], newerThanAPage)
		}
		rest := want(t, owner.get(backlog+"&offset=200"), http.StatusOK, "the backlog's second page")
		keys := append(keysIn(t, first), keysIn(t, rest)...)
		if !holds(keys, waiting) {
			t.Errorf("the backlog misses %s, the oldest work waiting", waiting)
		}
		for _, gone := range []string{committed, finished, theirs} {
			if holds(keys, gone) {
				t.Errorf("the backlog holds %s", gone)
			}
		}
	})

	t.Run("it is in rank order, oldest work first when nobody has reordered", func(t *testing.T) {
		keys := keysIn(t, want(t, owner.get(backlog), http.StatusOK, "the backlog"))
		if len(keys) == 0 || keys[0] != waiting {
			t.Errorf("the backlog starts %v, want %s first", keys[:min(len(keys), 3)], waiting)
		}
	})

	t.Run("a team's backlog is the work it carries", func(t *testing.T) {
		keys := keysIn(t, want(t, owner.get("/api/v1/projects/LONG/issues?sprint=none&team="+teamID), http.StatusOK, "the team's backlog"))
		if len(keys) != 1 || keys[0] != theirs {
			t.Errorf("the team's backlog holds %v, want just %s", keys, theirs)
		}
	})

	t.Run("a sprint or team that is neither an id nor none is refused", func(t *testing.T) {
		for _, query := range []string{"sprint=Sprint+3", "team=Platform"} {
			refused := want(t, owner.get("/api/v1/projects/LONG/issues?"+query), http.StatusBadRequest, query)
			wantCode(t, refused, "bad_request")
		}
	})
}
