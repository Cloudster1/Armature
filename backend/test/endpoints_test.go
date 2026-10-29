//go:build integration

package test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/bootstrap"
)

// Every operation, once, the way a client would use it; contract_test.go
// checks and counts each answer, and the coverage test refuses a surface with holes.
func TestEveryEndpointOverTheAPI(t *testing.T) {
	h := newHarness(t)
	evrKey := h.projectKey(t, "EVR")
	sprKey := h.projectKey(t, "SPR")
	api := newAPIServer(t, h)
	c := api.client(t)
	signedUp := c.signIn(t, h.person(t, "everything", auth.RoleOwner))
	me := obj(t, signedUp, "principal", "user")
	myID := me["id"].(string)
	myEmail := me["email"].(string)
	orgSlug := obj(t, signedUp, "principal", "org")["slug"].(string)

	// Health and the document need no session.
	want(t, c.get("/healthz"), http.StatusOK, "liveness")
	want(t, c.get("/readyz"), http.StatusOK, "readiness")
	want(t, c.get("/api/v1/openapi.json"), http.StatusOK, "openapi")
	want(t, c.get("/api/v1/build"), http.StatusOK, "build")
	want(t, c.get("/api/v1/meta"), http.StatusOK, "meta")

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
	want(t, c.post("/api/v1/projects", map[string]any{"name": "Everything", "key": evrKey, "template": "scrum"}), http.StatusCreated, "create project")
	want(t, c.get("/api/v1/projects"), http.StatusOK, "list projects")
	want(t, c.get("/api/v1/projects/"+evrKey), http.StatusOK, "get project")
	want(t, c.patch("/api/v1/projects/"+evrKey, map[string]any{"description": "All of it."}), http.StatusOK, "edit project")
	want(t, c.post("/api/v1/projects", map[string]any{"name": "Spare", "key": sprKey}), http.StatusCreated, "spare project")
	want(t, c.delete("/api/v1/projects/"+sprKey), http.StatusNoContent, "archive project")
	want(t, c.post("/api/v1/projects/"+sprKey+"/restore", nil), http.StatusNoContent, "restore project")

	// Workflows and schemes.
	walkWorkflows(t, h, c, evrKey, typeID)

	// Issues and the hierarchy.
	epic := want(t, c.post("/api/v1/projects/"+evrKey+"/issues", map[string]any{"summary": "the epic", "typeId": typeID[bootstrap.TypeEpic]}), http.StatusCreated, "epic")
	epicKey := obj(t, epic, "issue")["key"].(string)
	story := want(t, c.post("/api/v1/issues", map[string]any{"projectKey": evrKey, "summary": "the story", "typeId": typeID[bootstrap.TypeStory], "parentKey": epicKey}), http.StatusCreated, "story")
	storyKey := obj(t, story, "issue")["key"].(string)
	task := want(t, c.post("/api/v1/projects/"+evrKey+"/issues", map[string]any{"summary": "the task", "estimate": 3}), http.StatusCreated, "task")
	taskKey := obj(t, task, "issue")["key"].(string)
	want(t, c.get("/api/v1/issues?project="+evrKey+"&category=todo&orderBy=created"), http.StatusOK, "search issues")
	want(t, c.get("/api/v1/issues?q="+url.QueryEscape("statusCategory != done AND reporter = currentUser() ORDER BY priority DESC, key")), http.StatusOK, "search issues by query")
	want(t, c.get("/api/v1/projects/"+evrKey+"/issues?text=the"), http.StatusOK, "project issues")
	want(t, c.get("/api/v1/projects/"+evrKey+"/issues?q="+url.QueryEscape(`type = Story AND "Customer" IS EMPTY`)), http.StatusOK, "project issues by query")
	badQuery := want(t, c.get("/api/v1/projects/"+evrKey+"/issues?q="+url.QueryEscape("assigne = currentUser()")), http.StatusBadRequest, "bad query")
	if badQuery.ErrorCode() != "bad_query" || badQuery.Error()["position"] != float64(1) {
		t.Errorf("a bad query answered %v", badQuery.Error())
	}
	want(t, c.get("/api/v1/issues/"+storyKey), http.StatusOK, "get issue")
	want(t, c.patch("/api/v1/issues/"+taskKey, map[string]any{"priority": "high"}), http.StatusOK, "edit issue")
	want(t, c.put("/api/v1/issues/"+taskKey+"/parent", map[string]any{"parentKey": epicKey}), http.StatusOK, "set parent")
	want(t, c.get("/api/v1/issues/"+epicKey+"/children"), http.StatusOK, "children")
	want(t, c.get("/api/v1/issues/"+storyKey+"/hierarchy"), http.StatusOK, "hierarchy")
	want(t, c.get("/api/v1/projects/"+evrKey+"/hierarchy"), http.StatusOK, "project tree")
	want(t, c.put("/api/v1/issues/"+storyKey+"/schedule", map[string]any{"startDate": "2026-09-01", "dueDate": "2026-09-14"}), http.StatusOK, "schedule")
	want(t, c.put("/api/v1/projects/"+evrKey+"/plan/schedule", map[string]any{"moves": []any{map[string]any{
		"key": storyKey, "expected": map[string]any{"startDate": "2026-09-01", "dueDate": "2026-09-14"}, "startDate": "2026-09-01", "dueDate": "2026-09-14",
	}}}), http.StatusOK, "schedule the plan")
	want(t, c.get("/api/v1/projects/"+evrKey+"/plan"), http.StatusOK, "plan")
	matched := list(t, want(t, c.get("/api/v1/projects/"+evrKey+"/plan?q="+url.QueryEscape("key = "+storyKey)), http.StatusOK, "plan by query"), "matched")
	if len(matched) != 1 || matched[0] != storyKey {
		t.Errorf("the plan matched %v, want %s", matched, storyKey)
	}
	want(t, c.get("/api/v1/projects/"+evrKey+"/plan?q=nonsense"), http.StatusBadRequest, "plan with a bad query")
	link := want(t, c.post("/api/v1/issues/"+storyKey+"/links", map[string]any{"type": "blocks", "targetKey": taskKey}), http.StatusCreated, "link")
	want(t, c.post("/api/v1/issues/"+taskKey+"/links", map[string]any{"type": "blocks", "targetKey": storyKey}), http.StatusConflict, "a link back around")
	dependencies := list(t, want(t, c.get("/api/v1/projects/"+evrKey+"/plan"), http.StatusOK, "plan with a dependency"), "dependencies")
	if len(dependencies) != 1 || dependencies[0].(map[string]any)["linkId"] != idOf(t, link, "link") {
		t.Errorf("the plan's dependency does not name its link: %v", dependencies)
	}
	want(t, c.post("/api/v1/projects/"+evrKey+"/plan/align", map[string]any{}), http.StatusOK, "auto plan proposal")
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
	team := want(t, c.post("/api/v1/projects/"+evrKey+"/teams", map[string]any{"name": "Platform"}), http.StatusCreated, "team")
	teamID := idOf(t, team, "team")
	want(t, c.get("/api/v1/projects/"+evrKey+"/teams"), http.StatusOK, "teams")
	want(t, c.get("/api/v1/teams/"+teamID), http.StatusOK, "get team")
	want(t, c.patch("/api/v1/teams/"+teamID, map[string]any{"name": "Platform", "description": "Runs the platform.", "weeklyCapacity": 12}), http.StatusOK, "edit team")
	if got := want(t, c.get("/api/v1/teams/"+teamID), http.StatusOK, "get team again").Body["team"].(map[string]any)["weeklyCapacity"]; got != 12.0 {
		t.Errorf("weekly capacity over the api = %v, want 12", got)
	}
	want(t, c.post("/api/v1/teams/"+teamID+"/members", map[string]any{"userId": myID, "lead": true}), http.StatusOK, "join team")
	want(t, c.put("/api/v1/issues/"+storyKey+"/team", map[string]any{"teamId": teamID}), http.StatusOK, "hand to team")
	from, to := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"), time.Now().UTC().AddDate(0, 0, 13).Format("2006-01-02")
	sprint := want(t, c.post("/api/v1/projects/"+evrKey+"/sprints", map[string]any{"name": "Sprint 1", "teamId": teamID, "startsOn": from, "endsOn": to}), http.StatusCreated, "sprint")
	sprintID := idOf(t, sprint, "sprint")
	want(t, c.patch("/api/v1/sprints/"+sprintID, map[string]any{"name": "Sprint 1", "goal": "ship", "capacity": 20}), http.StatusOK, "edit sprint")
	want(t, c.put("/api/v1/issues/"+storyKey+"/sprint", map[string]any{"sprintId": sprintID}), http.StatusOK, "commit to sprint")
	filed := want(t, c.post("/api/v1/projects/"+evrKey+"/issues", map[string]any{"summary": "filed straight into the sprint", "teamId": teamID, "sprintId": sprintID, "estimate": 2}), http.StatusCreated, "file into sprint")
	filedKey := obj(t, filed, "issue")["key"].(string)
	want(t, c.get("/api/v1/projects/"+evrKey+"/sprints?team="+teamID), http.StatusOK, "sprints")
	want(t, c.get("/api/v1/sprints/"+sprintID+"/board"), http.StatusOK, "sprint board")
	want(t, c.post("/api/v1/sprints/"+sprintID+"/start", nil), http.StatusOK, "start sprint")
	spare := want(t, c.post("/api/v1/projects/"+evrKey+"/sprints", map[string]any{"name": "Sprint 2", "teamId": teamID}), http.StatusCreated, "spare sprint")
	want(t, c.post("/api/v1/sprints/"+sprintID+"/complete", map[string]any{"moveTo": nil}), http.StatusOK, "complete sprint")
	want(t, c.get("/api/v1/projects/"+evrKey+"/sprints?closed=true"), http.StatusOK, "closed sprints")
	want(t, c.delete("/api/v1/sprints/"+idOf(t, spare, "sprint")), http.StatusNoContent, "delete sprint")

	// Milestones.
	milestone := want(t, c.post("/api/v1/projects/"+evrKey+"/milestones", map[string]any{"name": "Release 1", "dueOn": to}), http.StatusCreated, "milestone")
	milestoneID := idOf(t, milestone, "milestone")
	want(t, c.patch("/api/v1/milestones/"+milestoneID, map[string]any{"name": "Release 1", "description": "The first cut."}), http.StatusOK, "edit milestone")
	want(t, c.put("/api/v1/issues/"+storyKey+"/milestone", map[string]any{"milestoneId": milestoneID}), http.StatusOK, "count towards milestone")
	want(t, c.get("/api/v1/projects/"+evrKey+"/issues?milestone="+milestoneID), http.StatusOK, "issues of a milestone")
	want(t, c.get("/api/v1/projects/"+evrKey+"/milestones?closed=true"), http.StatusOK, "milestones")
	want(t, c.post("/api/v1/milestones/"+milestoneID+"/close", nil), http.StatusOK, "close milestone")
	want(t, c.post("/api/v1/milestones/"+milestoneID+"/reopen", nil), http.StatusOK, "reopen milestone")
	want(t, c.delete("/api/v1/milestones/"+milestoneID), http.StatusNoContent, "delete milestone")

	// Boards.
	walkBoards(t, c, evrKey, storyKey)

	// Dashboards.
	walkDashboards(t, h, c, evrKey, sprintID, milestoneID)

	// Custom fields, groups, roles and tokens.
	walkFieldsGroupsAndTokens(t, h, api, c, evrKey, taskKey, myID)

	// Somebody else joins by invitation, with an account made on accepting it.
	invited := want(t, c.post("/api/v1/invites", map[string]any{"email": myEmail + ".other", "role": "member"}), http.StatusCreated, "invite")
	want(t, c.get("/api/v1/invites"), http.StatusOK, "invites")
	want(t, c.delete("/api/v1/invites/"+idOf(t, invited, "invite")), http.StatusNoContent, "revoke invite")
	other := api.client(t)
	forOther := want(t, c.post("/api/v1/invites", map[string]any{"email": h.email(t, "elsewhere"), "role": "member"}), http.StatusCreated, "invite the other")
	want(t, other.post("/api/v1/auth/invites/accept", map[string]any{"token": forOther.Body["token"], "name": "Other Person", "password": testPassword}), http.StatusOK, "accept invite")
	want(t, other.get("/api/v1/projects/"+evrKey), http.StatusOK, "the other sees the project")
	want(t, other.post("/api/v1/auth/logout", nil), http.StatusNoContent, "logout")

	// Repositories, the webhook, and the branch made on the host.
	walkRepositories(t, api, c, evrKey, storyKey)

	// The identity provider: configured, and the address that named the
	// organization sends the browser to the one the installation signs in at.
	h.keepProvider(t)
	provider := newIDP(t)
	want(t, c.put("/api/v1/oidc-provider", map[string]any{
		"issuer": provider.URL, "clientId": "armature-test", "clientSecret": "shh", "groupsClaim": "groups",
		"scopes": "openid profile email", "createGroups": false, "enabled": true,
	}), http.StatusOK, "configure provider")
	want(t, c.get("/api/v1/oidc-provider"), http.StatusOK, "get provider")
	browser := api.client(t).noRedirects()
	startReq, _ := http.NewRequest(http.MethodGet, api.URL+"/api/v1/auth/oidc/"+orgSlug+"/start?next=/projects", nil)
	startResp, _ := browser.raw(startReq)
	if startResp.StatusCode != http.StatusFound || !strings.HasPrefix(startResp.Header.Get("Location"), "/api/v1/auth/oidc/start") {
		t.Fatalf("oidc start at the old address: %d to %q", startResp.StatusCode, startResp.Header.Get("Location"))
	}

	// Team clean-up: a team carrying work or a board cannot go, so first the
	// work is taken back and the board removed.
	want(t, c.put("/api/v1/issues/"+storyKey+"/team", map[string]any{"teamId": nil}), http.StatusOK, "take work back")
	want(t, c.put("/api/v1/issues/"+filedKey+"/team", map[string]any{"teamId": nil}), http.StatusOK, "take the filed work back")
	for _, raw := range list(t, c.get("/api/v1/projects/"+evrKey+"/boards"), "boards") {
		if b := raw.(map[string]any); b["teamId"] == teamID {
			want(t, c.delete("/api/v1/boards/"+b["id"].(string)), http.StatusNoContent, "delete team board")
		}
	}
	want(t, c.delete("/api/v1/teams/"+teamID+"/members/"+myID), http.StatusOK, "leave team")
	// Platform has run a sprint, which keeps it; a team that never did can go.
	want(t, c.delete("/api/v1/teams/"+teamID), http.StatusConflict, "delete a team that has run a sprint")
	shortLived := want(t, c.post("/api/v1/projects/"+evrKey+"/teams", map[string]any{"name": "Short lived"}), http.StatusCreated, "short-lived team")
	want(t, c.delete("/api/v1/teams/"+idOf(t, shortLived, "team")), http.StatusNoContent, "delete team")

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
	t.Parallel()
	h := newHarness(t)
	hldKey := h.projectKey(t, "HLD")
	api := newAPIServer(t, h)
	agent := api.client(t)
	agent.signIn(t, h.person(t, "deskapi", auth.RoleOwner))
	want(t, agent.post("/api/v1/projects", map[string]any{"name": "Help", "key": hldKey, "template": "service-desk"}), http.StatusCreated, "desk")

	types := list(t, want(t, agent.get("/api/v1/projects/"+hldKey+"/request-types"), http.StatusOK, "request types"), "requestTypes")
	// The project's own types, not the library's first: a parallel test may add a custom type that sorts first.
	issueTypes := list(t, want(t, agent.get("/api/v1/projects/"+hldKey+"/issue-types"), http.StatusOK, "project issue types"), "issueTypes")
	made := want(t, agent.post("/api/v1/projects/"+hldKey+"/request-types", map[string]any{
		"name": "Something else", "description": "Anything not listed.", "issueTypeId": issueTypes[0].(map[string]any)["id"], "priority": "low",
	}), http.StatusCreated, "add request type")
	want(t, agent.patch("/api/v1/request-types/"+idOf(t, made, "requestType"), map[string]any{"category": "Hardware", "detailsTemplate": "Device:\n"}), http.StatusOK, "edit request type")
	want(t, agent.patch("/api/v1/request-types/"+uuid.NewString(), map[string]any{"category": "Hardware"}), http.StatusNotFound, "edit a request type that is not there")
	want(t, agent.delete("/api/v1/request-types/"+idOf(t, made, "requestType")), http.StatusNoContent, "remove request type")
	policies := list(t, want(t, agent.get("/api/v1/projects/"+hldKey+"/sla-policies"), http.StatusOK, "policies"), "policies")
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
	want(t, agent.get("/api/v1/projects/"+hldKey+"/queue?filter=open"), http.StatusOK, "queue")
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
	t.Parallel()
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
// dead invitation, an unsigned webhook, and a callback for a sign-in that
// never started.
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
	callback, _ := http.NewRequest(http.MethodGet, api.URL+"/api/v1/auth/oidc/callback?state=made-up&code=x", nil)
	if resp, _ := c.raw(callback); resp.StatusCode/100 != 4 && resp.StatusCode != http.StatusFound {
		t.Errorf("a callback for no sign-in answered %d", resp.StatusCode)
	}
}
