//go:build integration

package test

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/privacy"
)

// Personal data has a lifetime: rows past their use are pruned, a person can
// take their data and leave, and an organization can let a member go.
func TestPersonalDataHasALifetime(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	signedUp := owner.signup(t, h, "lifetime")
	slug := principalField(t, signedUp, "principal", "org", "slug").(string)
	ownerID := principalField(t, signedUp, "principal", "user", "id").(string)
	var orgID uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM org WHERE slug = $1`, slug).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Records", "key": "REC"}), http.StatusCreated, "a project")

	// Somebody else joins by invitation and leaves a comment behind.
	other := api.client(t)
	otherEmail := h.email(t, "leaving")
	invited := want(t, owner.post("/api/v1/invites", map[string]any{"email": otherEmail, "role": "member"}), http.StatusCreated, "invite")
	want(t, other.post("/api/v1/auth/invites/accept", map[string]any{"token": invited.Body["token"], "name": "Lee Ving", "password": testPassword}), http.StatusOK, "accept")
	otherID := principalField(t, other.get("/api/v1/auth/me"), "principal", "user", "id").(string)
	made := want(t, other.post("/api/v1/projects/REC/issues", map[string]any{"summary": "Keep the lights on"}), http.StatusCreated, "an issue")
	key := obj(t, made, "issue")["key"].(string)
	want(t, other.post("/api/v1/issues/"+key+"/comments", map[string]any{"text": "I will look at this on Monday."}), http.StatusCreated, "a comment")

	t.Run("what has served its purpose is pruned, and what is fresh stays", func(t *testing.T) {
		for _, q := range []string{
			`INSERT INTO user_session (user_id, token_hash, expires_at) SELECT $1, decode(md5(random()::text), 'hex'), now() - interval '3 days' WHERE $2::uuid IS NOT NULL`,
			`INSERT INTO user_session (user_id, token_hash, expires_at) SELECT $1, decode(md5(random()::text), 'hex'), now() + interval '3 days' WHERE $2::uuid IS NOT NULL`,
			`INSERT INTO notification (org_id, user_id, kind, title, event_id, created_at) VALUES ($2, $1, 'assigned', 'ancient', gen_random_uuid(), now() - interval '200 days')`,
			`INSERT INTO notification (org_id, user_id, kind, title, event_id, created_at) VALUES ($2, $1, 'assigned', 'recent', gen_random_uuid(), now() - interval '2 days')`,
			`INSERT INTO inbound_mail (message_id, from_email, subject, outcome, received_at) VALUES ('<old@' || $2::text || $1::text || '>', 'someone@elsewhere.test', 'old', 'unmatched', now() - interval '100 days')`,
		} {
			if _, err := h.super.Exec(context.Background(), q, ownerID, orgID); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
		if _, err := h.super.Exec(context.Background(), `UPDATE outbox_event SET published_at = now() - interval '40 days' WHERE org_id = $1 AND published_at IS NOT NULL`, orgID); err != nil {
			t.Fatal(err)
		}
		counts, err := privacy.NewRetention(h.cluster, privacy.DefaultPolicy(), slog.New(slog.DiscardHandler)).Once(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if counts["sessions"] < 1 || counts["notifications"] < 1 || counts["inbound-mail"] < 1 {
			t.Fatalf("pruned = %v", counts)
		}
		var sessions, notes int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM user_session WHERE user_id = $1 AND expires_at < now()`, ownerID).Scan(&sessions); err != nil {
			t.Fatal(err)
		}
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM notification WHERE user_id = $1`, ownerID).Scan(&notes); err != nil {
			t.Fatal(err)
		}
		if sessions != 0 || notes != 1 {
			t.Fatalf("expired sessions left = %d, notifications left = %d, want 0 and the recent one", sessions, notes)
		}
		want(t, owner.get("/api/v1/auth/me"), http.StatusOK, "the live session is untouched")
	})

	t.Run("a person can take everything held about them", func(t *testing.T) {
		export := other.get("/api/v1/auth/me/export")
		if export.Status != http.StatusOK || !strings.Contains(export.Raw, "I will look at this on Monday.") || !strings.Contains(export.Raw, "Keep the lights on") {
			t.Fatalf("export = %d %s", export.Status, export.Raw[:min(len(export.Raw), 300)])
		}
		if !strings.Contains(export.Raw, `"memberships"`) || !strings.Contains(export.Raw, `"sessions"`) {
			t.Fatal("the export lacks memberships or sessions")
		}
	})

	t.Run("an organization lets a member go, but never its last owner", func(t *testing.T) {
		refused := want(t, owner.delete("/api/v1/members/"+ownerID), http.StatusConflict, "the last owner")
		if refused.ErrorCode() != "last_owner" || !strings.Contains(refused.Raw, "another") && !strings.Contains(refused.Raw, "somebody else") {
			t.Fatalf("refusal = %s", refused.Raw)
		}
		want(t, other.delete("/api/v1/members/"+ownerID), http.StatusForbidden, "a member cannot remove anyone")
		bystander := h.joinExisting(t, &workspace{orgID: orgID}, h.email(t, "bystander"), "member")
		want(t, owner.delete("/api/v1/members/"+bystander.String()), http.StatusNoContent, "remove a member")
		want(t, owner.delete("/api/v1/members/"+bystander.String()), http.StatusNotFound, "and they are no longer here")
		want(t, owner.delete("/api/v1/members/"+uuid.NewString()), http.StatusNotFound, "nobody by that id")
	})

	t.Run("a person is erased, their work is not", func(t *testing.T) {
		token := want(t, other.post("/api/v1/tokens", map[string]any{"name": "script"}), http.StatusCreated, "a token")
		bearer := api.client(t)
		bearer.bearer = obj(t, token, "token")["secret"].(string)
		want(t, bearer.delete("/api/v1/auth/me"), http.StatusForbidden, "a token cannot erase its owner")

		want(t, other.delete("/api/v1/auth/me"), http.StatusNoContent, "erase")
		want(t, other.get("/api/v1/auth/me"), http.StatusUnauthorized, "the session is gone")
		want(t, bearer.get("/api/v1/auth/me"), http.StatusUnauthorized, "and so is the token")
		want(t, api.client(t).post("/api/v1/auth/login", map[string]string{"email": otherEmail, "password": testPassword}), http.StatusUnauthorized, "the address no longer signs in")

		var email, name string
		var active bool
		if err := h.super.QueryRow(context.Background(), `SELECT email::text, name, is_active FROM app_user WHERE id = $1`, otherID).Scan(&email, &name, &active); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(email, "@erased.invalid") || name != privacy.ErasedName || active {
			t.Fatalf("tombstone = %q %q active=%v", email, name, active)
		}
		var members int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM org_member WHERE user_id = $1`, otherID).Scan(&members); err != nil {
			t.Fatal(err)
		}
		if members != 0 {
			t.Fatalf("memberships left = %d", members)
		}
		comments := list(t, want(t, owner.get("/api/v1/issues/"+key+"/comments"), http.StatusOK, "the comment stays"), "comments")
		if len(comments) != 1 || comments[0].(map[string]any)["author"].(map[string]any)["name"] != privacy.ErasedName {
			t.Fatalf("comments after erasure = %v", comments)
		}
		want(t, owner.get("/api/v1/issues/"+key), http.StatusOK, "the issue stays")
		var audited int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = 'user.erased' AND target_id = $2`, orgID, otherID).Scan(&audited); err != nil {
			t.Fatal(err)
		}
		if audited != 1 {
			t.Fatalf("audit rows for the erasure = %d", audited)
		}
	})

	t.Run("the last owner cannot erase themselves either", func(t *testing.T) {
		refused := want(t, owner.delete("/api/v1/auth/me"), http.StatusConflict, "the last owner")
		if refused.ErrorCode() != "last_owner" {
			t.Fatalf("code = %q", refused.ErrorCode())
		}
	})
}
