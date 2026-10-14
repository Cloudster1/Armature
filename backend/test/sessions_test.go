//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
)

// currentSession is the id of the session a client is calling with, as the
// list marks it.
func currentSession(t *testing.T, c *client) string {
	t.Helper()
	for _, s := range list(t, want(t, c.get("/api/v1/auth/sessions"), http.StatusOK, "list sessions"), "sessions") {
		if row := s.(map[string]any); row["current"] == true {
			return row["id"].(string)
		}
	}
	t.Fatal("no session is marked as the current one")
	return ""
}

// A person sees where they are signed in and ends the sessions they do not
// recognise, one at a time or every one but the session in hand.
func TestAPersonEndsTheirOtherSessions(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)

	me := api.client(t)
	signedUp := me.signIn(t, h.person(t, "sessions", auth.RoleOwner))
	email := principalField(t, signedUp, "principal", "user", "email").(string)
	userID := principalField(t, signedUp, "principal", "user", "id").(string)
	signIn := func(what string) *client {
		c := api.client(t)
		want(t, c.post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusOK, what)
		return c
	}
	laptop := signIn("sign in on the laptop")
	phone := signIn("sign in on the phone")
	tablet := signIn("sign in on the tablet")

	listed := list(t, want(t, me.get("/api/v1/auth/sessions"), http.StatusOK, "list"), "sessions")
	if len(listed) != 4 {
		t.Fatalf("listed %d sessions, want 4: %v", len(listed), listed)
	}
	newest, oldest := listed[0].(map[string]any), listed[3].(map[string]any)
	if newest["id"] != currentSession(t, tablet) || oldest["id"] != currentSession(t, me) {
		t.Fatalf("not newest first: %v", listed)
	}
	for i, s := range listed {
		row := s.(map[string]any)
		if row["current"] != (i == 3) {
			t.Fatalf("session %d marked current = %v: %v", i, row["current"], row)
		}
		if row["userAgent"] == "" || row["ip"] == "" || row["proof"] != "password" || row["lastSeenAt"] == "" {
			t.Fatalf("a session without where and how it was opened: %v", row)
		}
	}

	// Another person's session is not there to end, and keeps working.
	stranger := api.client(t)
	stranger.signIn(t, h.person(t, "sessions-stranger", auth.RoleOwner))
	if got := me.delete("/api/v1/auth/sessions/" + currentSession(t, stranger)); got.Status != http.StatusNotFound {
		t.Fatalf("ended somebody else's session: %d %s", got.Status, got.Raw)
	}
	want(t, stranger.get("/api/v1/auth/me"), http.StatusOK, "the stranger is still signed in")

	// The session in hand is ended by signing out, and the refusal says so.
	refused := me.delete("/api/v1/auth/sessions/" + currentSession(t, me))
	if refused.Status != http.StatusConflict || !strings.Contains(refused.Raw, "Sign out") {
		t.Fatalf("ending the current session: %d %s", refused.Status, refused.Raw)
	}
	want(t, me.get("/api/v1/auth/me"), http.StatusOK, "the refused session stays")

	phoneID := currentSession(t, phone)
	want(t, me.delete("/api/v1/auth/sessions/"+phoneID), http.StatusNoContent, "end the phone's session")
	if got := phone.get("/api/v1/auth/me"); got.Status != http.StatusUnauthorized {
		t.Fatalf("the ended session still works: %d", got.Status)
	}
	want(t, laptop.get("/api/v1/auth/me"), http.StatusOK, "the laptop is untouched")
	if got := me.delete("/api/v1/auth/sessions/" + phoneID); got.Status != http.StatusNotFound {
		t.Fatalf("ending it twice: %d %s", got.Status, got.Raw)
	}

	// A token cannot see or end anybody's sessions, not even its owner's.
	token := want(t, me.post("/api/v1/tokens", map[string]any{"name": "ci", "scopes": []string{}, "projects": []string{}}), http.StatusCreated, "make a token")
	viaToken := api.client(t)
	viaToken.bearer = obj(t, token, "token")["secret"].(string)
	for _, got := range []response{
		viaToken.get("/api/v1/auth/sessions"),
		viaToken.delete("/api/v1/auth/sessions/" + currentSession(t, laptop)),
		viaToken.delete("/api/v1/auth/sessions"),
	} {
		if got.Status != http.StatusForbidden || got.ErrorCode() != "session_only" {
			t.Fatalf("a token reached the sessions: %d %s", got.Status, got.Raw)
		}
	}

	want(t, me.delete("/api/v1/auth/sessions"), http.StatusNoContent, "sign out everywhere else")
	for _, c := range []*client{laptop, tablet} {
		if got := c.get("/api/v1/auth/me"); got.Status != http.StatusUnauthorized {
			t.Fatalf("a session outlived signing out everywhere else: %d", got.Status)
		}
	}
	if left := list(t, want(t, me.get("/api/v1/auth/sessions"), http.StatusOK, "list after"), "sessions"); len(left) != 1 || left[0].(map[string]any)["current"] != true {
		t.Fatalf("after signing out everywhere else: %v", left)
	}
	want(t, stranger.get("/api/v1/auth/me"), http.StatusOK, "nobody else is signed out")

	// Both acts are in the installation's log, by the person who did them.
	for _, c := range []struct{ action, target string }{{"auth.session_ended", phoneID}, {"auth.sessions_ended", userID}} {
		var n int
		if err := h.super.QueryRow(context.Background(), `
			SELECT count(*) FROM audit_log
			WHERE action = $1 AND target_id = $2 AND actor_user_id = $3`, c.action, c.target, userID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("%s audited %d times, want once", c.action, n)
		}
	}
	var ended int
	if err := h.super.QueryRow(context.Background(), `
		SELECT (data->>'ended')::int FROM audit_log WHERE action = 'auth.sessions_ended' AND actor_user_id = $1`, userID).Scan(&ended); err != nil {
		t.Fatal(err)
	}
	if ended != 2 {
		t.Fatalf("signing out everywhere else recorded %d sessions ended, want 2", ended)
	}
}
