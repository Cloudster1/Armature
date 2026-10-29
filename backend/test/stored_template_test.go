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
	"github.com/armature/armature/backend/internal/template"
)

// ownScheme is a workflow scheme of the test's own, over a copy of the default workflow.
func ownScheme(t *testing.T, h *harness, c *client, prefix string) string {
	t.Helper()
	var defaultID string
	for _, raw := range list(t, want(t, c.get("/api/v1/workflows?q="+url.QueryEscape(bootstrap.DefaultWorkflowName)), http.StatusOK, "the workflows"), "workflows") {
		if w := raw.(map[string]any); w["name"] == bootstrap.DefaultWorkflowName {
			defaultID = w["id"].(string)
		}
	}
	if defaultID == "" {
		t.Fatalf("no workflow is called %q", bootstrap.DefaultWorkflowName)
	}
	workflowID := idOf(t, want(t, c.post("/api/v1/workflows/"+defaultID+"/copy", map[string]any{"name": h.name(t, prefix+" flow")}), http.StatusCreated, "copy the default workflow"), "workflow")
	return idOf(t, want(t, c.post("/api/v1/workflow-schemes", map[string]any{
		"name": h.name(t, prefix+" scheme"), "items": []map[string]any{{"workflowId": workflowID}},
	}), http.StatusCreated, "a scheme of the test's own"), "scheme")
}

// templateTypeNames reads the names of a template's types, in its order.
func templateTypeNames(tpl map[string]any) []string {
	var names []string
	for _, raw := range tpl["issueTypes"].([]any) {
		names = append(names, raw.(map[string]any)["name"].(string))
	}
	return names
}

// projectShape is what a template gave a project, read back over the API.
type projectShape struct {
	kind, scheme, planning, board string
	features, types               []string
}

func templateShapeOf(t *testing.T, c *client, key string) projectShape {
	t.Helper()
	p := obj(t, want(t, c.get("/api/v1/projects/"+key), http.StatusOK, "the project"), "project")
	s := projectShape{kind: p["kind"].(string), planning: p["planningMethod"].(string)}
	if id, ok := p["workflowSchemeId"].(string); ok {
		s.scheme = id
	}
	for _, f := range p["features"].([]any) {
		s.features = append(s.features, f.(string))
	}
	s.board = obj(t, want(t, c.get("/api/v1/projects/"+key+"/board"), http.StatusOK, "the project's board"), "board")["type"].(string)
	s.types = typeNames(t, c.get("/api/v1/projects/"+key+"/issue-types"))
	return s
}

func (s projectShape) String() string {
	return strings.Join([]string{s.kind, s.scheme, s.planning, s.board, strings.Join(s.features, ","), strings.Join(s.types, ",")}, " | ")
}

func (s projectShape) equal(o projectShape) bool {
	return s.kind == o.kind && s.scheme == o.scheme && s.planning == o.planning && s.board == o.board &&
		slices.Equal(s.features, o.features) && slices.Equal(s.types, o.types)
}

