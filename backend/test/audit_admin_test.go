//go:build integration

package test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/events"
)

// copyToAudit hands every event the organization emitted to the audit
// consumer, as the worker would, since no worker runs beside the suite.
func (h *harness) copyToAudit(t *testing.T, orgID string) {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT id, org_id, topic, payload FROM outbox_event WHERE org_id = $1 ORDER BY created_at`, orgID)
	if err != nil {
		t.Fatal(err)
	}
	var found []events.Event
	for rows.Next() {
		var e events.Event
		if err := rows.Scan(&e.ID, &e.OrgID, &e.Topic, &e.Payload); err != nil {
			t.Fatal(err)
		}
		found = append(found, e)
	}
	rows.Close()
	consumer := audit.NewConsumer(h.cluster, nil, slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	for _, e := range found {
		if err := consumer.Handle(context.Background(), e); err != nil {
			t.Fatalf("copy %s into the audit log: %v", e.Topic, err)
		}
	}
}

// Every change to who may do what or where the events go is audited with
// actor, object and time, a password sign-in too, and no secret goes with it.
func TestEveryAdministrativeChangeIsAudited(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	signedUp := owner.signup(t, h, "auditall")
	orgID := principalField(t, signedUp, "principal", "org", "id").(string)
	ownerID := principalField(t, signedUp, "principal", "user", "id").(string)
	email := principalField(t, signedUp, "principal", "user", "email").(string)
	project := want(t, owner.post("/api/v1/projects", map[string]any{"name": "Audited", "key": "AUD" + strings.ToUpper(uuid.New().String()[:3]), "template": "kanban"}), http.StatusCreated, "project")
	key := obj(t, project, "project")["key"].(string)

	// A password sign-in, from a browser of its own.
	want(t, api.client(t).post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusOK, "signing in with a password")

	// A webhook made, changed, given a new secret and removed. Its address
	// carries a credential in the path, as a chat service's incoming hook does.
	sink := httptest.NewServer(&receiver{status: http.StatusOK})
	defer sink.Close()
	const pathCredential = "T0AUDIT/B0AUDIT/hunter2hunter2hunter2"
	hookURL := sink.URL + "/services/" + pathCredential
	hook := want(t, owner.post("/api/v1/webhooks", map[string]any{"name": "Elsewhere", "url": hookURL, "topics": []string{"*"}}), http.StatusCreated, "a webhook")
	hookID := idOf(t, hook, "webhook")
	firstSecret := obj(t, hook, "webhook")["secret"].(string)
	want(t, owner.patch("/api/v1/webhooks/"+hookID, map[string]any{"name": "Elsewhere", "url": hookURL, "topics": []string{events.TopicIssueCreated}}), http.StatusOK, "changing the webhook")
	rotated := want(t, owner.post("/api/v1/webhooks/"+hookID+"/rotate-secret", nil), http.StatusOK, "rotating the secret")
	secondSecret := obj(t, rotated, "webhook")["secret"].(string)
	want(t, owner.delete("/api/v1/webhooks/"+hookID), http.StatusNoContent, "removing the webhook")

	// A rule made, changed and removed, and one with an incoming address whose
	// token is as good as a password.
	rule := want(t, owner.post("/api/v1/projects/"+key+"/automation/rules", map[string]any{
		"name": "Label new work", "trigger": map[string]any{"kind": "issue.created"},
		"actions": []any{map[string]any{"kind": "add_label", "value": "new"}},
	}), http.StatusCreated, "a rule")
	ruleID := idOf(t, rule, "rule")
	want(t, owner.patch("/api/v1/automation/rules/"+ruleID, map[string]any{
		"name": "Label new work", "enabled": false, "trigger": map[string]any{"kind": "issue.created"},
		"actions": []any{map[string]any{"kind": "add_label", "value": "fresh"}},
	}), http.StatusOK, "changing the rule")
	want(t, owner.delete("/api/v1/automation/rules/"+ruleID), http.StatusNoContent, "removing the rule")
	incoming := want(t, owner.post("/api/v1/automation/rules", map[string]any{
		"name": "From outside", "trigger": map[string]any{"kind": "incoming"},
		"actions": []any{map[string]any{"kind": "create_issue", "text": "An alert came in"}},
	}), http.StatusCreated, "a rule with an incoming address")
	incomingID := idOf(t, incoming, "rule")
	hookToken := obj(t, incoming, "rule")["trigger"].(map[string]any)["token"].(string)

	// An invitation sent and withdrawn.
	invited := want(t, owner.post("/api/v1/invites", map[string]string{"email": h.email(t, "auditinvitee"), "role": "admin"}), http.StatusCreated, "an invitation")
	inviteID := idOf(t, invited, "invite")
	inviteToken := invited.Body["token"].(string)
	want(t, owner.delete("/api/v1/invites/"+inviteID), http.StatusNoContent, "withdrawing the invitation")

	// A role defined, redefined to administer, and removed; a group made and removed.
	role := obj(t, want(t, owner.post("/api/v1/roles", map[string]any{"name": "Auditor", "permissions": []string{"read"}}), http.StatusCreated, "a role"), "role")["role"].(string)
	want(t, owner.patch("/api/v1/roles/"+role, map[string]any{"permissions": []string{"read", "org.administer"}}), http.StatusOK, "redefining the role to administer")
	want(t, owner.delete("/api/v1/roles/"+role), http.StatusNoContent, "removing the role")
	group := want(t, owner.post("/api/v1/groups", map[string]any{"name": "Short lived"}), http.StatusCreated, "a group")
	groupID := idOf(t, group, "group")
	want(t, owner.delete("/api/v1/groups/"+groupID), http.StatusNoContent, "removing the group")

	h.copyToAudit(t, orgID)

	// One entry per act, naming the actor, the object and the time, found
	// through the same filter the audit page uses.
	type expectation struct {
		action, targetType, targetID string
	}
	for _, e := range []expectation{
		{"auth.login", "user", ownerID},
		{"webhook.created", "webhook", hookID},
		{"webhook.updated", "webhook", hookID},
		{"webhook.secret_rotated", "webhook", hookID},
		{"webhook.deleted", "webhook", hookID},
		{"rule.created", "rule", ruleID},
		{"rule.updated", "rule", ruleID},
		{"rule.deleted", "rule", ruleID},
		{"rule.created", "rule", incomingID},
		{"invite.created", "invite", inviteID},
		{"invite.revoked", "invite", inviteID},
		{"role.defined", "role", ""},
		{"role.redefined", "role", ""},
		{"role.removed", "role", ""},
		{"group.deleted", "group", groupID},
	} {
		page := want(t, owner.get("/api/v1/audit?action="+url.QueryEscape(e.action)), http.StatusOK, "the log filtered to "+e.action)
		var entries []any
		for _, entry := range list(t, page, "entries") {
			if e.targetID == "" || entry.(map[string]any)["targetId"] == e.targetID {
				entries = append(entries, entry)
			}
		}
		if len(entries) != 1 {
			t.Errorf("%s about %q: %d entries, want one: %s", e.action, e.targetID, len(entries), page.Raw)
			continue
		}
		entry := entries[0].(map[string]any)
		if entry["actorId"] != ownerID || entry["actorName"] == "" {
			t.Errorf("%s names actor %v (%v), want the owner %s", e.action, entry["actorId"], entry["actorName"], ownerID)
		}
		if entry["targetType"] != e.targetType {
			t.Errorf("%s targets a %v, want a %s", e.action, entry["targetType"], e.targetType)
		}
		if e.targetID != "" && entry["targetId"] != e.targetID {
			t.Errorf("%s targets %v, want %s", e.action, entry["targetId"], e.targetID)
		}
		if at, _ := entry["createdAt"].(string); at == "" {
			t.Errorf("%s has no time", e.action)
		}
		offered := list(t, page, "actions")
		found := false
		for _, a := range offered {
			found = found || a == e.action
		}
		if !found {
			t.Errorf("the filter does not offer %s: %v", e.action, offered)
		}
	}
	// The role entries say which role, since a role has a key rather than an id.
	if redefined := list(t, owner.get("/api/v1/audit?action=role.redefined"), "entries"); len(redefined) == 1 {
		if data, _ := redefined[0].(map[string]any)["data"].(map[string]any); data["role"] != role {
			t.Errorf("role.redefined data = %v, want it to name %s", data, role)
		}
	}
	// The sign-in says where it came from, as the single sign-on entry does.
	if logins := list(t, owner.get("/api/v1/audit?action=auth.login"), "entries"); len(logins) == 1 {
		if ip, _ := logins[0].(map[string]any)["ip"].(string); ip == "" {
			t.Error("the password sign-in does not say where it came from")
		}
	}

	// No secret the acts handed out is written down, read straight from the
	// table and from the export a spreadsheet would keep.
	var written string
	if err := h.super.QueryRow(context.Background(), `
		SELECT COALESCE(string_agg(action || ' ' || data::text, E'\n'), '') FROM audit_log WHERE org_id = $1`, orgID).Scan(&written); err != nil {
		t.Fatal(err)
	}
	_, exported := owner.download("/api/v1/audit/export")
	secrets := map[string]string{
		"the webhook's first secret":    firstSecret,
		"the webhook's rotated secret":  secondSecret,
		"the credential in the address": pathCredential,
		"the rule's incoming token":     hookToken,
		"the invitation's token":        inviteToken,
	}
	for what, secret := range secrets {
		if secret == "" {
			t.Fatalf("%s was not handed out", what)
		}
		if strings.Contains(written, secret) {
			t.Errorf("%s is in the audit log: %s", what, written)
		}
		if strings.Contains(string(exported), secret) {
			t.Errorf("%s is in the audit export", what)
		}
	}
	if !strings.Contains(written, "webhook.secret_rotated") {
		t.Error("the rotation is not recorded as a rotation")
	}
}
