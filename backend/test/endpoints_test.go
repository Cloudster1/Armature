//go:build integration

package test

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/git"
)

// Every operation, once, the way a client would use it. The contract in
// contract_test.go checks each answer against the document and counts it;
// the coverage test at the end refuses a surface with holes. The assertions
// here are about the shape of a working day, not the fine print, which the
// tests per area cover.

// obj digs a nested object out of a decoded body.
func obj(t *testing.T, r response, keys ...string) map[string]any {
	t.Helper()
	v := principalField(t, r, keys...)
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("expected an object at %v, got %T in %s", keys, v, r.Raw)
	}
	return m
}

// list digs a list out of a decoded body.
func list(t *testing.T, r response, keys ...string) []any {
	t.Helper()
	v := principalField(t, r, keys...)
	l, ok := v.([]any)
	if !ok {
		t.Fatalf("expected a list at %v, got %T in %s", keys, v, r.Raw)
	}
	return l
}

// want fails unless the response has the status, and returns it for chaining.
func want(t *testing.T, r response, status int, what string) response {
	t.Helper()
	if r.Status != status {
		t.Fatalf("%s: got %d, want %d: %s", what, r.Status, status, r.Raw)
	}
	return r
}

func idOf(t *testing.T, r response, keys ...string) string {
	t.Helper()
	return obj(t, r, keys...)["id"].(string)
}

func TestEveryEndpointOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	signedUp := c.signup(t, h, "everything")
	me := obj(t, signedUp, "principal", "user")
	myID := me["id"].(string)
	myEmail := me["email"].(string)
	orgSlug := obj(t, signedUp, "principal", "org")["slug"].(string)

	// Health and the document need no session.
	want(t, c.get("/healthz"), http.StatusOK, "liveness")
	want(t, c.get("/readyz"), http.StatusOK, "readiness")
	want(t, c.get("/api/v1/openapi.json"), http.StatusOK, "openapi")

	// Metadata behind the pickers.
	want(t, c.get("/api/v1/members"), http.StatusOK, "members")
	types := list(t, want(t, c.get("/api/v1/issue-types"), http.StatusOK, "issue types"), "issueTypes")
	typeID := map[string]string{}
	for _, raw := range types {
		typ := raw.(map[string]any)
		typeID[typ["name"].(string)] = typ["id"].(string)
	}
	want(t, c.get("/api/v1/statuses"), http.StatusOK, "statuses")
	want(t, c.get("/api/v1/link-types"), http.StatusOK, "link types")
	want(t, c.get("/api/v1/roles"), http.StatusOK, "roles")
	want(t, c.get("/api/v1/access/me"), http.StatusOK, "my access")
	want(t, c.get("/api/v1/auth/me"), http.StatusOK, "me")

	// Projects.
	want(t, c.get("/api/v1/project-templates"), http.StatusOK, "templates")
	want(t, c.get("/api/v1/projects/key-check?name=Everything"), http.StatusOK, "key suggestion")
	want(t, c.post("/api/v1/projects", map[string]any{"name": "Everything", "key": "EVR", "template": "scrum"}), http.StatusCreated, "create project")
	want(t, c.get("/api/v1/projects"), http.StatusOK, "list projects")
	want(t, c.get("/api/v1/projects/EVR"), http.StatusOK, "get project")
	want(t, c.patch("/api/v1/projects/EVR", map[string]any{"description": "All of it."}), http.StatusOK, "edit project")
	want(t, c.post("/api/v1/projects", map[string]any{"name": "Spare", "key": "SPR"}), http.StatusCreated, "spare project")
	want(t, c.delete("/api/v1/projects/SPR"), http.StatusNoContent, "archive project")
	want(t, c.post("/api/v1/projects/SPR/restore", nil), http.StatusNoContent, "restore project")

	// Workflows and schemes.
	want(t, c.get("/api/v1/workflows/rule-types"), http.StatusOK, "rule types")
	want(t, c.post("/api/v1/statuses", map[string]any{"name": "Reviewing", "category": "in_progress"}), http.StatusCreated, "coin status")
	want(t, c.post("/api/v1/statuses", map[string]any{"name": "reviewing", "category": "todo"}), http.StatusBadRequest, "coin status twice")
	workflows := list(t, want(t, c.get("/api/v1/workflows"), http.StatusOK, "workflows"), "workflows")
	workflowID := workflows[0].(map[string]any)["id"].(string)
	got := want(t, c.get("/api/v1/workflows/"+workflowID), http.StatusOK, "get workflow")
	graph := graphOf(t, got)
	graph["name"] = "Copied by hand"
	copied := want(t, c.post("/api/v1/workflows/"+workflowID+"/copy", map[string]any{"name": "Everything copy"}), http.StatusCreated, "copy workflow")
	copyID := idOf(t, copied, "workflow")
	want(t, c.put("/api/v1/workflows/"+copyID, graph), http.StatusOK, "save workflow")
	graph["name"] = "Authored by hand"
	authored := want(t, c.post("/api/v1/workflows", graph), http.StatusCreated, "author workflow")
	authoredID := idOf(t, authored, "workflow")

	schemes := list(t, want(t, c.get("/api/v1/workflow-schemes"), http.StatusOK, "schemes"), "schemes")
	var defaultSchemeID string
	for _, raw := range schemes {
		if s := raw.(map[string]any); s["isDefault"] == true {
			defaultSchemeID = s["id"].(string)
		}
	}
	scheme := want(t, c.post("/api/v1/workflow-schemes", map[string]any{
		"name": "Everything scheme", "items": []map[string]any{{"workflowId": copyID}},
	}), http.StatusCreated, "create scheme")
	schemeID := idOf(t, scheme, "scheme")
	want(t, c.put("/api/v1/workflow-schemes/"+schemeID, map[string]any{
		"name": "Everything scheme", "items": []map[string]any{{"workflowId": copyID}, {"issueTypeId": typeID[bootstrap.TypeBug], "workflowId": authoredID}},
	}), http.StatusOK, "save scheme")
	want(t, c.put("/api/v1/projects/EVR/workflow-scheme", map[string]any{"schemeId": schemeID}), http.StatusOK, "override scheme")
	want(t, c.get("/api/v1/projects/EVR/workflows"), http.StatusOK, "project workflows")
	want(t, c.put("/api/v1/projects/EVR/workflow-scheme", map[string]any{"schemeId": nil}), http.StatusOK, "hand the scheme back")
	want(t, c.put("/api/v1/workflow-schemes/"+schemeID+"/default", nil), http.StatusNoContent, "make default")
	want(t, c.put("/api/v1/workflow-schemes/"+defaultSchemeID+"/default", nil), http.StatusNoContent, "restore default")
	want(t, c.delete("/api/v1/workflow-schemes/"+schemeID), http.StatusNoContent, "delete scheme")
	want(t, c.delete("/api/v1/workflows/"+copyID), http.StatusNoContent, "delete copied workflow")
	want(t, c.delete("/api/v1/workflows/"+authoredID), http.StatusNoContent, "delete authored workflow")

	// Issues and the hierarchy.
	epic := want(t, c.post("/api/v1/projects/EVR/issues", map[string]any{"summary": "the epic", "typeId": typeID[bootstrap.TypeEpic]}), http.StatusCreated, "epic")
	epicKey := obj(t, epic, "issue")["key"].(string)
	story := want(t, c.post("/api/v1/issues", map[string]any{"projectKey": "EVR", "summary": "the story", "typeId": typeID[bootstrap.TypeStory], "parentKey": epicKey}), http.StatusCreated, "story")
	storyKey := obj(t, story, "issue")["key"].(string)
	task := want(t, c.post("/api/v1/projects/EVR/issues", map[string]any{"summary": "the task", "estimate": 3}), http.StatusCreated, "task")
	taskKey := obj(t, task, "issue")["key"].(string)
	want(t, c.get("/api/v1/issues?project=EVR&category=todo&orderBy=created"), http.StatusOK, "search issues")
	want(t, c.get("/api/v1/issues?q="+url.QueryEscape("statusCategory != done AND reporter = currentUser() ORDER BY priority DESC, key")), http.StatusOK, "search issues by query")
	want(t, c.get("/api/v1/projects/EVR/issues?text=the"), http.StatusOK, "project issues")
	want(t, c.get("/api/v1/projects/EVR/issues?q="+url.QueryEscape(`type = Story AND "Customer" IS EMPTY`)), http.StatusOK, "project issues by query")
	badQuery := want(t, c.get("/api/v1/projects/EVR/issues?q="+url.QueryEscape("assigne = currentUser()")), http.StatusBadRequest, "bad query")
	if badQuery.ErrorCode() != "bad_query" || badQuery.Error()["position"] != float64(1) {
		t.Errorf("a bad query answered %v", badQuery.Error())
	}
	want(t, c.get("/api/v1/issues/"+storyKey), http.StatusOK, "get issue")
	want(t, c.patch("/api/v1/issues/"+taskKey, map[string]any{"priority": "high"}), http.StatusOK, "edit issue")
	want(t, c.put("/api/v1/issues/"+taskKey+"/parent", map[string]any{"parentKey": epicKey}), http.StatusOK, "set parent")
	want(t, c.get("/api/v1/issues/"+epicKey+"/children"), http.StatusOK, "children")
	want(t, c.get("/api/v1/issues/"+storyKey+"/hierarchy"), http.StatusOK, "hierarchy")
	want(t, c.get("/api/v1/projects/EVR/hierarchy"), http.StatusOK, "project tree")
	want(t, c.put("/api/v1/issues/"+storyKey+"/schedule", map[string]any{"startDate": "2026-09-01", "dueDate": "2026-09-14"}), http.StatusOK, "schedule")
	want(t, c.get("/api/v1/projects/EVR/plan"), http.StatusOK, "plan")
	matched := list(t, want(t, c.get("/api/v1/projects/EVR/plan?q="+url.QueryEscape("key = "+storyKey)), http.StatusOK, "plan by query"), "matched")
	if len(matched) != 1 || matched[0] != storyKey {
		t.Errorf("the plan matched %v, want %s", matched, storyKey)
	}
	want(t, c.get("/api/v1/projects/EVR/plan?q=nonsense"), http.StatusBadRequest, "plan with a bad query")
	link := want(t, c.post("/api/v1/issues/"+storyKey+"/links", map[string]any{"type": "blocks", "targetKey": taskKey}), http.StatusCreated, "link")
	want(t, c.post("/api/v1/issues/"+taskKey+"/links", map[string]any{"type": "blocks", "targetKey": storyKey}), http.StatusConflict, "a link back around")
	dependencies := list(t, want(t, c.get("/api/v1/projects/EVR/plan"), http.StatusOK, "plan with a dependency"), "dependencies")
	if len(dependencies) != 1 || dependencies[0].(map[string]any)["linkId"] != idOf(t, link, "link") {
		t.Errorf("the plan's dependency does not name its link: %v", dependencies)
	}
	want(t, c.get("/api/v1/issues/"+storyKey+"/links"), http.StatusOK, "links")
	want(t, c.delete("/api/v1/issues/"+storyKey+"/links/"+idOf(t, link, "link")), http.StatusNoContent, "unlink")
	transitions := list(t, want(t, c.get("/api/v1/issues/"+taskKey+"/transitions"), http.StatusOK, "transitions"), "transitions")
	want(t, c.post("/api/v1/issues/"+taskKey+"/transitions", map[string]any{"transitionId": transitions[0].(map[string]any)["id"], "comment": "moving along"}), http.StatusOK, "transition")
	comment := want(t, c.post("/api/v1/issues/"+taskKey+"/comments", map[string]any{"text": "a remark"}), http.StatusCreated, "comment")
	commentID := idOf(t, comment, "comment")
	want(t, c.patch("/api/v1/issues/"+taskKey+"/comments/"+commentID, map[string]any{"text": "a better remark"}), http.StatusOK, "edit comment")
	want(t, c.get("/api/v1/issues/"+taskKey+"/comments"), http.StatusOK, "comments")
	want(t, c.delete("/api/v1/issues/"+taskKey+"/comments/"+commentID), http.StatusNoContent, "delete comment")
	want(t, c.get("/api/v1/issues/"+taskKey+"/history"), http.StatusOK, "history")
	want(t, c.put("/api/v1/issues/"+storyKey+"/estimate", map[string]any{"estimate": 5}), http.StatusOK, "estimate")

	// Teams and sprints.
	team := want(t, c.post("/api/v1/projects/EVR/teams", map[string]any{"name": "Platform"}), http.StatusCreated, "team")
	teamID := idOf(t, team, "team")
	want(t, c.get("/api/v1/projects/EVR/teams"), http.StatusOK, "teams")
	want(t, c.get("/api/v1/teams/"+teamID), http.StatusOK, "get team")
	want(t, c.patch("/api/v1/teams/"+teamID, map[string]any{"name": "Platform", "description": "Runs the platform.", "weeklyCapacity": 12}), http.StatusOK, "edit team")
	if got := want(t, c.get("/api/v1/teams/"+teamID), http.StatusOK, "get team again").Body["team"].(map[string]any)["weeklyCapacity"]; got != 12.0 {
		t.Errorf("weekly capacity over the api = %v, want 12", got)
	}
	want(t, c.post("/api/v1/teams/"+teamID+"/members", map[string]any{"userId": myID, "lead": true}), http.StatusOK, "join team")
	want(t, c.put("/api/v1/issues/"+storyKey+"/team", map[string]any{"teamId": teamID}), http.StatusOK, "hand to team")
	from, to := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"), time.Now().UTC().AddDate(0, 0, 13).Format("2006-01-02")
	sprint := want(t, c.post("/api/v1/projects/EVR/sprints", map[string]any{"name": "Sprint 1", "teamId": teamID, "startsOn": from, "endsOn": to}), http.StatusCreated, "sprint")
	sprintID := idOf(t, sprint, "sprint")
	want(t, c.patch("/api/v1/sprints/"+sprintID, map[string]any{"name": "Sprint 1", "goal": "ship", "capacity": 20}), http.StatusOK, "edit sprint")
	want(t, c.put("/api/v1/issues/"+storyKey+"/sprint", map[string]any{"sprintId": sprintID}), http.StatusOK, "commit to sprint")
	want(t, c.post("/api/v1/projects/EVR/issues", map[string]any{"summary": "filed straight into the sprint", "sprintId": sprintID, "estimate": 2}), http.StatusCreated, "file into sprint")
	want(t, c.get("/api/v1/projects/EVR/sprints?team="+teamID), http.StatusOK, "sprints")
	want(t, c.get("/api/v1/sprints/"+sprintID+"/board"), http.StatusOK, "sprint board")
	want(t, c.post("/api/v1/sprints/"+sprintID+"/start", nil), http.StatusOK, "start sprint")
	spare := want(t, c.post("/api/v1/projects/EVR/sprints", map[string]any{"name": "Sprint 2", "teamId": teamID}), http.StatusCreated, "spare sprint")
	want(t, c.post("/api/v1/sprints/"+sprintID+"/complete", map[string]any{"moveTo": nil}), http.StatusOK, "complete sprint")
	want(t, c.get("/api/v1/projects/EVR/sprints?closed=true"), http.StatusOK, "closed sprints")
	want(t, c.delete("/api/v1/sprints/"+idOf(t, spare, "sprint")), http.StatusNoContent, "delete sprint")

	// Milestones.
	milestone := want(t, c.post("/api/v1/projects/EVR/milestones", map[string]any{"name": "Release 1", "dueOn": to}), http.StatusCreated, "milestone")
	milestoneID := idOf(t, milestone, "milestone")
	want(t, c.patch("/api/v1/milestones/"+milestoneID, map[string]any{"name": "Release 1", "description": "The first cut."}), http.StatusOK, "edit milestone")
	want(t, c.put("/api/v1/issues/"+storyKey+"/milestone", map[string]any{"milestoneId": milestoneID}), http.StatusOK, "count towards milestone")
	want(t, c.get("/api/v1/projects/EVR/issues?milestone="+milestoneID), http.StatusOK, "issues of a milestone")
	want(t, c.get("/api/v1/projects/EVR/milestones?closed=true"), http.StatusOK, "milestones")
	want(t, c.post("/api/v1/milestones/"+milestoneID+"/close", nil), http.StatusOK, "close milestone")
	want(t, c.post("/api/v1/milestones/"+milestoneID+"/reopen", nil), http.StatusOK, "reopen milestone")
	want(t, c.delete("/api/v1/milestones/"+milestoneID), http.StatusNoContent, "delete milestone")

	// Boards.
	want(t, c.get("/api/v1/projects/EVR/board"), http.StatusOK, "opening board")
	want(t, c.patch("/api/v1/projects/EVR/board", map[string]any{"groupBy": "assignee"}), http.StatusNoContent, "group board")
	boards := list(t, want(t, c.get("/api/v1/projects/EVR/boards"), http.StatusOK, "boards"), "boards")
	extra := want(t, c.post("/api/v1/projects/EVR/boards", map[string]any{"name": "Everything kanban", "type": "kanban"}), http.StatusCreated, "add board")
	extraID := idOf(t, extra, "board")
	want(t, c.get("/api/v1/boards/"+extraID), http.StatusOK, "get board")
	want(t, c.patch("/api/v1/boards/"+extraID, map[string]any{"name": "Everything kanban", "description": "all cards"}), http.StatusOK, "edit board")
	want(t, c.get("/api/v1/boards/"+extraID+"/backlog"), http.StatusOK, "backlog")
	// Every status already has a swimlane on a fresh board, so the new one
	// starts empty and is a place to drag states into later.
	lane := want(t, c.post("/api/v1/projects/EVR/board/swimlanes", map[string]any{"name": "Parked"}), http.StatusCreated, "add swimlane")
	laneID := idOf(t, lane, "swimlane")
	want(t, c.patch("/api/v1/projects/EVR/board/swimlanes/"+laneID, map[string]any{"name": "Parked for now"}), http.StatusNoContent, "edit swimlane")
	openingLanes := list(t, c.get("/api/v1/projects/EVR/board"), "board", "swimlanes")
	order := []any{}
	for _, raw := range openingLanes {
		order = append(order, raw.(map[string]any)["id"])
	}
	want(t, c.put("/api/v1/projects/EVR/board/swimlanes", map[string]any{"order": order}), http.StatusNoContent, "reorder swimlanes")
	var inProgress any
	for _, raw := range openingLanes {
		if lane := raw.(map[string]any); strings.Contains(lane["name"].(string), "Progress") {
			inProgress = lane["id"]
		}
	}
	want(t, c.post("/api/v1/projects/EVR/board/move", map[string]any{"issueKey": storyKey, "swimlaneId": inProgress}), http.StatusOK, "move card")
	want(t, c.delete("/api/v1/projects/EVR/board/swimlanes/"+laneID), http.StatusNoContent, "delete swimlane")
	want(t, c.delete("/api/v1/boards/"+extraID), http.StatusNoContent, "delete board")
	_ = boards

	// Dashboards.
	want(t, c.get("/api/v1/projects/EVR/dashboards"), http.StatusOK, "dashboards")
	dashboard := want(t, c.post("/api/v1/projects/EVR/dashboards", map[string]any{"name": "Second"}), http.StatusCreated, "add dashboard")
	dashboardID := idOf(t, dashboard, "dashboard")
	want(t, c.get("/api/v1/dashboard-templates?project=EVR"), http.StatusOK, "dashboard templates")
	want(t, c.get("/api/v1/dashboard-templates?project=NOPE"), http.StatusNotFound, "templates for a project that is not there")
	template := want(t, c.post("/api/v1/dashboards/"+dashboardID+"/template", map[string]any{"name": "Mondays", "description": "What we look at."}), http.StatusCreated, "save a template")
	templateID := idOf(t, template, "template")
	want(t, c.post("/api/v1/dashboards/"+dashboardID+"/template", map[string]any{"name": "Mondays"}), http.StatusConflict, "the same template name again")
	fromTemplate := want(t, c.post("/api/v1/projects/EVR/dashboards", map[string]any{"name": "From a template", "template": templateID}), http.StatusCreated, "a dashboard from a template")
	want(t, c.post("/api/v1/projects/EVR/dashboards", map[string]any{"name": "From nothing", "template": "nope"}), http.StatusBadRequest, "a template nobody has")
	aimedAt := idOf(t, want(t, c.post("/api/v1/projects/EVR/milestones", map[string]any{"name": "Release 2"}), http.StatusCreated, "a milestone to aim a dashboard at"), "milestone")
	forMilestone := want(t, c.post("/api/v1/projects/EVR/dashboards", map[string]any{"name": "Release 2 dashboard", "template": "milestone", "milestoneId": aimedAt}), http.StatusCreated, "a milestone's dashboard")
	want(t, c.delete("/api/v1/dashboards/"+idOf(t, forMilestone, "dashboard")), http.StatusNoContent, "delete the milestone's dashboard")
	want(t, c.post("/api/v1/projects/EVR/dashboards", map[string]any{"name": "Nobody's", "template": "milestone", "milestoneId": uuid.New().String()}), http.StatusNotFound, "a milestone that is not there")
	want(t, c.delete("/api/v1/dashboards/"+idOf(t, fromTemplate, "dashboard")), http.StatusNoContent, "delete the templated dashboard")
	want(t, c.delete("/api/v1/dashboard-templates/"+templateID), http.StatusNoContent, "delete the template")
	want(t, c.delete("/api/v1/dashboard-templates/"+templateID), http.StatusNotFound, "delete it again")
	want(t, c.patch("/api/v1/dashboards/"+dashboardID, map[string]any{"name": "Second look"}), http.StatusNoContent, "rename dashboard")
	want(t, c.get("/api/v1/projects/EVR/report-kinds"), http.StatusOK, "report kinds")
	for _, kind := range []string{"status_breakdown", "throughput", "workload", "team_workload", "cycle_time", "epics", "sprint", "velocity", "burndown", "sprint_history", "priority_breakdown", "type_breakdown"} {
		want(t, c.get("/api/v1/projects/EVR/reports/"+kind+"?days=30"), http.StatusOK, "report "+kind)
	}
	want(t, c.get("/api/v1/projects/EVR/reports/burndown?sprint="+sprintID), http.StatusOK, "one sprint's burndown")
	want(t, c.get("/api/v1/projects/EVR/reports/chart?groupBy=type&shape=bar"), http.StatusOK, "a chart")
	want(t, c.get("/api/v1/projects/EVR/reports/milestones"), http.StatusOK, "the milestones widget")
	want(t, c.get("/api/v1/projects/EVR/reports/milestones?milestone="+milestoneID), http.StatusOK, "one milestone's progress")
	want(t, c.get("/api/v1/projects/EVR/reports/milestones?milestone=nope"), http.StatusBadRequest, "a milestone id that is not one")
	chart := want(t, c.post("/api/v1/dashboards/"+dashboardID+"/widgets", map[string]any{"kind": "chart", "config": map[string]any{"groupBy": "type", "shape": "donut"}}), http.StatusCreated, "add a chart")
	want(t, c.delete("/api/v1/widgets/"+idOf(t, chart, "widget")), http.StatusNoContent, "remove the chart")
	filter := want(t, c.post("/api/v1/dashboards/"+dashboardID+"/widgets", map[string]any{"kind": "filter", "config": map[string]any{"filter": map[string]any{"types": []string{"Bug"}}}}), http.StatusCreated, "add the filter tile")
	want(t, c.delete("/api/v1/widgets/"+idOf(t, filter, "widget")), http.StatusNoContent, "remove the filter tile")
	widget := want(t, c.post("/api/v1/dashboards/"+dashboardID+"/widgets", map[string]any{"kind": "cycle_time"}), http.StatusCreated, "add widget")
	widgetID := idOf(t, widget, "widget")
	want(t, c.patch("/api/v1/widgets/"+widgetID, map[string]any{"kind": "cycle_time", "width": 2, "config": map[string]any{"days": 60}}), http.StatusOK, "edit widget")
	want(t, c.put("/api/v1/dashboards/"+dashboardID+"/widgets", map[string]any{"order": []any{widgetID}}), http.StatusNoContent, "reorder widgets")
	want(t, c.delete("/api/v1/widgets/"+widgetID), http.StatusNoContent, "remove widget")
	want(t, c.delete("/api/v1/dashboards/"+dashboardID), http.StatusNoContent, "delete dashboard")

	// Custom fields.
	want(t, c.get("/api/v1/field-kinds"), http.StatusOK, "field kinds")
	field := want(t, c.post("/api/v1/projects/EVR/fields", map[string]any{"name": "Customer", "kind": "select", "options": []string{"Acme", "Globex"}}), http.StatusCreated, "define field")
	fieldID := idOf(t, field, "field")
	want(t, c.get("/api/v1/projects/EVR/fields"), http.StatusOK, "fields")
	want(t, c.patch("/api/v1/fields/"+fieldID, map[string]any{"options": []string{"Acme", "Globex", "Initech"}}), http.StatusOK, "edit field")
	want(t, c.put("/api/v1/issues/"+taskKey+"/fields/"+fieldID, map[string]any{"value": "Globex"}), http.StatusOK, "answer field")
	want(t, c.get("/api/v1/issues/"+taskKey+"/fields"), http.StatusOK, "issue fields")
	want(t, c.delete("/api/v1/fields/"+fieldID), http.StatusNoContent, "delete field")

	// Groups, roles, invitations and tokens.
	group := want(t, c.post("/api/v1/groups", map[string]any{"name": "Platform people"}), http.StatusCreated, "group")
	groupID := idOf(t, group, "group")
	want(t, c.get("/api/v1/groups"), http.StatusOK, "groups")
	want(t, c.post("/api/v1/groups/"+groupID+"/members", map[string]any{"userId": myID}), http.StatusOK, "join group")
	want(t, c.get("/api/v1/groups/"+groupID), http.StatusOK, "get group")
	grant := want(t, c.post("/api/v1/role-assignments", map[string]any{"role": "scrum_master", "projectKey": "EVR", "groupId": groupID}), http.StatusCreated, "grant")
	want(t, c.get("/api/v1/role-assignments?project=EVR"), http.StatusOK, "assignments")
	want(t, c.delete("/api/v1/role-assignments/"+idOf(t, grant, "assignment")), http.StatusNoContent, "revoke")
	want(t, c.delete("/api/v1/groups/"+groupID+"/members/"+myID), http.StatusOK, "leave group")
	want(t, c.delete("/api/v1/groups/"+groupID), http.StatusNoContent, "delete group")

	token := want(t, c.post("/api/v1/tokens", map[string]any{"name": "script"}), http.StatusCreated, "token")
	secret := obj(t, token, "token")["secret"].(string)
	want(t, c.get("/api/v1/tokens"), http.StatusOK, "tokens")
	bearer := api.client(t)
	bearer.bearer = secret
	want(t, bearer.get("/api/v1/auth/me"), http.StatusOK, "me by token")
	want(t, c.delete("/api/v1/tokens/"+idOf(t, token, "token")), http.StatusNoContent, "revoke token")

	// Somebody else joins: by invitation, from an account of their own, and
	// can then switch between the two organizations.
	other := api.client(t)
	other.signup(t, h, "elsewhere")
	invited := want(t, c.post("/api/v1/invites", map[string]any{"email": principalField(t, c.get("/api/v1/auth/me"), "principal", "user", "email").(string) + ".other", "role": "member"}), http.StatusCreated, "invite")
	_ = invited
	want(t, c.get("/api/v1/invites"), http.StatusOK, "invites")
	want(t, c.delete("/api/v1/invites/"+idOf(t, invited, "invite")), http.StatusNoContent, "revoke invite")
	otherEmail := principalField(t, other.get("/api/v1/auth/me"), "principal", "user", "email").(string)
	forOther := want(t, c.post("/api/v1/invites", map[string]any{"email": otherEmail, "role": "member"}), http.StatusCreated, "invite the other")
	want(t, other.post("/api/v1/auth/invites/accept", map[string]any{"token": forOther.Body["token"]}), http.StatusOK, "accept invite")
	want(t, other.post("/api/v1/auth/switch-org", map[string]any{"slug": orgSlug}), http.StatusOK, "switch org")
	want(t, other.get("/api/v1/projects/EVR"), http.StatusOK, "the other sees the project")
	want(t, other.post("/api/v1/auth/logout", nil), http.StatusNoContent, "logout")

	// Repositories, the webhook, and the branch made on the host.
	stub := newStubHost(t)
	repo := want(t, c.post("/api/v1/projects/EVR/repositories", map[string]any{
		"host": "github", "name": "acme/everything", "apiBaseUrl": stub.URL, "accessToken": "ghp_test",
	}), http.StatusCreated, "connect repository")
	repoID := idOf(t, repo, "repository")
	webhookSecret := obj(t, repo, "repository")["webhookSecret"].(string)
	want(t, c.get("/api/v1/projects/EVR/repositories"), http.StatusOK, "repositories")
	want(t, c.patch("/api/v1/repositories/"+repoID, map[string]any{"host": "github", "name": "acme/everything", "defaultBranch": "trunk"}), http.StatusOK, "edit repository")
	body := push("trunk", "ab100002", storyKey+" wires it up", "nobody@example.test")
	req, _ := http.NewRequest(http.MethodPost, api.URL+"/api/v1/git/webhooks/"+repoID, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", git.SignGitHub([]byte(body), webhookSecret))
	if resp, raw := c.raw(req); resp.StatusCode != http.StatusOK {
		t.Fatalf("webhook: %d %s", resp.StatusCode, raw)
	}
	want(t, c.get("/api/v1/issues/"+storyKey+"/development"), http.StatusOK, "development")
	want(t, c.post("/api/v1/issues/"+storyKey+"/branches", map[string]any{"repositoryId": repoID}), http.StatusCreated, "branch")
	rotated := want(t, c.post("/api/v1/repositories/"+repoID+"/rotate-secret", nil), http.StatusOK, "rotate secret")
	_ = rotated
	want(t, c.delete("/api/v1/repositories/"+repoID), http.StatusNoContent, "disconnect")

	// The identity provider: configured, started, and the browser sent back.
	provider := newIDP(t)
	want(t, c.put("/api/v1/oidc-provider", map[string]any{
		"issuer": provider.URL, "clientId": "armature-test", "clientSecret": "shh", "groupsClaim": "groups",
		"scopes": "openid profile email", "createGroups": false, "enabled": true,
	}), http.StatusOK, "configure provider")
	want(t, c.get("/api/v1/oidc-provider"), http.StatusOK, "get provider")
	browser := api.client(t).noRedirects()
	startReq, _ := http.NewRequest(http.MethodGet, api.URL+"/api/v1/auth/oidc/"+orgSlug+"/start?next=/projects", nil)
	startResp, _ := browser.raw(startReq)
	if startResp.StatusCode != http.StatusFound {
		t.Fatalf("oidc start: %d", startResp.StatusCode)
	}
	state, nonce := stateFrom(t, startResp.Header.Get("Location"))
	provider.says("armature-test", map[string]any{"nonce": nonce, "email": myEmail, "sub": "subject-everything"})
	callback, _ := http.NewRequest(http.MethodGet, api.URL+"/api/v1/auth/oidc/callback?"+url.Values{"state": {state}, "code": {"any-code"}}.Encode(), nil)
	callbackResp, raw := browser.raw(callback)
	if callbackResp.StatusCode != http.StatusFound {
		t.Fatalf("oidc callback: %d %s", callbackResp.StatusCode, raw)
	}
	if !strings.HasSuffix(callbackResp.Header.Get("Location"), "/projects") {
		t.Errorf("after the callback the browser goes to %q", callbackResp.Header.Get("Location"))
	}

	// Team clean-up: a team carrying work or a board cannot go, so first the
	// work is taken back and the board removed.
	want(t, c.put("/api/v1/issues/"+storyKey+"/team", map[string]any{"teamId": nil}), http.StatusOK, "take work back")
	for _, raw := range list(t, c.get("/api/v1/projects/EVR/boards"), "boards") {
		if b := raw.(map[string]any); b["teamId"] == teamID {
			want(t, c.delete("/api/v1/boards/"+b["id"].(string)), http.StatusNoContent, "delete team board")
		}
	}
	want(t, c.delete("/api/v1/teams/"+teamID+"/members/"+myID), http.StatusOK, "leave team")
	want(t, c.delete("/api/v1/teams/"+teamID), http.StatusNoContent, "delete team")

	// And the issue itself, which only an administrator may do.
	want(t, c.delete("/api/v1/issues/"+taskKey), http.StatusNoContent, "delete issue")
}

