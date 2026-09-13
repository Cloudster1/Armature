//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/httpapi"
	"github.com/armature/armature/backend/internal/perm"
)

// far is one project's worth of things, made by somebody who may see it, for
// somebody who may not to try to reach.
type far struct {
	issueKey, otherKey  string
	comment, worklog    string
	attachment, sprint  string
	milestone, version  string
	component, team     string
	board, swimlane     string
	dashboard, share    string
	field, rule, link   string
	summary, otherOwned string
}

// A person granted a role in one project sees nothing of another: not its
// issues, not the things hanging off them, and not through an id.
func TestAProjectIsAWallInsideTheOrganization(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "walls")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Near", "key": "NEAR"}), http.StatusCreated, "the near project")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Far", "key": "FARR"}), http.StatusCreated, "the far project")
	nearIssue := obj(t, want(t, owner.post("/api/v1/projects/NEAR/issues", map[string]any{"summary": "near work"}), http.StatusCreated, "a near issue"), "issue")
	nearKey := nearIssue["key"].(string)

	it := buildFar(t, owner)
	member := api.asRole(t, h, owner, "NEAR", perm.User)

	t.Run("the far project is not there", func(t *testing.T) {
		listed := list(t, want(t, member.get("/api/v1/projects"), http.StatusOK, "the projects they see"), "projects")
		for _, p := range listed {
			if p.(map[string]any)["key"] == "FARR" {
				t.Fatalf("the far project is listed: %v", listed)
			}
		}
		for _, path := range []string{
			"/api/v1/projects/FARR",
			"/api/v1/projects/FARR/issues",
			"/api/v1/projects/FARR/plan",
			"/api/v1/projects/FARR/dashboards",
			"/api/v1/issues/" + it.issueKey,
			"/api/v1/issues/" + it.issueKey + "/comments",
			"/api/v1/issues/" + it.issueKey + "/history",
			"/api/v1/issues/" + it.issueKey + "/attachments",
			"/api/v1/attachments/" + it.attachment,
			"/api/v1/boards/" + it.board,
			"/api/v1/teams/" + it.team,
			"/api/v1/versions/" + it.version + "/notes",
		} {
			want(t, member.get(path), http.StatusNotFound, path)
		}
	})

	t.Run("the lists carry nothing of it", func(t *testing.T) {
		found := want(t, member.get("/api/v1/issues?q="+urlQuery(`project = FARR`)), http.StatusOK, "a query naming the far project")
		if strings.Contains(found.Raw, it.summary) {
			t.Fatalf("a query reached the far project: %s", found.Raw)
		}
		exported, csv := member.download("/api/v1/issues/export?project=FARR")
		if exported.StatusCode != http.StatusOK || strings.Contains(string(csv), it.summary) {
			t.Fatalf("the export carried the far project: %d %s", exported.StatusCode, csv)
		}
		suggested := want(t, member.get("/api/v1/issues/suggest?q="+urlQuery("far")), http.StatusOK, "suggestions")
		if strings.Contains(suggested.Raw, it.summary) {
			t.Fatalf("the search bar offered the far project's work: %s", suggested.Raw)
		}
		names := want(t, member.get("/api/v1/issues/suggest?q="+urlQuery("project = ")+"&at=10"), http.StatusOK, "project names")
		if strings.Contains(names.Raw, "FARR") {
			t.Fatalf("the search bar named the far project: %s", names.Raw)
		}
	})

	t.Run("an id is answered for its own project", func(t *testing.T) {
		for _, attempt := range []struct {
			what string
			do   func() response
		}{
			{"edit a far sprint", func() response { return member.patch("/api/v1/sprints/"+it.sprint, map[string]any{"name": "mine now"}) }},
			{"start a far sprint", func() response { return member.post("/api/v1/sprints/"+it.sprint+"/start", nil) }},
			{"edit a far milestone", func() response {
				return member.patch("/api/v1/milestones/"+it.milestone, map[string]any{"name": "mine now"})
			}},
			{"edit a far version", func() response { return member.patch("/api/v1/versions/"+it.version, map[string]any{"name": "9.9"}) }},
			{"edit a far component", func() response {
				return member.patch("/api/v1/components/"+it.component, map[string]any{"name": "mine now"})
			}},
			{"edit a far team", func() response { return member.patch("/api/v1/teams/"+it.team, map[string]any{"name": "mine now"}) }},
			{"edit a far board", func() response { return member.patch("/api/v1/boards/"+it.board, map[string]any{"name": "mine now"}) }},
			{"share a far dashboard", func() response {
				return member.post("/api/v1/dashboards/"+it.dashboard+"/shares", map[string]any{"name": "leak"})
			}},
			{"revoke a far share", func() response {
				return member.delete("/api/v1/dashboards/" + it.dashboard + "/shares/" + it.share)
			}},
			{"edit a far field", func() response { return member.patch("/api/v1/fields/"+it.field, map[string]any{"name": "mine now"}) }},
			{"delete a far worklog", func() response { return member.delete("/api/v1/worklogs/" + it.worklog) }},
			{"delete a far attachment", func() response { return member.delete("/api/v1/attachments/" + it.attachment) }},
			{"run a far rule", func() response { return member.post("/api/v1/automation/rules/"+it.rule+"/run", nil) }},
			{"edit a far comment through a near issue", func() response {
				return member.patch("/api/v1/issues/"+nearKey+"/comments/"+it.comment, map[string]any{"body": "mine now"})
			}},
		} {
			if got := attempt.do(); got.Status != http.StatusNotFound {
				t.Errorf("%s: got %d, want 404: %s", attempt.what, got.Status, got.Raw)
			}
		}
	})

	t.Run("a body cannot name what a path may not", func(t *testing.T) {
		want(t, member.post("/api/v1/issues", map[string]any{"projectKey": "FARR", "summary": "sneak"}), http.StatusNotFound, "filing into the far project")
		bulk := member.post("/api/v1/issues/bulk", map[string]any{"keys": []string{it.issueKey}, "change": map[string]any{"priority": "high"}})
		if bulk.Status != http.StatusForbidden {
			t.Errorf("bulk editing the far project: got %d, want 403: %s", bulk.Status, bulk.Raw)
		}
		moved := member.post("/api/v1/projects/NEAR/board/move", map[string]any{"issueKey": it.issueKey, "swimlaneId": nearSwimlane(t, member)})
		if moved.Status != http.StatusNotFound {
			t.Errorf("moving a far card on a near board: got %d, want 404: %s", moved.Status, moved.Raw)
		}
		lane := member.post("/api/v1/projects/NEAR/board/move", map[string]any{"issueKey": nearKey, "swimlaneId": it.swimlane})
		if lane.Status != http.StatusNotFound {
			t.Errorf("moving a near card into a far lane: got %d, want 404: %s", lane.Status, lane.Raw)
		}
		unlinked := member.delete("/api/v1/issues/" + nearKey + "/links/" + it.link)
		if unlinked.Status != http.StatusNotFound {
			t.Errorf("removing a far link from a near issue: got %d, want 404: %s", unlinked.Status, unlinked.Raw)
		}
	})

	t.Run("a rule reaches only its own project", func(t *testing.T) {
		nearRule := idOf(t, want(t, owner.post("/api/v1/projects/NEAR/automation/rules", map[string]any{
			"name": "Near rule", "trigger": map[string]any{"kind": "incoming"},
			"actions": []any{map[string]any{"kind": "add_comment", "text": "ran"}},
		}), http.StatusCreated, "a near rule"), "rule")
		outside := owner.post("/api/v1/automation/rules/"+nearRule+"/run", map[string]any{"issueKey": it.issueKey})
		if outside.Status != http.StatusUnprocessableEntity {
			t.Errorf("running a near rule against a far issue: got %d, want 422: %s", outside.Status, outside.Raw)
		}
	})

	t.Run("every route addressed by an id is walled", func(t *testing.T) {
		ids := map[string]string{
			"projectKey": "FARR", "issueKey": it.issueKey, "sprintID": it.sprint,
			"milestoneID": it.milestone, "versionID": it.version, "componentID": it.component,
			"teamID": it.team, "boardID": it.board, "swimlaneID": it.swimlane,
			"dashboardID": it.dashboard, "shareID": it.share, "fieldID": it.field,
			"attachmentID": it.attachment, "ruleID": it.rule, "worklogID": it.worklog,
			"commentID": it.comment, "linkID": it.link,
		}
		swept := 0
		for _, route := range httpapi.Catalog() {
			if !strings.Contains(route.Path, "{") {
				continue
			}
			if route.Public || route.Binary || route.Redirect || strings.HasPrefix(route.Path, "/portal/") {
				continue
			}
			path, ok := fill(route.Path, ids)
			if !ok {
				continue
			}
			got := member.do(route.Method, "/api/v1"+path, map[string]any{})
			if got.Status >= 200 && got.Status < 300 {
				t.Errorf("%s %s answered %d for another project's %s", route.Method, route.Path, got.Status, path)
			}
			swept++
		}
		if swept < 30 {
			t.Fatalf("the sweep covered %d routes, which is too few to mean anything", swept)
		}
	})
}

