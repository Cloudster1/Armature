package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The theme files kept under docs/design/themes are what somebody imports to
// try the product dressed up. Each has to be a package this version reads and
// a spec the editor would save. The Makefile mounts the directory and names
// it; outside that, there is nothing to check.
func TestEveryShippedThemeFileImports(t *testing.T) {
	dir := os.Getenv("ARMATURE_THEME_FILES")
	if dir == "" {
		t.Skip("ARMATURE_THEME_FILES is not set; run the suite with `make test`")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.armature-theme.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no theme files under %s: %v", dir, err)
	}
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var pkg Package
		if err := json.Unmarshal(body, &pkg); err != nil {
			t.Errorf("%s does not parse: %v", filepath.Base(file), err)
			continue
		}
		if pkg.Format != PackageFormat || strings.TrimSpace(pkg.Name) == "" {
			t.Errorf("%s is not a %s package with a name", filepath.Base(file), PackageFormat)
		}
		// The files the package carries are the theme's own once imported.
		assets := map[uuid.UUID]bool{}
		for _, a := range pkg.Assets {
			assets[a.ID] = true
		}
		spec := pkg.Spec
		if err := Validate(&spec, uuid.Nil, assets); err != nil {
			t.Errorf("%s does not validate: %v", filepath.Base(file), err)
		}
	}
}
