//go:build integration

package test

import (
	"bytes"
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/httpapi"
)

// resetMailWait is how long a test waits for the mail the API sends after it
// has answered.
const resetMailWait = 5 * time.Second

// resetSecret finds the secret after the # of a mailed link.
var resetSecret = regexp.MustCompile(`http://app\.test/reset-password#(\S+)`)

// mailsSoFar counts what the API has mailed, so a later wait looks only at
// what came after.
func (s *apiServer) mailsSoFar() int {
	s.mailer.mu.Lock()
	defer s.mailer.mu.Unlock()
	return len(s.mailer.sent)
}

// awaitMail waits for a mail to an address, about a subject, sent after the
// first `after` mails.
func (s *apiServer) awaitMail(t *testing.T, to, subject string, after int) string {
	t.Helper()
	deadline := time.Now().Add(resetMailWait)
	for time.Now().Before(deadline) {
		s.mailer.mu.Lock()
		for _, sent := range s.mailer.sent[after:] {
			if sent.To == to && strings.Contains(sent.Subject, subject) {
				s.mailer.mu.Unlock()
				return sent.Body
			}
		}
		s.mailer.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no mail to %s about %q within %v", to, subject, resetMailWait)
	return ""
}

// mailedTo says whether anything at all went to an address after the first `after` mails.
func (s *apiServer) mailedTo(to string, after int) bool {
	s.mailer.mu.Lock()
	defer s.mailer.mu.Unlock()
	for _, sent := range s.mailer.sent[after:] {
		if sent.To == to {
			return true
		}
	}
	return false
}

// askForReset asks for a link to an address and returns the secret it mailed.
func (s *apiServer) askForReset(t *testing.T, email string) string {
	t.Helper()
	before := s.mailsSoFar()
	want(t, s.client(t).post("/api/v1/auth/password-reset", map[string]string{"email": email}), http.StatusAccepted, "ask for a link")
	body := s.awaitMail(t, email, "Set a new password", before)
	match := resetSecret.FindStringSubmatch(body)
	if match == nil {
		t.Fatalf("the mail carries no link to the reset page: %q", body)
	}
	return match[1]
}

func (h *harness) resetRows(t *testing.T, userID string) int {
	t.Helper()
	var n int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM password_reset WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatalf("count reset links: %v", err)
	}
	return n
}