// Only an owner lets an owner go, whoever else administers the organization.
func TestOnlyAnOwnerLetsAnOwnerGo(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	ownerID := principalField(t, owner.signup(t, h, "twoowners"), "principal", "user", "id").(string)

	invite := want(t, owner.post("/api/v1/invites", map[string]string{"email": h.email(t, "admin"), "role": "admin"}), http.StatusCreated, "an admin invitation")
	admin := api.client(t)
	adminID := principalField(t, want(t, admin.post("/api/v1/auth/invites/accept", map[string]string{
		"token": invite.Body["token"].(string), "name": "The Admin", "password": testPassword,
	}), http.StatusOK, "the admin joins"), "principal", "user", "id").(string)

	// A second owner, so that removing the first is refused for who is asking
	// rather than for leaving the organization without one.
	second := want(t, owner.post("/api/v1/invites", map[string]string{"email": h.email(t, "second"), "role": "member"}), http.StatusCreated, "a second invitation")
	secondClient := api.client(t)
	secondID := principalField(t, want(t, secondClient.post("/api/v1/auth/invites/accept", map[string]string{
		"token": second.Body["token"].(string), "name": "The Second Owner", "password": testPassword,
	}), http.StatusOK, "they join"), "principal", "user", "id").(string)
	if _, err := h.super.Exec(context.Background(), `UPDATE org_member SET org_role = 'owner' WHERE user_id = $1`, secondID); err != nil {
		t.Fatal(err)
	}

	refused := admin.delete("/api/v1/members/" + secondID)
	if refused.Status != http.StatusForbidden {
		t.Fatalf("an admin removed an owner: %d %s", refused.Status, refused.Raw)
	}
	want(t, owner.delete("/api/v1/members/"+secondID), http.StatusNoContent, "an owner lets another owner go")
	want(t, owner.delete("/api/v1/members/"+adminID), http.StatusNoContent, "and an admin is ordinary")
	_ = ownerID
}

