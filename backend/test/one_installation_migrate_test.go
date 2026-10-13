//go:build integration

package test

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// As the plain owner a cluster migrates as, whom org hides its rows from: a
// guard that forgot to lift that would count none and let several through.
func TestTheUpgradeRefusesSeveralOrganizations(t *testing.T) {
	ctx := context.Background()
	provider, super := ownerDatabase(t, "armature_one_installation_test", "armature_one_installation_owner")

	const (
		beforeTheGuard = 928
		theGuard       = 929
		// One more than the guard lists, so the count of the rest shows.
		organizations = 11
		listed        = 10
	)
	if _, err := provider.UpTo(ctx, beforeTheGuard); err != nil {
		t.Fatalf("migrate to %d as the owner: %v", beforeTheGuard, err)
	}
	insertOrg := func(t *testing.T, slug, name string) {
		t.Helper()
		if _, err := super.Exec(ctx, `INSERT INTO org (slug, name) VALUES ($1, $2)`, slug, name); err != nil {
			t.Fatalf("insert organization %s: %v", slug, err)
		}
	}
	version := func(t *testing.T) int64 {
		t.Helper()
		v, err := provider.GetDBVersion(ctx)
		if err != nil {
			t.Fatalf("read version: %v", err)
		}
		return v
	}
	passes := func(t *testing.T, what string) {
		t.Helper()
		if _, err := provider.ApplyVersion(ctx, theGuard, true); err != nil {
			t.Fatalf("the guard refused %s: %v", what, err)
		}
		if _, err := provider.ApplyVersion(ctx, theGuard, false); err != nil {
			t.Fatalf("take the guard back after %s: %v", what, err)
		}
	}

	t.Run("an empty installation passes", func(t *testing.T) {
		passes(t, "an installation with no organization")
	})

	t.Run("one organization passes", func(t *testing.T) {
		insertOrg(t, "acme", "Acme Corp")
		passes(t, "one organization")
	})

	t.Run("several are refused, by name", func(t *testing.T) {
		insertOrg(t, "beta", "Beta Works")
		for i := 3; i <= organizations; i++ {
			insertOrg(t, fmt.Sprintf("extra-%02d", i), fmt.Sprintf("Extra %d", i))
		}

		// Forced, org shows its owner nothing, which is the case that matters.
		var forcedBefore bool
		if err := super.QueryRow(ctx, `SELECT relforcerowsecurity FROM pg_class WHERE oid = 'org'::regclass`).Scan(&forcedBefore); err != nil || !forcedBefore {
			t.Fatalf("org should force row security on its owner before the guard: forced=%v err=%v", forcedBefore, err)
		}

		_, err := provider.ApplyVersion(ctx, theGuard, true)
		if err == nil {
			t.Fatal("the upgrade ran over eleven organizations")
		}
		message := err.Error()
		for _, want := range []string{
			fmt.Sprintf("%d organizations", organizations),
			"acme (Acme Corp)", "beta (Beta Works)",
			fmt.Sprintf("and %d more", organizations-listed),
			"Delete the extra ones with the previous release before upgrading.",
		} {
			if !strings.Contains(message, want) {
				t.Errorf("the refusal does not say %q: %s", want, message)
			}
		}
		if v := version(t); v != beforeTheGuard {
			t.Errorf("the database is at %d after the refusal, want it left at %d", v, beforeTheGuard)
		}
		var forced bool
		if err := super.QueryRow(ctx, `SELECT relforcerowsecurity FROM pg_class WHERE oid = 'org'::regclass`).Scan(&forced); err != nil {
			t.Fatal(err)
		}
		if !forced {
			t.Error("the refusal left org's row security lifted")
		}
	})

	t.Run("with the extra ones deleted, the upgrade runs to the end", func(t *testing.T) {
		if _, err := super.Exec(ctx, `DELETE FROM org WHERE slug <> 'acme'`); err != nil {
			t.Fatal(err)
		}
		if _, err := provider.Up(ctx); err != nil {
			t.Fatalf("migrate the rest as the owner: %v", err)
		}
		all := provider.ListSources()
		if latest := all[len(all)-1].Version; version(t) != latest {
			t.Fatalf("database is at %d, the migrations go to %d", version(t), latest)
		}
	})
}
