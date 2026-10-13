//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/arrange"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/field"
)

// Until somebody arranges a project's issues, every type is drawn the way the
// installation draws it: the built-in way, unless everyone's is arranged.
func TestAnUnarrangedProjectIsDrawnTheInstallationsWay(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "arrange")
	arrangements := arrange.NewService(h.cluster)

	here, err := arrangements.InProject(ws.ctx, ws.project.Key)
	if err != nil {
		t.Fatalf("the project's arrangements: %v", err)
	}
	if len(here) == 0 {
		t.Fatal("no issue type was answered for")
	}
	everyone, err := arrangements.InOrg(ws.ctx)
	if err != nil {
		t.Fatalf("the installation's arrangements: %v", err)
	}
	if len(everyone) != len(here) {
		t.Errorf("the installation answers for %d types and the project for %d", len(everyone), len(here))
	}
	installation := map[uuid.UUID]arrange.Arrangement{}
	for _, each := range everyone {
		installation[each.IssueTypeID] = each
	}
	for _, each := range here {
		theirs := installation[each.IssueTypeID]
		if each.Origin.Scope != theirs.Origin.Scope || len(each.Places) != len(theirs.Places) {
			t.Errorf("%s is arranged by %s with %d places, want the installation's %s with %d", each.IssueTypeName, each.Origin.Scope, len(each.Places), theirs.Origin.Scope, len(theirs.Places))
		}
		if each.Origin.Scope == arrange.ScopeBuiltin && len(each.Places) != len(arrange.Default()) {
			t.Errorf("%s holds %d places, want the built-in's %d", each.IssueTypeName, len(each.Places), len(arrange.Default()))
		}
	}

	// The wall is the database's, not only the service's.
	_, err = h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO issue_arrangement (org_id) VALUES ($1)`, uuid.New())
		return err
	})
	if err == nil {
		t.Error("an arrangement was written for another organization")
	}
}

// Both ways over the API: read here, read the organization's, and be refused
// the project that is not yours and the organization you do not administer.
func TestArrangementsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	// Everyone's arrangement is what every project follows, so it is put back as found.
	putBackAfter(t, h, "issue_arrangement", "project_id IS NULL")
	arrKey := h.projectKey(t, "ARR")
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "arrangeapi", auth.RoleOwner))
	project := want(t, owner.post("/api/v1/projects", map[string]any{
		"name": "Arranged", "key": arrKey, "template": "kanban",
	}), http.StatusCreated, "project")
	key := obj(t, project, "project")["key"].(string)

	read := want(t, owner.get("/api/v1/projects/"+key+"/issue-arrangement"), http.StatusOK, "the project's arrangement")
	if len(list(t, read, "arrangements")) == 0 {
		t.Errorf("arrangements = %s, want one per issue type", read.Raw)
	}
	want(t, owner.get("/api/v1/issue-arrangement"), http.StatusOK, "the organization's arrangement")

	// A member who does not administer the organization does not read what
	// every project of it follows.
	member := api.client(t)
	member.signIn(t, h.person(t, "plainarrange", auth.RoleMember))
	want(t, member.get("/api/v1/issue-arrangement"), http.StatusForbidden, "the organization's, to somebody who does not administer it")

	// Arranging one type here, and for every project.
	places := []map[string]any{{"area": "people", "slot": "assignee"}, {"area": "hidden", "slot": "team"}}
	arranged := want(t, owner.put("/api/v1/projects/"+key+"/issue-arrangement", map[string]any{"places": places}), http.StatusOK, "arranging this project")
	if len(list(t, arranged, "arrangements")) == 0 {
		t.Errorf("arranging answered %s, want every type back", arranged.Raw)
	}
	want(t, owner.put("/api/v1/issue-arrangement", map[string]any{"places": places}), http.StatusOK, "arranging the organization")
	handed := want(t, owner.put("/api/v1/projects/"+key+"/issue-arrangement", map[string]any{"places": nil}), http.StatusOK, "handing the type back")
	// What every project follows is said to be everyone's.
	scopes := map[string]bool{}
	for _, each := range list(t, handed, "arrangements") {
		scopes[each.(map[string]any)["origin"].(map[string]any)["scope"].(string)] = true
	}
	if !scopes["everyone"] || scopes["organization"] {
		t.Errorf("handed back, the arrangements come from %v, want everyone's", scopes)
	}
	want(t, owner.put("/api/v1/projects/"+key+"/issue-arrangement", map[string]any{
		"places": []map[string]any{{"area": "people", "slot": "nonsense"}},
	}), http.StatusUnprocessableEntity, "a field this tracker does not know")

	want(t, member.put("/api/v1/projects/"+key+"/issue-arrangement", map[string]any{"places": places}), http.StatusForbidden, "arranging a project somebody does not administer")
	want(t, member.put("/api/v1/issue-arrangement", map[string]any{"places": places}), http.StatusForbidden, "arranging the organization without administering it")

	// A project somebody may not read is not refused, it is not there.
	stranger := api.client(t)
	stranger.signIn(t, h.outsider(t, "arrangestranger"))
	want(t, stranger.get("/api/v1/projects/"+key+"/issue-arrangement"), http.StatusNotFound, "a project behind its wall")
}

// A project's own arrangement beats the organization's, and the organization's
// beats the built-in, per issue type.
func TestAProjectArrangesItsOwnIssues(t *testing.T) {
	h := newHarness(t)
	// Everyone's arrangement is what every project follows, so it is put back as found.
	putBackAfter(t, h, "issue_arrangement", "project_id IS NULL")
	if _, err := h.super.Exec(context.Background(), `DELETE FROM issue_arrangement WHERE project_id IS NULL`); err != nil {
		t.Fatal(err)
	}
	ws := h.newProject(t, "arranging")
	arrangements := arrange.NewService(h.cluster)
	fields := field.NewService(h.cluster)
	story := h.issueTypeID(t, ws, "Story")

	own, _, err := fields.Create(ws.ctx, ws.project.Key, field.Input{Name: "Customer", Kind: field.Text})
	if err != nil {
		t.Fatalf("a field of this project: %v", err)
	}
	places := []arrange.Placement{
		{Area: arrange.AreaMain, Slot: arrange.SlotDescription},
		{Area: arrange.AreaPeople, Slot: arrange.SlotAssignee},
		{Area: arrange.AreaMore, FieldID: &own.ID},
		{Area: arrange.AreaHidden, Slot: arrange.SlotTeam},
	}
	if _, err := arrangements.SaveInProject(ws.ctx, ws.project.Key, &story, places); err != nil {
		t.Fatalf("arrange the story: %v", err)
	}
	if got := arrangementFor(t, arrangements, ws, story); got.Origin.Scope != arrange.ScopeProject || !got.Origin.Named {
		t.Errorf("origin = %+v, want the project's own, by name", got.Origin)
	} else if len(got.Places) != len(places) || !placed(got.Places, arrange.AreaHidden, arrange.SlotTeam) {
		t.Errorf("places = %+v, want the four that were saved, the team among them hidden", got.Places)
	}

	// Every other type still follows the built-in one.
	bug := h.issueTypeID(t, ws, "Bug")
	if got := arrangementFor(t, arrangements, ws, bug); got.Origin.Scope != arrange.ScopeBuiltin {
		t.Errorf("another type is arranged by %s, want the built-in", got.Origin.Scope)
	}

	// The organization answers for the types no project names. It arranges
	// only what it owns, so its own list names no field of one project.
	everywhere := []arrange.Placement{
		{Area: arrange.AreaMain, Slot: arrange.SlotDescription},
		{Area: arrange.AreaPeople, Slot: arrange.SlotAssignee},
	}
	if _, err := arrangements.SaveInOrg(ws.ctx, nil, everywhere); err != nil {
		t.Fatalf("arrange for the organization: %v", err)
	}
	if got := arrangementFor(t, arrangements, ws, bug); got.Origin.Scope != arrange.ScopeEveryone || got.Origin.Named {
		t.Errorf("origin = %+v, want the organization's fallback", got.Origin)
	}

	// Handing the type back leaves the organization's answer standing.
	if _, err := arrangements.SaveInProject(ws.ctx, ws.project.Key, &story, nil); err != nil {
		t.Fatalf("hand the story back: %v", err)
	}
	if got := arrangementFor(t, arrangements, ws, story); got.Origin.Scope != arrange.ScopeEveryone {
		t.Errorf("origin = %+v, want the organization's after handing it back", got.Origin)
	}

	// The organization arranges its own fields, not one project's.
	if _, err := arrangements.SaveInOrg(ws.ctx, &story, []arrange.Placement{{Area: arrange.AreaMore, FieldID: &own.ID}}); err == nil {
		t.Error("the organization placed a field belonging to one project")
	}
	// A field placed twice is a field in two places.
	twice := []arrange.Placement{{Area: arrange.AreaPeople, Slot: arrange.SlotAssignee}, {Area: arrange.AreaMore, Slot: arrange.SlotAssignee}}
	if _, err := arrangements.SaveInProject(ws.ctx, ws.project.Key, &story, twice); err == nil {
		t.Error("one field was placed twice")
	}
	// A field the project deletes takes its place with it.
	if _, err := arrangements.SaveInProject(ws.ctx, ws.project.Key, &story, places); err != nil {
		t.Fatal(err)
	}
	if _, err := fields.Delete(ws.ctx, own.ID); err != nil {
		t.Fatal(err)
	}
	for _, place := range arrangementFor(t, arrangements, ws, story).Places {
		if place.FieldID != nil {
			t.Errorf("a deleted field is still placed: %+v", place)
		}
	}
}

func placed(places []arrange.Placement, area arrange.Area, slot arrange.Slot) bool {
	for _, place := range places {
		if place.Area == area && place.Slot == slot {
			return true
		}
	}
	return false
}

func arrangementFor(t *testing.T, arrangements *arrange.Service, ws *workspace, issueTypeID uuid.UUID) arrange.Arrangement {
	t.Helper()
	all, err := arrangements.InProject(ws.ctx, ws.project.Key)
	if err != nil {
		t.Fatalf("read the arrangements: %v", err)
	}
	for _, each := range all {
		if each.IssueTypeID == issueTypeID {
			return each
		}
	}
	t.Fatalf("no arrangement for issue type %s", issueTypeID)
	return arrange.Arrangement{}
}
