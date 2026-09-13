//go:build integration

package test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/oidc"
	"github.com/armature/armature/backend/internal/perm"
)

// signInWith runs a whole sign-in against the stub provider and returns the
// session it produced.
func signInWith(t *testing.T, h *harness, svc *oidc.Service, ws *workspace, provider *idp, clientID string, claims map[string]any) (*oidc.Session, error) {
	t.Helper()

	target, err := svc.Start(context.Background(), ws.orgID, "/projects")
	if err != nil {
		return nil, err
	}
	state, nonce := stateFrom(t, target)

	// A real provider echoes the nonce it was given into the token it signs.
	echoed := map[string]any{"nonce": nonce}
	for k, v := range claims {
		echoed[k] = v
	}
	provider.says(clientID, echoed)

	orgID, identity, _, err := svc.Exchange(context.Background(), state, "any-code")
	if err != nil {
		return nil, err
	}
	return svc.SignIn(context.Background(), orgID, identity, time.Hour, "test", "")
}

// configured wires a stub provider into an organization and returns the service.
func configured(t *testing.T, h *harness, ws *workspace, provider *idp, createGroups bool) (*oidc.Service, string) {
	t.Helper()
	clientID := "armature-test"
	svc := oidc.NewService(h.cluster, "http://localhost:8080/api/v1/auth/oidc/callback")

	if _, _, err := svc.Save(ws.ctx, ws.orgID, oidc.Provider{
		Issuer: provider.URL, ClientID: clientID, ClientSecret: "shh",
		GroupsClaim: "groups", Scopes: "openid profile email",
		CreateGroups: createGroups, Enabled: true,
	}, ws.actor.UserID); err != nil {
		t.Fatalf("configure the provider: %v", err)
	}
	return svc, clientID
}

func TestSigningInThroughAProvider(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "sso")
	provider := newIDP(t)
	svc, clientID := configured(t, h, ws, provider, false)

	email := h.email(t, "ssouser")
	h.joinExisting(t, ws, email, "member")

	session, err := signInWith(t, h, svc, ws, provider, clientID, map[string]any{
		"email": email, "name": "SSO Person",
	})
	if err != nil {
		t.Fatalf("sign in: %v", err)
	}
	if session.Secret == "" || session.OrgID != ws.orgID {
		t.Errorf("session = %+v, want one in this organization", session)
	}

	// The session works: it authenticates as the person the token described.
	principal, err := h.authService().Authenticate(context.Background(), session.Secret)
	if err != nil {
		t.Fatalf("authenticate the new session: %v", err)
	}
	if principal.User.Email != email {
		t.Errorf("signed in as %s, want %s", principal.User.Email, email)
	}
}

// Authenticating is not the same as being let in.
func TestSomebodyWhoWasNeverInvitedIsRefused(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "uninvited")
	provider := newIDP(t)
	svc, clientID := configured(t, h, ws, provider, false)

	_, err := signInWith(t, h, svc, ws, provider, clientID, map[string]any{
		"email": "stranger@example.test", "name": "A Stranger",
	})
	if !errors.Is(err, oidc.ErrNotAMember) {
		t.Errorf("error = %v, want them refused", err)
	}
}