// A person who forgot their password asks for a link by mail and sets a new
// one with it; the link works once, for an hour, and ends what the old one opened.
func TestAForgottenPasswordIsResetByMail(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	const newPassword = "a fresh and adequate password"

	t.Run("the link sets a new password once and ends every session and token", func(t *testing.T) {
		httpapi.ResetThrottles()
		me := api.client(t)
		signedUp := me.signIn(t, h.person(t, "forgot", auth.RoleOwner))
		email := principalField(t, signedUp, "principal", "user", "email").(string)
		userID := principalField(t, signedUp, "principal", "user", "id").(string)
		token := want(t, me.post("/api/v1/tokens", map[string]any{"name": "ci", "scopes": []string{}, "projects": []string{}}), http.StatusCreated, "make a token")
		viaToken := api.client(t)
		viaToken.bearer = obj(t, token, "token")["secret"].(string)
		want(t, viaToken.get("/api/v1/auth/me"), http.StatusOK, "the token works before")

		secret := api.askForReset(t, email)

		var stored []byte
		if err := h.super.QueryRow(context.Background(), `SELECT token_hash FROM password_reset WHERE user_id = $1`, userID).Scan(&stored); err != nil {
			t.Fatalf("read the link: %v", err)
		}
		if !bytes.Equal(stored, auth.HashToken(secret)) || bytes.Contains(stored, []byte(secret)) {
			t.Errorf("the link is not kept as the digest of its secret")
		}

		visitor := api.client(t)
		short := visitor.post("/api/v1/auth/password-reset/confirm", map[string]string{"token": secret, "password": "short"})
		if short.Status != http.StatusUnprocessableEntity {
			t.Fatalf("a short password: got %d, want 422: %s", short.Status, short.Raw)
		}
		reset := want(t, visitor.post("/api/v1/auth/password-reset/confirm", map[string]string{"token": secret, "password": newPassword}), http.StatusNoContent, "set the new password")
		for _, c := range reset.Cookies {
			if c.Name == testCookieName && c.Value != "" {
				t.Errorf("setting the password opened a session")
			}
		}

		if got := me.get("/api/v1/auth/me"); got.Status != http.StatusUnauthorized {
			t.Errorf("the session from before survived the reset: %d", got.Status)
		}
		if got := viaToken.get("/api/v1/auth/me"); got.Status != http.StatusUnauthorized {
			t.Errorf("the API token from before survived the reset: %d", got.Status)
		}
		if got := api.client(t).post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}); got.Status != http.StatusUnauthorized {
			t.Errorf("the old password still signs in: %d", got.Status)
		}
		want(t, api.client(t).post("/api/v1/auth/login", map[string]string{"email": email, "password": newPassword}), http.StatusOK, "the new one does")

		again := api.client(t).post("/api/v1/auth/password-reset/confirm", map[string]string{"token": secret, "password": "and yet another password"})
		if again.Status != http.StatusGone || again.ErrorCode() != "reset_link_invalid" {
			t.Fatalf("a used link: got %d, want 410 reset_link_invalid: %s", again.Status, again.Raw)
		}
		if msg, _ := again.Error()["message"].(string); !strings.Contains(msg, "Ask for a new one from the sign-in page") {
			t.Errorf("a used link says %q, not what to do", msg)
		}
		if n := h.resetRows(t, userID); n != 0 {
			t.Errorf("%d reset links are left after one was used", n)
		}
	})

	t.Run("an expired link is refused and changes nothing", func(t *testing.T) {
		httpapi.ResetThrottles()
		signedUp := api.client(t).signIn(t, h.person(t, "expired", auth.RoleOwner))
		email := principalField(t, signedUp, "principal", "user", "email").(string)
		userID := principalField(t, signedUp, "principal", "user", "id").(string)
		secret := api.askForReset(t, email)
		if _, err := h.super.Exec(context.Background(), `UPDATE password_reset SET expires_at = now() - interval '1 minute' WHERE user_id = $1`, userID); err != nil {
			t.Fatalf("let the link expire: %v", err)
		}
		got := api.client(t).post("/api/v1/auth/password-reset/confirm", map[string]string{"token": secret, "password": newPassword})
		if got.Status != http.StatusGone || got.ErrorCode() != "reset_link_invalid" {
			t.Fatalf("an expired link: got %d, want 410 reset_link_invalid: %s", got.Status, got.Raw)
		}
		want(t, api.client(t).post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusOK, "the old password still signs in")
	})

	t.Run("asking again replaces the link before", func(t *testing.T) {
		httpapi.ResetThrottles()
		signedUp := api.client(t).signIn(t, h.person(t, "twice", auth.RoleOwner))
		email := principalField(t, signedUp, "principal", "user", "email").(string)
		userID := principalField(t, signedUp, "principal", "user", "id").(string)
		first := api.askForReset(t, email)
		second := api.askForReset(t, email)
		if n := h.resetRows(t, userID); n != 1 {
			t.Errorf("%d links wait for one person, want 1", n)
		}
		if got := api.client(t).post("/api/v1/auth/password-reset/confirm", map[string]string{"token": first, "password": newPassword}); got.Status != http.StatusGone {
			t.Errorf("the replaced link: got %d, want 410", got.Status)
		}
		want(t, api.client(t).post("/api/v1/auth/password-reset/confirm", map[string]string{"token": second, "password": newPassword}), http.StatusNoContent, "the latest link")
	})

	t.Run("the request and the reset are recorded once", func(t *testing.T) {
		httpapi.ResetThrottles()
		signedUp := api.client(t).signIn(t, h.person(t, "recorded", auth.RoleOwner))
		email := principalField(t, signedUp, "principal", "user", "email").(string)
		userID := principalField(t, signedUp, "principal", "user", "id").(string)

		secret := api.askForReset(t, email)
		want(t, api.client(t).post("/api/v1/auth/password-reset/confirm", map[string]string{"token": secret, "password": newPassword}), http.StatusNoContent, "reset")

		for _, action := range []string{"auth.password_reset_requested", "auth.password_reset"} {
			var n int
			if err := h.super.QueryRow(context.Background(), `
				SELECT count(*) FROM audit_log
				WHERE action = $1 AND target_id = $2 AND position($3 IN data::text) = 0`, action, userID, secret).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 1 {
				t.Errorf("%s is recorded %d times without the link's secret, want once", action, n)
			}
		}
	})
}

