package db

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// backendRoot is the module's root, seen from this package.
const backendRoot = "../.."

// organizationSetters are what binding a transaction to an organization took.
// Each is split so this file does not name what it looks for.
var organizationSetters = []string{
	"app." + "org_id",
	"set_" + "config(",
	"internal/" + "tenant\"",
	"With" + "System(",
	"ErrNo" + "Tenant",
}

// organizationSettersAllowed names the files that may name one, and why.
var organizationSettersAllowed = map[string]string{
	"test/installation_default_test.go": "sets the old variable to show current_org_id() no longer reads it",
}

// The installation is the one organization, so no transaction is bound to one:
// current_org_id() and the columns' default answer it without being told (#218).
func TestNothingSetsTheOrganization(t *testing.T) {
	var found []string
	err := filepath.WalkDir(backendRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(backendRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() || !strings.HasSuffix(rel, ".go") || rel == "internal/db/no_organization_test.go" {
			return nil
		}
		if _, ok := organizationSettersAllowed[rel]; ok {
			return nil
		}
		source, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, setter := range organizationSetters {
			if strings.Contains(string(source), setter) {
				found = append(found, rel+" names "+setter)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(found)
	if len(found) > 0 {
		t.Errorf("%d places still bind a transaction to an organization:\n%s", len(found), strings.Join(found, "\n"))
	}
}
