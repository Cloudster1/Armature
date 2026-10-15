//go:build integration

package test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
)

// The members and the users come a page at a time with their total, found by
// part of a name or an address, so thousands of accounts never travel at once.
func TestThePeopleListsArePaged(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "people-pager", auth.RoleOwner))

	// Every person of this test carries the marker in their name or their
	// address, so q finds exactly them among everybody the installation holds.
	marker := h.name(t, "Quill")
	person := func(name string, role auth.OrgRole, active bool) {
		t.Helper()
		email := h.person(t, "people", role)
		if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET name = $2, is_active = $3 WHERE email = $1`, email, name, active); err != nil {
			t.Fatalf("name %s: %v", name, err)
		}
	}
	person(marker+" Bea", auth.RoleAdmin, true)
	person("Zed "+marker, auth.RoleMember, true)
	person(marker+" Ada", auth.RoleMember, true)
	person(marker+" Cus", auth.RoleCustomer, true)
	person(marker+" Gone", auth.RoleMember, false)
	byAddress := h.person(t, marker, auth.RoleMember)
	if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET name = 'Elsewhere Person' WHERE email = $1`, byAddress); err != nil {
		t.Fatalf("rename the one found by address: %v", err)
	}
	h.waitForPrimary(t)

	listed := func(t *testing.T, path, field, query string) (names []string, total, limit, offset int) {
		t.Helper()
		resp := want(t, owner.get(path+"?"+query), http.StatusOK, field)
		for _, p := range list(t, resp, field) {
			names = append(names, p.(map[string]any)["name"].(string))
		}
		number := func(field string) int { n, _ := resp.Body[field].(float64); return int(n) }
		return names, number("total"), number("limit"), number("offset")
	}
	byMarker := "q=" + url.QueryEscape(strings.ToLower(marker))
	members := []string{marker + " Ada", marker + " Bea", marker + " Cus", "Elsewhere Person", "Zed " + marker}

	t.Run("the text finds a member by name or address, those it starts first", func(t *testing.T) {
		got, total, limit, _ := listed(t, "/api/v1/members", "members", byMarker)
		if total != len(members) || !slices.Equal(got, members) {
			t.Errorf("q finds %v of %d, want %v", got, total, members)
		}
		if limit != 50 {
			t.Errorf("the default limit is %d, want 50", limit)
		}
	})

	t.Run("staff leaves the customers out", func(t *testing.T) {
		got, total, _, _ := listed(t, "/api/v1/members", "members", byMarker+"&staff=true")
		staff := slices.DeleteFunc(slices.Clone(members), func(name string) bool { return name == marker+" Cus" })
		if total != len(staff) || !slices.Equal(got, staff) {
			t.Errorf("staff lists %v of %d, want %v", got, total, staff)
		}
	})

	t.Run("a page holds the limit and the total counts them all", func(t *testing.T) {
		var seen []string
		for _, offset := range []string{"0", "2", "4"} {
			got, total, limit, at := listed(t, "/api/v1/members", "members", byMarker+"&limit=2&offset="+offset)
			if total != len(members) || limit != 2 || at != len(seen) {
				t.Fatalf("offset %s: total %d, limit %d, offset %d, want %d, 2 and %d", offset, total, limit, at, len(members), len(seen))
			}
			seen = append(seen, got...)
		}
		if !slices.Equal(seen, members) {
			t.Errorf("the pages hold %v, want %v", seen, members)
		}
		if _, _, limit, _ := listed(t, "/api/v1/members", "members", byMarker+"&limit=5000"); limit != 200 {
			t.Errorf("a limit of 5000 reads %d, want 200", limit)
		}
	})

	t.Run("the text is only text, never a pattern", func(t *testing.T) {
		if got, total, _, _ := listed(t, "/api/v1/members", "members", "q="+url.QueryEscape(marker+"%")); total != 0 || len(got) != 0 {
			t.Errorf("a percent sign matched %v of %d, want nobody", got, total)
		}
	})

	t.Run("the users list finds switched off ones too, a page at a time", func(t *testing.T) {
		users := []string{marker + " Ada", marker + " Bea", marker + " Cus", marker + " Gone", "Elsewhere Person", "Zed " + marker}
		got, total, _, _ := listed(t, "/api/v1/users", "users", byMarker)
		if total != len(users) || !slices.Equal(got, users) {
			t.Errorf("q finds %v of %d, want %v", got, total, users)
		}
		var seen []string
		for _, offset := range []string{"0", "4"} {
			got, total, limit, at := listed(t, "/api/v1/users", "users", byMarker+"&limit=4&offset="+offset)
			if total != len(users) || limit != 4 || at != len(seen) {
				t.Fatalf("offset %s: total %d, limit %d, offset %d, want %d, 4 and %d", offset, total, limit, at, len(users), len(seen))
			}
			seen = append(seen, got...)
		}
		if !slices.Equal(seen, users) {
			t.Errorf("the pages hold %v, want %v", seen, users)
		}
	})
}
