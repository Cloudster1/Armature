// Package oidc signs people in through an external identity provider and keeps
// their group membership in step with what that provider says.
//
// The product does not become the source of truth for who somebody is or which
// groups they are in. It reads both from the token, and turns them into the
// roles it already understands.
package oidc

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Provider is one organization's identity provider.
type Provider struct {
	OrgID    uuid.UUID `json:"-"`
	Issuer   string    `json:"issuer"`
	ClientID string    `json:"clientId"`
	// ClientSecret is never sent to a client. Whether one is set is, because a
	// settings page has to be able to say so without revealing it.
	ClientSecret string `json:"-"`
	HasSecret    bool   `json:"hasSecret"`

	// GroupsClaim is the claim listing the groups somebody belongs to.
	// Providers disagree about its name, so it is configuration.
	GroupsClaim string `json:"groupsClaim"`
	Scopes      string `json:"scopes"`
	// CreateGroups makes a group named by the provider appear on first sight.
	// Off by default: an empty group grants nothing, and a group appearing on
	// its own is a surprise in an access settings page.
	CreateGroups bool      `json:"createGroups"`
	Enabled      bool      `json:"enabled"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ScopeList splits the configured scopes, always including openid, without
// which the provider has no reason to return an id token at all.
func (p Provider) ScopeList() []string {
	seen := map[string]bool{"openid": true}
	out := []string{"openid"}
	for _, scope := range strings.Fields(p.Scopes) {
		if seen[scope] {
			continue
		}
		seen[scope] = true
		out = append(out, scope)
	}
	return out
}

// Identity is what a verified token said about somebody.
type Identity struct {
	// Subject is the provider's own identifier. It is stable across an email
	// change, which an email is not.
	Subject string
	Email   string
	Name    string
	// Groups are the values of the configured claim, exactly as sent. They are
	// matched against a group's external reference rather than its name, so
	// renaming a group here does not break the mapping.
	Groups []string
}

var (
	// ErrNotConfigured is returned when an organization has no provider, or has
	// turned the one it has off.
	ErrNotConfigured = errors.New("this organization does not sign in through an identity provider")
	// ErrUnknownLogin is returned for a callback whose state does not match a
	// login this server started, which is what a forged callback looks like.
	ErrUnknownLogin = errors.New("that sign-in did not start here, or it has expired")
	// ErrNoEmail is returned when the token carries no email. Without one there
	// is no way to tie the identity to an invitation or an existing account.
	ErrNoEmail = errors.New("the identity provider did not send an email address")
	// ErrEmailUnverified is returned when the provider says it has not checked
	// the address it sent, so it cannot be matched to an account here.
	ErrEmailUnverified = errors.New("the identity provider has not verified that email address")
	// ErrNotAMember is returned when somebody authenticates correctly but has
	// not been invited. Signing in is not the same as being let in.
	ErrNotAMember = errors.New("you are not a member of that organization")
)

// howLongALoginMayTake bounds the window between the redirect out and the
// callback back. Long enough for somebody to type a password and answer a
// second factor; short enough that a captured link is not useful later.
const howLongALoginMayTake = 15 * time.Minute
