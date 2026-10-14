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

// tableNamed finds each table a statement reads or writes; a name followed by
// a parenthesis is a function, and SET follows ON CONFLICT's DO UPDATE.
var tableNamed = regexp.MustCompile(`(?i)\b(?:FROM|JOIN|INTO|UPDATE)\s+([a-z_][a-z0-9_]*)\b(\s*\()?`)

// organizationTables are the ones step 5 of #218 renames, which still carry
// the organization's id until then.
var organizationTables = map[string]bool{"org": true, "org_member": true, "org_role": true}

// orgIDAllowed names the files, or a file's declarations, that may name the
// column, and why: each is about the column itself, so step 5 rewrites it.
var orgIDAllowed = map[string]string{
	"internal/db/no_organization_test.go":                               "names the old session variable it refuses",
	"migrations/backfill_rls_test.go":                                   "checks migrations written while the column was the tenant",
	"test/installation_default_test.go":                                 "proves the column's default and current_org_id() answer the installation",
	"test/installation_keys_test.go":                                    "proves no key takes the column into account, with rows of two organizations",
	"test/triggers_without_organization_test.go":                        "proves no trigger reads the column, with rows of two organizations",
	"test/events_without_organization_test.go":                          "reads which organization a row the consumer wrote landed in",
	"test/outbox_test.go TestOutboxRelay":                               "proves the stream's message carries no organization field",
	"test/sign_in_test.go TestASignInLandsInTheInstallation":            "reads the two session columns 00931 emptied, which step 5 drops",
	"test/owner_migrate_test.go expectBackfilled":                       "reads what migrations made for each organization of a legacy database",
	"test/old_keys_test.go TestOldKeysComeOverFromTheColumnThatKeptOne": "writes rows at a version before the column had a default",
	"test/row_security_test.go TestRowSecurityGoesAsThePlainOwner":      "writes rows at a version before the column had a default",
}

// No query names the organization: the column's default fills it, and the
// installation is the only one a row can belong to (#218).
func TestNoQueryNamesTheOrganization(t *testing.T) {
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
				if err != nil || !namesTheOrganization.MatchString(text) || onlyOrganizationTables(text) {
					return true
				}
				for _, key := range []string{rel, rel + " " + declName(decl)} {
					if _, ok := orgIDAllowed[key]; ok {
						used[key] = true
						return true
					}
				}
				found = append(found, rel+":"+strconv.Itoa(fset.Position(lit.Pos()).Line)+": "+firstLineNaming(text))
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
		t.Errorf("%d queries still name the organization:\n%s", len(found), strings.Join(found, "\n"))
	}
	for key := range orgIDAllowed {
		if !used[key] {
			t.Errorf("%s is allowed to name the organization but no longer does; take it off the list", key)
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

// onlyOrganizationTables is a statement that reads or writes the organization's
// own tables and nothing else.
func onlyOrganizationTables(sql string) bool {
	named := false
	for _, m := range tableNamed.FindAllStringSubmatch(sql, -1) {
		if m[2] != "" || strings.EqualFold(m[1], "set") {
			continue
		}
		if !organizationTables[strings.ToLower(m[1])] {
			return false
		}
		named = true
	}
	return named
}

func firstLineNaming(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if namesTheOrganization.MatchString(line) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
