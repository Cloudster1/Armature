//go:build integration

package test

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
)

// The chart's bootstrap sets an installation up with sign-up closed from the
// start, so no first comer owns it (#324), and a rerun changes nothing.
func TestTheBootstrapOwnsAnInstallationWithSignUpClosed(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api, empty := emptyInstallation(t, h, "armature_bootstrap_test")
	api.api.Auth = empty.authService().Signups(auth.SignupClosed)
	ctx := context.Background()
	svc := empty.authService().Signups(auth.SignupClosed)

	if got := want(t, api.client(t).get("/api/v1/auth/signup"), http.StatusOK, "the sign-up offer").Body; got["open"] != false {
		t.Fatalf("an empty installation with sign-up closed offers a sign-up: %v", got)
	}

	admin, lead, dev := h.email(t, "boot-admin"), h.email(t, "boot-lead"), h.email(t, "boot-dev")
	in := auth.BootstrapInput{AdminEmail: admin, AdminPassword: testPassword, Members: []auth.BootstrapMember{
		{Email: lead, Role: auth.RoleAdmin}, {Email: dev, Role: auth.RoleMember},
	}}
	res, err := svc.Bootstrap(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OwnerCreated || !slices.Equal(res.MembersAdded, []string{lead, dev}) {
		t.Fatalf("the first run: %+v", res)
	}
	standing := func(email string) string {
		t.Helper()
		var s *string
		if err := empty.cluster.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT standing FROM app_user WHERE email = $1`, email).Scan(&s)
		}); err != nil {
			t.Fatal(err)
		}
		if s == nil {
			return ""
		}
		return *s
	}
	for email, role := range map[string]string{admin: "owner", lead: "admin", dev: "member"} {
		if got := standing(email); got != role {
			t.Errorf("%s stands as %q, not %s", email, got, role)
		}
	}
	want(t, api.client(t).post("/api/v1/auth/login", map[string]string{"email": admin, "password": testPassword}), http.StatusOK, "the administrator signs in")
	if got := api.client(t).post("/api/v1/auth/signup", map[string]string{"email": h.email(t, "late"), "password": testPassword, "name": "Late"}); got.Status == http.StatusCreated {
		t.Fatal("somebody signed up after the bootstrap, with sign-up closed")
	}

	in.AdminPassword = "the secret was rotated since"
	again, err := svc.Bootstrap(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if again.OwnerCreated || again.AdminCreated || len(again.MembersAdded) > 0 {
		t.Fatalf("a rerun changed something: %+v", again)
	}
	want(t, api.client(t).post("/api/v1/auth/login", map[string]string{"email": admin, "password": testPassword}), http.StatusOK, "the administrator keeps the password they had")
}

// On an installation somebody else set up, the bootstrap adds its
// administrator next to that owner, as the umbrella chart's earlier installs
// were set up by dynamo-connect's sign-up.
func TestTheBootstrapAddsItsAdministratorToAnInstallationSetUpBefore(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api, empty := emptyInstallation(t, h, "armature_bootstrap_later_test")
	api.api.Auth = empty.authService().Signups(auth.SignupFirst)
	ctx := context.Background()

	founder := h.email(t, "founder")
	want(t, api.client(t).post("/api/v1/auth/signup", map[string]string{"email": founder, "password": testPassword, "name": "Founder"}), http.StatusCreated, "the first account")

	admin := h.email(t, "boot-later")
	res, err := empty.authService().Bootstrap(ctx, auth.BootstrapInput{AdminEmail: admin, AdminPassword: testPassword})
	if err != nil {
		t.Fatal(err)
	}
	if res.OwnerCreated || !res.AdminCreated {
		t.Fatalf("the bootstrap on a set-up installation: %+v", res)
	}
	want(t, api.client(t).post("/api/v1/auth/login", map[string]string{"email": admin, "password": testPassword}), http.StatusOK, "the added administrator signs in")

	if _, err := empty.authService().Bootstrap(ctx, auth.BootstrapInput{AdminEmail: founder, AdminPassword: "not the founder's",
		Members: []auth.BootstrapMember{{Email: h.email(t, "bad"), Role: auth.RoleOwner}}}); err == nil {
		t.Fatal("the bootstrap let somebody in as a second owner")
	}
}