// graphOf turns a workflow as the API shows it back into the shape the editor
// sends: statuses per step, and transitions by status rather than by step.
func graphOf(t *testing.T, got response) map[string]any {
	t.Helper()
	wf := obj(t, got, "workflow")
	statusOfStep := map[string]string{}
	steps := []map[string]any{}
	for _, raw := range wf["steps"].([]any) {
		step := raw.(map[string]any)
		statusID := step["status"].(map[string]any)["id"].(string)
		statusOfStep[step["id"].(string)] = statusID
		steps = append(steps, map[string]any{"statusId": statusID, "isInitial": step["isInitial"]})
	}
	transitions := []map[string]any{}
	for _, raw := range wf["transitions"].([]any) {
		tr := raw.(map[string]any)
		item := map[string]any{"name": tr["name"], "description": tr["description"], "toStatusId": statusOfStep[tr["toStepId"].(string)]}
		if from, ok := tr["fromStepId"].(string); ok {
			item["fromStatusId"] = statusOfStep[from]
		}
		transitions = append(transitions, item)
	}
	return map[string]any{"name": wf["name"], "description": wf["description"], "steps": steps, "transitions": transitions}
}

// The service desk, end to end: the agent's side and the customer's.
func TestTheDeskOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	agent := api.client(t)
	agent.signup(t, h, "deskapi")
	want(t, agent.post("/api/v1/projects", map[string]any{"name": "Help", "key": "HLD", "template": "service-desk"}), http.StatusCreated, "desk")

	types := list(t, want(t, agent.get("/api/v1/projects/HLD/request-types"), http.StatusOK, "request types"), "requestTypes")
	issueTypes := list(t, agent.get("/api/v1/issue-types"), "issueTypes")
	made := want(t, agent.post("/api/v1/projects/HLD/request-types", map[string]any{
		"name": "Something else", "description": "Anything not listed.", "issueTypeId": issueTypes[0].(map[string]any)["id"], "priority": "low",
	}), http.StatusCreated, "add request type")
	want(t, agent.patch("/api/v1/request-types/"+idOf(t, made, "requestType"), map[string]any{"category": "Hardware", "detailsTemplate": "Device:\n"}), http.StatusOK, "edit request type")
	want(t, agent.patch("/api/v1/request-types/"+uuid.NewString(), map[string]any{"category": "Hardware"}), http.StatusNotFound, "edit a request type that is not there")
	want(t, agent.delete("/api/v1/request-types/"+idOf(t, made, "requestType")), http.StatusNoContent, "remove request type")
	policies := list(t, want(t, agent.get("/api/v1/projects/HLD/sla-policies"), http.StatusOK, "policies"), "policies")
	want(t, agent.patch("/api/v1/sla-policies/"+policies[0].(map[string]any)["id"].(string), map[string]any{"goals": map[string]int{"highest": 30, "high": 60, "medium": 240, "low": 480, "lowest": 960}}), http.StatusOK, "edit policy")

	// A customer is invited, joins, and raises a request through the portal.
	customerEmail := h.email(t, "customer")
	invited := want(t, agent.post("/api/v1/invites", map[string]any{"email": customerEmail, "role": "customer"}), http.StatusCreated, "invite customer")
	customer := api.client(t)
	want(t, customer.post("/api/v1/auth/invites/accept", map[string]any{"token": invited.Body["token"], "name": "Sam Customer", "password": testPassword}), http.StatusOK, "customer joins")
	desks := list(t, want(t, customer.get("/api/v1/portal/desks"), http.StatusOK, "desks"), "desks")
	if len(desks) == 0 {
		t.Fatal("the customer sees no desk")
	}
	raised := want(t, customer.post("/api/v1/portal/requests", map[string]any{
		"requestTypeId": types[0].(map[string]any)["id"], "summary": "The printer is on fire", "description": "Literally.",
	}), http.StatusCreated, "raise")
	key := obj(t, raised, "request")["key"].(string)
	want(t, customer.get("/api/v1/portal/requests"), http.StatusOK, "my requests")
	want(t, customer.get("/api/v1/portal/requests/"+key), http.StatusOK, "my request")
	want(t, customer.post("/api/v1/portal/requests/"+key+"/replies", map[string]any{"text": "Still burning."}), http.StatusCreated, "reply")

	// The agent answers, with a note the customer never sees.
	want(t, agent.get("/api/v1/projects/HLD/queue?filter=open"), http.StatusOK, "queue")
	want(t, agent.get("/api/v1/issues/"+key+"/timers"), http.StatusOK, "timers")
	want(t, agent.post("/api/v1/issues/"+key+"/notes", map[string]any{"text": "Known building. Send the big extinguisher."}), http.StatusCreated, "note")
	want(t, agent.post("/api/v1/issues/"+key+"/comments", map[string]any{"text": "Somebody is on the way."}), http.StatusCreated, "answer")
	if body := customer.get("/api/v1/portal/requests/" + key); strings.Contains(body.Raw, "big extinguisher") {
		t.Fatal("the customer was shown the internal note")
	}

	// And a customer is refused everywhere outside the portal.
	want(t, customer.get("/api/v1/projects"), http.StatusForbidden, "customer on the agent side")
}

