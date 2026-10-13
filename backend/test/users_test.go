//go:build integration

package test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/auth"
)

// An administrator makes accounts, resets them and switches them off: every
// account of the installation, whatever else it belongs to.
func TestAdministratorsManageLocalUsers(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	signedUp := owner.signIn(t, h.person(t, "users-owner", auth.RoleOwner))
	ownerID := principalField(t, signedUp, "principal", "user", "id").(string)

	email := h.email(t, "local")
	made := want(t, owner.post("/api/v1/users", map[string]string{
		"email": email, "name": "Local Person", "role": "member", "password": testPassword,
	}), http.StatusCreated, "create a local user")
	user := obj(t, made, "user")
	userID := user["id"].(string)
	if user["signsInWith"] != "password" || user["managed"] != true || user["isActive"] != true {
		t.Fatalf("a made account is a managed password account: %s", made.Raw)
	}

	t.Run("the person signs in with the password they were given", func(t *testing.T) {
		them := api.client(t)
		want(t, them.post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusOK, "sign in")
		want(t, them.get("/api/v1/auth/me"), http.StatusOK, "read themselves")
	})

	t.Run("the list shows them and what may be done", func(t *testing.T) {
		h.waitForPrimary(t)
		listed := want(t, owner.get("/api/v1/users"), http.StatusOK, "list users")
		found := false
		for _, row := range list(t, listed, "users") {
			u := row.(map[string]any)
			if u["id"] == userID {
				found = true
			}
			if u["id"] == ownerID && u["role"] != "owner" {
				t.Errorf("the owner is listed as %v", u["role"])
			}
		}
		if !found {
			t.Fatalf("the made account is not listed: %s", listed.Raw)
		}
	})

	t.Run("renaming and changing standing land", func(t *testing.T) {
		renamed := want(t, owner.patch("/api/v1/users/"+userID, map[string]any{"name": "Renamed Person", "role": "admin"}), http.StatusOK, "rename")
		u := obj(t, renamed, "user")
		if u["name"] != "Renamed Person" || u["role"] != "admin" {
			t.Fatalf("rename and role change did not land: %s", renamed.Raw)
		}
		// An administrator administers the installation; a member does not.
		h.waitForPrimary(t)
		them := api.client(t)
		want(t, them.post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusOK, "sign in as the administrator")
		want(t, them.get("/api/v1/invites"), http.StatusOK, "the new administrator administers")
		want(t, owner.patch("/api/v1/users/"+userID, map[string]any{"role": "member"}), http.StatusOK, "demote")
		h.waitForPrimary(t)
		if got := them.get("/api/v1/invites"); got.Status != http.StatusForbidden {
			t.Fatalf("a demoted administrator still administers: %d %s", got.Status, got.Raw)
		}
	})

	t.Run("a new password ends every session and the old one stops working", func(t *testing.T) {
		before := api.client(t)
		want(t, before.post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusOK, "sign in before the reset")
		want(t, owner.put("/api/v1/users/"+userID+"/password", map[string]string{"password": "a different adequate password"}), http.StatusNoContent, "set a password")
		if got := before.get("/api/v1/auth/me"); got.Status != http.StatusUnauthorized {
			t.Fatalf("the old session survived the reset: %d", got.Status)
		}
		if got := api.client(t).post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}); got.Status != http.StatusUnauthorized {
			t.Fatalf("the old password still signs in: %d", got.Status)
		}
		want(t, api.client(t).post("/api/v1/auth/login", map[string]string{"email": email, "password": "a different adequate password"}), http.StatusOK, "the new password signs in")
	})

	t.Run("switching off refuses the door and the open session, and switching on lets them back", func(t *testing.T) {
		open := api.client(t)
		want(t, open.post("/api/v1/auth/login", map[string]string{"email": email, "password": "a different adequate password"}), http.StatusOK, "sign in")
		off := want(t, owner.patch("/api/v1/users/"+userID, map[string]any{"isActive": false}), http.StatusOK, "deactivate")
		if obj(t, off, "user")["isActive"] != false {
			t.Fatalf("still active: %s", off.Raw)
		}
		if got := open.get("/api/v1/auth/me"); got.Status != http.StatusUnauthorized {
			t.Fatalf("the open session survived deactivation: %d", got.Status)
		}
		refused := api.client(t).post("/api/v1/auth/login", map[string]string{"email": email, "password": "a different adequate password"})
		if refused.Status != http.StatusForbidden || refused.ErrorCode() != "switched_off" || !strings.Contains(refused.Raw, "Ask an administrator") {
			t.Fatalf("a switched off account signed in: %d %s", refused.Status, refused.Raw)
		}
		want(t, owner.patch("/api/v1/users/"+userID, map[string]any{"isActive": true}), http.StatusOK, "reactivate")
		want(t, api.client(t).post("/api/v1/auth/login", map[string]string{"email": email, "password": "a different adequate password"}), http.StatusOK, "sign in again")
	})

	t.Run("what is refused", func(t *testing.T) {
		refuse := func(what string, got response, status int) {
			t.Helper()
			if got.Status != status {
				t.Errorf("%s: got %d, want %d: %s", what, got.Status, status, got.Raw)
			}
		}
		refuse("a short password", owner.post("/api/v1/users", map[string]string{
			"email": h.email(t, "short"), "name": "Short", "role": "member", "password": "short"}), http.StatusUnprocessableEntity)
		refuse("an owner made outright", owner.post("/api/v1/users", map[string]string{
			"email": h.email(t, "crown"), "name": "Crown", "role": "owner", "password": testPassword}), http.StatusUnprocessableEntity)
		refuse("something that is not an address", owner.post("/api/v1/users", map[string]string{
			"email": "nobody", "name": "Nobody", "role": "member", "password": testPassword}), http.StatusUnprocessableEntity)
		refuse("the address of the account that exists", owner.post("/api/v1/users", map[string]string{
			"email": email, "name": "Again", "role": "member", "password": testPassword}), http.StatusConflict)
		refuse("acting on yourself", owner.patch("/api/v1/users/"+ownerID, map[string]any{"name": "Me"}), http.StatusConflict)
		refuse("your own password from here", owner.put("/api/v1/users/"+ownerID+"/password", map[string]string{"password": testPassword}), http.StatusConflict)
		refuse("an owner's standing", owner.patch("/api/v1/users/"+ownerID, map[string]any{"role": "member"}), http.StatusConflict)
		refuse("a stranger", owner.patch("/api/v1/users/00000000-0000-0000-0000-000000000001", map[string]any{"name": "Ghost"}), http.StatusNotFound)
		refuse("a member's key", api.client(t).get("/api/v1/users"), http.StatusUnauthorized)
	})

	t.Run("an administrator manages anybody here", func(t *testing.T) {
		theirEmail := h.email(t, "twofold")
		made := want(t, owner.post("/api/v1/users", map[string]string{
			"email": theirEmail, "name": "Twofold", "role": "member", "password": testPassword,
		}), http.StatusCreated, "make them")
		theirID := obj(t, made, "user")["id"].(string)
		h.waitForPrimary(t)

		listed := want(t, owner.get("/api/v1/users"), http.StatusOK, "list")
		for _, row := range list(t, listed, "users") {
			if u := row.(map[string]any); u["id"] == theirID && u["managed"] != true {
				t.Errorf("a member is not offered as managed: %v", u)
			}
		}
		want(t, owner.patch("/api/v1/users/"+theirID, map[string]any{"name": "Ours too"}), http.StatusOK, "rename them")
		want(t, owner.put("/api/v1/users/"+theirID+"/password", map[string]string{"password": "their replaced adequate password"}), http.StatusNoContent, "reset them")
		want(t, owner.patch("/api/v1/users/"+theirID, map[string]any{"isActive": false}), http.StatusOK, "switch them off")
		want(t, owner.patch("/api/v1/users/"+theirID, map[string]any{"isActive": true}), http.StatusOK, "and on")

		taken := owner.post("/api/v1/users", map[string]string{"email": theirEmail, "name": "Taken", "role": "member", "password": testPassword})
		if taken.Status != http.StatusConflict || strings.Contains(taken.Raw, "Invite them") {
			t.Errorf("an address with an account: %d %s, want 409 pointing at the list rather than an invitation", taken.Status, taken.Raw)
		}
	})
}

