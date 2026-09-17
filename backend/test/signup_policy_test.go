//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
)

func TestSignupPolicy(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	ctx := context.Background()

	t.Run("open, the default, says so and lets a signup through", func(t *testing.T) {
		c := api.client(t)
		if got := c.get("/api/v1/auth/signup"); got.Status != http.StatusOK || got.Body["open"] != true {
			t.Fatalf("GET /auth/signup = %d %s, want open", got.Status, got.Raw)
		}
		c.signup(t, h, "opendoor")
	})

	t.Run("closed refuses a signup and creates nothing", func(t *testing.T) {
		api.api.Auth = h.authService().Signups(auth.SignupClosed)
		t.Cleanup(func() { api.api.Auth = h.authService() })

		c := api.client(t)
		if got := c.get("/api/v1/auth/signup"); got.Status != http.StatusOK || got.Body["open"] != false {
			t.Fatalf("GET /auth/signup = %d %s, want closed", got.Status, got.Raw)
		}

		email := h.email(t, "shut")
		slug := h.orgSlug(t, "shut")
		refused := c.post("/api/v1/auth/signup", map[string]string{
			"email": email, "password": testPassword, "name": "Shut Out", "orgName": "Shut Out", "orgSlug": slug,
		})
		if refused.Status != http.StatusForbidden || refused.ErrorCode() != "signup_closed" {
			t.Fatalf("signup while closed = %d %s, want 403 signup_closed", refused.Status, refused.Raw)
		}

		var users, orgs int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM app_user WHERE email = $1`, email).Scan(&users); err != nil {
			t.Fatal(err)
		}
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM org WHERE slug = $1`, slug).Scan(&orgs); err != nil {
			t.Fatal(err)
		}
		if users != 0 || orgs != 0 {
			t.Errorf("a refused signup left %d accounts and %d organizations behind", users, orgs)
		}
	})

	t.Run("first refuses once an organization exists", func(t *testing.T) {
		svc := h.authService().Signups(auth.SignupFirst)
		h.signup(t, h.authService(), "already")

		open, err := svc.SignupAllowed(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if open {
			t.Error("first-only reports open although organizations exist")
		}
		_, err = svc.Signup(ctx, auth.SignupInput{
			Email: h.email(t, "second"), Password: testPassword, Name: "Second", OrgName: "Second", OrgSlug: h.orgSlug(t, "second"),
		})
		if !errors.Is(err, auth.ErrSignupClosed) {
			t.Errorf("second organization under first-only: err = %v, want ErrSignupClosed", err)
		}
	})

	// The shared database always has organizations in it, so an empty
	// installation is stood in for by a temporary table of the same name,
	// which Postgres resolves before the real one for the rest of the
	// transaction. The query under test is the real one.
	t.Run("first lets the very first organization in", func(t *testing.T) {
		tx, err := h.super.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `CREATE TEMP TABLE org (id uuid) ON COMMIT DROP`); err != nil {
			t.Fatal(err)
		}

		open, err := auth.SignupAllowedIn(ctx, tx, auth.SignupFirst)
		if err != nil {
			t.Fatal(err)
		}
		if !open {
			t.Error("first-only refuses an installation with no organization yet")
		}

		if _, err := tx.Exec(ctx, `INSERT INTO org (id) VALUES (gen_random_uuid())`); err != nil {
			t.Fatal(err)
		}
		if open, err := auth.SignupAllowedIn(ctx, tx, auth.SignupFirst); err != nil || open {
			t.Errorf("first-only after one organization: open = %v, err = %v, want refused", open, err)
		}
	})
}