// Nothing behind the session is reachable without one: every authenticated
// operation, called anonymously with placeholder parameters, answers 401.
func TestEveryEndpointRefusesAnAnonymousCaller(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	anonymous := api.client(t)
	placeholders := strings.NewReplacer(
		"{projectKey}", "ZZZ", "{issueKey}", "ZZZ-1", "{kind}", "status_breakdown", "{orgSlug}", "nobody",
	)
	for _, r := range apiContract.routes {
		if r.Public {
			continue
		}
		path := placeholders.Replace(r.Path)
		for strings.Contains(path, "{") {
			open, close := strings.IndexByte(path, '{'), strings.IndexByte(path, '}')
			path = path[:open] + "00000000-0000-0000-0000-000000000000" + path[close+1:]
		}
		var body any
		if r.Method != http.MethodGet && r.Method != http.MethodDelete {
			body = map[string]any{}
		}
		if resp := anonymous.do(r.Method, "/api/v1"+path, body); resp.Status != http.StatusUnauthorized {
			t.Errorf("%s %s answered %d to nobody, want 401", r.Method, r.Path, resp.Status)
		}
	}
}

// The public entry points refuse what they should: a malformed sign-up, a
// dead invitation, an unsigned webhook, a sign-in for no organization, and a
// callback for a sign-in that never started.
func TestThePublicEntryPointsRefuseBadInput(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t).noRedirects()

	want(t, c.post("/api/v1/auth/signup", map[string]any{"email": "not an address"}), http.StatusUnprocessableEntity, "malformed signup")
	want(t, c.post("/api/v1/auth/invites/accept", map[string]any{"token": "expired-or-made-up"}), http.StatusGone, "dead invitation")
	req, _ := http.NewRequest(http.MethodPost, api.URL+"/api/v1/git/webhooks/00000000-0000-0000-0000-000000000000", strings.NewReader(`{}`))
	req.Header.Set("X-GitHub-Event", "push")
	if resp, _ := c.raw(req); resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("an unsigned webhook for no repository answered %d", resp.StatusCode)
	}
	start, _ := http.NewRequest(http.MethodGet, api.URL+"/api/v1/auth/oidc/no-such-org/start", nil)
	if resp, _ := c.raw(start); resp.StatusCode != http.StatusNotFound {
		t.Errorf("sign-in for no organization answered %d, want 404", resp.StatusCode)
	}
	callback, _ := http.NewRequest(http.MethodGet, api.URL+"/api/v1/auth/oidc/callback?state=made-up&code=x", nil)
	if resp, _ := c.raw(callback); resp.StatusCode/100 != 4 && resp.StatusCode != http.StatusFound {
		t.Errorf("a callback for no sign-in answered %d", resp.StatusCode)
	}
}