// Asking for a link says nothing about whether the address has an account,
// and cannot be used to fill a mailbox or to guess at links.
func TestAskingForAPasswordLinkTellsNothing(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	const newPassword = "a fresh and adequate password"

	t.Run("an unknown address gets the same answer and no mail", func(t *testing.T) {
		httpapi.ResetThrottles()
		known := principalField(t, api.client(t).signIn(t, h.person(t, "known", auth.RoleOwner)), "principal", "user", "email").(string)
		unknown := h.email(t, "nobody")

		before := api.mailsSoFar()
		forUnknown := api.client(t).post("/api/v1/auth/password-reset", map[string]string{"email": unknown})
		forKnown := api.client(t).post("/api/v1/auth/password-reset", map[string]string{"email": known})
		if forUnknown.Status != forKnown.Status || forUnknown.Raw != forKnown.Raw {
			t.Errorf("the answers differ: unknown %d %s, known %d %s", forUnknown.Status, forUnknown.Raw, forKnown.Status, forKnown.Raw)
		}
		// The known one's mail arriving shows the sends have run; the unknown
		// address never had one started.
		api.awaitMail(t, known, "Set a new password", before)
		if api.mailedTo(unknown, before) {
			t.Errorf("an address with no account was mailed")
		}
	})

	t.Run("a provider account is told to use it, and a customer gets nothing", func(t *testing.T) {
		httpapi.ResetThrottles()
		api.client(t).signIn(t, h.person(t, "sso-org", auth.RoleOwner))
		ctx := context.Background()
		// The installation has one provider; the one it had comes back after.
		h.keepProvider(t)
		if _, err := h.super.Exec(ctx, `DELETE FROM oidc_provider`); err != nil {
			t.Fatalf("clear the provider: %v", err)
		}
		if _, err := h.super.Exec(ctx, `INSERT INTO oidc_provider (issuer, client_id, client_secret) VALUES ('https://idp.test', 'armature', 'secret')`); err != nil {
			t.Fatalf("configure a provider: %v", err)
		}
		member := func(prefix, role string) (string, string) {
			email := h.email(t, prefix)
			var id string
			if err := h.super.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ($1, 'Passwordless') RETURNING id`, email).Scan(&id); err != nil {
				t.Fatalf("make %s: %v", prefix, err)
			}
			if _, err := h.super.Exec(ctx, `INSERT INTO org_member (user_id, org_role) VALUES ($1, $2)`, id, role); err != nil {
				t.Fatalf("add %s: %v", prefix, err)
			}
			return email, id
		}
		ssoEmail, ssoID := member("sso", "member")
		customerEmail, customerID := member("customer", "customer")

		before := api.mailsSoFar()
		want(t, api.client(t).post("/api/v1/auth/password-reset", map[string]string{"email": ssoEmail}), http.StatusAccepted, "a provider account asks")
		want(t, api.client(t).post("/api/v1/auth/password-reset", map[string]string{"email": customerEmail}), http.StatusAccepted, "a customer asks")
		body := api.awaitMail(t, ssoEmail, "How you sign in", before)
		if !strings.Contains(body, "single sign-on") {
			t.Errorf("the provider mail does not say how to sign in: %q", body)
		}
		// Single sign-on asks for no organization, so the mail names none.
		if strings.Contains(body, "organization") {
			t.Errorf("the provider mail asks for an organization: %q", body)
		}
		if strings.Contains(body, "/reset-password#") {
			t.Errorf("a provider account was sent a link")
		}
		if api.mailedTo(customerEmail, before) {
			t.Errorf("a customer without a password was mailed")
		}
		for _, id := range []string{ssoID, customerID} {
			if n := h.resetRows(t, id); n != 0 {
				t.Errorf("an account without a password got %d reset links", n)
			}
		}
	})

	t.Run("asking is braked per mailbox and per caller", func(t *testing.T) {
		httpapi.ResetThrottles()
		target := h.email(t, "flooded")
		for i := 0; i < httpapi.ResetAddressRequestsPerWindow; i++ {
			want(t, api.client(t).post("/api/v1/auth/password-reset", map[string]string{"email": target}), http.StatusAccepted, "ask within the limit")
		}
		if got := api.client(t).post("/api/v1/auth/password-reset", map[string]string{"email": target}); got.Status != http.StatusTooManyRequests {
			t.Errorf("one mailbox past its limit: got %d, want 429: %s", got.Status, got.Raw)
		}

		httpapi.ResetThrottles()
		for i := 0; i < httpapi.ResetRequestsPerWindow; i++ {
			want(t, api.client(t).post("/api/v1/auth/password-reset", map[string]string{"email": h.email(t, "spread")}), http.StatusAccepted, "ask for many mailboxes")
		}
		if got := api.client(t).post("/api/v1/auth/password-reset", map[string]string{"email": h.email(t, "spread")}); got.Status != http.StatusTooManyRequests {
			t.Errorf("one caller past its limit: got %d, want 429: %s", got.Status, got.Raw)
		}
	})

	t.Run("guessing at links is braked", func(t *testing.T) {
		httpapi.ResetThrottles()
		guesser := api.client(t)
		for i := 0; i < httpapi.CredentialTriesPerWindow; i++ {
			want(t, guesser.post("/api/v1/auth/password-reset/confirm", map[string]string{"token": "guess-" + strings.Repeat("x", i), "password": newPassword}), http.StatusGone, "a made up link")
		}
		if got := guesser.post("/api/v1/auth/password-reset/confirm", map[string]string{"token": "one-more", "password": newPassword}); got.Status != http.StatusTooManyRequests {
			t.Errorf("the guess past the limit: got %d, want 429: %s", got.Status, got.Raw)
		}
	})

	t.Run("without mail nobody is told anything about the address", func(t *testing.T) {
		httpapi.ResetThrottles()
		mailer := api.api.Mailer
		api.api.Mailer = nil
		t.Cleanup(func() { api.api.Mailer = mailer })
		known := principalField(t, api.client(t).signIn(t, h.person(t, "unmailed", auth.RoleOwner)), "principal", "user", "email").(string)
		forKnown := api.client(t).post("/api/v1/auth/password-reset", map[string]string{"email": known})
		forUnknown := api.client(t).post("/api/v1/auth/password-reset", map[string]string{"email": h.email(t, "unmailed-nobody")})
		// Each error names its own request, so the rest of it is what must agree.
		if forKnown.Status != http.StatusServiceUnavailable || forKnown.ErrorCode() != "mail_unavailable" ||
			forUnknown.Status != forKnown.Status || forUnknown.Error()["message"] != forKnown.Error()["message"] {
			t.Errorf("without mail: known %d %s, unknown %d %s", forKnown.Status, forKnown.Raw, forUnknown.Status, forUnknown.Raw)
		}
	})
}
