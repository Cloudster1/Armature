//go:build integration

package test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/perm"
)

// The projects list comes a page at a time with its total, narrowed by text and
// by what the reader may see, so thousands of projects never travel at once.
func TestTheProjectsListIsPaged(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "pager", auth.RoleOwner))

	// Every project of this test carries the marker in its name, so q finds
	// exactly them among everything the installation holds.
	marker := h.name(t, "Paged")
	keys := make([]string, 5)
	for i := range keys {
		keys[i] = h.projectKey(t, "PAGE")
		want(t, owner.post("/api/v1/projects", map[string]any{"name": marker + " project", "key": keys[i]}), http.StatusCreated, "a project")
	}
	slices.Sort(keys)

	listed := func(t *testing.T, c *client, query string) (keys []string, total, limit, offset int) {
		t.Helper()
		resp := want(t, c.get("/api/v1/projects?"+query), http.StatusOK, "the projects")
		for _, p := range list(t, resp, "projects") {
			keys = append(keys, p.(map[string]any)["key"].(string))
		}
		number := func(field string) int { n, _ := resp.Body[field].(float64); return int(n) }
		return keys, number("total"), number("limit"), number("offset")
	}
	byMarker := "q=" + url.QueryEscape(marker)

	t.Run("a page holds the limit, in key order, and the total counts them all", func(t *testing.T) {
		var seen []string
		for _, offset := range []string{"0", "2", "4"} {
			got, total, limit, at := listed(t, owner, byMarker+"&limit=2&offset="+offset)
			if total != len(keys) || limit != 2 || at != len(seen) {
				t.Fatalf("offset %s: total %d, limit %d, offset %d, want %d, 2 and %d", offset, total, limit, at, len(keys), len(seen))
			}
			seen = append(seen, got...)
		}
		if !slices.Equal(seen, keys) {
			t.Errorf("the pages hold %v, want %v", seen, keys)
		}
	})

	t.Run("a page is fifty unless asked, and never more than two hundred", func(t *testing.T) {
		if _, _, limit, _ := listed(t, owner, byMarker); limit != 50 {
			t.Errorf("the default limit is %d, want 50", limit)
		}
		if _, _, limit, _ := listed(t, owner, byMarker+"&limit=5000"); limit != 200 {
			t.Errorf("a limit of 5000 reads %d, want 200", limit)
		}
	})

	t.Run("the text finds a project by its key, in any case", func(t *testing.T) {
		got, total, _, _ := listed(t, owner, "q="+strings.ToLower(keys[3]))
		if total != 1 || !slices.Equal(got, keys[3:4]) {
			t.Errorf("q=%s lists %v of %d, want only %s", strings.ToLower(keys[3]), got, total, keys[3])
		}
	})

	t.Run("a member sees and counts only the projects they may read", func(t *testing.T) {
		member := api.asRole(t, h, owner, keys[1], perm.User)
		got, total, _, _ := listed(t, member, byMarker)
		if total != 1 || !slices.Equal(got, keys[1:2]) {
			t.Errorf("the member lists %v of %d, want only %s", got, total, keys[1])
		}
	})

	t.Run("a key confined to one project lists that one", func(t *testing.T) {
		created := want(t, owner.post("/api/v1/tokens", map[string]any{"name": "one project", "projects": []string{keys[2]}}), http.StatusCreated, "a confined key")
		confined := api.client(t)
		confined.bearer = principalField(t, created, "token", "secret").(string)
		h.waitForPrimary(t)
		got, total, _, _ := listed(t, confined, byMarker)
		if total != 1 || !slices.Equal(got, keys[2:3]) {
			t.Errorf("the key lists %v of %d, want only %s", got, total, keys[2])
		}
	})

	t.Run("an archived project is left out unless asked for", func(t *testing.T) {
		want(t, owner.delete("/api/v1/projects/"+keys[0]), http.StatusNoContent, "archiving one")
		h.waitForPrimary(t)
		if got, total, _, _ := listed(t, owner, byMarker); total != len(keys)-1 || slices.Contains(got, keys[0]) {
			t.Errorf("the default list holds %v of %d, want the %d left without %s", got, total, len(keys)-1, keys[0])
		}
		if got, total, _, _ := listed(t, owner, byMarker+"&archived=true"); total != len(keys) || !slices.Contains(got, keys[0]) {
			t.Errorf("asking for archived ones lists %v of %d, want all %d", got, total, len(keys))
		}
	})
}