// A stored template gives a project exactly its kind, board, planning, pages,
// scheme and types; the project keeps them however the template changes later.
func TestAStoredTemplateIsAppliedOnce(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	admin := api.client(t)
	signedIn := admin.signIn(t, h.person(t, "templatekeeper", auth.RoleAdmin))
	adminID := principalField(t, signedIn, "principal", "user", "id").(string)
	lib := libraryIDs(t, admin)
	ticket := h.name(t, "Ticket")
	ticketID := idOf(t, want(t, admin.post("/api/v1/issue-types", map[string]any{"name": ticket, "icon": "task", "level": 0}), http.StatusCreated, "a type of our own"), "issueType")
	scheme := ownScheme(t, h, admin, "Stored")

	name := h.name(t, "Errands")
	made := obj(t, want(t, admin.post("/api/v1/project-templates", map[string]any{
		"name": name, "description": "Small jobs around the office.", "kind": "business", "boardType": "scrum", "planningMethod": "scrum",
		"features": []string{"calendar", "board"}, "workflowSchemeId": scheme, "issueTypeIds": []string{ticketID, lib[bootstrap.TypeBug]},
	}), http.StatusCreated, "store a template"), "template")
	key := made["key"].(string)
	if made["builtin"] != false || made["archived"] != false || made["scheme"].(map[string]any)["id"] != scheme {
		t.Errorf("the stored template reads back as %v", made)
	}
	if got := templateTypeNames(made); !slices.Equal(got, []string{ticket, bootstrap.TypeBug}) {
		t.Errorf("the template's types are %v, want %s then Bug", got, ticket)
	}

	t.Run("listed after the built-ins", func(t *testing.T) {
		listed := list(t, want(t, admin.get("/api/v1/project-templates"), http.StatusOK, "the templates"), "templates")
		builtins := template.All()
		for i, tpl := range builtins {
			row := listed[i].(map[string]any)
			if row["key"] != tpl.Key || row["builtin"] != true || len(row["issueTypes"].([]any)) == 0 {
				t.Errorf("entry %d is %v, want the built-in %s with its types", i, row, tpl.Key)
			}
		}
		if got := find(t, listed, "key", "task-tracking")["scheme"]; got == nil || got.(map[string]any)["name"] != "Task tracking workflows" {
			t.Errorf("Task tracking names its scheme as %v", got)
		}
		if find(t, listed, "key", template.Default)["scheme"] != nil {
			t.Error("Kanban names a scheme, want everyone's default")
		}
		ours := slices.IndexFunc(listed, func(raw any) bool { return raw.(map[string]any)["key"] == key })
		if ours < len(builtins) {
			t.Errorf("the stored template is at %d, want after the %d built-ins", ours, len(builtins))
		}
	})

	pkey := h.projectKey(t, "STO")
	p := obj(t, want(t, admin.post("/api/v1/projects", map[string]any{"name": "Errands", "key": pkey, "template": key}), http.StatusCreated, "a project from it"), "project")
	if p["template"] != key {
		t.Errorf("the project began from %v, want %s", p["template"], key)
	}
	before := templateShapeOf(t, admin, pkey)
	wantShape := projectShape{kind: "business", scheme: scheme, planning: "scrum", board: "scrum",
		features: []string{"board", "calendar"}, types: []string{ticket, bootstrap.TypeBug}}
	if !before.equal(wantShape) {
		t.Fatalf("the project is %v, want %v", before, wantShape)
	}

	// The template changes in every way it can, and the project does not.
	edited := obj(t, want(t, admin.patch("/api/v1/project-templates/"+key, map[string]any{
		"name": h.name(t, "Errands revised"), "kind": "software", "boardType": "kanban", "planningMethod": "kanban",
		"features": []string{"board", "sprints"}, "defaultWorkflowScheme": true, "issueTypeIds": []string{lib[bootstrap.TypeTask]},
	}), http.StatusOK, "edit the template"), "template")
	if edited["scheme"] != nil || !slices.Equal(templateTypeNames(edited), []string{bootstrap.TypeTask}) {
		t.Errorf("the edited template reads back as %v", edited)
	}
	if after := templateShapeOf(t, admin, pkey); !after.equal(before) {
		t.Errorf("editing the template changed the project: %v, was %v", after, before)
	}
	later := h.projectKey(t, "STL")
	want(t, admin.post("/api/v1/projects", map[string]any{"name": "Later", "key": later, "template": key}), http.StatusCreated, "a project from the edited template")
	if got := templateShapeOf(t, admin, later); got.kind != "software" || got.scheme != "" || got.board != "kanban" || !slices.Equal(got.types, []string{bootstrap.TypeTask}) {
		t.Errorf("a project from the edited template is %v", got)
	}

	archived := obj(t, want(t, admin.post("/api/v1/project-templates/"+key+"/archive", nil), http.StatusOK, "archive the template"), "template")
	if archived["archived"] != true {
		t.Errorf("the archived template reads back as %v", archived)
	}
	if after := templateShapeOf(t, admin, pkey); !after.equal(before) {
		t.Errorf("archiving the template changed the project: %v, was %v", after, before)
	}
	offered := list(t, want(t, admin.get("/api/v1/project-templates"), http.StatusOK, "the templates on offer"), "templates")
	if slices.ContainsFunc(offered, func(raw any) bool { return raw.(map[string]any)["key"] == key }) {
		t.Error("the archived template is still on offer")
	}
	all := want(t, admin.get("/api/v1/project-templates?archived=true"), http.StatusOK, "every template")
	if find(t, all.Body["templates"], "key", key)["archived"] != true {
		t.Error("the archived template is not listed with archived=true")
	}
	r := admin.post("/api/v1/projects", map[string]any{"name": "Refused", "key": h.projectKey(t, "STR"), "template": key})
	if r.Status != http.StatusConflict || !strings.Contains(r.Raw, "archived") {
		t.Errorf("a project from an archived template: %d %s, want 409 saying it is archived", r.Status, r.Raw)
	}

	restored := obj(t, want(t, admin.post("/api/v1/project-templates/"+key+"/restore", nil), http.StatusOK, "restore the template"), "template")
	if restored["archived"] != false {
		t.Errorf("the restored template reads back as %v", restored)
	}
	want(t, admin.post("/api/v1/projects", map[string]any{"name": "Again", "key": h.projectKey(t, "STA"), "template": key}), http.StatusCreated, "a project from the restored template")

	t.Run("every change is in the audit log", func(t *testing.T) {
		for _, action := range []string{"project_template.created", "project_template.updated", "project_template.archived", "project_template.restored"} {
			page := want(t, admin.get("/api/v1/audit?action="+url.QueryEscape(action)+"&actor="+adminID), http.StatusOK, "the log of "+action)
			if !slices.ContainsFunc(list(t, page, "entries"), func(raw any) bool {
				row := raw.(map[string]any)
				return row["targetId"] == key && row["targetType"] == "project_template"
			}) {
				t.Errorf("no %s entry for the template", action)
			}
		}
	})

	t.Run("a built-in stays as it is", func(t *testing.T) {
		r := admin.patch("/api/v1/project-templates/scrum", map[string]any{"name": "Mine"})
		if r.Status != http.StatusConflict {
			t.Errorf("changing Scrum: %d %s, want 409", r.Status, r.Raw)
		}
	})

	t.Run("a member is refused", func(t *testing.T) {
		member := api.client(t)
		member.signIn(t, h.person(t, "templateuser", auth.RoleMember))
		for _, r := range []response{
			member.post("/api/v1/project-templates", map[string]any{"name": h.name(t, "Never")}),
			member.post("/api/v1/project-templates", map[string]any{"from": "scrum"}),
			member.patch("/api/v1/project-templates/"+key, map[string]any{"name": "Mine"}),
			member.post("/api/v1/project-templates/"+key+"/archive", nil),
			member.post("/api/v1/project-templates/"+key+"/restore", nil),
			member.get("/api/v1/project-templates?archived=true"),
		} {
			if r.Status != http.StatusForbidden {
				t.Errorf("a member got %d, want 403: %s", r.Status, r.Raw)
			}
		}
		// Reading what is on offer is everyone's, since anyone may be making a project.
		want(t, member.get("/api/v1/project-templates"), http.StatusOK, "a member reads the templates")
	})
}

