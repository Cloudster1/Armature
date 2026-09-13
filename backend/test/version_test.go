//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/component"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/version"
)

// A version is a milestone that ships: it collects the issues that fix it and
// the ones it affects, is released, and only then archived; its notes are the
// finished work by type.
func TestAVersionCollectsWhatFixesItAndShips(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "versions")
	versions := version.NewService(h.cluster)

	release := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	v1, _, err := versions.Create(ws.ctx, ws.project.Key, version.Input{Name: ptr("1.0"), ReleaseOn: ptr(&release)}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := versions.Create(ws.ctx, ws.project.Key, version.Input{Name: ptr("1.0")}, ws.actor.UserID); err == nil {
		t.Error("a second 1.0 was accepted")
	}
	late := release.AddDate(0, 1, 0)
	if _, _, err := versions.Create(ws.ctx, ws.project.Key, version.Input{Name: ptr("x"), StartOn: ptr(&late), ReleaseOn: ptr(&release)}, ws.actor.UserID); err == nil {
		t.Error("a version starting after its release was accepted")
	}

	fixed := ws.newIssue(t, "Fix the login")
	affected := ws.newIssue(t, "Slow search")
	if _, _, err := ws.issues.SetVersions(ws.ctx, fixed.Key, []uuid.UUID{v1.ID}, nil, ws.actor); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.issues.SetVersions(ws.ctx, affected.Key, []uuid.UUID{v1.ID}, []uuid.UUID{v1.ID}, ws.actor); err != nil {
		t.Fatal(err)
	}
	got, _ := ws.issues.ByKey(ws.ctx, affected.Key)
	if len(got.FixVersions) != 1 || got.FixVersions[0].Name != "1.0" || len(got.AffectsVersions) != 1 {
		t.Fatalf("issue versions = fix %+v affects %+v", got.FixVersions, got.AffectsVersions)
	}
	history, _ := ws.issues.History(ws.ctx, affected.Key)
	if len(history) < 2 || history[0].Changes[0].Field != "fixVersion" || history[0].Changes[0].To != "1.0" {
		t.Errorf("history = %+v, want the version named", history[0])
	}

	// Another project's version is refused by the service and by the database.
	other := h.newWorkspace(t, "otherversions")
	theirs, _, _ := versions.Create(other.ctx, other.project.Key, version.Input{Name: ptr("9.9")}, other.actor.UserID)
	if _, _, err := ws.issues.SetVersions(ws.ctx, fixed.Key, []uuid.UUID{theirs.ID}, nil, ws.actor); err == nil {
		t.Error("another project's version was accepted by the service")
	}
	if _, err := h.super.Exec(context.Background(), `INSERT INTO issue_version (org_id, issue_id, version_id, role) VALUES ($1, $2, $3, 'fix')`, ws.orgID, fixed.ID, theirs.ID); err == nil || !strings.Contains(err.Error(), "own project") {
		t.Errorf("the database accepted another project's version: %v", err)
	}

	// Progress counts what fixes it; the notes list only the finished.
	ws.move(t, h, fixed.Key, "Start progress")
	ws.move(t, h, fixed.Key, "Ready for review")
	ws.move(t, h, fixed.Key, "Approve")
	current, _ := versions.ByID(ws.ctx, v1.ID)
	if current.Progress.Issues != 2 || current.Progress.Done != 1 {
		t.Errorf("progress = %+v, want 1 of 2 done", current.Progress)
	}
	notes, err := versions.ReleaseNotes(ws.ctx, v1.ID)
	if err != nil || len(notes.Groups) != 1 || len(notes.Groups[0].Issues) != 1 || notes.Groups[0].Issues[0].Key != fixed.Key || notes.Open != 1 {
		t.Fatalf("notes = %+v %v, want the one done issue and one open", notes, err)
	}

	// Archive before release is refused; after release it is not, and an
	// archived version cannot be named.
	if _, _, err := versions.Archive(ws.ctx, v1.ID, ws.actor.UserID); err == nil {
		t.Error("archiving an unreleased version was accepted")
	}
	if _, err := h.super.Exec(context.Background(), `UPDATE version SET archived_at = now() WHERE id = $1`, v1.ID); err == nil || !strings.Contains(err.Error(), "version_archived_after_release") {
		t.Errorf("the database archived an unreleased version: %v", err)
	}
	if _, _, err := versions.Release(ws.ctx, v1.ID, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := versions.Archive(ws.ctx, v1.ID, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.issues.SetVersions(ws.ctx, affected.Key, []uuid.UUID{v1.ID}, nil, ws.actor); err == nil {
		t.Error("an archived version was named")
	}
	listed, _ := versions.List(ws.ctx, ws.project.Key, false)
	if len(listed) != 0 {
		t.Errorf("unarchived list = %d, want the archived one hidden", len(listed))
	}
}

// A component owns new work when it says so: an issue filed in it with no
// assignee goes to its default assignee, and only then.
func TestAComponentTakesNewWork(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "components")
	bob := h.joinExisting(t, ws, h.email(t, "bobcomp"), "member")
	components := component.NewService(h.cluster)

	bobPtr := &bob
	billing, _, err := components.Create(ws.ctx, ws.project.Key, component.Input{Name: ptr("Billing"), DefaultAssigneeID: &bobPtr})
	if err != nil {
		t.Fatal(err)
	}
	if billing.DefaultAssignee == nil || billing.DefaultAssignee.ID != bob {
		t.Fatalf("component = %+v", billing)
	}
	if _, _, err := components.Create(ws.ctx, ws.project.Key, component.Input{Name: ptr("billing")}); err == nil {
		t.Error("a second Billing was accepted")
	}

	taken, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: ws.project.Key, Summary: "Invoice is wrong", ComponentIDs: []uuid.UUID{billing.ID}}, ws.actor)
	if err != nil {
		t.Fatal(err)
	}
	if taken.Assignee == nil || taken.Assignee.ID != bob || len(taken.Components) != 1 || taken.Components[0].Name != "Billing" {
		t.Fatalf("issue = assignee %+v components %+v, want bob in Billing", taken.Assignee, taken.Components)
	}
	history, _ := ws.issues.History(ws.ctx, taken.Key)
	if len(history) != 1 || len(history[0].Changes) != 2 || history[0].Changes[1].Field != "assignee" {
		t.Errorf("history = %+v, want the default assignee written down", history)
	}
	me := ws.actor.UserID
	mine, _, _ := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: ws.project.Key, Summary: "Mine", AssigneeID: &me, ComponentIDs: []uuid.UUID{billing.ID}}, ws.actor)
	if mine.Assignee.ID != me {
		t.Error("a named assignee was overridden by the component's")
	}

	// Another project's component is refused by the service and by the database.
	other := h.newWorkspace(t, "othercomp")
	theirs, _, _ := components.Create(other.ctx, other.project.Key, component.Input{Name: ptr("Theirs")})
	if _, _, err := ws.issues.SetComponents(ws.ctx, taken.Key, []uuid.UUID{theirs.ID}, ws.actor); err == nil {
		t.Error("another project's component was accepted by the service")
	}
	if _, err := h.super.Exec(context.Background(), `INSERT INTO issue_component (org_id, issue_id, component_id) VALUES ($1, $2, $3)`, ws.orgID, taken.ID, theirs.ID); err == nil || !strings.Contains(err.Error(), "own project") {
		t.Errorf("the database accepted another project's component: %v", err)
	}

	if _, _, err := ws.issues.SetComponents(ws.ctx, taken.Key, nil, ws.actor); err != nil {
		t.Fatal(err)
	}
	listed, _ := components.List(ws.ctx, ws.project.Key)
	if len(listed) != 1 || listed[0].Issues != 1 {
		t.Errorf("components = %+v, want Billing with one issue left", listed)
	}
}

