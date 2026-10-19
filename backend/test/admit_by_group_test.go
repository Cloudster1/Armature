//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/oauth"
	"github.com/armature/armature/backend/internal/oidc"
	"github.com/armature/armature/backend/internal/perm"
)

// mappedGroup is a provider group holding a role in the workspace's project,
// and an unmapped one the provider also names that holds nothing.
func mappedGroup(t *testing.T, h *harness, ws *workspace) (mapped, unmapped string) {
	t.Helper()
	perms := h.perms()
	mapped, unmapped = h.name(t, "mapped"), h.name(t, "unmapped")
	group, _, err := perms.CreateGroup(ws.ctx, perm.GroupInput{Name: mapped, ExternalRef: mapped}, ws.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := perms.Grant(ws.ctx, perm.GrantInput{Role: perm.ScrumMaster, ProjectKey: ws.project.Key, GroupID: &group.ID}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := perms.CreateGroup(ws.ctx, perm.GroupInput{Name: unmapped, ExternalRef: unmapped}, ws.actor.UserID); err != nil {
		t.Fatal(err)
	}
	return mapped, unmapped
}

// admitting turns admission by group on or off, keeping the rest as configured.
func admitting(t *testing.T, svc *oidc.Service, ws *workspace, on bool) {
	t.Helper()
	p, err := svc.ProviderFor(ws.ctx)
	if err != nil {
		t.Fatal(err)
	}
	p.AdmitByGroup = on
	if _, _, err := svc.Save(ws.ctx, *p, ws.actor.UserID); err != nil {
		t.Fatalf("admit by group %v: %v", on, err)
	}
}

// standingOf is the account's standing, "" for none, and whether it exists.
func standingOf(t *testing.T, h *harness, email string) (uuid.UUID, string, bool) {
	t.Helper()
	var (
		id       uuid.UUID
		standing *string
	)
	err := h.super.QueryRow(context.Background(), `SELECT id, standing FROM app_user WHERE email = $1`, email).Scan(&id, &standing)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	if standing == nil {
		return id, "", true
	}
	return id, *standing, true
}

func waitingToJoin(t *testing.T, h *harness, userID uuid.UUID) bool {
	t.Helper()
	var waiting bool
	if err := h.super.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM join_request WHERE user_id = $1)`, userID).Scan(&waiting); err != nil {
		t.Fatal(err)
	}
	return waiting
}

// admittedBy is the group the audit log says let the person in, and how.
func admittedBy(t *testing.T, h *harness, userID uuid.UUID) (group, via string) {
	t.Helper()
	err := h.super.QueryRow(context.Background(), `
		SELECT data->>'group', data->>'via' FROM audit_log
		WHERE action = 'member.admitted' AND target_id = $1 AND actor_user_id = $1`, userID).Scan(&group, &via)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return group, via
}

// A group the provider names that holds a role is a way in once an
// administrator says so; anybody else still asks, and nothing changes while
// the setting is off.
func TestAGroupWithARoleLetsSomebodyInAtSignIn(t *testing.T) {
	h := newHarness(t)
	ws := h.newProject(t, "admitsso")
	provider := newIDP(t)
	svc, clientID := configured(t, h, ws, provider, false)
	mapped, unmapped := mappedGroup(t, h, ws)
	signIn := func(email string, groups ...any) error {
		_, err := signInWith(t, h, svc, ws, provider, clientID, map[string]any{"email": email, "name": "Grouped Person", "groups": groups})
		return err
	}

	t.Run("off, a mapped group is no way in", func(t *testing.T) {
		email := h.email(t, "off")
		if err := signIn(email, mapped); !errors.Is(err, oidc.ErrNotAMember) {
			t.Fatalf("error = %v, want them refused", err)
		}
		id, standing, _ := standingOf(t, h, email)
		if standing != "" || !waitingToJoin(t, h, id) {
			t.Errorf("standing %q, waiting %v; want no standing and a join request", standing, waitingToJoin(t, h, id))
		}
	})

	admitting(t, svc, ws, true)

	t.Run("a new person in a mapped group is let in with its role", func(t *testing.T) {
		email := h.email(t, "mapped")
		if err := signIn(email, unmapped, mapped); err != nil {
			t.Fatalf("sign in: %v", err)
		}
		id, standing, _ := standingOf(t, h, email)
		if standing != string(auth.RoleMember) {
			t.Errorf("standing = %q, want member", standing)
		}
		set := h.resolve(t, ws, id, false)
		if !set.Can(perm.SprintManage, ws.project.Key) {
			t.Error("the mapped group's role did not come with the admission")
		}
		if waitingToJoin(t, h, id) {
			t.Error("somebody let in by group is still waiting to join")
		}
		if group, via := admittedBy(t, h, id); group != mapped || via != "sign_in" {
			t.Errorf("the audit log says admitted by %q via %q, want %s at sign-in", group, via, mapped)
		}
	})

	t.Run("somebody already waiting is let in and stops waiting", func(t *testing.T) {
		email := h.email(t, "waiting")
		if err := signIn(email, unmapped); !errors.Is(err, oidc.ErrNotAMember) {
			t.Fatalf("error = %v, want them refused", err)
		}
		id, _, _ := standingOf(t, h, email)
		if !waitingToJoin(t, h, id) {
			t.Fatal("somebody in no mapped group was not given a join request")
		}
		if err := signIn(email, mapped); err != nil {
			t.Fatalf("sign in once in the mapped group: %v", err)
		}
		if _, standing, _ := standingOf(t, h, email); standing != string(auth.RoleMember) || waitingToJoin(t, h, id) {
			t.Errorf("standing %q, waiting %v; want a member no longer waiting", standing, waitingToJoin(t, h, id))
		}
	})

	t.Run("a group with no role is no way in", func(t *testing.T) {
		email := h.email(t, "unmapped")
		if err := signIn(email, unmapped); !errors.Is(err, oidc.ErrNotAMember) {
			t.Fatalf("error = %v, want them refused", err)
		}
		id, standing, _ := standingOf(t, h, email)
		if standing != "" || !waitingToJoin(t, h, id) {
			t.Errorf("standing %q, waiting %v; want a join request only", standing, waitingToJoin(t, h, id))
		}
	})

	t.Run("somebody switched off is refused whatever their groups", func(t *testing.T) {
		email := h.email(t, "switchedoff")
		if _, err := h.super.Exec(context.Background(), `INSERT INTO app_user (email, name, is_active) VALUES ($1, 'Switched Off', false)`, email); err != nil {
			t.Fatal(err)
		}
		if err := signIn(email, mapped); !errors.Is(err, auth.ErrUserInactive) {
			t.Fatalf("error = %v, want them refused as switched off", err)
		}
		if _, standing, _ := standingOf(t, h, email); standing != "" {
			t.Errorf("a switched off account was given standing %q", standing)
		}
	})
}

// A partner's exchange connects somebody who never opened Armature when their
// group holds a role, and leaves anybody else as it did before.
func TestAGroupWithARoleLetsAPartnerConnectSomebodyNew(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	provider := newIDP(t)
	ws := h.newProject(t, "admitexchange")
	_, armatureClient := configured(t, h, ws, provider, false)
	mapped, unmapped := mappedGroup(t, h, ws)
	ada := api.client(t)
	ada.signIn(t, h.person(t, "ada", auth.RoleAdmin))

	setting := func(on bool) {
		t.Helper()
		was := obj(t, want(t, ada.get("/api/v1/oidc-provider"), http.StatusOK, "the provider"), "provider")
		saved := obj(t, want(t, ada.put("/api/v1/oidc-provider", map[string]any{
			"issuer": was["issuer"], "clientId": was["clientId"], "clientSecret": "", "groupsClaim": was["groupsClaim"],
			"scopes": was["scopes"], "createGroups": was["createGroups"], "admitByGroup": on, "enabled": true,
		}), http.StatusOK, "admitting by group"), "provider")
		if saved["admitByGroup"] != on {
			t.Fatalf("admitByGroup = %v, want %v", saved["admitByGroup"], on)
		}
		h.waitForPrimary(t)
	}

	made := want(t, ada.post("/api/v1/oauth/clients", map[string]any{
		"name": h.name(t, "Exciter"), "redirectUris": []string{partnerReturn}, "scopes": []string{"read", "issue.write"},
	}), http.StatusCreated, "a client")
	exciter := partner{id: obj(t, made, "client")["id"].(string), secret: obj(t, made, "client")["secret"].(string), c: api.client(t)}
	want(t, ada.patch("/api/v1/oauth/clients/"+exciter.id, map[string]any{"keycloakClientId": "exciter-kc"}), http.StatusOK, "naming its Keycloak client")
	exchange := func(email string, groups ...any) response {
		token, err := provider.sign(provider.key, map[string]any{
			"iss": provider.URL, "aud": []string{armatureClient}, "azp": "exciter-kc",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "sub": "kc-" + email,
			"email": email, "email_verified": true, "name": "Grouped Person", "groups": groups,
		})
		if err != nil {
			t.Fatal(err)
		}
		return exciter.token(url.Values{"grant_type": {oauth.GrantTokenExchange}, "subject_token": {token},
			"subject_token_type": {oauth.TokenTypeAccess}, "scope": {"read issue.write"}})
	}

	t.Run("off, nobody new is connected", func(t *testing.T) {
		email := h.email(t, "off")
		if resp := exchange(email, mapped); resp.Body["error"] != "invalid_grant" {
			t.Errorf("an exchange for somebody new with the setting off: %d %s", resp.Status, resp.Raw)
		}
		if _, _, exists := standingOf(t, h, email); exists {
			t.Error("an exchange made an account with the setting off")
		}
	})

	setting(true)

	t.Run("a new person in a mapped group is connected at their first sign-in", func(t *testing.T) {
		email := h.email(t, "mapped")
		granted := want(t, exchange(email, mapped), http.StatusOK, "the exchange")
		if granted.Body["refresh_token"] == nil || granted.Body["scope"] != "read issue.write" {
			t.Errorf("the grant = %s", granted.Raw)
		}
		me := want(t, bearer(api, t, granted.Body["access_token"]).get("/api/v1/auth/me"), http.StatusOK, "who it is")
		if principalField(t, me, "principal", "user", "email") != email {
			t.Errorf("the exchange connected %s", me.Raw)
		}
		id, standing, _ := standingOf(t, h, email)
		if standing != string(auth.RoleMember) {
			t.Errorf("standing = %q, want member", standing)
		}
		if !h.resolve(t, ws, id, false).Can(perm.SprintManage, ws.project.Key) {
			t.Error("the mapped group's role did not come with the admission")
		}
		if group, via := admittedBy(t, h, id); group != mapped || via != "token_exchange" {
			t.Errorf("the audit log says admitted by %q via %q, want %s at an exchange", group, via, mapped)
		}
		want(t, exchange(email, mapped), http.StatusOK, "signing in to the partner again")
	})

	t.Run("a group with no role connects nobody and makes no account", func(t *testing.T) {
		email := h.email(t, "unmapped")
		if resp := exchange(email, unmapped); resp.Body["error"] != "invalid_grant" {
			t.Errorf("an exchange for somebody in no mapped group: %d %s", resp.Status, resp.Raw)
		}
		if _, _, exists := standingOf(t, h, email); exists {
			t.Error("an exchange made an account for somebody it did not let in")
		}
	})

	t.Run("somebody switched off is refused whatever their groups", func(t *testing.T) {
		email := h.email(t, "switchedoff")
		if _, err := h.super.Exec(context.Background(), `INSERT INTO app_user (email, name, is_active) VALUES ($1, 'Switched Off', false)`, email); err != nil {
			t.Fatal(err)
		}
		if resp := exchange(email, mapped); resp.Body["error"] != "invalid_grant" {
			t.Errorf("an exchange for a switched off account: %d %s", resp.Status, resp.Raw)
		}
		if _, standing, _ := standingOf(t, h, email); standing != "" {
			t.Errorf("a switched off account was given standing %q", standing)
		}
	})
}
