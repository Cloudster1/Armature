//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/issuetype"
)

// Administrators keep their own issue types; the built-in ones stay as code
// expects them, and the database refuses what the service does.
func TestAdministratorsKeepALibraryOfIssueTypes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	signedIn := owner.signIn(t, h.person(t, "typekeeper", auth.RoleAdmin))
	ownerID := principalField(t, signedIn, "principal", "user", "id").(string)
	member := api.client(t)
	member.signIn(t, h.person(t, "typeuser", auth.RoleMember))

	types := want(t, owner.get("/api/v1/issue-types"), http.StatusOK, "the library")
	for _, name := range []string{bootstrap.TypeTask, bootstrap.TypeBug, bootstrap.TypeEpic, bootstrap.TypeSubtask} {
		if got := find(t, types.Body["issueTypes"], "name", name); got["builtin"] != true || got["retired"] != false {
			t.Fatalf("%s is not a built-in type that is offered: %v", name, got)
		}
	}
	taskID := find(t, types.Body["issueTypes"], "name", bootstrap.TypeTask)["id"].(string)

	changeName := h.name(t, "Change request")
	made := obj(t, want(t, owner.post("/api/v1/issue-types", map[string]any{
		"name": changeName, "description": "A change somebody asks for.", "icon": "change", "level": 0,
	}), http.StatusCreated, "add a type"), "issueType")
	changeID := made["id"].(string)
	if made["builtin"] != false || made["retired"] != false || made["icon"] != "change" || made["level"] != float64(0) {
		t.Fatalf("made = %v", made)
	}

	t.Run("a type of the installation's own may sit at any level", func(t *testing.T) {
		for level := issuetype.MinLevel; level <= issuetype.MaxLevel; level++ {
			got := obj(t, want(t, owner.post("/api/v1/issue-types", map[string]any{"name": h.name(t, "Levelled"), "icon": "research", "level": level}),
				http.StatusCreated, "a type at a level"), "issueType")
			if got["level"] != float64(level) || got["isSubtask"] != (level < 0) {
				t.Errorf("a type made at level %d came back as %v", level, got)
			}
		}
	})

	t.Run("what the service refuses", func(t *testing.T) {
		for _, c := range []struct {
			what   string
			got    response
			status int
		}{
			{"the same name twice", owner.post("/api/v1/issue-types", map[string]any{"name": strings.ToUpper(changeName)}), http.StatusConflict},
			{"no name", owner.post("/api/v1/issue-types", map[string]any{"icon": "bug"}), http.StatusUnprocessableEntity},
			{"an icon the web does not draw", owner.post("/api/v1/issue-types", map[string]any{"name": h.name(t, "Rocket"), "icon": "rocket"}), http.StatusUnprocessableEntity},
			{"a level outside the hierarchy", owner.post("/api/v1/issue-types", map[string]any{"name": h.name(t, "Lofty"), "level": issuetype.MaxLevel + 1}), http.StatusUnprocessableEntity},
			{"renaming a built-in type", owner.patch("/api/v1/issue-types/"+taskID, map[string]any{"name": h.name(t, "Chore")}), http.StatusConflict},
			{"moving a built-in type", owner.patch("/api/v1/issue-types/"+taskID, map[string]any{"level": 1}), http.StatusConflict},
			{"retiring a built-in type", owner.post("/api/v1/issue-types/"+taskID+"/retire", nil), http.StatusConflict},
			{"deleting a built-in type", owner.delete("/api/v1/issue-types/" + taskID), http.StatusConflict},
			{"a type that is not there", owner.patch("/api/v1/issue-types/00000000-0000-7000-8000-000000000000", map[string]any{"name": "X"}), http.StatusNotFound},
		} {
			if c.got.Status != c.status {
				t.Errorf("%s: got %d, want %d: %s", c.what, c.got.Status, c.status, c.got.Raw)
			}
		}
	})

	t.Run("a member is refused every change", func(t *testing.T) {
		want(t, member.get("/api/v1/issue-types"), http.StatusOK, "a member reads the library")
		for what, got := range map[string]response{
			"add":       member.post("/api/v1/issue-types", map[string]any{"name": h.name(t, "Sneaky")}),
			"edit":      member.patch("/api/v1/issue-types/"+changeID, map[string]any{"name": h.name(t, "Sneakier")}),
			"retire":    member.post("/api/v1/issue-types/"+changeID+"/retire", nil),
			"reinstate": member.post("/api/v1/issue-types/"+changeID+"/reinstate", nil),
			"delete":    member.delete("/api/v1/issue-types/" + changeID),
		} {
			if got.Status != http.StatusForbidden {
				t.Errorf("a member could %s a type: %d %s", what, got.Status, got.Raw)
			}
		}
	})

	renamed := h.name(t, "Change")
	edited := obj(t, want(t, owner.patch("/api/v1/issue-types/"+changeID, map[string]any{
		"name": renamed, "description": "Asked for, not broken.", "icon": "improvement", "level": 1,
	}), http.StatusOK, "edit the type"), "issueType")
	if edited["name"] != renamed || edited["icon"] != "improvement" || edited["description"] != "Asked for, not broken." || edited["level"] != float64(1) {
		t.Fatalf("edited = %v", edited)
	}
	want(t, owner.patch("/api/v1/issue-types/"+changeID, map[string]any{"level": 0}), http.StatusOK, "back to the working level")

	// An issue of the type: it may no longer change level or be deleted, only retired.
	key := h.projectKey(t, "ITL")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Typed", "key": key}), http.StatusCreated, "project")
	allowType(t, owner, key, changeID)
	want(t, owner.post("/api/v1/projects/"+key+"/issues", map[string]any{"summary": "Move the server", "typeId": changeID}), http.StatusCreated, "an issue of the type")

	moved := owner.patch("/api/v1/issue-types/"+changeID, map[string]any{"level": 1})
	if moved.Status != http.StatusConflict || !strings.Contains(moved.Raw, "level cannot change") {
		t.Errorf("a type in use changed level: %d %s", moved.Status, moved.Raw)
	}
	refused := owner.delete("/api/v1/issue-types/" + changeID)
	if refused.Status != http.StatusConflict || !strings.Contains(refused.Raw, "Retire it instead") {
		t.Fatalf("deleting a type in use should say to retire it: %d %s", refused.Status, refused.Raw)
	}

	retired := obj(t, want(t, owner.post("/api/v1/issue-types/"+changeID+"/retire", nil), http.StatusOK, "retire"), "issueType")
	if retired["retired"] != true || retired["retiredAt"] == nil {
		t.Fatalf("retired = %v", retired)
	}
	if offered := typeNames(t, owner.get("/api/v1/issue-types")); slices.Contains(offered, renamed) {
		t.Error("a retired type is still offered")
	}
	all := want(t, owner.get("/api/v1/issue-types?retired=true"), http.StatusOK, "the library with retired types")
	if got := find(t, all.Body["issueTypes"], "name", renamed); got["retired"] != true {
		t.Fatalf("the retired type is not listed as retired: %v", got)
	}
	reinstated := obj(t, want(t, owner.post("/api/v1/issue-types/"+changeID+"/reinstate", nil), http.StatusOK, "reinstate"), "issueType")
	if reinstated["retired"] != false || !slices.Contains(typeNames(t, owner.get("/api/v1/issue-types")), renamed) {
		t.Fatalf("the reinstated type is not offered again: %v", reinstated)
	}

	spareName := h.name(t, "Spare")
	spareID := idOf(t, want(t, owner.post("/api/v1/issue-types", map[string]any{"name": spareName, "icon": "question"}), http.StatusCreated, "a type nothing uses"), "issueType")
	want(t, owner.delete("/api/v1/issue-types/"+spareID), http.StatusNoContent, "delete a type nothing uses")
	if slices.Contains(typeNames(t, owner.get("/api/v1/issue-types?retired=true")), spareName) {
		t.Error("the deleted type is still listed")
	}

	t.Run("every change is in the audit log", func(t *testing.T) {
		for _, e := range []struct{ action, target string }{
			{"issue_type.created", changeID},
			{"issue_type.updated", changeID},
			{"issue_type.retired", changeID},
			{"issue_type.reinstated", changeID},
			{"issue_type.created", spareID},
			{"issue_type.deleted", spareID},
		} {
			page := want(t, owner.get("/api/v1/audit?action="+url.QueryEscape(e.action)+"&actor="+ownerID), http.StatusOK, "the log of "+e.action)
			found := false
			for _, entry := range list(t, page, "entries") {
				row := entry.(map[string]any)
				found = found || (row["targetId"] == e.target && row["targetType"] == "issue_type")
			}
			if !found {
				t.Errorf("no %s entry for %s", e.action, e.target)
			}
		}
	})

	t.Run("SQL refuses the same", func(t *testing.T) {
		ctx := context.Background()
		// Each attempt runs in a transaction that is rolled back, so one the
		// database wrongly took never reaches the other tests.
		refusedInSQL := func(what, statement string, args ...any) {
			t.Helper()
			tx, err := h.super.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if _, err := tx.Exec(ctx, statement, args...); err == nil {
				t.Errorf("SQL allowed %s", what)
			}
		}
		refusedInSQL("renaming a built-in type", `UPDATE issue_type SET name = 'Chore' WHERE id = $1`, taskID)
		refusedInSQL("moving a built-in type", `UPDATE issue_type SET hierarchy_level = 1 WHERE id = $1`, taskID)
		refusedInSQL("retiring a built-in type", `UPDATE issue_type SET retired_at = now() WHERE id = $1`, taskID)
		refusedInSQL("deleting a built-in type", `DELETE FROM issue_type WHERE id = $1`, taskID)
		refusedInSQL("making a type of the installation's own built in", `UPDATE issue_type SET builtin = true WHERE id = $1`, changeID)
		refusedInSQL("an icon the web does not draw", `INSERT INTO issue_type (name, icon) VALUES ($1, 'rocket')`, h.name(t, "Rocket"))
		refusedInSQL("deleting a type issues use", `DELETE FROM issue_type WHERE id = $1`, changeID)
	})

	t.Run("the database and the web know the same icons", func(t *testing.T) {
		var definition string
		if err := h.super.QueryRow(context.Background(), `
			SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'issue_type_icon_known'`).Scan(&definition); err != nil {
			t.Fatal(err)
		}
		var known []string
		for _, m := range regexp.MustCompile(`'([a-z]+)'`).FindAllStringSubmatch(definition, -1) {
			known = append(known, m[1])
		}
		want := issuetype.IconNames()
		slices.Sort(known)
		slices.Sort(want)
		if !slices.Equal(known, want) {
			t.Errorf("the database knows %v, the API %v", known, want)
		}
	})
}

// typeNames lists the names of the types a listing answered with.
func typeNames(t *testing.T, r response) []string {
	t.Helper()
	var names []string
	for _, raw := range list(t, r, "issueTypes") {
		names = append(names, raw.(map[string]any)["name"].(string))
	}
	return names
}
