//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/oidc"
)

// Somebody the provider vouches for is still refused, but the refusal is kept
// where an administrator can turn it into a membership with one click.
func TestAStrangerAtTheProviderCanBeLetInFromTheUsersPage(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	ws := h.newWorkspace(t, "letin")
	provider := newIDP(t)
	svc, clientID := configured(t, h, ws, provider, false)

	owner := api.client(t)
	base, _ := url.Parse(api.URL)
	owner.http.Jar.SetCookies(base, []*http.Cookie{{Name: testCookieName, Value: ws.owner.SessionSecret}})

	email := h.email(t, "stranger")
	claims := map[string]any{"email": email, "name": "A Stranger"}
	if _, err := signInWith(t, h, svc, ws, provider, clientID, claims); !errors.Is(err, oidc.ErrNotAMember) {
		t.Fatalf("error = %v, want them refused", err)
	}
	// Asking twice is one request.
	if _, err := signInWith(t, h, svc, ws, provider, clientID, claims); !errors.Is(err, oidc.ErrNotAMember) {
		t.Fatalf("second error = %v, want them refused", err)
	}

	h.waitForPrimary(t)
	listed := want(t, owner.get("/api/v1/users/requests"), http.StatusOK, "list requests")
	requests := list(t, listed, "requests")
	if len(requests) != 1 {
		t.Fatalf("requests = %s, want the one stranger", listed.Raw)
	}
	request := requests[0].(map[string]any)
	if request["email"] != email || request["name"] != "A Stranger" {
		t.Fatalf("request = %v", request)
	}
	userID := request["userId"].(string)

	// A member may not see or decide this.
	member := api.client(t)
	memberEmail := h.email(t, "plain")
	want(t, owner.post("/api/v1/users", map[string]string{"email": memberEmail, "name": "Plain Member", "role": "member", "password": testPassword}), http.StatusCreated, "make a member")
	want(t, member.post("/api/v1/auth/login", map[string]string{"email": memberEmail, "password": testPassword}), http.StatusOK, "member signs in")
	want(t, member.get("/api/v1/users/requests"), http.StatusForbidden, "member lists requests")
	want(t, member.post("/api/v1/users/requests/"+userID+"/admit", map[string]string{"role": "member"}), http.StatusForbidden, "member admits")
	want(t, member.delete("/api/v1/users/requests/"+userID), http.StatusForbidden, "member declines")

	t.Run("turned away, they may ask again", func(t *testing.T) {
		want(t, owner.delete("/api/v1/users/requests/"+userID), http.StatusNoContent, "decline")
		want(t, owner.delete("/api/v1/users/requests/"+userID), http.StatusNotFound, "decline twice")
		h.waitForPrimary(t)
		if got := list(t, want(t, owner.get("/api/v1/users/requests"), http.StatusOK, "list"), "requests"); len(got) != 0 {
			t.Fatalf("a declined request is still listed: %v", got)
		}
		if _, err := signInWith(t, h, svc, ws, provider, clientID, claims); !errors.Is(err, oidc.ErrNotAMember) {
			t.Fatalf("error = %v, want them refused", err)
		}
	})

	t.Run("let in, their next sign-in succeeds", func(t *testing.T) {
		h.waitForPrimary(t)
		if got := owner.post("/api/v1/users/requests/"+userID+"/admit", map[string]string{"role": "owner"}); got.Status != http.StatusUnprocessableEntity {
			t.Fatalf("admitted as owner: %d %s", got.Status, got.Raw)
		}
		admitted := want(t, owner.post("/api/v1/users/requests/"+userID+"/admit", map[string]string{"role": "member"}), http.StatusOK, "admit")
		user := obj(t, admitted, "user")
		if user["role"] != "member" || user["signsInWith"] != "provider" {
			t.Fatalf("admitted user = %s", admitted.Raw)
		}
		want(t, owner.post("/api/v1/users/requests/"+userID+"/admit", map[string]string{"role": "member"}), http.StatusNotFound, "admit twice")

		session, err := signInWith(t, h, svc, ws, provider, clientID, claims)
		if err != nil {
			t.Fatalf("sign in after being let in: %v", err)
		}
		if session.OrgID != ws.orgID {
			t.Fatalf("session in %s, want %s", session.OrgID, ws.orgID)
		}
	})
}

// The requests are tenant rows like any other: one organization's
// administrators never see, nor decide, who is waiting at another's door.
// Tried straight through SQL, as the application role.
func TestJoinRequestsStayWithinTheirTenant(t *testing.T) {
	h := newHarness(t)
	orgA, ctxA := h.makeOrg(t, "door-a")
	orgB, ctxB := h.makeOrg(t, "door-b")

	var stranger uuid.UUID
	if err := h.super.QueryRow(context.Background(), `
		INSERT INTO app_user (email, name) VALUES ($1, 'Stranger') RETURNING id`,
		h.email(t, "stranger")).Scan(&stranger); err != nil {
		t.Fatalf("make the stranger: %v", err)
	}
	if _, err := h.cluster.Write(ctxA, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO org_join_request (org_id, user_id) VALUES ($1, $2)`, orgA, stranger)
		return err
	}); err != nil {
		t.Fatalf("org A notes a request: %v", err)
	}

	count := func(ctx context.Context) int {
		var n int
		if err := h.cluster.ReadPrimary(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM org_join_request`).Scan(&n)
		}); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	if got := count(ctxA); got != 1 {
		t.Errorf("org A sees %d requests, want its own 1", got)
	}
	if got := count(ctxB); got != 0 {
		t.Errorf("org B sees %d of org A's requests, want none", got)
	}

	_, err := h.cluster.Write(ctxB, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO org_join_request (org_id, user_id) VALUES ($1, $2)`, orgA, stranger)
		return err
	})
	if err == nil {
		t.Errorf("org B wrote a request into org A's door")
	}
	_, err = h.cluster.Write(ctxB, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM org_join_request WHERE org_id = $1`, orgA)
		if err == nil && tag.RowsAffected() != 0 {
			return errors.New("org B deleted org A's request")
		}
		return err
	})
	if err != nil {
		t.Errorf("%v", err)
	}
	_ = orgB
}
