//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/perm"
)

// find pulls one object out of a list in a response body by a field value.
func find(t *testing.T, list any, field, value string) map[string]any {
	t.Helper()
	rows, ok := list.([]any)
	if !ok {
		t.Fatalf("expected a list, got %T", list)
	}
	for _, row := range rows {
		if item, ok := row.(map[string]any); ok && item[field] == value {
			return item
		}
	}
	t.Fatalf("no row with %s = %q among %d", field, value, len(rows))
	return nil
}

func TestAPIWorkflowScopes(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	owner.signup(t, h, "wfapi")

	created := owner.post("/api/v1/projects", map[string]string{"name": "Scoped", "key": "SCOPE"})
	if created.Status != http.StatusCreated {
		t.Fatalf("create project returned %d: %s", created.Status, created.Raw)
	}

	types := owner.get("/api/v1/issue-types")
	bug := find(t, types.Body["issueTypes"], "name", "Bug")["id"].(string)
	statuses := owner.get("/api/v1/statuses")
	review := find(t, statuses.Body["statuses"], "name", "In Review")["id"].(string)
	done := find(t, statuses.Body["statuses"], "name", "Done")["id"].(string)

	t.Run("a project starts out with the organization deciding everything", func(t *testing.T) {
		resp := owner.get("/api/v1/projects/SCOPE/workflows")
		if resp.Status != http.StatusOK {
			t.Fatalf("returned %d: %s", resp.Status, resp.Raw)
		}
		if resp.Body["schemeId"] != nil {
			t.Errorf("schemeId = %v, want none until the project overrides", resp.Body["schemeId"])
		}
		row := find(t, resp.Body["assignments"], "issueTypeName", "Bug")
		origin := row["origin"].(map[string]any)
		if origin["scope"] != "tenant" {
			t.Errorf("scope = %v, want tenant", origin["scope"])
		}
	})

	var workflowID, schemeID string

	t.Run("an administrator can author a workflow", func(t *testing.T) {
		resp := owner.post("/api/v1/workflows", map[string]any{
			"name": "Bug triage",
			"steps": []map[string]any{
				{"statusId": review, "isInitial": true},
				{"statusId": done},
			},
			"transitions": []map[string]any{
				{"name": "Resolve", "fromStatusId": review, "toStatusId": done},
			},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("returned %d: %s", resp.Status, resp.Raw)
		}
		workflowID = resp.Body["workflow"].(map[string]any)["id"].(string)
	})

	t.Run("a workflow that could not work comes back as a bad request", func(t *testing.T) {
		resp := owner.post("/api/v1/workflows", map[string]any{
			"name":  "Nowhere",
			"steps": []map[string]any{{"statusId": review}},
		})
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("returned %d: %s", resp.Status, resp.Raw)
		}
		message, _ := resp.Error()["message"].(string)
		if message == "" || message == "Something went wrong on our side." {
			t.Errorf("message = %q, want the reason it was refused", message)
		}
	})

	t.Run("a scheme naming one issue type overrides only that type", func(t *testing.T) {
		scheme := owner.post("/api/v1/workflow-schemes", map[string]any{
			"name":  "Scoped bugs",
			"items": []map[string]any{{"issueTypeId": bug, "workflowId": workflowID}},
		})
		if scheme.Status != http.StatusCreated {
			t.Fatalf("returned %d: %s", scheme.Status, scheme.Raw)
		}
		schemeID = scheme.Body["scheme"].(map[string]any)["id"].(string)

		applied := owner.put("/api/v1/projects/SCOPE/workflow-scheme", map[string]any{"schemeId": schemeID})
		if applied.Status != http.StatusOK {
			t.Fatalf("returned %d: %s", applied.Status, applied.Raw)
		}

		resolved := owner.get("/api/v1/projects/SCOPE/workflows")
		bugRow := find(t, resolved.Body["assignments"], "issueTypeName", "Bug")
		if bugRow["workflowName"] != "Bug triage" {
			t.Errorf("bug uses %v, want Bug triage", bugRow["workflowName"])
		}
		if origin := bugRow["origin"].(map[string]any); origin["scope"] != "project" || origin["named"] != true {
			t.Errorf("bug origin = %v, want a named project mapping", origin)
		}
		taskRow := find(t, resolved.Body["assignments"], "issueTypeName", "Task")
		if origin := taskRow["origin"].(map[string]any); origin["scope"] != "tenant" {
			t.Errorf("task origin = %v, want the organization", origin)
		}
	})

	t.Run("an issue of the overridden type opens where the override says", func(t *testing.T) {
		resp := owner.post("/api/v1/projects/SCOPE/issues", map[string]any{
			"summary": "found by the override", "typeId": bug,
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("returned %d: %s", resp.Status, resp.Raw)
		}
		status := resp.Body["issue"].(map[string]any)["status"].(map[string]any)
		if status["name"] != "In Review" {
			t.Errorf("opened in %v, want In Review", status["name"])
		}
	})

	t.Run("the listing says which scheme the organization answers with", func(t *testing.T) {
		resp := owner.get("/api/v1/workflow-schemes")
		mine := find(t, resp.Body["schemes"], "id", schemeID)
		if mine["isDefault"] != false {
			t.Errorf("the project's scheme is marked as the organization's own")
		}
		keys, _ := mine["projectKeys"].([]any)
		if len(keys) != 1 || keys[0] != "SCOPE" {
			t.Errorf("projectKeys = %v, want just SCOPE", keys)
		}
	})

	t.Run("handing the decision back is a null, not an omission", func(t *testing.T) {
		resp := owner.put("/api/v1/projects/SCOPE/workflow-scheme", map[string]any{"schemeId": nil})
		if resp.Status != http.StatusOK {
			t.Fatalf("returned %d: %s", resp.Status, resp.Raw)
		}
		resolved := owner.get("/api/v1/projects/SCOPE/workflows")
		bugRow := find(t, resolved.Body["assignments"], "issueTypeName", "Bug")
		if origin := bugRow["origin"].(map[string]any); origin["scope"] != "tenant" {
			t.Errorf("bug origin = %v, want the organization again", origin)
		}
	})
}

func TestAPIWorkflowAdministrationIsGuarded(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	owner.signup(t, h, "wfguard")
	owner.post("/api/v1/projects", map[string]string{"name": "Guarded", "key": "GUARD"})

	invite := owner.post("/api/v1/invites", map[string]string{
		"email": h.email(t, "wfmember"), "role": "member",
	})
	member := api.client(t)
	member.post("/api/v1/auth/invites/accept", map[string]string{
		"token": invite.Body["token"].(string), "name": "Member", "password": testPassword,
	})

	// Reading the configuration is ordinary work: it explains why a button is
	// or is not there. Changing it is not.
	for _, path := range []string{"/api/v1/workflows", "/api/v1/workflow-schemes", "/api/v1/projects/GUARD/workflows"} {
		if resp := member.get(path); resp.Status != http.StatusOK {
			t.Errorf("GET %s as a member returned %d, want it readable", path, resp.Status)
		}
	}

	for _, ep := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/workflows"},
		{http.MethodPost, "/api/v1/workflow-schemes"},
		{http.MethodPut, "/api/v1/projects/GUARD/workflow-scheme"},
		{http.MethodPut, "/api/v1/projects/GUARD/workflow-assignments/00000000-0000-0000-0000-000000000000"},
		{http.MethodDelete, "/api/v1/workflow-schemes/00000000-0000-0000-0000-000000000000"},
	} {
		resp := member.do(ep.method, ep.path, map[string]any{"name": "Sneaky"})
		if resp.Status != http.StatusForbidden {
			t.Errorf("%s %s as a member returned %d, want 403", ep.method, ep.path, resp.Status)
		}
	}
}

