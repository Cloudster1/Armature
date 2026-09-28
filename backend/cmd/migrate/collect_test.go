package main

import (
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/armature/armature/backend/migrations"
)

// goose panics rather than erroring on two files with one version, and that
// only surfaces at deploy time; this catches it in the unit suite.
func TestMigrationVersionsAreUnique(t *testing.T) {
	goose.SetBaseFS(migrations.FS)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("goose refused the embedded migrations: %v", r)
		}
	}()
	if _, err := goose.CollectMigrations(".", 0, goose.MaxVersion); err != nil {
		t.Fatal(err)
	}
}
