//go:build integration

package test

import (
	"net/http"
	"testing"
)

// One press makes an organization of the caller's own with a production line
// in it, and the session is there when the response arrives.
func TestADemoFactoryIsOnePressAway(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	signedUp := owner.signup(t, h, "showroom")
	home := principalField(t, signedUp, "principal", "org", "slug").(string)

	made := want(t, owner.post("/api/v1/organizations/demo", nil), http.StatusCreated, "make the demo")
	if made.Body["projectKey"] != "LINE" {
		t.Fatalf("made = %s", made.Raw)
	}
	demoSlug := obj(t, made, "organization")["slug"].(string)
	if demoSlug == home {
		t.Fatalf("the demo landed in the home organization")
	}

	me := want(t, owner.get("/api/v1/auth/me"), http.StatusOK, "where the session is")
	if principalField(t, me, "principal", "org", "slug") != demoSlug || principalField(t, me, "principal", "role") != "owner" {
		t.Fatalf("the session did not move into the demo as its owner: %s", me.Raw)
	}

	h.waitForPrimary(t)
	issues := want(t, owner.get("/api/v1/projects/LINE/issues?state=all"), http.StatusOK, "the line's issues")
	if got := len(list(t, issues, "issues")); got < 12 {
		t.Fatalf("the demo holds %d issues, want a line's worth: %s", got, issues.Raw)
	}
	sprints := list(t, want(t, owner.get("/api/v1/projects/LINE/sprints"), http.StatusOK, "windows"), "sprints")
	// The closed window is history; the running one and the planned one are listed.
	if len(sprints) < 2 {
		t.Fatalf("the demo has %d maintenance windows, want the running and the planned one", len(sprints))
	}
	milestones := list(t, want(t, owner.get("/api/v1/projects/LINE/milestones"), http.StatusOK, "milestones"), "milestones")
	if len(milestones) != 2 {
		t.Fatalf("the demo has %d milestones, want 2", len(milestones))
	}

	// Back home, the demo is one of the organizations to choose from.
	want(t, owner.post("/api/v1/auth/switch-org", map[string]string{"slug": home}), http.StatusOK, "back home")
	orgs := list(t, want(t, owner.get("/api/v1/auth/me"), http.StatusOK, "memberships"), "organizations")
	if len(orgs) != 2 {
		t.Fatalf("the person belongs to %d organizations, want home and the demo", len(orgs))
	}
}
