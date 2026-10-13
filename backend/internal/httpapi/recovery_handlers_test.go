package httpapi

import (
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
)

// A provider account must not be handed a link, and a link must lead to the
// page that reads its secret from after the #.
func TestAPasswordMailCarriesALinkOrTheWayIn(t *testing.T) {
	s := &Server{AppBaseURL: "https://tracker.example"}

	link := s.passwordResetMail(&auth.ResetRequest{Email: "ada@example.com", Secret: "s3cret"})
	if link.To != "ada@example.com" || !strings.Contains(link.Body, "https://tracker.example/reset-password#s3cret") {
		t.Errorf("the link mail = %+v", link)
	}

	pointer := s.passwordResetMail(&auth.ResetRequest{Email: "ada@example.com", Providers: []string{"acme", "globex"}})
	if strings.Contains(pointer.Body, "/reset-password") {
		t.Errorf("a provider account was sent a link: %q", pointer.Body)
	}
	if !strings.Contains(pointer.Body, "single sign-on") {
		t.Errorf("the provider mail does not say how to sign in: %q", pointer.Body)
	}
	// Single sign-on asks for nothing, so the mail names no organization to give it.
	if strings.Contains(pointer.Body, "acme") || strings.Contains(pointer.Body, "organization") {
		t.Errorf("the provider mail asks for an organization: %q", pointer.Body)
	}
}
