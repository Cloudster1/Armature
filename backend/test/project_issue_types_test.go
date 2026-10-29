//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/perm"
)

// libraryIDs reads the whole library, retired types too, by name.
func libraryIDs(t *testing.T, c *client) map[string]string {
	t.Helper()
	ids := map[string]string{}
	for _, raw := range list(t, want(t, c.get("/api/v1/issue-types?retired=true"), http.StatusOK, "the library"), "issueTypes") {
		row := raw.(map[string]any)
		ids[row["name"].(string)] = row["id"].(string)
	}
	return ids
}

// setProjectTypes makes ids, in order, the types a project allows.
func setProjectTypes(c *client, key string, ids ...string) response {
	if ids == nil {
		ids = []string{}
	}
	return c.put("/api/v1/projects/"+key+"/issue-types", map[string]any{"typeIds": ids})
}

// allowType adds types after the ones a project allows already.
func allowType(t *testing.T, c *client, key string, ids ...string) {
	t.Helper()
	var current []string
	for _, raw := range list(t, want(t, c.get("/api/v1/projects/"+key+"/issue-types?retired=true"), http.StatusOK, "the project's types"), "issueTypes") {
		current = append(current, raw.(map[string]any)["id"].(string))
	}
	want(t, setProjectTypes(c, key, append(current, ids...)...), http.StatusOK, "allow a type")
}

// typedProject is an owner, a project of theirs and the library's ids.
func typedProject(t *testing.T, h *harness, api *apiServer, prefix string, template string) (*client, string, map[string]string) {
	t.Helper()
	owner := api.client(t)
	owner.signIn(t, h.person(t, strings.ToLower(prefix)+"owner", auth.RoleOwner))
	key := h.projectKey(t, prefix)
	body := map[string]any{"name": prefix + " project", "key": key}
	if template != "" {
		body["template"] = template
	}
	want(t, owner.post("/api/v1/projects", body), http.StatusCreated, "a project")
	return owner, key, libraryIDs(t, owner)
}

// fileIssue files one issue and answers its key.
func fileIssue(t *testing.T, c *client, key string, body map[string]any) string {
	t.Helper()
	return obj(t, want(t, c.post("/api/v1/projects/"+key+"/issues", body), http.StatusCreated, "file "+body["summary"].(string)), "issue")["key"].(string)
}

