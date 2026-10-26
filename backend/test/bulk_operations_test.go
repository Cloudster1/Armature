//go:build integration

package test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/perm"
)

// bulkBench is a project of the owner's with what a bulk edit reaches for.
type bulkBench struct {
	h     *harness
	api   *apiServer
	owner *client
	key   string
}

func newBulkBench(t *testing.T, prefix string) *bulkBench {
	t.Helper()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, strings.ToLower(prefix), auth.RoleOwner))
	key := h.projectKey(t, prefix)
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Bulk " + prefix, "key": key}), http.StatusCreated, "the project")
	return &bulkBench{h: h, api: api, owner: owner, key: key}
}

func (b *bulkBench) project(t *testing.T, prefix string) string {
	t.Helper()
	key := b.h.projectKey(t, prefix)
	want(t, b.owner.post("/api/v1/projects", map[string]any{"name": "Bulk " + prefix, "key": key}), http.StatusCreated, "another project")
	return key
}

func (b *bulkBench) issue(t *testing.T, projectKey, summary string, extra map[string]any) string {
	t.Helper()
	body := map[string]any{"summary": summary}
	for k, v := range extra {
		body[k] = v
	}
	return obj(t, want(t, b.owner.post("/api/v1/projects/"+projectKey+"/issues", body), http.StatusCreated, summary), "issue")["key"].(string)
}

func (b *bulkBench) read(t *testing.T, key string) map[string]any {
	t.Helper()
	return obj(t, want(t, b.owner.get("/api/v1/issues/"+key), http.StatusOK, "read "+key), "issue")
}

func bulkEdit(t *testing.T, c *client, keys []string, change map[string]any, status int, what string) response {
	t.Helper()
	return want(t, c.post("/api/v1/issues/bulk", map[string]any{"keys": keys, "change": change}), status, what)
}

func appliedAndRefused(t *testing.T, r response) ([]string, map[string]string) {
	t.Helper()
	var applied []string
	for _, k := range list(t, r, "applied") {
		applied = append(applied, k.(string))
	}
	refused := map[string]string{}
	for _, raw := range list(t, r, "refused") {
		row := raw.(map[string]any)
		refused[row["key"].(string)] = row["reason"].(string)
	}
	return applied, refused
}

func namesOf(t *testing.T, issue map[string]any, field string) []string {
	t.Helper()
	var out []string
	raw, _ := issue[field].([]any)
	for _, item := range raw {
		out = append(out, item.(map[string]any)["name"].(string))
	}
	slices.Sort(out)
	return out
}

func TestBulkEditRemovesLabelsAndNeverCoinsThem(t *testing.T) {
	t.Parallel()
	b := newBulkBench(t, "BLBL")
	retired, kept, never := b.h.name(t, "retired"), b.h.name(t, "kept"), b.h.name(t, "never")
	keys := []string{b.issue(t, b.key, "one", nil), b.issue(t, b.key, "two", nil)}
	for _, key := range keys {
		want(t, b.owner.put("/api/v1/issues/"+key+"/labels", map[string]any{"labels": []string{retired, kept}}), http.StatusOK, "label "+key)
	}

	r := bulkEdit(t, b.owner, keys, map[string]any{"removeLabels": []string{strings.ToUpper(retired), never}}, http.StatusOK, "strip the retired label")
	if applied, refused := appliedAndRefused(t, r); len(applied) != 2 || len(refused) != 0 {
		t.Fatalf("applied %v, refused %v", applied, refused)
	}
	for _, key := range keys {
		if got := namesOf(t, b.read(t, key), "labels"); !slices.Equal(got, []string{kept}) {
			t.Errorf("%s carries %v, want only %s", key, got, kept)
		}
	}
	var coined int
	if err := b.h.super.QueryRow(context.Background(), `SELECT count(*) FROM label WHERE lower(name) = lower($1)`, never).Scan(&coined); err != nil {
		t.Fatal(err)
	}
	if coined != 0 {
		t.Errorf("removing %s coined it", never)
	}
	bulkEdit(t, b.owner, keys, map[string]any{"addLabels": []string{kept}, "removeLabels": []string{kept}}, http.StatusUnprocessableEntity, "the same label added and removed")
}