// Duplicating a built-in copies its workflow, as a real scheme, its pages and its types.
func TestDuplicatingABuiltinCopiesIt(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	admin := api.client(t)
	admin.signIn(t, h.person(t, "duplicator", auth.RoleAdmin))

	scrum, _ := template.Find("scrum")
	name := h.name(t, "Our Scrum")
	copied := obj(t, want(t, admin.post("/api/v1/project-templates", map[string]any{"from": "scrum", "name": name}), http.StatusCreated, "duplicate Scrum"), "template")
	if copied["kind"] != string(scrum.Kind) || copied["boardType"] != string(scrum.BoardType) || copied["planningMethod"] != string(scrum.PlanningMethod) {
		t.Errorf("the copy of Scrum is %v", copied)
	}
	var features []string
	for _, f := range copied["features"].([]any) {
		features = append(features, f.(string))
	}
	var scrumFeatures []string
	for _, f := range scrum.Features {
		scrumFeatures = append(scrumFeatures, string(f))
	}
	if want := scrumFeatures; !slices.Equal(features, want) {
		t.Errorf("the copy has the pages %v, Scrum has %v", features, want)
	}
	// Scrum follows everyone's workflow, so its copy does too.
	if copied["scheme"] != nil {
		t.Errorf("the copy of Scrum names the scheme %v, want everyone's default", copied["scheme"])
	}
	builtins := list(t, want(t, admin.get("/api/v1/project-templates"), http.StatusOK, "the templates"), "templates")
	if got, want := templateTypeNames(copied), templateTypeNames(find(t, builtins, "key", "scrum")); !slices.Equal(got, want) {
		t.Errorf("the copy has the types %v, Scrum has %v", got, want)
	}

	// A built-in that brings a workflow gives its copy the scheme a project from it gets.
	tasks := obj(t, want(t, admin.post("/api/v1/project-templates", map[string]any{"from": "task-tracking", "name": h.name(t, "Our tasks")}), http.StatusCreated, "duplicate Task tracking"), "template")
	scheme, _ := tasks["scheme"].(map[string]any)
	if scheme == nil || scheme["name"] != "Task tracking workflows" || scheme["id"] == nil {
		t.Fatalf("the copy of Task tracking names the scheme %v", tasks["scheme"])
	}
	key := h.projectKey(t, "DUP")
	p := obj(t, want(t, admin.post("/api/v1/projects", map[string]any{"name": "Chores", "key": key, "template": "task-tracking"}), http.StatusCreated, "a project from Task tracking"), "project")
	if p["workflowSchemeId"] != scheme["id"] {
		t.Errorf("the copy names %v, a project from Task tracking follows %v", scheme["id"], p["workflowSchemeId"])
	}

	// A copy of a copy, named for what it copies when no name is given.
	again := obj(t, want(t, admin.post("/api/v1/project-templates", map[string]any{"from": copied["key"]}), http.StatusCreated, "duplicate the copy"), "template")
	t.Cleanup(func() {
		_, _ = h.super.Exec(context.Background(), `DELETE FROM project_template WHERE id = $1`, again["key"])
	})
	if again["name"] != name+" copy" || !slices.Equal(templateTypeNames(again), templateTypeNames(copied)) {
		t.Errorf("the copy of the copy is %v", again)
	}
	r := admin.post("/api/v1/project-templates", map[string]any{"from": "scrum", "name": strings.ToUpper(name)})
	if r.Status != http.StatusConflict {
		t.Errorf("a second template named %q regardless of case: %d %s, want 409", name, r.Status, r.Raw)
	}
	r = admin.post("/api/v1/project-templates", map[string]any{"from": "waterfall", "name": h.name(t, "Never")})
	if r.Status != http.StatusBadRequest {
		t.Errorf("duplicating a template that does not exist: %d %s, want 400", r.Status, r.Raw)
	}
}

