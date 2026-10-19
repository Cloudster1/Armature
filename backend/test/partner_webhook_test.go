//go:build integration

package test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/netguard"
	"github.com/armature/armature/backend/internal/webhook"
)

// A connected app an administrator allowed sets up its own webhook, so no
// secret is copied by hand.

// partnerInbox is where a partner's events land, answering every delivery 200.
func partnerInbox(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// adminApp is a partner connected by an administrator with webhooks.partner,
// and the administrator's browser.
func adminApp(t *testing.T, h *harness, api *apiServer, name string) (partner, *client, *client) {
	t.Helper()
	ada := api.client(t)
	ada.signIn(t, h.person(t, "ada", auth.RoleAdmin))
	p := register(t, api, h.name(t, name), "read issue.write webhooks.partner")
	grant := connect(t, p, ada, "read issue.write webhooks.partner")
	return p, ada, bearer(api, t, grant["access_token"])
}

func TestAPartnerManagesItsOwnWebhook(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	stator, ada, app := adminApp(t, h, api, "Stator")
	inbox := partnerInbox(t)

	if resp := want(t, app.get("/api/v1/webhooks/partner"), http.StatusOK, "no webhook yet"); resp.Body["webhook"] != nil {
		t.Fatalf("a new app has a webhook: %s", resp.Raw)
	}
	want(t, app.patch("/api/v1/webhooks/partner", map[string]any{"topics": []string{"issue.updated"}}), http.StatusNotFound, "changing none")

	made := want(t, app.post("/api/v1/webhooks/partner", map[string]any{
		"url": inbox.URL + "/armature", "topics": []string{"issue.updated", "issue.deleted", "issue.updated"},
	}), http.StatusCreated, "the partner's webhook")
	hook := obj(t, made, "webhook")
	if !strings.HasPrefix(hook["secret"].(string), "armature_whs_") || hook["version"] != float64(1) {
		t.Errorf("made = %v", hook)
	}
	if topics := hook["topics"].([]any); len(topics) != 2 || topics[0] != "issue.deleted" {
		t.Errorf("topics = %v, want them sorted and once each", topics)
	}
	if partner := hook["partner"].(map[string]any); partner["clientId"] != stator.id || !strings.HasSuffix(hook["name"].(string), " (connected app)") {
		t.Errorf("the endpoint does not say whose it is: %v", hook)
	}
	id := hook["id"].(string)

	t.Run("the secret is shown once", func(t *testing.T) {
		if read := obj(t, want(t, app.get("/api/v1/webhooks/partner"), http.StatusOK, "reading it"), "webhook"); read["secret"] != nil {
			t.Errorf("a read showed the secret: %v", read)
		}
		resp := app.post("/api/v1/webhooks/partner", map[string]any{"url": inbox.URL, "topics": []string{"issue.updated"}})
		if resp.Status != http.StatusConflict || resp.ErrorCode() != "partner_webhook_exists" {
			t.Errorf("a second webhook: %d %s", resp.Status, resp.Raw)
		}
	})

	t.Run("it changes, rotates and tests its own", func(t *testing.T) {
		changed := obj(t, want(t, app.patch("/api/v1/webhooks/partner", map[string]any{"topics": []string{"issue.transitioned"}}), http.StatusOK, "new topics"), "webhook")
		if changed["url"] != inbox.URL+"/armature" || changed["topics"].([]any)[0] != "issue.transitioned" {
			t.Errorf("changed = %v", changed)
		}
		rotated := obj(t, want(t, app.post("/api/v1/webhooks/partner/rotate-secret", nil), http.StatusOK, "a new secret"), "webhook")
		if rotated["secret"] == hook["secret"] || rotated["secret"] == nil {
			t.Errorf("the secret did not change: %v", rotated)
		}
		delivery := obj(t, want(t, app.post("/api/v1/webhooks/partner/test", nil), http.StatusOK, "a ping"), "delivery")
		if delivery["status"] != float64(200) {
			t.Errorf("the ping = %v", delivery)
		}
	})

	t.Run("the administrator sees it and may only switch it or remove it", func(t *testing.T) {
		var listed map[string]any
		for _, each := range list(t, want(t, ada.get("/api/v1/webhooks"), http.StatusOK, "the list"), "webhooks") {
			if each.(map[string]any)["id"] == id {
				listed = each.(map[string]any)
			}
		}
		if listed == nil || listed["partner"] == nil {
			t.Fatalf("the list does not mark the partner's webhook: %v", listed)
		}
		resp := ada.patch("/api/v1/webhooks/"+id, map[string]any{"name": listed["name"], "url": "https://elsewhere.example.test/in", "topics": listed["topics"]})
		if resp.Status != http.StatusConflict || resp.ErrorCode() != "partner_owned" || !strings.Contains(resp.Error()["message"].(string), "manages this webhook") {
			t.Errorf("an administrator moving it: %d %s", resp.Status, resp.Raw)
		}
		if resp := ada.post("/api/v1/webhooks/"+id+"/rotate-secret", nil); resp.ErrorCode() != "partner_owned" {
			t.Errorf("an administrator rotating it: %d %s", resp.Status, resp.Raw)
		}
		off := obj(t, want(t, ada.patch("/api/v1/webhooks/"+id, map[string]any{"name": "renamed", "url": listed["url"], "topics": listed["topics"], "enabled": false}), http.StatusOK, "switching it off"), "webhook")
		if off["enabled"] != false || off["name"] != listed["name"] {
			t.Errorf("switched off = %v", off)
		}
	})

	t.Run("it is named after its app, through a rename", func(t *testing.T) {
		renamed := h.name(t, "Stator Two")
		want(t, ada.patch("/api/v1/oauth/clients/"+stator.id, map[string]any{"name": renamed}), http.StatusOK, "renaming the app")
		if read := obj(t, want(t, app.get("/api/v1/webhooks/partner"), http.StatusOK, "reading it"), "webhook"); read["name"] != renamed+" (connected app)" {
			t.Errorf("after the rename the webhook is called %v", read["name"])
		}
		var listed map[string]any
		for _, each := range list(t, want(t, ada.get("/api/v1/oauth/clients"), http.StatusOK, "the apps"), "clients") {
			if each.(map[string]any)["id"] == stator.id {
				listed = each.(map[string]any)
			}
		}
		if listed == nil || listed["webhook"].(map[string]any)["id"] != id {
			t.Errorf("the app does not show its webhook: %v", listed)
		}
	})

	t.Run("it removes its own, also when it has none", func(t *testing.T) {
		want(t, app.delete("/api/v1/webhooks/partner"), http.StatusNoContent, "removing")
		want(t, app.delete("/api/v1/webhooks/partner"), http.StatusNoContent, "removing nothing")
		want(t, app.post("/api/v1/webhooks/partner/rotate-secret", nil), http.StatusNotFound, "rotating nothing")
		want(t, app.post("/api/v1/webhooks/partner/test", nil), http.StatusNotFound, "testing nothing")
	})
}

func TestOnlyAnAllowedAppOfAnAdministratorReachesItsWebhook(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	stator, ada, app := adminApp(t, h, api, "Stator")
	inbox := partnerInbox(t)
	want(t, app.post("/api/v1/webhooks/partner", map[string]any{"url": inbox.URL, "topics": []string{"issue.updated"}}), http.StatusCreated, "the webhook")

	t.Run("topics are named, never everything", func(t *testing.T) {
		for _, topics := range [][]string{{"*"}, {"automation.rule_failed"}, {}, {"no.such.topic"}} {
			resp := app.patch("/api/v1/webhooks/partner", map[string]any{"topics": topics})
			if resp.Status != http.StatusUnprocessableEntity || resp.Error()["fields"].(map[string]any)["topics"] == nil {
				t.Errorf("topics %v: %d %s", topics, resp.Status, resp.Raw)
			}
		}
	})

	t.Run("another app reaches nothing of it", func(t *testing.T) {
		_, _, other := adminApp(t, h, api, "Exciter")
		if resp := want(t, other.get("/api/v1/webhooks/partner"), http.StatusOK, "the other app's"); resp.Body["webhook"] != nil {
			t.Errorf("one app read another's webhook: %s", resp.Raw)
		}
	})

	t.Run("a session or an app without the scope is refused", func(t *testing.T) {
		if resp := ada.get("/api/v1/webhooks/partner"); resp.Status != http.StatusForbidden {
			t.Errorf("a session: %d %s", resp.Status, resp.Raw)
		}
		narrowed := want(t, stator.token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {connect(t, stator, ada, "read")["refresh_token"].(string)}, "scope": {"read"}}), http.StatusOK, "narrowing to read")
		reader := bearer(api, t, narrowed.Body["access_token"])
		resp := reader.get("/api/v1/webhooks/partner")
		if resp.Status != http.StatusForbidden || resp.ErrorCode() != "insufficient_scope" {
			t.Errorf("an app without webhooks.partner: %d %s", resp.Status, resp.Raw)
		}
	})

	t.Run("a member is never asked for it", func(t *testing.T) {
		mia := api.client(t)
		mia.signIn(t, h.person(t, "mia", auth.RoleMember))
		_, challenge := pkce(t)
		page := want(t, mia.get("/api/v1/oauth/consent?"+authorizeQuery(stator.id, "read webhooks.partner", challenge)), http.StatusOK, "the page")
		if consent := obj(t, page, "consent"); consent["mayAllow"] != false || consent["mayCancel"] != true {
			t.Errorf("a member was asked for the webhook: %s", page.Raw)
		}
	})

	t.Run("an administrator who is no longer one is refused", func(t *testing.T) {
		gus := api.client(t)
		gusEmail := h.person(t, "gus", auth.RoleAdmin)
		gus.signIn(t, gusEmail)
		app := bearer(api, t, connect(t, stator, gus, "read webhooks.partner")["access_token"])
		want(t, app.get("/api/v1/webhooks/partner"), http.StatusOK, "while Gus administers")
		if _, err := h.super.Exec(context.Background(), `
			DELETE FROM role_assignment WHERE user_id = (SELECT id FROM app_user WHERE email = $1) AND role = 'global_administrator'`, gusEmail); err != nil {
			t.Fatal(err)
		}
		resp := app.get("/api/v1/webhooks/partner")
		if resp.Status != http.StatusForbidden || !strings.Contains(resp.Error()["message"].(string), "is no longer an Armature administrator") {
			t.Errorf("after Gus stopped administering: %d %s", resp.Status, resp.Raw)
		}
	})

	t.Run("the database keeps one webhook per app, gone with the app", func(t *testing.T) {
		_, err := h.super.Exec(context.Background(), `
			INSERT INTO webhook_endpoint (name, url, secret, topics, client_id) VALUES ($1, 'https://x.test/in', 's', '{issue.updated}', $2)`,
			h.name(t, "Second"), stator.id)
		if err == nil || !strings.Contains(err.Error(), "webhook_endpoint_client_id_key") {
			t.Errorf("a second webhook for one app went in: %v", err)
		}
		want(t, ada.delete("/api/v1/oauth/clients/"+stator.id), http.StatusNoContent, "removing the app")
		var left int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM webhook_endpoint WHERE client_id = $1`, stator.id).Scan(&left); err != nil || left != 0 {
			t.Errorf("the app's webhook outlived it: %d %v", left, err)
		}
	})
}

func TestTheOutboundGuardNamesWhatToAllow(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	api.api.Webhooks = webhook.NewService(h.cluster, log).WithAllow(netguard.ParseAllow(""))
	_, ada, app := adminApp(t, h, api, "Stator")
	inbox := partnerInbox(t)

	resp := app.post("/api/v1/webhooks/partner", map[string]any{"url": inbox.URL + "/in", "topics": []string{"issue.updated"}})
	if resp.Status != http.StatusUnprocessableEntity || resp.ErrorCode() != "outbound_refused" ||
		!strings.Contains(resp.Error()["message"].(string), "Add 127.0.0.1 to network.outboundAllow (ARMATURE_OUTBOUND_ALLOW)") {
		t.Errorf("a receiver inside the network: %d %s", resp.Status, resp.Raw)
	}
	if resp := ada.post("/api/v1/webhooks", map[string]any{"name": h.name(t, "Inside"), "url": inbox.URL, "topics": []string{"*"}}); resp.ErrorCode() != "outbound_refused" {
		t.Errorf("an administrator's webhook inside the network: %d %s", resp.Status, resp.Raw)
	}
	if resp := app.post("/api/v1/webhooks/partner", map[string]any{"url": "https://nowhere.invalid/in", "topics": []string{"issue.updated"}}); resp.Status != http.StatusUnprocessableEntity || resp.ErrorCode() != "invalid" {
		t.Errorf("a receiver that does not resolve: %d %s", resp.Status, resp.Raw)
	}

	api.api.Webhooks = webhook.NewService(h.cluster, log).WithAllow(netguard.ParseAllow("127.0.0.1"))
	want(t, app.post("/api/v1/webhooks/partner", map[string]any{"url": inbox.URL + "/in", "topics": []string{"issue.updated"}}), http.StatusCreated, "once the operator allows it")
}
