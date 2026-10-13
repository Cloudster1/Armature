//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
)

// Everybody signed in reads what the installation is called; its
// administrators rename it, and the log says who did.
func TestTheInstallationIsNamedByItsAdministrators(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	signedUp := owner.signIn(t, h.person(t, "named", auth.RoleOwner))
	orgID := principalField(t, signedUp, "principal", "org", "id").(string)
	member := api.client(t)
	member.signIn(t, h.person(t, "reader", auth.RoleMember))

	// Everybody reads this name, so it is put back as it was found.
	var was string
	if err := h.super.QueryRow(context.Background(), `SELECT name FROM org WHERE id = $1`, orgID).Scan(&was); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := h.super.Exec(context.Background(), `UPDATE org SET name = $2 WHERE id = $1`, orgID, was); err != nil {
			t.Errorf("put the installation's name back: %v", err)
		}
	})
	renames := h.auditCount(t, orgID, "installation.renamed")

	nameOf := func(c *client) any {
		t.Helper()
		return obj(t, want(t, c.get("/api/v1/installation"), http.StatusOK, "read the installation"), "installation")["name"]
	}
	if got := nameOf(member); got != was {
		t.Fatalf("a member reads the installation as %v, want the name it was made with", got)
	}

	want(t, member.put("/api/v1/installation", map[string]string{"name": "Taken Over"}), http.StatusForbidden, "a member renames it")
	if blank := owner.put("/api/v1/installation", map[string]string{"name": "   "}); blank.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a blank name: %d %s", blank.Status, blank.Raw)
	}
	if h.auditCount(t, orgID, "installation.renamed") != renames {
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
	if h.auditCount(t, orgID, "installation.renamed") != renames+1 {
		t.Error("the rename was not recorded")
	}
}