// A desk template sets the desk up, and the service refuses what the database does.
func TestAStoredDeskTemplateSetsUpTheDesk(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	admin := api.client(t)
	admin.signIn(t, h.person(t, "deskkeeper", auth.RoleAdmin))
	lib := libraryIDs(t, admin)

	desk := obj(t, want(t, admin.post("/api/v1/project-templates", map[string]any{"from": "service-desk", "name": h.name(t, "Our desk"),
		"issueTypeIds": []string{lib[bootstrap.TypeTask]}}), http.StatusCreated, "a desk template with Task alone"), "template")["key"].(string)
	key := h.projectKey(t, "DSK")
	want(t, admin.post("/api/v1/projects", map[string]any{"name": "Help", "key": key, "template": desk}), http.StatusCreated, "a desk from it")
	requests := list(t, want(t, admin.get("/api/v1/projects/"+key+"/request-types"), http.StatusOK, "the request types"), "requestTypes")
	if len(requests) == 0 {
		t.Error("the desk was not set up: it has no request types")
	}
	if got := typeNames(t, admin.get("/api/v1/projects/"+key+"/issue-types")); !slices.Equal(got, []string{bootstrap.TypeTask}) {
		t.Errorf("the desk allows %v, want Task alone", got)
	}

	for what, body := range map[string]map[string]any{
		"a desk that is not a service project": {"name": h.name(t, "Bad desk"), "kind": "software", "desk": true},
		"no types":                             {"name": h.name(t, "No types"), "issueTypeIds": []string{}},
		"no standard type":                     {"name": h.name(t, "Epics only"), "issueTypeIds": []string{lib[bootstrap.TypeEpic]}},
		"a scheme that does not exist":         {"name": h.name(t, "Lost scheme"), "workflowSchemeId": "01900000-0000-7000-8000-000000000009"},
	} {
		r := admin.post("/api/v1/project-templates", body)
		if r.Status != http.StatusUnprocessableEntity {
			t.Errorf("%s: %d %s, want 422", what, r.Status, r.Raw)
		}
	}
}