// A project allows its own types: a new one every built-in type, in the
// library's order; its administrators choose, its readers only read.
func TestEachProjectAllowsItsOwnIssueTypes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner, key, _ := typedProject(t, h, api, "PIT", "")
	ticket := h.name(t, "Ticket")
	ticketID := idOf(t, want(t, owner.post("/api/v1/issue-types", map[string]any{"name": ticket, "icon": "task", "level": 0}), http.StatusCreated, "a type of our own"), "issueType")
	lib := libraryIDs(t, owner)

	offered := typeNames(t, want(t, owner.get("/api/v1/projects/"+key+"/issue-types"), http.StatusOK, "the project's types"))
	for _, name := range []string{bootstrap.TypeInitiative, bootstrap.TypeEpic, bootstrap.TypeTask, bootstrap.TypeBug, bootstrap.TypeStory, bootstrap.TypeSubtask} {
		if !slices.Contains(offered, name) {
			t.Errorf("a new project does not allow %s: %v", name, offered)
		}
	}
	if slices.Contains(offered, ticket) {
		t.Errorf("a new type reached a project nobody added it to: %v", offered)
	}

	set := want(t, setProjectTypes(owner, key, lib[bootstrap.TypeBug], lib[bootstrap.TypeTask], lib[bootstrap.TypeEpic]), http.StatusOK, "narrow the project")
	if got := typeNames(t, set); !slices.Equal(got, []string{bootstrap.TypeBug, bootstrap.TypeTask, bootstrap.TypeEpic}) {
		t.Errorf("the project's types came back as %v", got)
	}
	if got := typeNames(t, owner.get("/api/v1/projects/"+key+"/issue-types")); !slices.Equal(got, []string{bootstrap.TypeBug, bootstrap.TypeTask, bootstrap.TypeEpic}) {
		t.Errorf("the project's types read back as %v", got)
	}

	t.Run("field layouts, workflows and search keep every type", func(t *testing.T) {
		arranged := want(t, owner.get("/api/v1/projects/"+key+"/issue-arrangement"), http.StatusOK, "the field layouts")
		find(t, arranged.Body["arrangements"], "issueTypeName", bootstrap.TypeStory)
		flows := want(t, owner.get("/api/v1/projects/"+key+"/workflows"), http.StatusOK, "the workflows")
		find(t, flows.Body["assignments"], "issueTypeName", bootstrap.TypeStory)
		want(t, owner.get("/api/v1/projects/"+key+"/issues?q="+url.QueryEscape("type = Story")), http.StatusOK, "a search by a type the project does not use")
	})

	reader := api.asRole(t, h, owner, key, perm.Reader)
	want(t, reader.get("/api/v1/projects/"+key+"/issue-types"), http.StatusOK, "a reader reads the project's types")
	want(t, setProjectTypes(reader, key, lib[bootstrap.TypeTask]), http.StatusForbidden, "a reader chooses the project's types")

	t.Run("what a set must hold", func(t *testing.T) {
		for what, got := range map[string]response{
			"no types":           setProjectTypes(owner, key),
			"no standard type":   setProjectTypes(owner, key, lib[bootstrap.TypeEpic], lib[bootstrap.TypeSubtask]),
			"a type named twice": setProjectTypes(owner, key, lib[bootstrap.TypeTask], lib[bootstrap.TypeTask]),
		} {
			if got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: got %d: %s", what, got.Status, got.Raw)
			}
		}
	})

	t.Run("a type issues use stays", func(t *testing.T) {
		fileIssue(t, owner, key, map[string]any{"summary": "Broken", "typeId": lib[bootstrap.TypeBug]})
		refusedWith(t, setProjectTypes(owner, key, lib[bootstrap.TypeTask]), http.StatusConflict, "conflict",
			"1 issue is Bug in "+key+", so Bug stays. Change their type or move them first.")
		counted := find(t, want(t, owner.get("/api/v1/projects/"+key+"/issue-types"), http.StatusOK, "the types").Body["issueTypes"], "name", bootstrap.TypeBug)
		if counted["issues"] != float64(1) {
			t.Errorf("Bug is listed with %v issues, want 1", counted["issues"])
		}
	})

	t.Run("a retired type is not added", func(t *testing.T) {
		gone := idOf(t, want(t, owner.post("/api/v1/issue-types", map[string]any{"name": h.name(t, "Gone"), "icon": "task"}), http.StatusCreated, "a type to retire"), "issueType")
		want(t, owner.post("/api/v1/issue-types/"+gone+"/retire", nil), http.StatusOK, "retire it")
		got := setProjectTypes(owner, key, lib[bootstrap.TypeBug], lib[bootstrap.TypeTask], gone)
		if got.Status != http.StatusConflict || !strings.Contains(got.Raw, "is retired") {
			t.Errorf("a retired type was added: %d %s", got.Status, got.Raw)
		}
	})

	t.Run("a project's only standard type stays offered", func(t *testing.T) {
		bare := h.projectKey(t, "PITB")
		want(t, owner.post("/api/v1/projects", map[string]any{"name": "Bare", "key": bare}), http.StatusCreated, "a second project")
		want(t, setProjectTypes(owner, bare, ticketID, lib[bootstrap.TypeEpic]), http.StatusOK, "only our own standard type")
		only := ticket + " is the only standard type " + bare + " uses. Allow another standard type there first."
		refusedWith(t, owner.post("/api/v1/issue-types/"+ticketID+"/retire", nil), http.StatusConflict, "conflict", only)
		refusedWith(t, owner.delete("/api/v1/issue-types/"+ticketID), http.StatusConflict, "conflict", only)
		refusedWith(t, owner.patch("/api/v1/issue-types/"+ticketID, map[string]any{"level": 1}), http.StatusConflict, "conflict", only)

		// With another standard type it may be retired, and new issues skip it.
		want(t, setProjectTypes(owner, bare, ticketID, lib[bootstrap.TypeTask]), http.StatusOK, "add Task after it")
		want(t, owner.post("/api/v1/issue-types/"+ticketID+"/retire", nil), http.StatusOK, "retire it")
		if got := typeNames(t, owner.get("/api/v1/projects/"+bare+"/issue-types")); !slices.Equal(got, []string{bootstrap.TypeTask}) {
			t.Errorf("the project offers %v once its first type retired, want only Task", got)
		}
		if got := typeNames(t, owner.get("/api/v1/projects/"+bare+"/issue-types?retired=true")); !slices.Equal(got, []string{ticket, bootstrap.TypeTask}) {
			t.Errorf("the project keeps %v, want the retired type too", got)
		}
		made := obj(t, want(t, owner.post("/api/v1/projects/"+bare+"/issues", map[string]any{"summary": "No type given"}), http.StatusCreated, "an issue without a type"), "issue")
		if made["type"].(map[string]any)["name"] != bootstrap.TypeTask {
			t.Errorf("an issue without a type became %v, want Task", made["type"])
		}
		// Kept while retired, so an unchanged set is saved as it is.
		want(t, setProjectTypes(owner, bare, ticketID, lib[bootstrap.TypeTask]), http.StatusOK, "save the set with its retired type")
	})
}

