//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
)

// emptyInstallation is a database of its own, migrated to the end with nobody
// in it, and the api over it: what a new installation's first sign-up meets.
func emptyInstallation(t *testing.T, h *harness, dbName string) (*apiServer, *harness) {
	t.Helper()
	ctx := context.Background()
	provider, _ := ownerDatabase(t, dbName, dbName+"_owner")
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("migrate %s: %v", dbName, err)
	}
	cfg := shared.cfg.DB
	cfg.PrimaryURL = withDatabase(t, cfg.PrimaryURL, dbName, "", "")
	cfg.AdminURL = withDatabase(t, cfg.AdminURL, dbName, "", "")
	// The primary alone, so no read waits on a replica catching up with a new database.
	cfg.ReplicaURLs = nil
	cluster, err := db.Open(ctx, cfg, shared.log)
	if err != nil {
		t.Fatalf("open %s: %v", dbName, err)
	}
	t.Cleanup(cluster.Close)
	empty := *h
	empty.cluster = cluster
	return newAPIServer(t, &empty), &empty
}

// The installation's first account sets it up and administers it; after that
// ARMATURE_SIGNUP decides, and no sign-up makes a second organization.
func TestTheFirstAccountSetsTheInstallationUp(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	firstKey := h.projectKey(t, "FIRST")
	api, empty := emptyInstallation(t, h, "armature_first_account_test")
	ctx := context.Background()
	with := func(policy auth.SignupPolicy) { api.api.Auth = empty.authService().Signups(policy) }
	signUp := func(c *client, name string) response {
		return c.post("/api/v1/auth/signup", map[string]string{"email": h.email(t, name), "password": testPassword, "name": name})
	}
	state := func(t *testing.T) map[string]any {
		t.Helper()
		return want(t, api.client(t).get("/api/v1/auth/signup"), http.StatusOK, "what a sign-up would do").Body
	}
	organizations := func(t *testing.T) int {
		t.Helper()
		var n int
		if err := empty.cluster.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM org`).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}

	t.Run("closed lets nobody in, not even the first", func(t *testing.T) {
		with(auth.SignupClosed)
		if got := state(t); got["open"] != false || got["first"] != false {
			t.Errorf("closed says %v, want neither open nor first", got)
		}
		refused := signUp(api.client(t), "shut")
		if refused.Status != http.StatusForbidden || refused.ErrorCode() != "signup_closed" {
			t.Fatalf("a sign-up while closed = %d %s, want 403 signup_closed", refused.Status, refused.Raw)
		}
		if n := organizations(t); n != 0 {
			t.Errorf("a refused sign-up left %d organizations", n)
		}
	})

	var ownerOrg map[string]any
	t.Run("the first account asks only for the person, and owns what it sets up", func(t *testing.T) {
		with(auth.SignupFirst)
		if got := state(t); got["open"] != true || got["first"] != true {
			t.Fatalf("an empty installation says %v, want open and first", got)
		}
		first := api.client(t)
		made := want(t, signUp(first, "founder"), http.StatusCreated, "the first sign-up")
		if role := principalField(t, made, "principal", "role"); role != "owner" {
			t.Errorf("the first account is %v, want owner", role)
		}
		ownerOrg = obj(t, made, "principal", "org")
		if ownerOrg["name"] != "Armature" {
			t.Errorf("the installation is called %v, want Armature, ARMATURE_NAME's default", ownerOrg["name"])
		}
		// It administers: inviting is an administrator's.
		want(t, first.get("/api/v1/invites"), http.StatusOK, "the first account administers")
		want(t, first.post("/api/v1/projects", map[string]any{"name": "First work", "key": firstKey}), http.StatusCreated, "and has what a project needs")
		if n := organizations(t); n != 1 {
			t.Errorf("the first sign-up left %d organizations, want 1", n)
		}
	})

	t.Run("first closes sign-up after it", func(t *testing.T) {
		if got := state(t); got["open"] != false || got["first"] != false {
			t.Errorf("a set up installation under first says %v, want closed", got)
		}
		refused := signUp(api.client(t), "second")
		if refused.Status != http.StatusForbidden || refused.ErrorCode() != "signup_closed" {
			t.Fatalf("a second sign-up under first = %d %s, want 403 signup_closed", refused.Status, refused.Raw)
		}
	})

	t.Run("open lets later people in as plain members of the same installation", func(t *testing.T) {
		with(auth.SignupOpen)
		if got := state(t); got["open"] != true || got["first"] != false {
			t.Errorf("open on a set up installation says %v, want open and not first", got)
		}
		joiner := api.client(t)
		joinerEmail := h.email(t, "joiner")
		joined := want(t, joiner.post("/api/v1/auth/signup", map[string]string{"email": joinerEmail, "password": testPassword, "name": "Joiner"}), http.StatusCreated, "a later sign-up")
		if role := principalField(t, joined, "principal", "role"); role != "member" {
			t.Errorf("a later sign-up is %v, want member", role)
		}
		if org := obj(t, joined, "principal", "org"); org["id"] != ownerOrg["id"] {
			t.Errorf("a later sign-up is in %v, want the installation %v", org, ownerOrg)
		}
		if got := joiner.get("/api/v1/invites"); got.Status != http.StatusForbidden {
			t.Errorf("a member signed up administers: %d %s", got.Status, got.Raw)
		}
		if n := organizations(t); n != 1 {
			t.Errorf("open sign-ups left %d organizations, want 1", n)
		}
		taken := api.client(t).post("/api/v1/auth/signup", map[string]string{"email": joinerEmail, "password": testPassword, "name": "Twin"})
		if taken.Status != http.StatusConflict || taken.ErrorCode() != "email_taken" {
			t.Errorf("signing up twice with one address = %d %s, want 409 email_taken", taken.Status, taken.Raw)
		}
	})

	t.Run("a sign-up still checks what the person typed", func(t *testing.T) {
		for what, body := range map[string]map[string]string{
			"no address":     {"password": testPassword, "name": "A"},
			"no name":        {"email": h.email(t, "nameless"), "password": testPassword},
			"short password": {"email": h.email(t, "short"), "password": "short", "name": "A"},
		} {
			if got := api.client(t).post("/api/v1/auth/signup", body); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: got %d %s, want 422", what, got.Status, got.Raw)
			}
		}
		// An organization field is refused rather than quietly ignored.
		named := api.client(t).post("/api/v1/auth/signup", map[string]string{
			"email": h.email(t, "namer"), "password": testPassword, "name": "Namer", "orgName": "Mine",
		})
		if named.Status != http.StatusBadRequest {
			t.Errorf("a sign-up naming an organization = %d %s, want 400", named.Status, named.Raw)
		}
	})
}