// The designer sends the picture and the rules together and reads both back.
func TestAPIWorkflowDesigner(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	owner.signup(t, h, "designer")

	statuses := owner.get("/api/v1/statuses")
	todo := find(t, statuses.Body["statuses"], "name", "To Do")["id"].(string)
	done := find(t, statuses.Body["statuses"], "name", "Done")["id"].(string)

	t.Run("the rule catalogue says what each rule is called and needs", func(t *testing.T) {
		resp := owner.get("/api/v1/workflows/rule-types")
		if resp.Status != http.StatusOK {
			t.Fatalf("returned %d: %s", resp.Status, resp.Raw)
		}
		comment := find(t, resp.Body["ruleTypes"], "type", "postfunction.add_comment")
		if comment["label"] != "Add a comment" || comment["kind"] != "postfunction" {
			t.Errorf("add_comment described as %v", comment)
		}
		options, _ := comment["options"].([]any)
		if len(options) != 1 || options[0].(map[string]any)["name"] != "text" {
			t.Errorf("add_comment options = %v, want the text option", options)
		}
	})

	var workflowID, finishID string

	t.Run("a workflow is created with its layout and rules", func(t *testing.T) {
		resp := owner.post("/api/v1/workflows", map[string]any{
			"name": "Drawn",
			"steps": []map[string]any{
				{"statusId": todo, "isInitial": true, "layout": map[string]int{"x": 40, "y": 60}},
				{"statusId": done, "layout": map[string]int{"x": 320, "y": 60}},
			},
			"transitions": []map[string]any{
				{"name": "Finish", "fromStatusId": todo, "toStatusId": done, "rules": []map[string]any{
					{"kind": "validator", "type": "validator.comment_required"},
					{"kind": "postfunction", "type": "postfunction.add_comment", "config": map[string]any{"text": "Finished."}},
				}},
			},
		})
		if resp.Status != http.StatusCreated {
			t.Fatalf("returned %d: %s", resp.Status, resp.Raw)
		}
		created := resp.Body["workflow"].(map[string]any)
		workflowID = created["id"].(string)
		steps := created["steps"].([]any)
		layout, _ := steps[1].(map[string]any)["layout"].(map[string]any)
		if layout["x"] != float64(320) {
			t.Errorf("Done drawn at %v, want x 320", layout)
		}
		finishID = created["transitions"].([]any)[0].(map[string]any)["id"].(string)
	})

	t.Run("reading it back lists the rules by transition", func(t *testing.T) {
		resp := owner.get("/api/v1/workflows/" + workflowID)
		if resp.Status != http.StatusOK {
			t.Fatalf("returned %d: %s", resp.Status, resp.Raw)
		}
		rules, _ := resp.Body["rules"].(map[string]any)[finishID].([]any)
		if len(rules) != 2 {
			t.Fatalf("Finish has %d rules, want 2: %s", len(rules), resp.Raw)
		}
		second := rules[1].(map[string]any)
		if second["type"] != "postfunction.add_comment" || second["config"].(map[string]any)["text"] != "Finished." {
			t.Errorf("second rule = %v", second)
		}
	})

	t.Run("a save that leaves the rules out keeps them", func(t *testing.T) {
		resp := owner.put("/api/v1/workflows/"+workflowID, map[string]any{
			"name": "Drawn",
			"steps": []map[string]any{
				{"statusId": todo, "isInitial": true, "layout": map[string]int{"x": 40, "y": 60}},
				{"statusId": done, "layout": map[string]int{"x": 400, "y": 60}},
			},
			"transitions": []map[string]any{
				{"id": finishID, "name": "Finish", "fromStatusId": todo, "toStatusId": done},
			},
		})
		if resp.Status != http.StatusOK {
			t.Fatalf("returned %d: %s", resp.Status, resp.Raw)
		}
		read := owner.get("/api/v1/workflows/" + workflowID)
		if rules, _ := read.Body["rules"].(map[string]any)[finishID].([]any); len(rules) != 2 {
			t.Errorf("Finish has %d rules after a save that said nothing about them, want 2", len(rules))
		}
	})

	t.Run("a rule the engine does not have is refused with its name", func(t *testing.T) {
		resp := owner.put("/api/v1/workflows/"+workflowID, map[string]any{
			"name":  "Drawn",
			"steps": []map[string]any{{"statusId": todo, "isInitial": true}, {"statusId": done}},
			"transitions": []map[string]any{
				{"id": finishID, "name": "Finish", "fromStatusId": todo, "toStatusId": done, "rules": []map[string]any{
					{"kind": "condition", "type": "condition.moon_phase"},
				}},
			},
		})
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("returned %d: %s", resp.Status, resp.Raw)
		}
		message, _ := resp.Error()["message"].(string)
		if !strings.Contains(message, "condition.moon_phase") {
			t.Errorf("message = %q, want it to name the rule", message)
		}
	})
}

