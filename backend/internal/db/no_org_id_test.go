package db

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// namesTheOrganization is a column or function the installation no longer needs
// a query to name. Split, so this file does not name what it looks for.
var namesTheOrganization = regexp.MustCompile(`\b` + "org" + `_id\b|current_` + "org" + `_id`)

// orgIDAllowed names the files, or a file's declarations, that may name the
// column, and why: each is about the column itself, or runs before it went.
var orgIDAllowed = map[string]string{
	"internal/db/no_organization_test.go":                                 "names the old session variable it refuses",
	"migrations/backfill_rls_test.go":                                     "checks migrations written while the column was the tenant",
	"test/schema_without_organization_test.go":                            "looks for the column in the catalog and drops it from a database of its own",
	"test/installation_keys_test.go orgKeyed":                             "lists the keys that took the column into account before 00933",
	"test/installation_keys_test.go TestTheUpgradeRefusesKeysThatCollide": "writes rows of two organizations at a version before 00933",
	"test/outbox_test.go TestOutboxRelay":                                 "proves the stream's message carries no organization field",
	"test/owner_migrate_test.go expectBackfilled":                         "reads what migrations made for each organization of a legacy database",
	"test/old_keys_test.go TestOldKeysComeOverFromTheColumnThatKeptOne":   "writes rows at a version before the column had a default",
	"test/row_security_test.go TestRowSecurityGoesAsThePlainOwner":        "writes rows at a version before the column had a default",
}

// namesTheOldTables is a table the schema renamed or a column it moved: the
// organization's row, its members, roles, invitations and requests to join.
var namesTheOldTables = regexp.MustCompile(`\b` + "org" + `_(member|role|invite|join_request|wide_only)\b|(?i:\b(from|join|into|update|table|references)\s+` + "org" + `\b)`)

// oldTablesAllowed names the files, or a file's declarations, that may name an
// old table, and why: each runs at a version from before the rename.
var oldTablesAllowed = map[string]string{}

// No query names the organization: the tables no longer carry it, and the
// installation is the only one a row can belong to (#218).
func TestNoQueryNamesTheOrganization(t *testing.T) {
	refuseStrings(t, namesTheOrganization, orgIDAllowed, "name the organization")
}

// No query names the organization's tables: the installation, its roles and
// invitations have their own names, and a member's standing is on the account (#218).
func TestNoQueryNamesTheOrganizationsTables(t *testing.T) {
	refuseStrings(t, namesTheOldTables, oldTablesAllowed, "name the organization's tables")
}

// refuseStrings fails for every string in the backend's Go that matches, unless
// allowed, and for every allowance that is no longer needed.
func refuseStrings(t *testing.T, pattern *regexp.Regexp, allowed map[string]string, what string) {
	t.Helper()
	var found []string
	used := map[string]bool{}
	err := filepath.WalkDir(backendRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(backendRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() || !strings.HasSuffix(rel, ".go") || rel == "internal/db/no_org_id_test.go" {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			ast.Inspect(decl, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil || !pattern.MatchString(text) {
					return true
				}
				for _, key := range []string{rel, rel + " " + declName(decl)} {
					if _, ok := allowed[key]; ok {
						used[key] = true
						return true
					}
				}
				found = append(found, rel+":"+strconv.Itoa(fset.Position(lit.Pos()).Line)+": "+firstLineMatching(pattern, text))
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(found)
	if len(found) > 0 {
		t.Errorf("%d queries still %s:\n%s", len(found), what, strings.Join(found, "\n"))
	}
	for key := range allowed {
		if !used[key] {
			t.Errorf("%s is allowed to %s but no longer does; take it off the list", key, what)
		}
	}
}

// declName is a function's name, or the first name a const or var block gives.
func declName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return d.Name.Name
	case *ast.GenDecl:
		for _, spec := range d.Specs {
			if v, ok := spec.(*ast.ValueSpec); ok && len(v.Names) > 0 {
				return v.Names[0].Name
			}
		}
	}
	return ""
}

func firstLineMatching(pattern *regexp.Regexp, text string) string {
	for _, line := range strings.Split(text, "\n") {
		if pattern.MatchString(line) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
