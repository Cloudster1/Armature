//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
)

// The portal's door is named by the desk: its key in the address finds it, and
// the addresses that named an organization send the same request on.
func TestTheDoorIsNamedByItsDesk(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	agent := api.client(t)
	slug := principalField(t, agent.signIn(t, h.person(t, "keydoor", auth.RoleOwner)), "principal", "org", "slug").(string)
	front, code := h.projectKey(t, "FRD"), h.projectKey(t, "COD")
	want(t, agent.post("/api/v1/projects", map[string]any{"name": "Front desk", "key": front, "template": "service-desk"}), http.StatusCreated, "a desk")
	want(t, agent.post("/api/v1/projects", map[string]any{"name": "Code", "key": code}), http.StatusCreated, "a software project")

	visitor := api.client(t)
	door := want(t, visitor.get("/api/v1/desk?desk="+strings.ToLower(front)), http.StatusOK, "the desk's door, in any case")
	if desk := obj(t, door, "desk"); desk["key"] != front || desk["name"] != "Front desk" || desk["open"] != false {
		t.Errorf("desk = %v, want the front desk, asking for a code", desk)
	}
	if door.Body["installation"] != shared.installation.Name {
		t.Errorf("installation = %v, want the name of the installation holding the desk", door.Body["installation"])
	}
	if _, named := want(t, visitor.get("/api/v1/desk"), http.StatusOK, "the installation's door").Body["desk"]; named {
		t.Error("a door whose address names no desk answered with one")
	}
	want(t, visitor.get("/api/v1/desk?desk="+h.projectKey(t, "NOB")), http.StatusNotFound, "no desk has that key")
	want(t, visitor.get("/api/v1/desk?desk="+code), http.StatusNotFound, "a software project has no door")
	want(t, visitor.post("/api/v1/desk/codes", map[string]any{"email": h.email(t, "nowhere"), "desk": h.projectKey(t, "NOB")}), http.StatusNotFound, "no code for a desk that is not there")

	address := h.email(t, "front")
	want(t, visitor.post("/api/v1/desk/codes", map[string]any{"email": address, "desk": front}), http.StatusAccepted, "a code")
	if mail := api.mailer.sent[len(api.mailer.sent)-1]; mail.To != address || !strings.Contains(mail.Subject, "Front desk") {
		t.Errorf("code mail = %+v, want it to name the desk", mail)
	}
	entered := want(t, visitor.post("/api/v1/desk/sessions", map[string]any{"email": address, "code": api.mailer.lastCode(t), "desk": front}), http.StatusOK, "in with the code")
	if principalField(t, entered, "principal", "org", "slug") != slug || principalField(t, entered, "principal", "role") != "customer" {
		t.Errorf("principal = %s, want a customer where the desk is", entered.Raw)
	}

	t.Run("the addresses that named an organization send the same request on", func(t *testing.T) {
		mover := api.client(t).noRedirects()
		for _, moved := range []struct{ method, from, to string }{
			{http.MethodGet, "/api/v1/desk/" + slug + "?desk=" + front, "/api/v1/desk?desk=" + front},
			{http.MethodPost, "/api/v1/desk/" + slug + "/codes", "/api/v1/desk/codes"},
			{http.MethodPost, "/api/v1/desk/" + slug + "/sessions", "/api/v1/desk/sessions"},
		} {
			req, _ := http.NewRequest(moved.method, api.URL+moved.from, strings.NewReader(`{}`))
			resp, _ := mover.raw(req)
			if resp.StatusCode != http.StatusPermanentRedirect || resp.Header.Get("Location") != moved.to {
				t.Errorf("%s %s answered %d to %q, want 308 to %s", moved.method, moved.from, resp.StatusCode, resp.Header.Get("Location"), moved.to)
			}
		}

		// A client that follows the redirect brings its body along and is let in.
		// The plain client keeps the contract out of it: the old address documents only the redirect.
		later := h.email(t, "later")
		body, _ := json.Marshal(map[string]any{"email": later, "desk": front})
		resp, err := http.Post(api.URL+"/api/v1/desk/"+slug+"/codes", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted || api.mailer.sent[len(api.mailer.sent)-1].To != later {
			t.Fatalf("a code asked for at the old address answered %d", resp.StatusCode)
		}
	})

}