// The database keeps a template whole whatever the service believed.
func TestTheDatabaseKeepsATemplateWhole(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	admin := api.client(t)
	admin.signIn(t, h.person(t, "templateguard", auth.RoleAdmin))
	lib := libraryIDs(t, admin)
	lonely := idOf(t, want(t, admin.post("/api/v1/issue-types", map[string]any{"name": h.name(t, "Lonely"), "icon": "task", "level": 0}), http.StatusCreated, "a type of our own"), "issueType")
	gone := idOf(t, want(t, admin.post("/api/v1/issue-types", map[string]any{"name": h.name(t, "Gone"), "icon": "task", "level": 0}), http.StatusCreated, "a type to retire"), "issueType")
	want(t, admin.post("/api/v1/issue-types/"+gone+"/retire", nil), http.StatusOK, "retire it")
	scheme := ownScheme(t, h, admin, "Guarded")
	tpl := obj(t, want(t, admin.post("/api/v1/project-templates", map[string]any{
		"name": h.name(t, "Guarded"), "workflowSchemeId": scheme, "issueTypeIds": []string{lonely, lib[bootstrap.TypeEpic]},
	}), http.StatusCreated, "a template"), "template")["key"].(string)

	ctx := context.Background()
	fill := strings.NewReplacer(":tpl", "'"+tpl+"'", ":lonely", "'"+lonely+"'", ":gone", "'"+gone+"'", ":scheme", "'"+scheme+"'",
		":task", "'"+lib[bootstrap.TypeTask]+"'", ":epic", "'"+lib[bootstrap.TypeEpic]+"'", ":name", "'"+h.name(t, "Raw")+"'")
	// Each attempt is rolled back after the deferred rules are checked; refusedBy
	// names the rule that must refuse it, or is empty when the database must take it.
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
	const standard = template.StandardTypeConstraint
	try("a desk template that is not a service project", "project_template_desk_is_service", `UPDATE project_template SET desk = true WHERE id = :tpl`)
	try("a desk template of a service project", "", `UPDATE project_template SET kind = 'service', desk = true WHERE id = :tpl`)
	try("the desk's pages on a software template", "project_template_features_desk", `UPDATE project_template SET features = '{board,queues}' WHERE id = :tpl`)
	try("a page that does not exist", "project_template_features_known", `UPDATE project_template SET features = '{board,gantt}' WHERE id = :tpl`)
	try("a template with no types", standard, `INSERT INTO project_template (name) VALUES (:name)`)
	try("a template with only an epic", standard,
		`INSERT INTO project_template (id, name) VALUES ('01900000-0000-7000-8000-00000000abcd', :name)`,
		`INSERT INTO project_template_issue_type (template_id, issue_type_id) VALUES ('01900000-0000-7000-8000-00000000abcd', :epic)`)
	try("a template made whole in one go", "",
		`INSERT INTO project_template (id, name) VALUES ('01900000-0000-7000-8000-00000000abcd', :name)`,
		`INSERT INTO project_template_issue_type (template_id, issue_type_id) VALUES ('01900000-0000-7000-8000-00000000abcd', :task)`)
	try("taking away a template's last standard type", standard, `DELETE FROM project_template_issue_type WHERE template_id = :tpl AND issue_type_id = :lonely`)
	try("retiring a template's only standard type", standard, `UPDATE issue_type SET retired_at = now() WHERE id = :lonely`)
	try("adding a retired type to a template", "issue_type_not_retired", `INSERT INTO project_template_issue_type (template_id, issue_type_id) VALUES (:tpl, :gone)`)
	try("deleting a type a template uses", "project_template_issue_type_issue_type_id_fkey", `DELETE FROM issue_type WHERE id = :lonely`)
	try("deleting a scheme a template uses", "project_template_workflow_scheme_id_fkey", `DELETE FROM workflow_scheme WHERE id = :scheme`)
	try("a second template by the same name in other letters", "project_template_name_idx",
		`INSERT INTO project_template (name) SELECT upper(name) FROM project_template WHERE id = :tpl`)
	try("deleting a template", "", `DELETE FROM project_template WHERE id = :tpl`)

	t.Run("the service says the same", func(t *testing.T) {
		for what, r := range map[string]response{
			"deleting the scheme":     admin.delete("/api/v1/workflow-schemes/" + scheme),
			"deleting the type":       admin.delete("/api/v1/issue-types/" + lonely),
			"retiring the type":       admin.post("/api/v1/issue-types/"+lonely+"/retire", nil),
			"adding the retired type": admin.patch("/api/v1/project-templates/"+tpl, map[string]any{"issueTypeIds": []string{lonely, gone}}),
		} {
			if r.Status != http.StatusConflict && r.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s, want a refusal", what, r.Status, r.Raw)
			}
			if !strings.Contains(r.Raw, "template") && !strings.Contains(r.Raw, "retired") {
				t.Errorf("%s is refused without saying why: %s", what, r.Raw)
			}
		}
	})
}