// Every operation over the API, the way the pages use them.
func TestVersionsAndComponentsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "versionsapi")
	project := want(t, c.post("/api/v1/projects", map[string]any{"name": "Ships", "key": "SHP" + strings.ToUpper(uuid.New().String()[:3]), "template": "kanban"}), http.StatusCreated, "project")
	key := obj(t, project, "project")["key"].(string)

	want(t, c.post("/api/v1/projects/"+key+"/versions", map[string]any{"name": ""}), http.StatusUnprocessableEntity, "a nameless version")
	made := want(t, c.post("/api/v1/projects/"+key+"/versions", map[string]any{"name": "2.0", "releaseOn": "2026-12-01T00:00:00Z"}), http.StatusCreated, "a version")
	versionID := idOf(t, made, "version")
	want(t, c.post("/api/v1/projects/"+key+"/versions", map[string]any{"name": "2.0"}), http.StatusConflict, "a duplicate")
	want(t, c.get("/api/v1/projects/"+key+"/versions?archived=true"), http.StatusOK, "versions")
	want(t, c.patch("/api/v1/versions/"+versionID, map[string]any{"description": "The big one", "clearRelease": true}), http.StatusOK, "editing")
	want(t, c.post("/api/v1/versions/"+versionID+"/archive", nil), http.StatusConflict, "archiving unreleased")

	issueMade := want(t, c.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "Ship it"}), http.StatusCreated, "issue")
	issueKey := obj(t, issueMade, "issue")["key"].(string)
	set := want(t, c.put("/api/v1/issues/"+issueKey+"/versions", map[string]any{"fix": []string{versionID}, "affects": []string{}}), http.StatusOK, "naming the version")
	if fix := list(t, set, "issue", "fixVersions"); len(fix) != 1 {
		t.Errorf("fixVersions = %v", fix)
	}
	want(t, c.put("/api/v1/issues/"+issueKey+"/versions", map[string]any{"fix": []string{uuid.New().String()}}), http.StatusNotFound, "a version that is not here")
	want(t, c.get("/api/v1/versions/"+versionID+"/notes"), http.StatusOK, "notes")
	want(t, c.get("/api/v1/projects/"+key+"/reports/versions"), http.StatusOK, "the releases report")
	want(t, c.get("/api/v1/projects/"+key+"/reports/chart?groupBy=fixVersion"), http.StatusOK, "a chart by version")
	want(t, c.get("/api/v1/projects/"+key+"/issues?q=fixVersion+IN+(unreleasedVersions())"), http.StatusOK, "a query by version")
	want(t, c.post("/api/v1/versions/"+versionID+"/release", nil), http.StatusOK, "releasing")
	want(t, c.post("/api/v1/versions/"+versionID+"/unrelease", nil), http.StatusOK, "taking it back")
	want(t, c.post("/api/v1/versions/"+versionID+"/release", nil), http.StatusOK, "releasing again")
	want(t, c.post("/api/v1/versions/"+versionID+"/archive", nil), http.StatusOK, "archiving")
	want(t, c.patch("/api/v1/versions/"+versionID, map[string]any{"name": "2.1"}), http.StatusConflict, "editing an archived version")
	want(t, c.post("/api/v1/versions/"+versionID+"/unrelease", nil), http.StatusConflict, "unreleasing an archived version")
	want(t, c.get("/api/v1/versions/"+uuid.New().String()+"/notes"), http.StatusNotFound, "notes of nothing")
	want(t, c.delete("/api/v1/versions/"+versionID), http.StatusNoContent, "deleting")
	want(t, c.delete("/api/v1/versions/"+versionID), http.StatusNotFound, "deleting twice")

	want(t, c.post("/api/v1/projects/"+key+"/components", map[string]any{"name": ""}), http.StatusUnprocessableEntity, "a nameless component")
	comp := want(t, c.post("/api/v1/projects/"+key+"/components", map[string]any{"name": "Billing"}), http.StatusCreated, "a component")
	compID := idOf(t, comp, "component")
	want(t, c.get("/api/v1/projects/"+key+"/components"), http.StatusOK, "components")
	want(t, c.patch("/api/v1/components/"+compID, map[string]any{"description": "Money in, money out"}), http.StatusOK, "editing")
	want(t, c.put("/api/v1/issues/"+issueKey+"/components", map[string]any{"components": []string{compID}}), http.StatusOK, "putting the issue in it")
	want(t, c.put("/api/v1/issues/"+issueKey+"/components", map[string]any{"components": []string{uuid.New().String()}}), http.StatusNotFound, "a component that is not here")
	want(t, c.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "In billing", "componentIds": []string{compID}}), http.StatusCreated, "filing into a component")
	want(t, c.get("/api/v1/projects/"+key+"/reports/chart?groupBy=component"), http.StatusOK, "a chart by component")
	want(t, c.patch("/api/v1/components/"+uuid.New().String(), map[string]any{"name": "x"}), http.StatusNotFound, "editing nothing")
	want(t, c.delete("/api/v1/components/"+compID), http.StatusNoContent, "deleting")
	want(t, c.delete("/api/v1/components/"+compID), http.StatusNotFound, "deleting twice")
}
