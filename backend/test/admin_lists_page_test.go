//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/bootstrap"
)

// pageOf reads a paged listing: the names under one field of each row, and
// the total, the limit and the offset the answer carries.
func pageOf(t *testing.T, c *client, path, rows, field string) (names []string, total, limit, offset int) {
	t.Helper()
	resp := want(t, c.get(path), http.StatusOK, path)
	for _, row := range list(t, resp, rows) {
		names = append(names, row.(map[string]any)[field].(string))
	}
	number := func(key string) int {
		n, ok := resp.Body[key].(float64)
		if !ok {
			t.Fatalf("%s answers no %s: %s", path, key, resp.Raw)
		}
		return int(n)
	}
	return names, number("total"), number("limit"), number("offset")
}

// walkPages reads every page of a search two at a time and checks each says
// where it starts and how many match in all.
func walkPages(t *testing.T, c *client, path, rows, field string, want []string) {
	t.Helper()
	var seen []string
	for offset := 0; offset < len(want); offset += 2 {
		got, total, limit, at := pageOf(t, c, path+"&limit=2&offset="+strconv.Itoa(offset), rows, field)
		if total != len(want) || limit != 2 || at != offset {
			t.Fatalf("offset %d: total %d, limit %d, offset %d, want %d, 2 and %d", offset, total, limit, at, len(want), offset)
		}
		seen = append(seen, got...)
	}
	if !slices.Equal(seen, want) {
		t.Errorf("the pages hold %v, want %v", seen, want)
	}
}

// The workflow library and its schemes come a page at a time with their totals,
// searched by name, so an installation of thousands never sends them all.
func TestTheWorkflowLibraryIsPaged(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "librarian", auth.RoleOwner))

	defaults, _, _, _ := pageOf(t, owner, "/api/v1/workflows?q="+url.QueryEscape(bootstrap.DefaultWorkflowName), "workflows", "id")
	if len(defaults) == 0 {
		t.Fatalf("no workflow is called %q", bootstrap.DefaultWorkflowName)
	}
	marker := h.name(t, "Shelved")
	var workflowNames, workflowIDs []string
	for _, suffix := range []string{"e", "a", "d", "b", "c"} {
		name := marker + " " + suffix
		workflowIDs = append(workflowIDs, idOf(t, want(t, owner.post("/api/v1/workflows/"+defaults[0]+"/copy", map[string]any{"name": name}), http.StatusCreated, "copy "+name), "workflow"))
		workflowNames = append(workflowNames, name)
	}
	slices.Sort(workflowNames)
	var schemeNames, schemeIDs []string
	for i, suffix := range []string{"c", "a", "b"} {
		name := marker + " scheme " + suffix
		schemeIDs = append(schemeIDs, idOf(t, want(t, owner.post("/api/v1/workflow-schemes", map[string]any{
			"name": name, "items": []map[string]any{{"workflowId": workflowIDs[i]}},
		}), http.StatusCreated, "scheme "+name), "scheme"))
		schemeNames = append(schemeNames, name)
	}
	slices.Sort(schemeNames)
	h.waitForPrimary(t)
	byMarker := "?q=" + url.QueryEscape(strings.ToUpper(marker))

	t.Run("the workflows come a page at a time by name, with the total of the search", func(t *testing.T) {
		walkPages(t, owner, "/api/v1/workflows"+byMarker, "workflows", "name", workflowNames)
	})

	t.Run("a page is fifty unless asked, and never more than two hundred", func(t *testing.T) {
		for path, rows := range map[string]string{"/api/v1/workflows": "workflows", "/api/v1/workflow-schemes": "schemes"} {
			if _, _, limit, _ := pageOf(t, owner, path+byMarker, rows, "name"); limit != 50 {
				t.Errorf("%s: the default limit is %d, want 50", path, limit)
			}
			if _, _, limit, _ := pageOf(t, owner, path+byMarker+"&limit=5000", rows, "name"); limit != 200 {
				t.Errorf("%s: a limit of 5000 reads %d, want 200", path, limit)
			}
		}
	})

	t.Run("the schemes come a page at a time by name, each with its own mappings", func(t *testing.T) {
		walkPages(t, owner, "/api/v1/workflow-schemes"+byMarker, "schemes", "name", schemeNames)
		resp := want(t, owner.get("/api/v1/workflow-schemes"+byMarker), http.StatusOK, "the schemes")
		for i, id := range schemeIDs {
			items := find(t, resp.Body["schemes"], "id", id)["items"].([]any)
			if len(items) != 1 || items[0].(map[string]any)["workflowId"] != workflowIDs[i] {
				t.Errorf("scheme %s maps %v, want only %s", id, items, workflowIDs[i])
			}
		}
	})

	t.Run("the default scheme leads the first page", func(t *testing.T) {
		resp := want(t, owner.get("/api/v1/workflow-schemes?limit=1"), http.StatusOK, "the first scheme")
		if first := list(t, resp, "schemes"); len(first) != 1 || first[0].(map[string]any)["isDefault"] != true {
			t.Errorf("the first scheme is %v, want the default", first)
		}
	})

	t.Run("one scheme is read by its id", func(t *testing.T) {
		got := obj(t, want(t, owner.get("/api/v1/workflow-schemes/"+schemeIDs[1]), http.StatusOK, "one scheme"), "scheme")
		items := got["items"].([]any)
		if got["name"] != marker+" scheme a" || len(items) != 1 || items[0].(map[string]any)["workflowName"] != marker+" a" {
			t.Errorf("the scheme reads %v", got)
		}
		want(t, owner.get("/api/v1/workflow-schemes/"+workflowIDs[0]), http.StatusNotFound, "a scheme that is not there")
	})
}