// Filing an issue takes a type the project allows; with none given, its first
// standard one.
func TestFilingAnIssueKeepsToTheProjectsTypes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner, key, lib := typedProject(t, h, api, "PTF", "")
	want(t, setProjectTypes(owner, key, lib[bootstrap.TypeBug], lib[bootstrap.TypeEpic]), http.StatusOK, "narrow the project")

	refusedWith(t, owner.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "A task", "typeId": lib[bootstrap.TypeTask]}), http.StatusConflict, "type_not_allowed",
		key+" does not use Task; ask its administrator to allow it or pick another type.")
	made := obj(t, want(t, owner.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "No type given"}), http.StatusCreated, "an issue without a type"), "issue")
	if made["type"].(map[string]any)["name"] != bootstrap.TypeBug {
		t.Errorf("an issue without a type became %v, want the first standard type, Bug", made["type"])
	}

	allowType(t, owner, key, lib[bootstrap.TypeTask])
	task := fileIssue(t, owner, key, map[string]any{"summary": "A task", "typeId": lib[bootstrap.TypeTask]})
	want(t, owner.post("/api/v1/issues/"+task+"/clone", map[string]any{"summary": "Another task"}), http.StatusCreated, "a clone of an allowed type")

	// A type of our own is filed once allowed, and no longer once retired.
	chore := idOf(t, want(t, owner.post("/api/v1/issue-types", map[string]any{"name": h.name(t, "Chore"), "icon": "task"}), http.StatusCreated, "a type"), "issueType")
	if got := owner.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "A chore", "typeId": chore}); got.Status != http.StatusConflict {
		t.Errorf("an issue of a type nobody allowed: %d %s", got.Status, got.Raw)
	}
	allowType(t, owner, key, chore)
	fileIssue(t, owner, key, map[string]any{"summary": "A chore", "typeId": chore})
	want(t, owner.post("/api/v1/issue-types/"+chore+"/retire", nil), http.StatusOK, "retire it")
	got := owner.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "Another chore", "typeId": chore})
	if got.Status != http.StatusConflict || got.Error()["code"] != "type_retired" {
		t.Errorf("an issue of a retired type: %d %s", got.Status, got.Raw)
	}
}

// Changing an issue's type takes a type its project allows.
func TestChangingTypeKeepsToTheProjectsTypes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner, key, lib := typedProject(t, h, api, "PTT", "")
	want(t, setProjectTypes(owner, key, lib[bootstrap.TypeTask], lib[bootstrap.TypeBug]), http.StatusOK, "narrow the project")
	task := fileIssue(t, owner, key, map[string]any{"summary": "Work", "typeId": lib[bootstrap.TypeTask]})

	refusedWith(t, owner.patch("/api/v1/issues/"+task, map[string]any{"typeId": lib[bootstrap.TypeStory]}), http.StatusConflict, "type_not_allowed",
		key+" does not use Story; ask its administrator to allow it or change the type first.")
	allowType(t, owner, key, lib[bootstrap.TypeStory])
	changed := obj(t, want(t, owner.patch("/api/v1/issues/"+task, map[string]any{"typeId": lib[bootstrap.TypeStory]}), http.StatusOK, "change to an allowed type"), "issue")
	if changed["type"].(map[string]any)["name"] != bootstrap.TypeStory {
		t.Errorf("the type is %v, want Story", changed["type"])
	}

	old := idOf(t, want(t, owner.post("/api/v1/issue-types", map[string]any{"name": h.name(t, "Old"), "icon": "task"}), http.StatusCreated, "a type"), "issueType")
	allowType(t, owner, key, old)
	want(t, owner.post("/api/v1/issue-types/"+old+"/retire", nil), http.StatusOK, "retire it")
	if got := owner.patch("/api/v1/issues/"+task, map[string]any{"typeId": old}); got.Status != http.StatusConflict || got.Error()["code"] != "type_retired" {
		t.Errorf("a change to a retired type: %d %s", got.Status, got.Raw)
	}
}

