//go:build integration

package test

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/privacy"
)

// Personal data has a lifetime: rows past their use are pruned, a person can
// take their data and leave, and letting a member go switches them off.
func TestPersonalDataHasALifetime(t *testing.T) {
	h := newHarness(t)
	recKey := h.projectKey(t, "REC")
	api := newAPIServer(t, h)
	owner := api.client(t)
	signedUp := owner.signIn(t, h.person(t, "lifetime", auth.RoleOwner))
	slug := principalField(t, signedUp, "principal", "org", "slug").(string)
	ownerID := principalField(t, signedUp, "principal", "user", "id").(string)
	var orgID uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM org WHERE slug = $1`, slug).Scan(&orgID); err != nil {
		t.Fatal(err)
	}
	want(t, owner.post("/api/v1/projects", map[string]any{"name": "Records", "key": recKey}), http.StatusCreated, "a project")

	// Somebody else joins by invitation and leaves a comment behind.
	other := api.client(t)
	otherEmail := h.email(t, "leaving")
	invited := want(t, owner.post("/api/v1/invites", map[string]any{"email": otherEmail, "role": "member"}), http.StatusCreated, "invite")
	want(t, other.post("/api/v1/auth/invites/accept", map[string]any{"token": invited.Body["token"], "name": "Lee Ving", "password": testPassword}), http.StatusOK, "accept")
	otherID := principalField(t, other.get("/api/v1/auth/me"), "principal", "user", "id").(string)
	h.erasable(otherID)
	made := want(t, other.post("/api/v1/projects/"+recKey+"/issues", map[string]any{"summary": "Keep the lights on"}), http.StatusCreated, "an issue")
	key := obj(t, made, "issue")["key"].(string)
	want(t, other.post("/api/v1/issues/"+key+"/comments", map[string]any{"text": "I will look at this on Monday."}), http.StatusCreated, "a comment")

	t.Run("what has served its purpose is pruned, and what is fresh stays", func(t *testing.T) {
		for _, q := range []string{
			`INSERT INTO user_session (user_id, token_hash, expires_at) SELECT $1, decode(md5(random()::text), 'hex'), now() - interval '3 days' WHERE $2::uuid IS NOT NULL`,
			`INSERT INTO user_session (user_id, token_hash, expires_at) SELECT $1, decode(md5(random()::text), 'hex'), now() + interval '3 days' WHERE $2::uuid IS NOT NULL`,
			`INSERT INTO notification (org_id, user_id, kind, title, event_id, created_at) VALUES ($2, $1, 'assigned', 'ancient', gen_random_uuid(), now() - interval '200 days')`,
			`INSERT INTO notification (org_id, user_id, kind, title, event_id, created_at) VALUES ($2, $1, 'assigned', 'recent', gen_random_uuid(), now() - interval '2 days')`,
			`INSERT INTO inbound_mail (message_id, from_email, subject, outcome, received_at) VALUES ('<old@' || $2::text || $1::text || '>', 'someone@elsewhere.test', 'old', 'unmatched', now() - interval '100 days')`,
			`INSERT INTO login_challenge (user_id, token_hash, expires_at) SELECT $1, decode(md5(random()::text), 'hex'), now() - interval '3 days' WHERE $2::uuid IS NOT NULL`,
		} {
			if _, err := h.super.Exec(context.Background(), q, ownerID, orgID); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
		if _, err := h.super.Exec(context.Background(), `UPDATE outbox_event SET published_at = now() - interval '40 days' WHERE org_id = $1 AND payload->>'key' LIKE $2`, orgID, recKey+"-%"); err != nil {
			t.Fatal(err)
		}
		counts, err := privacy.NewRetention(h.cluster, privacy.DefaultPolicy(), slog.New(slog.DiscardHandler)).Once(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if counts["sessions"] < 1 || counts["notifications"] < 1 || counts["inbound-mail"] < 1 || counts["login-challenges"] < 1 {
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

	t.Run("letting a member go switches their account off, and switching on brings them back", func(t *testing.T) {
		want(t, owner.delete("/api/v1/members/"+ownerID), http.StatusConflict, "letting yourself go")
		want(t, other.delete("/api/v1/members/"+ownerID), http.StatusForbidden, "a member cannot let anyone go")
		goerEmail := h.email(t, "goer")
		goer := api.client(t)
		invited := want(t, owner.post("/api/v1/invites", map[string]any{"email": goerEmail, "role": "member"}), http.StatusCreated, "invite")
		goerID := principalField(t, want(t, goer.post("/api/v1/auth/invites/accept", map[string]any{"token": invited.Body["token"], "name": "Goer", "password": testPassword}), http.StatusOK, "accept"), "principal", "user", "id").(string)

		want(t, owner.delete("/api/v1/members/"+goerID), http.StatusNoContent, "let them go")
		if got := goer.get("/api/v1/auth/me"); got.Status != http.StatusUnauthorized {
			t.Errorf("their open session survived: %d", got.Status)
		}
		refused := api.client(t).post("/api/v1/auth/login", map[string]string{"email": goerEmail, "password": testPassword})
		if refused.Status != http.StatusForbidden || refused.ErrorCode() != "switched_off" {
			t.Errorf("signing in after being let go = %d %s, want 403 switched_off", refused.Status, refused.Raw)
		}
		var active bool
		var members int
		if err := h.super.QueryRow(context.Background(), `
			SELECT u.is_active, (SELECT count(*) FROM org_member m WHERE m.user_id = u.id AND m.org_id = $2)
			FROM app_user u WHERE u.id = $1`, goerID, orgID).Scan(&active, &members); err != nil {
			t.Fatal(err)
		}
		if active || members != 1 {
			t.Errorf("after being let go: active = %v, memberships here = %d; want switched off and still here", active, members)
		}
		want(t, owner.patch("/api/v1/users/"+goerID, map[string]any{"isActive": true}), http.StatusOK, "switch them on")
		want(t, api.client(t).post("/api/v1/auth/login", map[string]string{"email": goerEmail, "password": testPassword}), http.StatusOK, "and they sign in again")
		want(t, owner.delete("/api/v1/members/"+uuid.NewString()), http.StatusNotFound, "nobody by that id")
	})

	t.Run("a person is erased, their work is not", func(t *testing.T) {
		token := want(t, other.post("/api/v1/tokens", map[string]any{"name": "script"}), http.StatusCreated, "a token")
		bearer := api.client(t)
		bearer.bearer = obj(t, token, "token")["secret"].(string)
		want(t, bearer.delete("/api/v1/auth/me"), http.StatusForbidden, "a token cannot erase its owner")
		want(t, other.post("/api/v1/issues/"+key+"/watchers", map[string]any{}), http.StatusCreated, "they watch their issue")
		want(t, owner.post("/api/v1/issues/"+key+"/watchers", map[string]any{}), http.StatusCreated, "and so does the owner")

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
		// An erased person is on no watcher list, not even as "Former user".
		var watching int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM issue_watcher WHERE user_id = $1`, otherID).Scan(&watching); err != nil {
			t.Fatal(err)
		}
		if watching != 0 {
			t.Fatalf("watcher rows left for the erased person = %d", watching)
		}
		watchers := list(t, want(t, owner.get("/api/v1/issues/"+key+"/watchers"), http.StatusOK, "the watchers"), "watchers")
		if len(watchers) != 1 || watchers[0].(map[string]any)["userId"] != ownerID {
			t.Fatalf("watchers after erasure = %v, want the owner alone", watchers)
		}
		var audited int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = 'user.erased' AND target_id = $2`, orgID, otherID).Scan(&audited); err != nil {
			t.Fatal(err)
		}
		if audited != 1 {
			t.Fatalf("audit rows for the erasure = %d", audited)
		}
	})
}

// The last active administrator cannot erase themselves. Only an installation
// of its own has one administrator; the shared one has every test's.
func TestTheLastActiveAdministratorCannotEraseThemselves(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api, empty := emptyInstallation(t, h, "armature_last_administrator_erase_test")
	api.api.Auth = empty.authService().Signups(auth.SignupFirst)
	founder := api.client(t)
	want(t, founder.post("/api/v1/auth/signup", map[string]string{"email": h.email(t, "founder"), "password": testPassword, "name": "Founder"}), http.StatusCreated, "the first account")

	refused := want(t, founder.delete("/api/v1/auth/me"), http.StatusConflict, "the last active administrator")
	if refused.ErrorCode() != "last_administrator" || !strings.Contains(refused.Raw, "Make somebody else an administrator first") {
		t.Fatalf("refusal = %s", refused.Raw)
	}
}