// A project's workflow page names its own scheme and the default, so the page
// need not read every scheme to say which decided.
func TestAProjectsWorkflowsNameTheSchemes(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "namer", auth.RoleOwner))
	key := h.projectKey(t, "NAMED")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Named schemes", "key": key}), http.StatusCreated, "the project")

	resp := want(t, owner.get("/api/v1/projects/"+key+"/workflows"), http.StatusOK, "the project's workflows")
	if resp.Body["defaultSchemeName"] != bootstrap.DefaultSchemeName {
		t.Errorf("defaultSchemeName = %v, want %q", resp.Body["defaultSchemeName"], bootstrap.DefaultSchemeName)
	}
	if name, has := resp.Body["schemeName"]; has {
		t.Errorf("a project following the default names a scheme of its own: %v", name)
	}

	defaults, _, _, _ := pageOf(t, owner, "/api/v1/workflows?q="+url.QueryEscape(bootstrap.DefaultWorkflowName), "workflows", "id")
	if len(defaults) == 0 {
		t.Fatalf("no workflow is called %q", bootstrap.DefaultWorkflowName)
	}
	own := h.name(t, "Own scheme")
	scheme := idOf(t, want(t, owner.post("/api/v1/workflow-schemes", map[string]any{
		"name": own, "items": []map[string]any{{"workflowId": defaults[0]}},
	}), http.StatusCreated, "a scheme"), "scheme")
	want(t, owner.put("/api/v1/projects/"+key+"/workflow-scheme", map[string]any{"schemeId": scheme}), http.StatusOK, "the project takes it")
	h.waitForPrimary(t)

	resp = want(t, owner.get("/api/v1/projects/"+key+"/workflows"), http.StatusOK, "the project's workflows")
	if resp.Body["schemeName"] != own || resp.Body["schemeId"] != scheme {
		t.Errorf("the project's scheme reads %v %v, want %s %s", resp.Body["schemeId"], resp.Body["schemeName"], scheme, own)
	}
	if resp.Body["defaultSchemeName"] != bootstrap.DefaultSchemeName {
		t.Errorf("defaultSchemeName = %v, want %q", resp.Body["defaultSchemeName"], bootstrap.DefaultSchemeName)
	}
}