// A move, of one issue, of everything under it, or of many at once, goes only
// into a project that allows every type it carries.
func TestMovingKeepsToTheTargetsTypes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner, source, lib := typedProject(t, h, api, "PTM", "")
	target := h.projectKey(t, "PTMT")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Target", "key": target}), http.StatusCreated, "the target")

	parent := fileIssue(t, owner, source, map[string]any{"summary": "Big", "typeId": lib[bootstrap.TypeEpic]})
	child := fileIssue(t, owner, source, map[string]any{"summary": "Part", "typeId": lib[bootstrap.TypeStory], "parentKey": parent})
	lone := fileIssue(t, owner, source, map[string]any{"summary": "Alone", "typeId": lib[bootstrap.TypeEpic]})
	moveTo := func(key string) response {
		return owner.post("/api/v1/issues/"+key+"/move", map[string]any{"projectKey": target})
	}

	want(t, setProjectTypes(owner, target, lib[bootstrap.TypeTask], lib[bootstrap.TypeStory]), http.StatusOK, "no epics in the target")
	noEpic := target + " does not use Epic; ask its administrator to allow it or change the type first."
	refusedWith(t, moveTo(lone), http.StatusConflict, "type_not_allowed", noEpic)
	_, refused := appliedAndRefused(t, bulkEdit(t, owner, []string{lone}, map[string]any{"moveTo": map[string]any{"projectKey": target}}, http.StatusOK, "a bulk move"))
	if refused[lone] != noEpic {
		t.Errorf("the bulk move refused %s with %q, want %q", lone, refused[lone], noEpic)
	}

	want(t, setProjectTypes(owner, target, lib[bootstrap.TypeTask], lib[bootstrap.TypeEpic]), http.StatusOK, "no stories in the target")
	refusedWith(t, moveTo(parent), http.StatusConflict, "type_not_allowed",
		target+" does not use Story; ask its administrator to allow it or change the type of "+child+" first.")

	allowType(t, owner, target, lib[bootstrap.TypeStory])
	moved := obj(t, want(t, moveTo(parent), http.StatusOK, "move the epic and its story"), "issue")
	if !strings.HasPrefix(moved["key"].(string), target+"-") {
		t.Errorf("the epic is %v after the move", moved["key"])
	}
	applied, refused := appliedAndRefused(t, bulkEdit(t, owner, []string{lone}, map[string]any{"moveTo": map[string]any{"projectKey": target}}, http.StatusOK, "a bulk move"))
	if len(applied) != 1 || len(refused) != 0 {
		t.Errorf("the bulk move applied %v and refused %v", applied, refused)
	}
}

// An import refuses a row of a type the project does not use, naming the row.
func TestImportKeepsToTheProjectsTypes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner, key, lib := typedProject(t, h, api, "PTI", "")
	want(t, setProjectTypes(owner, key, lib[bootstrap.TypeTask]), http.StatusOK, "only tasks")
	const mapping = `{"summary":[0],"type":[1]}`

	for _, dry := range []string{"?dryRun=true", ""} {
		report := want(t, owner.uploadWith("/api/v1/projects/"+key+"/import"+dry, []byte("Summary,Type\nA crash,Bug\nA chore,Task\n"), mapping), http.StatusOK, "import")
		refused := list(t, report, "report", "refused")
		if len(refused) != 1 || len(list(t, report, "report", "imported")) != 1 {
			t.Fatalf("import%s: %s", dry, report.Raw)
		}
		row := refused[0].(map[string]any)
		if row["row"] != float64(2) || row["reason"] != key+" does not use Bug; ask its administrator to allow it or say which of its types the row means" {
			t.Errorf("import%s refused %v", dry, row)
		}
	}

	allowType(t, owner, key, lib[bootstrap.TypeBug])
	report := want(t, owner.uploadWith("/api/v1/projects/"+key+"/import", []byte("Summary,Type\nAnother crash,Bug\n"), mapping), http.StatusOK, "import once allowed")
	if len(list(t, report, "report", "imported")) != 1 || len(list(t, report, "report", "refused")) != 0 {
		t.Errorf("import once allowed: %s", report.Raw)
	}
}