func TestBulkEditAddsAndRemovesVersionsAndComponents(t *testing.T) {
	t.Parallel()
	b := newBulkBench(t, "BLVR")
	old := idOf(t, want(t, b.owner.post("/api/v1/projects/"+b.key+"/versions", map[string]any{"name": "0.9"}), http.StatusCreated, "a version"), "version")
	next := idOf(t, want(t, b.owner.post("/api/v1/projects/"+b.key+"/versions", map[string]any{"name": "1.0"}), http.StatusCreated, "another version"), "version")
	engine := idOf(t, want(t, b.owner.post("/api/v1/projects/"+b.key+"/components", map[string]any{"name": "Engine"}), http.StatusCreated, "a component"), "component")
	wheels := idOf(t, want(t, b.owner.post("/api/v1/projects/"+b.key+"/components", map[string]any{"name": "Wheels"}), http.StatusCreated, "another component"), "component")

	shipped := b.issue(t, b.key, "named in a version since archived", nil)
	plain := b.issue(t, b.key, "named in nothing", nil)
	want(t, b.owner.put("/api/v1/issues/"+shipped+"/versions", map[string]any{"fix": []string{old}, "affects": []string{old}}), http.StatusOK, "name the old version")
	want(t, b.owner.put("/api/v1/issues/"+shipped+"/components", map[string]any{"components": []string{wheels}}), http.StatusOK, "put it in Wheels")
	want(t, b.owner.post("/api/v1/versions/"+old+"/release", nil), http.StatusOK, "release the old version")
	want(t, b.owner.post("/api/v1/versions/"+old+"/archive", nil), http.StatusOK, "archive the old version")

	r := bulkEdit(t, b.owner, []string{shipped, plain}, map[string]any{
		"fixVersions": map[string]any{"add": []string{next}},
		"components":  map[string]any{"add": []string{engine}, "remove": []string{wheels}},
	}, http.StatusOK, "add a version and change components")
	if applied, refused := appliedAndRefused(t, r); len(applied) != 2 {
		t.Fatalf("applied %v, refused %v; an archived version already named must not refuse the issue", applied, refused)
	}
	after := b.read(t, shipped)
	if got := namesOf(t, after, "fixVersions"); !slices.Equal(got, []string{"0.9", "1.0"}) {
		t.Errorf("fix versions = %v, want the archived one kept and 1.0 added", got)
	}
	if got := namesOf(t, after, "affectsVersions"); !slices.Equal(got, []string{"0.9"}) {
		t.Errorf("affects versions = %v, want them untouched", got)
	}
	if got := namesOf(t, after, "components"); !slices.Equal(got, []string{"Engine"}) {
		t.Errorf("components = %v, want Wheels swapped for Engine", got)
	}
	if got := namesOf(t, b.read(t, plain), "fixVersions"); !slices.Equal(got, []string{"1.0"}) {
		t.Errorf("the plain issue's fix versions = %v", got)
	}

	r = bulkEdit(t, b.owner, []string{plain}, map[string]any{"fixVersions": map[string]any{"remove": []string{next}, "add": []string{old}}}, http.StatusOK, "name an archived version")
	if _, refused := appliedAndRefused(t, r); !strings.Contains(refused[plain], "archived") {
		t.Errorf("refused = %v, want the archived version named", refused)
	}
	if got := namesOf(t, b.read(t, plain), "fixVersions"); !slices.Equal(got, []string{"1.0"}) {
		t.Errorf("a refused issue lost its version: %v", got)
	}
}

