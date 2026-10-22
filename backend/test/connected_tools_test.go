//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
)

// The avatar menu's "Switch to" group lists the tools this installation has a
// connected app for, at the app's address, to everyone signed in (#327).
func TestTheConnectedToolsAreListedToEveryoneSignedIn(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	ada := api.client(t)
	ada.signIn(t, h.person(t, "ada", auth.RoleAdmin))
	mia := api.client(t)
	mia.signIn(t, h.person(t, "mia", auth.RoleMember))

	// The names are the test's own, so the realm's client id is what makes
	// each app stand for its tool, and the addresses are what is asserted on.
	connect := func(product string) (id, address string) {
		t.Helper()
		name := h.name(t, product)
		address = "https://" + strings.ToLower(name) + ".example.test"
		made := want(t, ada.post("/api/v1/oauth/clients", map[string]any{
			"name": name, "clientUri": address, "redirectUris": []string{address + "/oauth/callback"},
			"scopes": []string{"read"}, "keycloakClientId": product,
		}), http.StatusCreated, "connecting "+product)
		id = obj(t, made, "client")["id"].(string)
		t.Cleanup(func() { ada.delete("/api/v1/oauth/clients/" + id) })
		return id, address
	}
	tools := func(c *client) map[string]string {
		t.Helper()
		out := map[string]string{}
		var order []string
		for _, each := range list(t, want(t, c.get("/api/v1/installation/tools"), http.StatusOK, "the connected tools"), "tools") {
			tool := each.(map[string]any)
			out[tool["product"].(string)] = tool["url"].(string)
			order = append(order, tool["product"].(string))
		}
		if len(order) == 2 && order[0] != "stator" {
			t.Errorf("the tools are listed as %v; Stator comes first", order)
		}
		return out
	}

	_, statorAddress := connect("stator")
	t.Run("one connected is listed, to a member too", func(t *testing.T) {
		got := tools(mia)
		if got["stator"] != statorAddress {
			t.Errorf("Stator is at %q, want %q", got["stator"], statorAddress)
		}
	})

	exciterID, exciterAddress := connect("exciter")
	t.Run("both connected are both listed", func(t *testing.T) {
		got := tools(mia)
		if got["stator"] != statorAddress || got["exciter"] != exciterAddress {
			t.Errorf("the tools = %v, want Stator at %s and Exciter at %s", got, statorAddress, exciterAddress)
		}
	})

	want(t, ada.delete("/api/v1/oauth/clients/"+exciterID), http.StatusNoContent, "removing Exciter's app")
	t.Run("a removed connection is gone", func(t *testing.T) {
		got := tools(ada)
		if got["exciter"] == exciterAddress {
			t.Errorf("Exciter is still listed at %s after its app was removed", exciterAddress)
		}
		if got["stator"] != statorAddress {
			t.Errorf("Stator is at %q, want %q", got["stator"], statorAddress)
		}
	})

	t.Run("a customer has only the portal", func(t *testing.T) {
		invited := want(t, ada.post("/api/v1/invites", map[string]any{"email": h.email(t, "toolscustomer"), "role": "customer"}), http.StatusCreated, "invite a customer")
		cara := api.client(t)
		want(t, cara.post("/api/v1/auth/invites/accept", map[string]any{"token": invited.Body["token"], "name": "Cara Customer", "password": testPassword}), http.StatusOK, "the customer joins")
		want(t, cara.get("/api/v1/installation/tools"), http.StatusForbidden, "a customer asking for the tools")
	})
}