// The hierarchy offers a child only the types the project allows.
func TestHierarchyOffersTheProjectsTypes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner, key, lib := typedProject(t, h, api, "PTH", "")
	epic := fileIssue(t, owner, key, map[string]any{"summary": "Big", "typeId": lib[bootstrap.TypeEpic]})
	want(t, setProjectTypes(owner, key, lib[bootstrap.TypeTask], lib[bootstrap.TypeEpic]), http.StatusOK, "no stories")

	childTypes := func() []string {
		var names []string
		for _, raw := range list(t, want(t, owner.get("/api/v1/issues/"+epic+"/hierarchy"), http.StatusOK, "the hierarchy"), "childTypes") {
			names = append(names, raw.(map[string]any)["name"].(string))
		}
		return names
	}
	if got := childTypes(); !slices.Equal(got, []string{bootstrap.TypeTask}) {
		t.Errorf("an epic offers children of %v, want only Task", got)
	}
	refusedWith(t, owner.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "Part", "typeId": lib[bootstrap.TypeStory], "parentKey": epic}), http.StatusConflict, "type_not_allowed",
		key+" does not use Story; ask its administrator to allow it or pick another type.")

	allowType(t, owner, key, lib[bootstrap.TypeStory])
	if got := childTypes(); !slices.Contains(got, bootstrap.TypeStory) {
		t.Errorf("an epic offers children of %v once Story is allowed", got)
	}
	fileIssue(t, owner, key, map[string]any{"summary": "Part", "typeId": lib[bootstrap.TypeStory], "parentKey": epic})
}

// A rule that files a subtask files one of a type the project allows.
func TestAutomationKeepsToTheProjectsTypes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner, key, lib := typedProject(t, h, api, "PTA", "")
	want(t, setProjectTypes(owner, key, lib[bootstrap.TypeTask], lib[bootstrap.TypeBug]), http.StatusOK, "no subtasks")
	task := fileIssue(t, owner, key, map[string]any{"summary": "Work", "typeId": lib[bootstrap.TypeTask]})

	rule := map[string]any{
		"name": "Break it down", "trigger": map[string]any{"kind": "issue.created"},
		"actions": []any{map[string]any{"kind": "create_subissue", "text": "First step"}},
	}
	ruleID := idOf(t, want(t, owner.post("/api/v1/projects/"+key+"/automation/rules", rule), http.StatusCreated, "a rule"), "rule")
	rule["enabled"] = false
	want(t, owner.patch("/api/v1/automation/rules/"+ruleID, rule), http.StatusOK, "only run by hand")
	action := func() map[string]any {
		run := obj(t, want(t, owner.post("/api/v1/automation/rules/"+ruleID+"/run", map[string]any{"issueKey": task}), http.StatusOK, "run the rule"), "run")
		return run["actions"].([]any)[0].(map[string]any)
	}

	if got := action(); got["ok"] != false || got["note"] != key+" uses no subtask type; ask its administrator to allow one" {
		t.Errorf("the rule filed a subtask in a project without subtasks: %v", got)
	}
	allowType(t, owner, key, lib[bootstrap.TypeSubtask])
	if got := action(); got["ok"] != true {
		t.Errorf("the rule did not file a subtask once allowed: %v", got)
	}
}