func TestBulkEditSetsAndClearsFields(t *testing.T) {
	t.Parallel()
	b := newBulkBench(t, "BLFD")
	elsewhere := b.project(t, "BLFE")
	cost := idOf(t, want(t, b.owner.post("/api/v1/projects/"+b.key+"/fields", map[string]any{"name": "Cost", "kind": "number"}), http.StatusCreated, "a field"), "field")
	here, there := b.issue(t, b.key, "costs something", nil), b.issue(t, elsewhere, "in a project without the field", nil)

	r := bulkEdit(t, b.owner, []string{here, there}, map[string]any{"fields": map[string]any{cost: 12}}, http.StatusOK, "set a field")
	applied, refused := appliedAndRefused(t, r)
	if len(applied) != 1 || applied[0] != here || !strings.Contains(refused[there], "another project") {
		t.Fatalf("applied %v, refused %v", applied, refused)
	}
	valueOf := func() any {
		for _, raw := range list(t, want(t, b.owner.get("/api/v1/issues/"+here+"/fields"), http.StatusOK, "the issue's fields"), "values") {
			if v := raw.(map[string]any); v["field"].(map[string]any)["id"] == cost {
				return v["value"]
			}
		}
		return nil
	}
	if got := valueOf(); got != float64(12) {
		t.Errorf("cost = %v, want 12", got)
	}
	bulkEdit(t, b.owner, []string{here}, map[string]any{"fields": map[string]any{cost: nil}}, http.StatusOK, "clear the field")
	if got := valueOf(); got != nil {
		t.Errorf("cost = %v after clearing, want none", got)
	}
}

func TestBulkMoveMapsStatusesAndCarriesWhatIsUnder(t *testing.T) {
	t.Parallel()
	b := newBulkBench(t, "BLMV")
	target := b.project(t, "BLMT")
	types := map[string]string{}
	for _, raw := range list(t, want(t, b.owner.get("/api/v1/issue-types"), http.StatusOK, "issue types"), "issueTypes") {
		typ := raw.(map[string]any)
		types[typ["name"].(string)] = typ["id"].(string)
	}
	statuses := map[string]string{}
	for _, raw := range list(t, want(t, b.owner.get("/api/v1/projects/"+target+"/statuses"), http.StatusOK, "the target's statuses"), "statuses") {
		s := raw.(map[string]any)
		statuses[s["name"].(string)] = s["id"].(string)
	}
	story := b.issue(t, b.key, "a story", map[string]any{"typeId": types[bootstrap.TypeStory]})
	step := b.issue(t, b.key, "its step", map[string]any{"typeId": types[bootstrap.TypeSubtask], "parentKey": story})
	alone := b.issue(t, b.key, "on its own", nil)

	// The subtask comes first, so it is refused on its own and then found moved with its story.
	r := bulkEdit(t, b.owner, []string{step, story, alone}, map[string]any{"moveTo": map[string]any{
		"projectKey": target, "statuses": map[string]any{statuses[bootstrap.StatusToDo]: statuses[bootstrap.StatusInProgress]},
	}}, http.StatusOK, "move three")
	applied, refused := appliedAndRefused(t, r)
	if len(applied) != 3 || len(refused) != 0 {
		t.Fatalf("applied %v, refused %v; want the step counted with its story", applied, refused)
	}
	moved := map[string]string{}
	for _, raw := range list(t, r, "moved") {
		change := raw.(map[string]any)
		moved[change["from"].(string)] = change["to"].(string)
	}
	if len(moved) != 3 || !strings.HasPrefix(moved[step], target+"-") {
		t.Fatalf("moved = %v, want all three under %s", moved, target)
	}
	for old, now := range moved {
		after := b.read(t, now)
		if after["status"].(map[string]any)["name"] != bootstrap.StatusInProgress {
			t.Errorf("%s landed in %v, want the mapped status", now, after["status"])
		}
		if found := b.read(t, old); found["key"] != now {
			t.Errorf("the old key %s finds %v, want %s", old, found["key"], now)
		}
	}

	coined := idOf(t, want(t, b.owner.post("/api/v1/statuses", map[string]any{"name": b.h.name(t, "Nowhere"), "category": "in_progress"}), http.StatusCreated, "a status no workflow has"), "status")
	stray := b.issue(t, b.key, "mapped to nowhere", nil)
	r = bulkEdit(t, b.owner, []string{stray}, map[string]any{"moveTo": map[string]any{
		"projectKey": target, "statuses": map[string]any{statuses[bootstrap.StatusToDo]: coined},
	}}, http.StatusOK, "map to a status the target lacks")
	if _, refused := appliedAndRefused(t, r); !strings.Contains(refused[stray], "map To Do to one of its statuses") {
		t.Errorf("refused = %v, want the status named", refused)
	}
	if after := b.read(t, stray); after["key"] != stray {
		t.Errorf("a refused move moved %s to %v", stray, after["key"])
	}

	bulkEdit(t, b.owner, []string{stray}, map[string]any{"moveTo": map[string]any{"projectKey": target}, "priority": "high"}, http.StatusUnprocessableEntity, "a move with another change")
	bulkEdit(t, b.owner, []string{stray}, map[string]any{"moveTo": map[string]any{"projectKey": target}, "delete": true}, http.StatusUnprocessableEntity, "a move and a delete")
}