// The point of reading groups from the token: revoking access at the provider
// has to revoke it here.
func TestGroupsFollowTheProvider(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "claims")
	provider := newIDP(t)
	svc, clientID := configured(t, h, ws, provider, false)
	perms := h.perms()

	email := h.email(t, "claimed")
	userID := h.joinExisting(t, ws, email, "member")

	// A group tied to the value the provider will send, with a role on it.
	group, _, err := perms.CreateGroup(ws.ctx, perm.GroupInput{
		Name: "Platform engineers", ExternalRef: "platform-engineers",
	}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := perms.Grant(ws.ctx, perm.GrantInput{
		Role: perm.ScrumMaster, ProjectKey: ws.project.Key, GroupID: &group.ID,
	}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}

	t.Run("a claim puts somebody in the group and brings the role", func(t *testing.T) {
		if _, err := signInWith(t, h, svc, ws, provider, clientID, map[string]any{
			"email": email, "name": "Claimed", "groups": []any{"platform-engineers"},
		}); err != nil {
			t.Fatal(err)
		}
		if !h.resolve(t, ws, userID, false).Can(perm.SprintManage, ws.project.Key) {
			t.Error("the group named in the claim did not bring its role")
		}
	})

	t.Run("and losing the claim takes it away again", func(t *testing.T) {
		if _, err := signInWith(t, h, svc, ws, provider, clientID, map[string]any{
			"email": email, "name": "Claimed", "groups": []any{},
		}); err != nil {
			t.Fatal(err)
		}
		if h.resolve(t, ws, userID, false).Can(perm.SprintManage, ws.project.Key) {
			t.Error("access survived being removed at the identity provider")
		}
	})

	// A group somebody maintains by hand is none of the provider's business.
	t.Run("a group kept by hand is left alone", func(t *testing.T) {
		local, _, err := perms.CreateGroup(ws.ctx, perm.GroupInput{Name: "On call"}, ws.actor.UserID)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := perms.AddToGroup(ws.ctx, local.ID, userID, ws.actor.UserID); err != nil {
			t.Fatal(err)
		}
		if _, err := signInWith(t, h, svc, ws, provider, clientID, map[string]any{
			"email": email, "name": "Claimed", "groups": []any{},
		}); err != nil {
			t.Fatal(err)
		}
		found, err := perms.GroupByID(ws.ctx, local.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(found.Members) != 1 {
			t.Error("signing in emptied a group the provider does not own")
		}
	})
}

func TestAGroupTheProviderNamesCanBeCreatedOnSight(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "autogroup")
	provider := newIDP(t)
	svc, clientID := configured(t, h, ws, provider, true)

	email := h.email(t, "autojoined")
	h.joinExisting(t, ws, email, "member")

	if _, err := signInWith(t, h, svc, ws, provider, clientID, map[string]any{
		"email": email, "name": "Auto", "groups": []any{"designers"},
	}); err != nil {
		t.Fatal(err)
	}

	groups, err := h.perms().Groups(ws.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, g := range groups {
		if g.ExternalRef == "designers" {
			found = true
			if !g.FromProvider() {
				t.Error("a group the provider made is not marked as its own")
			}
			if g.MemberCount != 1 {
				t.Errorf("members = %d, want the person who was in it", g.MemberCount)
			}
		}
	}
	if !found {
		t.Errorf("groups = %+v, want the one the provider named", groups)
	}
}

// The tokens a verifier has to refuse. Each of these is a way somebody would
// try to sign in as anybody at all.
func TestForgedTokensAreRefused(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "forgery")
	provider := newIDP(t)
	svc, clientID := configured(t, h, ws, provider, false)

	email := h.email(t, "victim")
	h.joinExisting(t, ws, email, "member")
	good := map[string]any{"email": email, "name": "Victim"}

	t.Run("a token signed with somebody else's key", func(t *testing.T) {
		other, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
		provider.signWith = other
		defer func() { provider.signWith = nil }()

		if _, err := signInWith(t, h, svc, ws, provider, clientID, good); err == nil {
			t.Fatal("a token signed with an unknown key was accepted")
		}
	})

	t.Run("an unsigned token", func(t *testing.T) {
		provider.alg = "none"
		defer func() { provider.alg = "" }()

		if _, err := signInWith(t, h, svc, ws, provider, clientID, good); err == nil {
			t.Fatal("an unsigned token was accepted")
		}
	})

	t.Run("a token meant for a different client", func(t *testing.T) {
		if _, err := signInWith(t, h, svc, ws, provider, "somebody-elses-client", good); err == nil {
			t.Fatal("a token addressed to another client was accepted")
		}
	})

	t.Run("a token that has expired", func(t *testing.T) {
		expired := map[string]any{"email": email, "name": "Victim",
			"exp": time.Now().Add(-time.Hour).Unix()}
		if _, err := signInWith(t, h, svc, ws, provider, clientID, expired); err == nil {
			t.Fatal("an expired token was accepted")
		}
	})

	t.Run("a token from a different issuer", func(t *testing.T) {
		wrong := map[string]any{"email": email, "name": "Victim", "iss": "https://evil.test"}
		if _, err := signInWith(t, h, svc, ws, provider, clientID, wrong); err == nil {
			t.Fatal("a token from another issuer was accepted")
		}
	})

	// A token from a real sign-in, replayed against a different one. The nonce
	// is what makes this fail, and nothing else in the token would.
	t.Run("a token belonging to a different sign-in", func(t *testing.T) {
		target, err := svc.Start(context.Background(), ws.orgID, "")
		if err != nil {
			t.Fatal(err)
		}
		state, _ := stateFrom(t, target)

		elsewhere, err := svc.Start(context.Background(), ws.orgID, "")
		if err != nil {
			t.Fatal(err)
		}
		_, otherNonce := stateFrom(t, elsewhere)

		provider.says(clientID, map[string]any{"email": email, "name": "Victim", "nonce": otherNonce})
		if _, _, _, err := svc.Exchange(context.Background(), state, "code"); err == nil {
			t.Fatal("a token from a different sign-in was accepted")
		}
	})

	t.Run("a token with no email to tie it to anybody", func(t *testing.T) {
		if _, err := signInWith(t, h, svc, ws, provider, clientID, map[string]any{"name": "Nobody"}); !errors.Is(err, oidc.ErrNoEmail) {
			t.Errorf("error = %v, want it refused for having no email", err)
		}
	})
}

