package main

import (
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
)

func TestTheBootstrapReadsItsEnvironment(t *testing.T) {
	env := map[string]string{
		"ARMATURE_BOOTSTRAP_ADMIN_EMAIL":    " admin@example.com ",
		"ARMATURE_BOOTSTRAP_ADMIN_PASSWORD": "a sufficiently long password",
		"ARMATURE_BOOTSTRAP_MEMBERS":        "lead@example.com=admin, dev@example.com=member,",
	}
	in, err := bootstrapInput(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if in.AdminEmail != "admin@example.com" || len(in.Members) != 2 || in.Members[0] != (auth.BootstrapMember{Email: "lead@example.com", Role: auth.RoleAdmin}) {
		t.Fatalf("read %+v", in)
	}

	env["ARMATURE_BOOTSTRAP_MEMBERS"] = "lead@example.com"
	if _, err := bootstrapInput(func(k string) string { return env[k] }); err == nil || !strings.Contains(err.Error(), "email=role") {
		t.Fatalf("a member without a role: %v", err)
	}
	delete(env, "ARMATURE_BOOTSTRAP_ADMIN_PASSWORD")
	if _, err := bootstrapInput(func(k string) string { return env[k] }); err == nil || !strings.Contains(err.Error(), "bootstrap.admin") {
		t.Fatalf("no password: %v", err)
	}
}
