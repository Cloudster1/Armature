package config

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var (
	envName      = regexp.MustCompile(`^ARMATURE_[A-Z0-9]+(_[A-Z0-9]+)*$`)
	backquoted   = regexp.MustCompile("`([A-Z0-9_]+)`")
	backendRoot  = filepath.Join("..", "..")
	notOperators = []string{
		// The seed fills a development stack; an operator never runs it.
		filepath.Join("cmd", "seed"),
		// The integration suite's settings are the Makefile's business.
		"test",
	}
)

// An operator learns what can be set from the README's table alone, so a
// variable the code reads must be there, and one listed there must be read.
func TestEveryVariableReadIsInTheReadmeAndBack(t *testing.T) {
	path := os.Getenv("ARMATURE_README_FILE")
	if path == "" {
		t.Skip("ARMATURE_README_FILE is not set; run the suite with `make test`")
	}
	documented := readmeVariables(t, path)
	read := variablesRead(t)

	for _, name := range sortedKeys(read) {
		if !documented[name] {
			t.Errorf("%s is read in %s but missing from the README's table of variables", name, read[name])
		}
	}
	for _, name := range sortedKeys(documented) {
		if _, ok := read[name]; !ok {
			t.Errorf("%s is in the README's table of variables but nothing reads it", name)
		}
	}
}

// readmeVariables reads the first column of the README's table. A row such as
// `ARMATURE_DB_MAX_CONNS` / `_MIN_CONNS` names ARMATURE_DB_MIN_CONNS too.
func readmeVariables(t *testing.T, path string) map[string]bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	out := map[string]bool{}
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		line := scan.Text()
		if !strings.HasPrefix(line, "| `ARMATURE_") {
			continue
		}
		cell := strings.SplitN(line, "|", 3)[1]
		names := backquoted.FindAllStringSubmatch(cell, -1)
		first := names[0][1]
		group, _, _ := strings.Cut(strings.TrimPrefix(first, "ARMATURE_"), "_")
		for _, m := range names {
			name := m[1]
			if strings.HasPrefix(name, "_") {
				name = "ARMATURE_" + group + name
			}
			out[name] = true
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatalf("%s has no table of ARMATURE_ variables", path)
	}
	return out
}

// variablesRead finds every string in the backend that is exactly a variable's
// name, with the file it is in.
func variablesRead(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(backendRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(backendRoot, path)
		if d.IsDir() {
			if slices.Contains(notOperators, rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if s, err := strconv.Unquote(lit.Value); err == nil && envName.MatchString(s) {
				out[s] = rel
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out["ARMATURE_DB_PRIMARY_URL"]; !ok {
		t.Fatalf("found no variables under %s; the walk is looking in the wrong place", backendRoot)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
