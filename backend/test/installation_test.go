//go:build integration

package test

import (
	"net/http"
	"testing"
)

// Everybody signed in reads what the installation is called; its
// administrators rename it, and the log says who did.
func TestTheInstallationIsNamedByItsAdministrators(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	signedUp := owner.signup(t, h, "named")
	orgID := principalField(t, signedUp, "principal", "org", "id").(string)
	invited := want(t, owner.post("/api/v1/invites", map[string]any{"email": h.email(t, "reader"), "role": "member"}), http.StatusCreated, "invite")
	member := api.client(t)
	want(t, member.post("/api/v1/auth/invites/accept", map[string]any{"token": invited.Body["token"], "name": "Reader", "password": testPassword}), http.StatusOK, "accept")

	nameOf := func(c *client) any {
		t.Helper()
		return obj(t, want(t, c.get("/api/v1/installation"), http.StatusOK, "read the installation"), "installation")["name"]
	}
	if got := nameOf(member); got != "named Company" {
		t.Fatalf("a member reads the installation as %v, want the name it was made with", got)
	}

	want(t, member.put("/api/v1/installation", map[string]string{"name": "Taken Over"}), http.StatusForbidden, "a member renames it")
	if blank := owner.put("/api/v1/installation", map[string]string{"name": "   "}); blank.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a blank name: %d %s", blank.Status, blank.Raw)
	}
	if h.auditCount(t, orgID, "installation.renamed") != 0 {
		t.Fatal("a refused rename was recorded")
	}

	renamed := want(t, owner.put("/api/v1/installation", map[string]string{"name": "  The Tracker "}), http.StatusOK, "rename it")
	if got := obj(t, renamed, "installation")["name"]; got != "The Tracker" {
		t.Fatalf("renamed to %v, want the name trimmed", got)
	}
	if got := nameOf(member); got != "The Tracker" {
		t.Errorf("the member still reads %v", got)
	}
	if got := principalField(t, member.get("/api/v1/auth/me"), "principal", "org", "name"); got != "The Tracker" {
		t.Errorf("the member's session still calls it %v", got)
	}
	if h.auditCount(t, orgID, "installation.renamed") != 1 {
		t.Error("the rename was not recorded")
	}

	// Until the suite stops making an organization per test, each reads its own.
	elsewhere := api.client(t)
	elsewhere.signup(t, h, "elsewhere")
	if got := nameOf(elsewhere); got != "elsewhere Company" {
		t.Errorf("another test's installation reads %v", got)
	}
}
