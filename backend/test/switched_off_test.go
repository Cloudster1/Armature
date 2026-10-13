//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
)

// The sentence somebody with no membership left is refused with at every door.
const switchedOff = "Your account has been switched off. Ask an administrator."

// Nobody is signed in without the installation's organization: somebody with no
// membership left gets no session and no principal, by any door.
func TestSomebodyWithoutAMembershipIsSwitchedOff(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	ctx := context.Background()

	owner := api.client(t)
	owner.signIn(t, h.person(t, "keeps", auth.RoleOwner))
	join := func(prefix string) (*client, string) {
		t.Helper()
		email := h.email(t, prefix)
		invited := want(t, owner.post("/api/v1/invites", map[string]any{"email": email, "role": "member"}), http.StatusCreated, "invite")
		them := api.client(t)
		joined := want(t, them.post("/api/v1/auth/invites/accept", map[string]any{"token": invited.Body["token"], "name": "Let Go", "password": testPassword}), http.StatusOK, "accept")
		if principalField(t, joined, "principal", "org") == nil {
			t.Fatal("an invitation signed somebody in with no organization")
		}
		return them, email
	}
	letGo := func(email string) {
		t.Helper()
		if _, err := h.super.Exec(ctx, `DELETE FROM org_member WHERE user_id = (SELECT id FROM app_user WHERE email = $1)`, email); err != nil {
			t.Fatal(err)
		}
	}
	refused := func(t *testing.T, r response, door string) {
		t.Helper()
		if r.Status != http.StatusForbidden || r.ErrorCode() != "switched_off" || r.Error()["message"] != switchedOff {
			t.Fatalf("%s: got %d %s, want 403 switched_off saying %q", door, r.Status, r.Raw, switchedOff)
		}
	}

	t.Run("an open session ends with the membership", func(t *testing.T) {
		member, email := join("open")
		want(t, member.get("/api/v1/auth/me"), http.StatusOK, "signed in")
		letGo(email)
		if me := member.get("/api/v1/auth/me"); me.Status != http.StatusUnauthorized {
			t.Fatalf("a session with no organization behind it answered %d %s", me.Status, me.Raw)
		}
	})

	t.Run("a password sign-in is refused", func(t *testing.T) {
		_, email := join("password")
		letGo(email)
		sessions := func() int {
			t.Helper()
			var n int
			if err := h.super.QueryRow(ctx, `SELECT count(*) FROM user_session s JOIN app_user u ON u.id = s.user_id WHERE u.email = $1`, email).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		}
		before := sessions()
		visitor := api.client(t)
		refused(t, visitor.post("/api/v1/auth/login", map[string]string{"email": email, "password": testPassword}), "password sign-in")
		if opened := sessions() - before; opened != 0 {
			t.Errorf("%d sessions were opened for somebody with no membership", opened)
		}
	})

	t.Run("a sign-in finished with a second factor is refused", func(t *testing.T) {
		member, email := join("twostep")
		factor := enrol(t, member)
		letGo(email)
		visitor := api.client(t)
		challenge := challengeFor(t, visitor, email)
		refused(t, visitor.post("/api/v1/auth/login/second-factor", map[string]string{"challenge": challenge, "code": factor.next(t)}), "the code step")
	})

	t.Run("an API token is refused", func(t *testing.T) {
		member, email := join("token")
		made := want(t, member.post("/api/v1/tokens", map[string]any{"name": "script"}), http.StatusCreated, "make a token")
		script := api.client(t)
		script.bearer = principalField(t, made, "token", "secret").(string)
		want(t, script.get("/api/v1/auth/me"), http.StatusOK, "the token works")
		letGo(email)
		refused(t, script.get("/api/v1/auth/me"), "API token")
	})
}