// A callback whose state this server never issued is what a forged callback
// looks like, and a state is good for exactly one sign-in.
func TestACallbackMustBelongToASignInThatStartedHere(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "state")
	provider := newIDP(t)
	svc, clientID := configured(t, h, ws, provider, false)

	email := h.email(t, "stateful")
	h.joinExisting(t, ws, email, "member")
	provider.says(clientID, map[string]any{"email": email, "name": "Stateful"})

	t.Run("an unknown state is refused", func(t *testing.T) {
		_, _, _, err := svc.Exchange(context.Background(), "never-issued", "any-code")
		if !errors.Is(err, oidc.ErrUnknownLogin) {
			t.Errorf("error = %v, want it refused", err)
		}
	})

	t.Run("and a state is good exactly once", func(t *testing.T) {
		target, err := svc.Start(context.Background(), ws.orgID, "")
		if err != nil {
			t.Fatal(err)
		}
		state, nonce := stateFrom(t, target)
		provider.says(clientID, map[string]any{"email": email, "name": "Stateful", "nonce": nonce})

		if _, _, _, err := svc.Exchange(context.Background(), state, "code"); err != nil {
			t.Fatalf("the first exchange: %v", err)
		}
		if _, _, _, err := svc.Exchange(context.Background(), state, "code"); !errors.Is(err, oidc.ErrUnknownLogin) {
			t.Errorf("error = %v, want the replay refused", err)
		}
	})
}

func TestSignInIsRefusedWhenTheProviderIsOff(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "disabled")
	provider := newIDP(t)
	svc, _ := configured(t, h, ws, provider, false)

	if _, err := svc.Disable(ws.ctx, ws.orgID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(context.Background(), ws.orgID, ""); !errors.Is(err, oidc.ErrNotConfigured) {
		t.Errorf("error = %v, want it refused", err)
	}
}

// The secret is never handed back, and saving without one keeps the stored one.
func TestTheClientSecretIsKeptOutOfSight(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "secret")
	provider := newIDP(t)
	svc, clientID := configured(t, h, ws, provider, false)

	saved, _, err := svc.Save(ws.ctx, ws.orgID, oidc.Provider{
		Issuer: provider.URL, ClientID: clientID, ClientSecret: "", Enabled: true,
	}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.HasSecret {
		t.Error("saving without a secret erased the one that was there")
	}

	// And it is not in the JSON a client would receive.
	encoded, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "shh") {
		t.Errorf("the client secret is in the response: %s", encoded)
	}
}