// A desk's request types file issues as types the project allows, and keep
// those types in it.
func TestDeskKeepsToTheProjectsTypes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner, key, lib := typedProject(t, h, api, "PTD", "service-desk")
	want(t, setProjectTypes(owner, key, lib[bootstrap.TypeTask], lib[bootstrap.TypeBug]), http.StatusOK, "only what the request types use")
	refusedWith(t, setProjectTypes(owner, key, lib[bootstrap.TypeTask]), http.StatusConflict, "conflict",
		"A request type of "+key+" files issues as Bug, so Bug stays. Point the request type at another type first.")

	notStory := key + " does not use Story; ask its administrator to allow it or pick another type."
	refusedWith(t, owner.post("/api/v1/projects/"+key+"/request-types", map[string]any{"name": "Ask for a feature", "issueTypeId": lib[bootstrap.TypeStory]}), http.StatusConflict, "type_not_allowed", notStory)
	existing := list(t, want(t, owner.get("/api/v1/projects/"+key+"/request-types"), http.StatusOK, "the request types"), "requestTypes")[0].(map[string]any)["id"].(string)
	refusedWith(t, owner.patch("/api/v1/request-types/"+existing, map[string]any{"issueTypeId": lib[bootstrap.TypeStory]}), http.StatusConflict, "type_not_allowed", notStory)

	allowType(t, owner, key, lib[bootstrap.TypeStory])
	want(t, owner.post("/api/v1/projects/"+key+"/request-types", map[string]any{"name": "Ask for a feature", "issueTypeId": lib[bootstrap.TypeStory]}), http.StatusCreated, "a request type of an allowed type")
	want(t, owner.patch("/api/v1/request-types/"+existing, map[string]any{"issueTypeId": lib[bootstrap.TypeStory]}), http.StatusOK, "point a request type at an allowed type")
}