func TestBulkDeleteCountsWhatTheCascadeTook(t *testing.T) {
	t.Parallel()
	b := newBulkBench(t, "BLDL")
	types := map[string]string{}
	for _, raw := range list(t, want(t, b.owner.get("/api/v1/issue-types"), http.StatusOK, "issue types"), "issueTypes") {
		typ := raw.(map[string]any)
		types[typ["name"].(string)] = typ["id"].(string)
	}
	story := b.issue(t, b.key, "a story", map[string]any{"typeId": types[bootstrap.TypeStory]})
	step := b.issue(t, b.key, "its step", map[string]any{"typeId": types[bootstrap.TypeSubtask], "parentKey": story})
	kept := b.issue(t, b.key, "not selected", nil)

	r := bulkEdit(t, b.owner, []string{story, step}, map[string]any{"delete": true}, http.StatusOK, "delete a story and its step")
	if applied, refused := appliedAndRefused(t, r); len(applied) != 2 || len(refused) != 0 {
		t.Fatalf("applied %v, refused %v; want the step counted with its story", applied, refused)
	}
	// Straight from the table, so the service's word does not stand in for it.
	var left int
	if err := b.h.super.QueryRow(context.Background(), `
		SELECT count(*) FROM issue i JOIN project p ON p.id = i.project_id WHERE p.key = $1`, b.key).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 1 {
		t.Errorf("%d issues left in %s, want only %s", left, b.key, kept)
	}
	bulkEdit(t, b.owner, []string{kept}, map[string]any{"delete": true, "priority": "low"}, http.StatusUnprocessableEntity, "a delete with another change")
}

func TestBulkTransitionsAreTheOnesEverySelectedIssueShares(t *testing.T) {
	t.Parallel()
	b := newBulkBench(t, "BLTR")
	first, second, started := b.issue(t, b.key, "one", nil), b.issue(t, b.key, "two", nil), b.issue(t, b.key, "started", nil)
	bulkEdit(t, b.owner, []string{started}, map[string]any{"transition": "Start progress"}, http.StatusOK, "start one")

	names := func(keys ...string) []string {
		t.Helper()
		var out []string
		for _, n := range list(t, want(t, b.owner.get("/api/v1/issues/bulk/transitions?keys="+strings.Join(keys, ",")), http.StatusOK, "shared transitions"), "names") {
			out = append(out, n.(string))
		}
		return out
	}
	if got := names(first, second); !slices.Contains(got, "Start progress") {
		t.Errorf("two issues to do share %v, want Start progress among them", got)
	}
	if got := names(first, started); slices.Contains(got, "Start progress") {
		t.Errorf("an issue to do and one in progress share %v, want no Start progress", got)
	}
	far := newBulkBench(t, "BLTF")
	if got := names(first, far.issue(t, far.key, "unreadable", nil)); !slices.Contains(got, "Start progress") {
		t.Errorf("an issue the caller cannot read narrowed the names to %v", got)
	}
	want(t, b.owner.get("/api/v1/issues/bulk/transitions?keys="), http.StatusBadRequest, "no keys")
}

