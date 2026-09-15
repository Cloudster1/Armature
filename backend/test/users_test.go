//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// An administrator makes the organization's own accounts, resets them and
// switches them off; the tenant wall holds for accounts that belong elsewhere.
func TestAdministratorsManageLocalUsers(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	signedUp := owner.signup(t, h, "users-owner")
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
		// An administrator holds the organization; a member does not.
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
		if refused.Status != http.StatusForbidden || !strings.Contains(refused.Raw, "deactivated") {
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

	t.Run("somebody who also belongs elsewhere is not this organization's to change", func(t *testing.T) {
		elsewhere := api.client(t)
		theirs := elsewhere.signup(t, h, "elsewhere")
		theirEmail := principalField(t, theirs, "principal", "user", "email").(string)
		theirID := principalField(t, theirs, "principal", "user", "id").(string)

		refused := owner.post("/api/v1/users", map[string]string{"email": theirEmail, "name": "Taken", "role": "member", "password": testPassword})
		if refused.Status != http.StatusConflict || !strings.Contains(refused.Raw, "Invite them") {
			t.Fatalf("an address with an account elsewhere was not refused: %d %s", refused.Status, refused.Raw)
		}

		invite := want(t, owner.post("/api/v1/invites", map[string]string{"email": theirEmail, "role": "member"}), http.StatusCreated, "invite them")
		want(t, elsewhere.post("/api/v1/auth/invites/accept", map[string]string{"token": invite.Body["token"].(string)}), http.StatusOK, "they accept, signed in")
		h.waitForPrimary(t)

		listed := want(t, owner.get("/api/v1/users"), http.StatusOK, "list")
		for _, row := range list(t, listed, "users") {
			u := row.(map[string]any)
			if u["id"] == theirID && u["managed"] != false {
				t.Errorf("somebody with another organization is offered as managed: %v", u)
			}
		}
		if got := owner.patch("/api/v1/users/"+theirID, map[string]any{"name": "Ours now"}); got.Status != http.StatusConflict {
			t.Errorf("renaming somebody who belongs elsewhere: got %d, want 409: %s", got.Status, got.Raw)
		}
		if got := owner.put("/api/v1/users/"+theirID+"/password", map[string]string{"password": testPassword}); got.Status != http.StatusConflict {
			t.Errorf("resetting somebody who belongs elsewhere: got %d, want 409: %s", got.Status, got.Raw)
		}
		if got := owner.patch("/api/v1/users/"+theirID, map[string]any{"isActive": false}); got.Status != http.StatusConflict {
			t.Errorf("switching off somebody who belongs elsewhere: got %d, want 409: %s", got.Status, got.Raw)
		}
	})

	t.Run("an account the provider signs in gets no password here", func(t *testing.T) {
		orgID := principalField(t, signedUp, "principal", "org", "id").(string)
		ctx := context.Background()
		if _, err := h.super.Exec(ctx, `
			INSERT INTO oidc_provider (org_id, issuer, client_id, client_secret)
			VALUES ($1, 'https://idp.test', 'armature', 'secret')`, orgID); err != nil {
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
		// Renaming them is still this organization's, as is switching them off.
		want(t, owner.patch("/api/v1/users/"+ssoID, map[string]any{"name": "Provider Renamed"}), http.StatusOK, "rename a provider account")
	})

	t.Run("the last owner cannot be switched off, and SQL refuses it too", func(t *testing.T) {
		second := api.client(t)
		secondUp := second.signup(t, h, "sole")
		secondID := principalField(t, secondUp, "principal", "user", "id").(string)
		// Through the service: the owner of another organization, put here as
		// an administrator by hand, is still the sole owner over there.
		var msg string
		err := h.super.QueryRow(context.Background(), `UPDATE app_user SET is_active = false WHERE id = $1 RETURNING name`, secondID).Scan(&msg)
		if err == nil || !strings.Contains(err.Error(), "cannot be switched off") {
			t.Fatalf("SQL switched off a sole owner: err = %v", err)
		}
		// A second active owner frees the first.
		var otherOwner string
		if err := h.super.QueryRow(context.Background(), `INSERT INTO app_user (email, name) VALUES ($1, 'Other Owner') RETURNING id`, h.email(t, "other-owner")).Scan(&otherOwner); err != nil {
			t.Fatal(err)
		}
		orgID := principalField(t, secondUp, "principal", "org", "id").(string)
		if _, err := h.super.Exec(context.Background(), `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'owner')`, orgID, otherOwner); err != nil {
			t.Fatal(err)
		}
		if _, err := h.super.Exec(context.Background(), `UPDATE app_user SET is_active = false WHERE id = $1`, secondID); err != nil {
			t.Fatalf("with another owner the switch is allowed: %v", err)
		}
	})
}

// A person changes their own password by proving the one they have, and every
// other session of theirs ends.
func TestAPersonChangesTheirOwnPassword(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	me := api.client(t)
	signedUp := me.signup(t, h, "own-password")
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
