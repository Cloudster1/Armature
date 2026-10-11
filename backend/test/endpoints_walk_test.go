//go:build integration

package test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/git"
)

// The stretches of TestEveryEndpointOverTheAPI that need nothing from the
// rest of the walk but the client and a few keys.

func walkWorkflows(t *testing.T, c *client, typeID map[string]string) {
	t.Helper()
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
}

func walkBoards(t *testing.T, c *client, storyKey string) {
	t.Helper()
	want(t, c.get("/api/v1/projects/EVR/board"), http.StatusOK, "opening board")
	list(t, want(t, c.get("/api/v1/projects/EVR/boards"), http.StatusOK, "boards"), "boards")
	extra := want(t, c.post("/api/v1/projects/EVR/boards", map[string]any{"name": "Everything kanban", "type": "kanban"}), http.StatusCreated, "add board")
	extraID := idOf(t, extra, "board")
	want(t, c.get("/api/v1/boards/"+extraID), http.StatusOK, "get board")
	want(t, c.patch("/api/v1/boards/"+extraID, map[string]any{"name": "Everything kanban", "description": "all cards", "groupBy": "assignee"}), http.StatusOK, "edit board")
	want(t, c.get("/api/v1/boards/"+extraID+"/backlog"), http.StatusOK, "backlog")
	// Every status already has a swimlane on a fresh board, so the new one
	// starts empty and is a place to drag states into later.
	lane := want(t, c.post("/api/v1/boards/"+extraID+"/swimlanes", map[string]any{"name": "Parked"}), http.StatusCreated, "add swimlane")
	laneID := idOf(t, lane, "swimlane")
	want(t, c.patch("/api/v1/boards/"+extraID+"/swimlanes/"+laneID, map[string]any{"name": "Parked for now"}), http.StatusNoContent, "edit swimlane")
	order := []any{}
	for _, raw := range list(t, c.get("/api/v1/boards/"+extraID), "board", "swimlanes") {
		order = append(order, raw.(map[string]any)["id"])
	}
	want(t, c.put("/api/v1/boards/"+extraID+"/swimlanes", map[string]any{"order": order}), http.StatusNoContent, "reorder swimlanes")
	var inProgress any
	for _, raw := range list(t, c.get("/api/v1/projects/EVR/board"), "board", "swimlanes") {
		if lane := raw.(map[string]any); strings.Contains(lane["name"].(string), "Progress") {
			inProgress = lane["id"]
		}
	}
	want(t, c.post("/api/v1/projects/EVR/board/move", map[string]any{"issueKey": storyKey, "swimlaneId": inProgress}), http.StatusOK, "move card")
	want(t, c.delete("/api/v1/boards/"+extraID+"/swimlanes/"+laneID), http.StatusNoContent, "delete swimlane")
	want(t, c.delete("/api/v1/boards/"+extraID), http.StatusNoContent, "delete board")
}

func walkDashboards(t *testing.T, c *client, sprintID, milestoneID string) {
	t.Helper()
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
}

func walkFieldsGroupsAndTokens(t *testing.T, api *apiServer, c *client, taskKey, myID string) {
	t.Helper()
	want(t, c.get("/api/v1/field-kinds"), http.StatusOK, "field kinds")
	field := want(t, c.post("/api/v1/projects/EVR/fields", map[string]any{"name": "Customer", "kind": "select", "options": []string{"Acme", "Globex"}}), http.StatusCreated, "define field")
	fieldID := idOf(t, field, "field")
	want(t, c.get("/api/v1/projects/EVR/fields"), http.StatusOK, "fields")
	want(t, c.patch("/api/v1/fields/"+fieldID, map[string]any{"options": []string{"Acme", "Globex", "Initech"}}), http.StatusOK, "edit field")
	want(t, c.put("/api/v1/issues/"+taskKey+"/fields/"+fieldID, map[string]any{"value": "Globex"}), http.StatusOK, "answer field")
	want(t, c.get("/api/v1/issues/"+taskKey+"/fields"), http.StatusOK, "issue fields")
	want(t, c.delete("/api/v1/fields/"+fieldID), http.StatusNoContent, "delete field")

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
}

// walkRepositories connects a repository, takes a signed push on its webhook
// and makes a branch on the host.
func walkRepositories(t *testing.T, api *apiServer, c *client, storyKey string) {
	t.Helper()
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
	want(t, c.post("/api/v1/repositories/"+repoID+"/rotate-secret", nil), http.StatusOK, "rotate secret")
	want(t, c.delete("/api/v1/repositories/"+repoID), http.StatusNoContent, "disconnect")
}