// fill puts the far project's ids into a route template, and says whether the
// template named anything it does not know.
func fill(template string, ids map[string]string) (string, bool) {
	out := template
	for {
		open := strings.IndexByte(out, '{')
		if open < 0 {
			return out, true
		}
		close := strings.IndexByte(out, '}')
		if close < open {
			return out, false
		}
		id, ok := ids[out[open+1:close]]
		if !ok {
			return out, false
		}
		out = out[:open] + id + out[close+1:]
	}
}

func urlQuery(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, " ", "%20"), "=", "%3D")
}

func nearSwimlane(t *testing.T, c *client) string {
	t.Helper()
	board := obj(t, want(t, c.get("/api/v1/projects/NEAR/board"), http.StatusOK, "the near board"), "board")
	lanes := board["swimlanes"].([]any)
	return lanes[0].(map[string]any)["id"].(string)
}

// buildFar fills the far project with one of everything a route can address.
func buildFar(t *testing.T, owner *client) far {
	t.Helper()
	it := far{summary: "the far project's secret"}
	issue := obj(t, want(t, owner.post("/api/v1/projects/FARR/issues", map[string]any{"summary": it.summary}), http.StatusCreated, "a far issue"), "issue")
	it.issueKey = issue["key"].(string)
	other := obj(t, want(t, owner.post("/api/v1/projects/FARR/issues", map[string]any{"summary": "the far project's other work"}), http.StatusCreated, "another far issue"), "issue")
	it.otherKey = other["key"].(string)

	it.comment = idOf(t, want(t, owner.post("/api/v1/issues/"+it.issueKey+"/comments", map[string]any{"text": "said in confidence"}), http.StatusCreated, "a far comment"), "comment")
	it.worklog = idOf(t, want(t, owner.post("/api/v1/issues/"+it.issueKey+"/worklogs", map[string]any{"minutes": 30}), http.StatusCreated, "a far worklog"), "worklog")
	it.attachment = idOf(t, want(t, owner.upload("/api/v1/issues/"+it.issueKey+"/attachments", "file", "far.txt", "text/plain", []byte("kept here")), http.StatusCreated, "a far attachment"), "attachment")
	it.sprint = idOf(t, want(t, owner.post("/api/v1/projects/FARR/sprints", map[string]any{"name": "Far sprint"}), http.StatusCreated, "a far sprint"), "sprint")
	it.milestone = idOf(t, want(t, owner.post("/api/v1/projects/FARR/milestones", map[string]any{"name": "Far stone", "dueOn": "2026-10-01"}), http.StatusCreated, "a far milestone"), "milestone")
	it.version = idOf(t, want(t, owner.post("/api/v1/projects/FARR/versions", map[string]any{"name": "1.0"}), http.StatusCreated, "a far version"), "version")
	it.component = idOf(t, want(t, owner.post("/api/v1/projects/FARR/components", map[string]any{"name": "Far engine"}), http.StatusCreated, "a far component"), "component")
	it.team = idOf(t, want(t, owner.post("/api/v1/projects/FARR/teams", map[string]any{"name": "Far team"}), http.StatusCreated, "a far team"), "team")
	it.field = idOf(t, want(t, owner.post("/api/v1/projects/FARR/fields", map[string]any{"name": "Far cost", "kind": "number"}), http.StatusCreated, "a far field"), "field")
	it.rule = idOf(t, want(t, owner.post("/api/v1/projects/FARR/automation/rules", map[string]any{
		"name": "Far rule", "trigger": map[string]any{"kind": "incoming"},
		"actions": []any{map[string]any{"kind": "add_comment", "text": "ran"}},
	}), http.StatusCreated, "a far rule"), "rule")

	board := obj(t, want(t, owner.get("/api/v1/projects/FARR/board"), http.StatusOK, "the far board"), "board")
	it.board = board["id"].(string)
	it.swimlane = board["swimlanes"].([]any)[0].(map[string]any)["id"].(string)

	dashboards := list(t, want(t, owner.get("/api/v1/projects/FARR/dashboards"), http.StatusOK, "the far dashboards"), "dashboards")
	it.dashboard = dashboards[0].(map[string]any)["id"].(string)
	it.share = idOf(t, want(t, owner.post("/api/v1/dashboards/"+it.dashboard+"/shares", map[string]any{"name": "Far link"}), http.StatusCreated, "a far share"), "share")

	linked := want(t, owner.post("/api/v1/issues/"+it.issueKey+"/links", map[string]any{"type": "blocks", "targetKey": it.otherKey}), http.StatusCreated, "a far link")
	it.link = idOf(t, linked, "link")
	if _, err := uuid.Parse(it.link); err != nil {
		t.Fatalf("the far link has no id: %s", linked.Raw)
	}
	return it
}