// An account the provider signs in gets no password here. The provider is the
// installation's one, so this runs alone and leaves the one it found.
func TestAnAccountTheProviderSignsInGetsNoPasswordHere(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	orgID := principalField(t, owner.signIn(t, h.person(t, "sso-owner", auth.RoleOwner)), "principal", "org", "id").(string)
	ctx := context.Background()

	h.keepProvider(t)
	if _, err := h.super.Exec(ctx, `
		INSERT INTO oidc_provider (org_id, issuer, client_id, client_secret)
		VALUES ($1, 'https://idp.test', 'armature', 'secret')
		ON CONFLICT (org_id) DO NOTHING`, orgID); err != nil {
		t.Fatalf("configure a provider: %v", err)
	}
	var ssoID string
	if err := h.super.QueryRow(ctx, `INSERT INTO app_user (email, name) VALUES ($1, 'Provider Person') RETURNING id`, h.email(t, "sso")).Scan(&ssoID); err != nil {
		t.Fatalf("make a provider account: %v", err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'member')`, orgID, ssoID); err != nil {
		t.Fatalf("add them: %v", err)
	}
	h.waitForPrimary(t)
	listed := want(t, owner.get("/api/v1/users"), http.StatusOK, "list")
	for _, row := range list(t, listed, "users") {
		u := row.(map[string]any)
		if u["id"] == ssoID && u["signsInWith"] != "provider" {
			t.Errorf("a passwordless account under a provider signs in with %v", u["signsInWith"])
		}
	}
	if got := owner.put("/api/v1/users/"+ssoID+"/password", map[string]string{"password": testPassword}); got.Status != http.StatusConflict {
		t.Errorf("a provider account was given a password: %d %s", got.Status, got.Raw)
	}
	// Renaming them is still the installation's, as is switching them off.
	want(t, owner.patch("/api/v1/users/"+ssoID, map[string]any{"name": "Provider Renamed"}), http.StatusOK, "rename a provider account")
}

// The last active administrator stays on. Every installation the suite shares
// has many, so this one is a database of its own, set up by its first account.
func TestTheLastActiveAdministratorCannotBeSwitchedOff(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	const dbName = "armature_last_administrator_switch_test"
	api, empty := emptyInstallation(t, h, dbName)
	api.api.Auth = empty.authService().Signups(auth.SignupFirst)
	ctx := context.Background()
	super, err := pgx.Connect(ctx, withDatabase(t, os.Getenv(envSuperuser), dbName, "", ""))
	if err != nil {
		t.Fatalf("connect superuser to %s: %v", dbName, err)
	}
	t.Cleanup(func() { _ = super.Close(context.Background()) })

	soleUp := want(t, api.client(t).post("/api/v1/auth/signup", map[string]string{"email": h.email(t, "sole"), "password": testPassword, "name": "Sole"}),
		http.StatusCreated, "the first account")
	soleID := principalField(t, soleUp, "principal", "user", "id").(string)
	orgID := principalField(t, soleUp, "principal", "org", "id").(string)
	switchOff := func(id string) error {
		_, err := super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE id = $1`, id)
		return err
	}
	join := func(email, role string) string {
		t.Helper()
		var id string
		if err := super.QueryRow(ctx, `INSERT INTO app_user (email, name, password_hash) VALUES ($1, $2, $3) RETURNING id`, email, role, shared.passwordHash).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := super.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, $3)`, orgID, id, role); err != nil {
			t.Fatal(err)
		}
		return id
	}

	// An owner alone is the last administrator.
	if err := switchOff(soleID); err == nil || !strings.Contains(err.Error(), "cannot be switched off") {
		t.Fatalf("SQL switched off the only administrator: err = %v", err)
	}
	// An active administrator beside them frees the owner, who is no longer special.
	adminID := join(h.email(t, "beside"), "admin")
	if err := switchOff(soleID); err != nil {
		t.Fatalf("with another active administrator the owner can be switched off: %v", err)
	}
	if err := switchOff(adminID); err == nil || !strings.Contains(err.Error(), "the last active administrator") {
		t.Fatalf("SQL switched off the last active administrator: err = %v", err)
	}

	// The service refuses first, in words: asked by somebody granted the
	// administrator's role without the standing.
	grantedEmail := h.email(t, "granted")
	grantedID := join(grantedEmail, "member")
	if _, err := super.Exec(ctx, `INSERT INTO role_assignment (org_id, role, user_id) VALUES ($1, 'global_administrator', $2)`, orgID, grantedID); err != nil {
		t.Fatal(err)
	}
	granted := api.client(t)
	want(t, granted.post("/api/v1/auth/login", map[string]string{"email": grantedEmail, "password": testPassword}), http.StatusOK, "the granted one signs in")
	refused := granted.patch("/api/v1/users/"+adminID, map[string]any{"isActive": false})
	if refused.Status != http.StatusConflict || refused.ErrorCode() != "last_administrator" || !strings.Contains(refused.Raw, "Make somebody else an administrator first") {
		t.Fatalf("switching off the last active administrator = %d %s, want 409 last_administrator", refused.Status, refused.Raw)
	}
}

// A person changes their own password by proving the one they have, and every
// other session of theirs ends.
func TestAPersonChangesTheirOwnPassword(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)

	me := api.client(t)
	signedUp := me.signIn(t, h.person(t, "own-password", auth.RoleOwner))
	email := principalField(t, signedUp, "principal", "user", "email").(string)

	other := api.client(t)
	want(t, other.post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), http.StatusOK, "a second session")

	wrong := me.put("/api/v1/auth/me/password", map[string]string{"currentPassword": "not it at all really", "newPassword": "a brand new adequate password"})
	if wrong.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a wrong current password: got %d, want 422: %s", wrong.Status, wrong.Raw)
	}
	short := me.put("/api/v1/auth/me/password", map[string]string{"currentPassword": testPassword, "newPassword": "short"})
	if short.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a short new password: got %d, want 422: %s", short.Status, short.Raw)
	}
	want(t, me.put("/api/v1/auth/me/password", map[string]string{"currentPassword": testPassword, "newPassword": "a brand new adequate password"}), http.StatusNoContent, "change it")

	want(t, me.get("/api/v1/auth/me"), http.StatusOK, "the session that asked stays")
	if got := other.get("/api/v1/auth/me"); got.Status != http.StatusUnauthorized {
		t.Fatalf("the other session survived: %d", got.Status)
	}
	if got := api.client(t).post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}); got.Status != http.StatusUnauthorized {
		t.Fatalf("the old password still signs in: %d", got.Status)
	}
	want(t, api.client(t).post("/api/v1/auth/login", map[string]string{"email": email, "password": "a brand new adequate password"}), http.StatusOK, "the new one does")

	// A token cannot change the password of the person it belongs to.
	token := want(t, me.post("/api/v1/tokens", map[string]any{"name": "ci", "scopes": []string{}, "projects": []string{}}), http.StatusCreated, "make a token")
	viaToken := api.client(t)
	viaToken.bearer = obj(t, token, "token")["secret"].(string)
	if got := viaToken.put("/api/v1/auth/me/password", map[string]string{"currentPassword": "a brand new adequate password", "newPassword": "yet another adequate password"}); got.Status != http.StatusForbidden {
		t.Fatalf("a token changed a password: %d %s", got.Status, got.Raw)
	}
}