// The database holds a project to its types whatever writes the row.
func TestProjectTypesInSQL(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	ctx := context.Background()
	owner, desk, lib := typedProject(t, h, api, "PTS", "service-desk")
	narrow := h.projectKey(t, "PTSN")
	bare := h.projectKey(t, "PTSB")
	for _, key := range []string{narrow, bare} {
		want(t, owner.post("/api/v1/projects", map[string]any{"name": key, "key": key}), http.StatusCreated, "a project")
	}
	want(t, setProjectTypes(owner, narrow, lib[bootstrap.TypeTask], lib[bootstrap.TypeEpic]), http.StatusOK, "narrow a project")
	fringe := idOf(t, want(t, owner.post("/api/v1/issue-types", map[string]any{"name": h.name(t, "Fringe"), "icon": "task"}), http.StatusCreated, "a type nobody allows"), "issueType")
	lonely := idOf(t, want(t, owner.post("/api/v1/issue-types", map[string]any{"name": h.name(t, "Lonely"), "icon": "task"}), http.StatusCreated, "a project's only standard type"), "issueType")
	want(t, setProjectTypes(owner, bare, lonely, lib[bootstrap.TypeEpic]), http.StatusOK, "a project with one standard type")
	gone := idOf(t, want(t, owner.post("/api/v1/issue-types", map[string]any{"name": h.name(t, "Gone"), "icon": "task"}), http.StatusCreated, "a type to retire"), "issueType")
	allowType(t, owner, desk, gone)
	want(t, owner.post("/api/v1/issue-types/"+gone+"/retire", nil), http.StatusOK, "retire it")
	bug := fileIssue(t, owner, desk, map[string]any{"summary": "Broken", "typeId": lib[bootstrap.TypeBug]})

	var deskID, narrowID, bugID, statusID string
	if err := h.super.QueryRow(ctx, `
		SELECT p.id, (SELECT id FROM project WHERE key = $2), i.id, i.status_id FROM issue i JOIN project p ON p.id = i.project_id
		WHERE p.key = $1 AND i.key_num = $3`, desk, narrow, strings.TrimPrefix(bug, desk+"-")).Scan(&deskID, &narrowID, &bugID, &statusID); err != nil {
		t.Fatal(err)
	}

	// Each attempt runs in a transaction that is rolled back, with the deferred
	// rules checked at its end; refusedBy names the rule, or is empty when the
	// database must take it.
	fill := strings.NewReplacer(":desk", "'"+deskID+"'", ":narrow", "'"+narrowID+"'", ":bug_issue", "'"+bugID+"'", ":status", "'"+statusID+"'",
		":fringe", "'"+fringe+"'", ":lonely", "'"+lonely+"'", ":gone", "'"+gone+"'", ":task", "'"+lib[bootstrap.TypeTask]+"'", ":bug_type", "'"+lib[bootstrap.TypeBug]+"'")
	try := func(what, refusedBy string, statements ...string) {
		t.Helper()
		tx, err := h.super.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		for i, statement := range statements {
			if _, err = tx.Exec(ctx, fill.Replace(statement)); err != nil {
				if i < len(statements)-1 {
					t.Fatalf("%s: setting up: %v", what, err)
				}
				break
			}
		}
		if err == nil {
			_, err = tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
		}
		var pgErr *pgconn.PgError
		switch {
		case refusedBy == "" && err != nil:
			t.Errorf("SQL refused %s: %v", what, err)
		case refusedBy != "" && err == nil:
			t.Errorf("SQL allowed %s", what)
		case refusedBy != "" && (!errors.As(err, &pgErr) || pgErr.ConstraintName != refusedBy):
			t.Errorf("SQL refused %s, but not by %s: %v", what, refusedBy, err)
		}
	}
	const (
		newIssue = `INSERT INTO issue (project_id, key_num, issue_type_id, status_id, summary, rank) VALUES (:desk, 99999, %s, :status, 'Raw', 'a')`
		allowed  = issue.ProjectTypeConstraint
		retired  = issue.RetiredTypeConstraint
		standard = issue.StandardTypeConstraint
	)
	try("an issue of a type the project does not use", allowed, strings.Replace(newIssue, "%s", ":fringe", 1))
	try("an issue of a type the project allows", "", `INSERT INTO project_issue_type (project_id, issue_type_id) VALUES (:desk, :fringe)`, strings.Replace(newIssue, "%s", ":fringe", 1))
	try("changing an issue to a type the project does not use", allowed, `UPDATE issue SET issue_type_id = :fringe WHERE id = :bug_issue`)
	try("moving an issue into a project without its type", allowed, `UPDATE issue SET project_id = :narrow, key_num = 99999 WHERE id = :bug_issue`)
	try("taking a type away while an issue uses it", allowed, `DELETE FROM project_issue_type WHERE project_id = :desk AND issue_type_id = :bug_type`)
	try("a request type of a type the project does not use", issue.RequestTypeTypeConstraint, `UPDATE request_type SET issue_type_id = :fringe WHERE project_id = :desk`)
	try("an issue of a retired type", retired, strings.Replace(newIssue, "%s", ":gone", 1))
	try("changing an issue to a retired type", retired, `UPDATE issue SET issue_type_id = :gone WHERE id = :bug_issue`)
	try("adding a retired type to a project", retired, `INSERT INTO project_issue_type (project_id, issue_type_id) VALUES (:narrow, :gone)`)
	try("taking away a project's last standard type", standard, `DELETE FROM project_issue_type WHERE project_id = :narrow AND issue_type_id = :task`)
	try("retiring a project's only standard type", standard, `UPDATE issue_type SET retired_at = now() WHERE id = :lonely`)
	try("deleting a project's only standard type", standard, `DELETE FROM issue_type WHERE id = :lonely`)
	try("replacing a project's standard type in one go", "",
		`DELETE FROM project_issue_type WHERE project_id = :narrow AND issue_type_id = :task`,
		`INSERT INTO project_issue_type (project_id, issue_type_id) VALUES (:narrow, :fringe)`)

	t.Run("a project made in SQL starts with the built-in types", func(t *testing.T) {
		var allowed, builtin int
		tx, err := h.super.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		raw := h.projectKey(t, "PTSR")
		if _, err := tx.Exec(ctx, `INSERT INTO project (key, name) VALUES ($1, 'Raw')`, raw); err != nil {
			t.Fatal(err)
		}
		if err := tx.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM project_issue_type pit JOIN project p ON p.id = pit.project_id WHERE p.key = $1),
			       (SELECT count(*) FROM issue_type WHERE builtin AND retired_at IS NULL)`, raw).Scan(&allowed, &builtin); err != nil {
			t.Fatal(err)
		}
		if allowed != builtin || builtin == 0 {
			t.Errorf("a project made in SQL allows %d types, want the %d built-in ones", allowed, builtin)
		}
	})

	t.Run("deleting a project still removes everything", func(t *testing.T) {
		if _, err := h.super.Exec(ctx, `DELETE FROM project WHERE id = $1`, deskID); err != nil {
			t.Fatalf("a desk with issues and request types could not be deleted: %v", err)
		}
		var left int
		if err := h.super.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM project_issue_type WHERE project_id = $1) + (SELECT count(*) FROM issue WHERE project_id = $1)
			     + (SELECT count(*) FROM request_type WHERE project_id = $1)`, deskID).Scan(&left); err != nil {
			t.Fatal(err)
		}
		if left != 0 {
			t.Errorf("%d rows of the deleted project are left", left)
		}
	})
}
