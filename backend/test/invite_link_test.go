//go:build integration

package test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/httpapi"
)

func TestInvitationLinks(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	owner.signup(t, h, "linkowner")

	t.Run("an invitation is mailed with a link that opens the invitation page", func(t *testing.T) {
		invitee := h.email(t, "linked")
		created := owner.post("/api/v1/invites", map[string]string{"email": invitee, "role": "member"})
		if created.Status != http.StatusCreated {
			t.Fatalf("invite returned %d: %s", created.Status, created.Raw)
		}
		token, _ := created.Body["token"].(string)
		link, _ := created.Body["link"].(string)
		if want := "http://app.test/invite#" + token; link != want {
			t.Errorf("link = %q, want %q", link, want)
		}
		if created.Body["mailed"] != true {
			t.Errorf("mailed = %v, want true with a mailer configured", created.Body["mailed"])
		}

		var body string
		api.mailer.mu.Lock()
		for _, sent := range api.mailer.sent {
			if sent.To == invitee {
				body = sent.Body
			}
		}
		api.mailer.mu.Unlock()
		if !strings.Contains(body, link) {
			t.Errorf("no mail to %s carried the link; got body %q", invitee, body)
		}
	})

	t.Run("the link says where it leads before anybody accepts, and not after", func(t *testing.T) {
		invitee := h.email(t, "previewed")
		created := owner.post("/api/v1/invites", map[string]string{"email": invitee, "role": "admin"})
		token, _ := created.Body["token"].(string)

		stranger := api.client(t)
		preview := stranger.post("/api/v1/auth/invites/preview", map[string]string{"token": token})
		if preview.Status != http.StatusOK {
			t.Fatalf("preview returned %d: %s", preview.Status, preview.Raw)
		}
		invite, _ := preview.Body["invite"].(map[string]any)
		if invite["email"] != invitee || invite["role"] != "admin" || invite["orgName"] != "linkowner Company" {
			t.Errorf("preview = %v, want the address, the role and the organization's name", invite)
		}

		accepted := stranger.post("/api/v1/auth/invites/accept", map[string]string{"token": token, "name": "Previewed Person", "password": testPassword})
		if accepted.Status != http.StatusOK {
			t.Fatalf("accept returned %d: %s", accepted.Status, accepted.Raw)
		}
		if again := api.client(t).post("/api/v1/auth/invites/preview", map[string]string{"token": token}); again.ErrorCode() != "invite_invalid" {
			t.Errorf("preview of a used invitation = %d %s, want invite_invalid", again.Status, again.Raw)
		}
	})

	t.Run("a made up link tells nothing", func(t *testing.T) {
		made := api.client(t).post("/api/v1/auth/invites/preview", map[string]string{"token": "not-a-real-invitation"})
		if made.Status != http.StatusGone || made.ErrorCode() != "invite_invalid" {
			t.Errorf("preview of an unknown token = %d %s, want 410 invite_invalid", made.Status, made.Raw)
		}
	})

	t.Run("guessing at links is braked", func(t *testing.T) {
		httpapi.ResetThrottles()
		guesser := api.client(t)
		for i := 0; i < httpapi.CredentialTriesPerWindow; i++ {
			want(t, guesser.post("/api/v1/auth/invites/preview", map[string]string{"token": "guess-" + strings.Repeat("x", i)}), http.StatusGone, "a made up link")
		}
		if braked := guesser.post("/api/v1/auth/invites/preview", map[string]string{"token": "one-more-guess"}); braked.Status != http.StatusTooManyRequests {
			t.Errorf("the guess past the limit answered %d: %s", braked.Status, braked.Raw)
		}
	})

	// A real link opened before signing in is refused with "sign in first",
	// which is the page working as meant, not somebody guessing.
	t.Run("a valid link that asks its owner to sign in is not counted as a guess", func(t *testing.T) {
		httpapi.ResetThrottles()
		existing := api.client(t)
		address := principalField(t, existing.signup(t, h, "hasaccount"), "principal", "user", "email").(string)
		token := owner.post("/api/v1/invites", map[string]string{"email": address, "role": "member"}).Body["token"].(string)

		visitor := api.client(t)
		for i := 0; i <= httpapi.CredentialTriesPerWindow; i++ {
			got := visitor.post("/api/v1/auth/invites/accept", map[string]string{"token": token, "name": "Somebody", "password": testPassword})
			if got.ErrorCode() != "sign_in_to_accept" {
				t.Fatalf("attempt %d answered %d %s, want sign_in_to_accept every time", i+1, got.Status, got.Raw)
			}
		}
	})

	t.Run("without mail the link is still handed back", func(t *testing.T) {
		mailer := api.api.Mailer
		api.api.Mailer = nil
		t.Cleanup(func() { api.api.Mailer = mailer })

		created := owner.post("/api/v1/invites", map[string]string{"email": h.email(t, "unmailed"), "role": "member"})
		if created.Status != http.StatusCreated {
			t.Fatalf("invite returned %d: %s", created.Status, created.Raw)
		}
		if created.Body["mailed"] != false || !strings.HasPrefix(created.Body["link"].(string), "http://app.test/invite#") {
			t.Errorf("without mail: mailed = %v, link = %v", created.Body["mailed"], created.Body["link"])
		}
	})
}