// The grants come a page at a time with their total, found by who holds them,
// where, and which role, in the order a person looks for one.
func TestTheRoleAssignmentsArePaged(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "granter", auth.RoleOwner))

	marker := unique("Granted")
	key := h.projectKey(t, "GRANT")
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Granting", "key": key}), http.StatusCreated, "the project")
	idOfEmail := func(email string) string {
		var id string
		if err := h.super.QueryRow(context.Background(), `SELECT id FROM app_user WHERE email = $1`, email).Scan(&id); err != nil {
			t.Fatalf("find %s: %v", email, err)
		}
		return id
	}
	people := map[string]string{}
	for _, who := range []string{"a", "b", "c"} {
		people[who] = idOfEmail(h.outsider(t, marker+" "+who))
	}
	group := idOf(t, want(t, owner.post("/api/v1/groups", map[string]any{"name": h.name(t, marker+" group")}), http.StatusCreated, "a group"), "group")
	grant := func(body map[string]any) {
		t.Helper()
		want(t, owner.post("/api/v1/role-assignments", body), http.StatusCreated, "a grant")
	}
	grant(map[string]any{"role": "user", "projectKey": key, "userId": people["c"]})
	grant(map[string]any{"role": "project_administrator", "projectKey": key, "userId": people["b"]})
	grant(map[string]any{"role": "user", "projectKey": key, "groupId": group})
	grant(map[string]any{"role": "global_administrator", "userId": people["a"]})
	h.waitForPrimary(t)

	who := func(t *testing.T, query string) (names []string, total, limit, offset int) {
		t.Helper()
		resp := want(t, owner.get("/api/v1/role-assignments?"+query), http.StatusOK, "the grants")
		for _, raw := range list(t, resp, "assignments") {
			a := raw.(map[string]any)
			name, _ := a["userName"].(string)
			if group, _ := a["groupName"].(string); group != "" {
				name = "group"
			}
			names = append(names, strings.TrimPrefix(name, marker+" "))
		}
		number := func(key string) int { n, _ := resp.Body[key].(float64); return int(n) }
		return names, number("total"), number("limit"), number("offset")
	}
	byMarker := "q=" + url.QueryEscape(strings.ToLower(marker))

	t.Run("a page holds the limit in the order a person looks, and the total counts them all", func(t *testing.T) {
		// Over all projects first, then by project, the strongest role first.
		order := []string{"a", "b", "c", "group"}
		var seen []string
		for _, offset := range []string{"0", "2"} {
			got, total, limit, at := who(t, byMarker+"&limit=2&offset="+offset)
			if total != len(order) || limit != 2 || strconv.Itoa(at) != offset {
				t.Fatalf("offset %s: total %d, limit %d, offset %d", offset, total, limit, at)
			}
			seen = append(seen, got...)
		}
		if !slices.Equal(seen, order) {
			t.Errorf("the pages hold %v, want %v", seen, order)
		}
	})

	t.Run("a page is fifty unless asked, and never more than two hundred", func(t *testing.T) {
		if _, _, limit, _ := who(t, byMarker); limit != 50 {
			t.Errorf("the default limit is %d, want 50", limit)
		}
		if _, _, limit, _ := who(t, byMarker+"&limit=5000"); limit != 200 {
			t.Errorf("a limit of 5000 reads %d, want 200", limit)
		}
	})

	t.Run("the text finds the project's grants by its key", func(t *testing.T) {
		got, total, _, _ := who(t, "q="+strings.ToLower(key))
		if total != 3 || !slices.Equal(got, []string{"b", "c", "group"}) {
			t.Errorf("q=%s lists %v of %d, want b, c and the group", key, got, total)
		}
	})

	t.Run("a role narrows to its grants", func(t *testing.T) {
		got, total, _, _ := who(t, byMarker+"&role=user")
		if total != 2 || !slices.Equal(got, []string{"c", "group"}) {
			t.Errorf("role=user lists %v of %d, want c and the group", got, total)
		}
	})

	t.Run("a project keeps the grants that answer there, those over all projects included", func(t *testing.T) {
		got, total, _, _ := who(t, byMarker+"&project="+key)
		if total != 4 || !slices.Contains(got, "a") {
			t.Errorf("project=%s lists %v of %d, want all four with a's over all projects", key, got, total)
		}
	})
}

// Without a search the labels come a page at a time by name; with one, those
// starting with it first, and either way with how many there are.
func TestTheLabelsArePaged(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "labeller", auth.RoleOwner))

	word := strings.ToLower(unique("shelf"))
	var names []string
	for _, prefix := range []string{"d", "", "b", "a"} {
		name := h.name(t, prefix+word)
		want(t, owner.post("/api/v1/labels", map[string]any{"name": name}), http.StatusCreated, "coin "+name)
		names = append(names, name)
	}
	h.waitForPrimary(t)
	// Starting with the word comes first, then the rest by name.
	starting, inside := names[1], []string{names[3], names[2], names[0]}
	ordered := append([]string{starting}, inside...)

	t.Run("a search comes a page at a time, those starting with it first", func(t *testing.T) {
		walkPages(t, owner, "/api/v1/labels?q="+word, "labels", "name", ordered)
	})

	t.Run("without a search the answer is one page, with the total of every label", func(t *testing.T) {
		got, total, limit, offset := pageOf(t, owner, "/api/v1/labels", "labels", "name")
		if limit != 50 || offset != 0 || len(got) > limit || total < len(names) {
			t.Errorf("the plain list holds %d with total %d, limit %d and offset %d", len(got), total, limit, offset)
		}
		if _, _, limit, _ := pageOf(t, owner, "/api/v1/labels?limit=5000", "labels", "name"); limit != 200 {
			t.Errorf("a limit of 5000 reads %d, want 200", limit)
		}
	})
}