// Every change asks for what its single-issue route asks for, in the issue's
// own project, and a refusal names the issue rather than failing the request.
func TestBulkEditChecksEachIssueAgainstTheCallersPermissions(t *testing.T) {
	t.Parallel()
	b := newBulkBench(t, "BLPM")
	team := idOf(t, want(t, b.owner.post("/api/v1/projects/"+b.key+"/teams", map[string]any{"name": "Crew"}), http.StatusCreated, "a team"), "team")
	sprint := idOf(t, want(t, b.owner.post("/api/v1/projects/"+b.key+"/sprints", map[string]any{"name": "Sprint"}), http.StatusCreated, "a sprint"), "sprint")
	key := b.issue(t, b.key, "guarded", nil)

	reader := b.api.asRole(t, b.h, b.owner, b.key, perm.Reader)
	user := b.api.asRole(t, b.h, b.owner, b.key, perm.User)
	scrum := b.api.asRole(t, b.h, b.owner, b.key, perm.ScrumMaster)
	for _, attempt := range []struct {
		who    string
		c      *client
		change map[string]any
		reason string
	}{
		{"a reader sets a priority", reader, map[string]any{"priority": "high"}, "You cannot change issues in " + b.key + "."},
		{"a reader moves it through its workflow", reader, map[string]any{"transition": "Start progress"}, "issue.transition"},
		{"a user puts it in a sprint", user, map[string]any{"sprintId": sprint}, "sprint.manage"},
		{"a user gives it a team", user, map[string]any{"teamId": team}, "team.manage"},
		{"a scrum master deletes it", scrum, map[string]any{"delete": true}, "project.administer"},
	} {
		r := bulkEdit(t, attempt.c, []string{key}, attempt.change, http.StatusOK, attempt.who)
		applied, refused := appliedAndRefused(t, r)
		if len(applied) != 0 || !strings.Contains(refused[key], attempt.reason) || !strings.HasSuffix(refused[key], ".") {
			t.Errorf("%s: applied %v, refused %v; want it refused naming %q", attempt.who, applied, refused, attempt.reason)
		}
	}
	if after := b.read(t, key); after["priority"] == "high" || after["sprint"] != nil || after["team"] != nil {
		t.Errorf("a refused edit changed the issue: %v", after)
	}

	t.Run("a transition takes only issue.transition", func(t *testing.T) {
		transitioner := b.api.asRole(t, b.h, b.owner, b.key, perm.User)
		r := bulkEdit(t, transitioner, []string{key}, map[string]any{"transition": "Start progress"}, http.StatusOK, "a user starts it")
		if applied, refused := appliedAndRefused(t, r); len(applied) != 1 {
			t.Errorf("applied %v, refused %v", applied, refused)
		}
	})

	t.Run("an issue the caller cannot read is not found, and the rest is done", func(t *testing.T) {
		far := newBulkBench(t, "BLPF")
		hidden := far.issue(t, far.key, "far away", nil)
		r := bulkEdit(t, user, []string{key, hidden}, map[string]any{"priority": "low"}, http.StatusOK, "a mixed selection")
		applied, refused := appliedAndRefused(t, r)
		if !slices.Equal(applied, []string{key}) || refused[hidden] != "That issue was not found." {
			t.Errorf("applied %v, refused %v", applied, refused)
		}
		if after := far.read(t, hidden); after["priority"] == "low" {
			t.Error("the far issue was changed")
		}
	})

	t.Run("a move needs to be allowed to file issues in the target", func(t *testing.T) {
		target := b.project(t, "BLPT")
		mover := b.api.asRole(t, b.h, b.owner, b.key, perm.User)
		want(t, mover.post("/api/v1/issues/bulk", map[string]any{"keys": []string{key}, "change": map[string]any{"moveTo": map[string]any{"projectKey": target}}}),
			http.StatusForbidden, "move into a project the caller only reads")
		if after := b.read(t, key); after["key"] != key {
			t.Errorf("a refused move moved the issue to %v", after["key"])
		}
	})

}