// A project administrator is not an organization administrator: they may
// decide one row on their own project and nothing wider.
func TestAPIProjectAdministratorMapsOneType(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	owner.signup(t, h, "rowapi")
	if created := owner.post("/api/v1/projects", map[string]string{"name": "Rowwise", "key": "ROW"}); created.Status != http.StatusCreated {
		t.Fatalf("create project returned %d: %s", created.Status, created.Raw)
	}
	statuses := owner.get("/api/v1/statuses")
	review := find(t, statuses.Body["statuses"], "name", "In Review")["id"].(string)
	done := find(t, statuses.Body["statuses"], "name", "Done")["id"].(string)
	built := owner.post("/api/v1/workflows", map[string]any{
		"name":        "Bug triage",
		"steps":       []map[string]any{{"statusId": review, "isInitial": true}, {"statusId": done}},
		"transitions": []map[string]any{{"name": "Resolve", "fromStatusId": review, "toStatusId": done}},
	})
	workflowID := built.Body["workflow"].(map[string]any)["id"].(string)
	types := owner.get("/api/v1/issue-types")
	bug := find(t, types.Body["issueTypes"], "name", "Bug")["id"].(string)

	admin := api.asRole(t, h, owner, "ROW", perm.ProjectAdministrator)

	t.Run("mapping a type answers with the whole table", func(t *testing.T) {
		resp := admin.put("/api/v1/projects/ROW/workflow-assignments/"+bug, map[string]any{"workflowId": workflowID})
		if resp.Status != http.StatusOK {
			t.Fatalf("returned %d: %s", resp.Status, resp.Raw)
		}
		if resp.Body["schemeId"] == nil {
			t.Error("schemeId is null after mapping; the row has nowhere to live")
		}
		row := find(t, resp.Body["assignments"], "issueTypeName", "Bug")
		if row["workflowName"] != "Bug triage" {
			t.Errorf("bug uses %v, want Bug triage", row["workflowName"])
		}
		if origin := row["origin"].(map[string]any); origin["scope"] != "project" || origin["named"] != true {
			t.Errorf("origin = %v, want the project naming it", origin)
		}
	})

	t.Run("a workflow that is not the organization's is refused with a sentence", func(t *testing.T) {
		resp := admin.put("/api/v1/projects/ROW/workflow-assignments/"+bug, map[string]any{"workflowId": uuid.New().String()})
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("returned %d: %s", resp.Status, resp.Raw)
		}
		if message, _ := resp.Error()["message"].(string); !strings.Contains(message, "organization's workflows") {
			t.Errorf("message = %q, want it to say what to choose", message)
		}
	})

	t.Run("an issue type the organization does not have is refused", func(t *testing.T) {
		resp := admin.put("/api/v1/projects/ROW/workflow-assignments/"+uuid.New().String(), map[string]any{"workflowId": workflowID})
		if resp.Status != http.StatusBadRequest {
			t.Fatalf("returned %d: %s", resp.Status, resp.Raw)
		}
		if resp := admin.put("/api/v1/projects/ROW/workflow-assignments/not-a-uuid", map[string]any{"workflowId": workflowID}); resp.Status != http.StatusBadRequest {
			t.Errorf("a garbage id returned %d, want 400", resp.Status)
		}
	})

	t.Run("null hands the type back and the scheme goes with it", func(t *testing.T) {
		resp := admin.put("/api/v1/projects/ROW/workflow-assignments/"+bug, map[string]any{"workflowId": nil})
		if resp.Status != http.StatusOK {
			t.Fatalf("returned %d: %s", resp.Status, resp.Raw)
		}
		if resp.Body["schemeId"] != nil {
			t.Errorf("schemeId = %v, want none once nothing is mapped", resp.Body["schemeId"])
		}
		row := find(t, resp.Body["assignments"], "issueTypeName", "Bug")
		if origin := row["origin"].(map[string]any); origin["scope"] != "tenant" {
			t.Errorf("origin = %v, want the organization again", origin)
		}
	})

	t.Run("drawing a scheme of their own is still not theirs to do", func(t *testing.T) {
		if resp := admin.post("/api/v1/workflow-schemes", map[string]any{"name": "Sneaky"}); resp.Status != http.StatusForbidden {
			t.Errorf("creating a scheme as a project administrator returned %d, want 403", resp.Status)
		}
	})
}
