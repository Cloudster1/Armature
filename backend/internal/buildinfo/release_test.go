package buildinfo

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// semver is the release shape make release accepts: no build metadata, which
// only a build past a tag carries.
var semver = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

// The release is written into four files; a hand edit to one of them would
// ship a chart that pulls an image nobody built.
func TestTheFilesThatCarryTheReleaseAgree(t *testing.T) {
	dir := os.Getenv("ARMATURE_RELEASE_FILES")
	if dir == "" {
		t.Skip("ARMATURE_RELEASE_FILES is not set; run the suite with `make test`")
	}
	read := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	version := strings.TrimSpace(string(read("VERSION")))
	if !semver.MatchString(version) {
		t.Fatalf("VERSION holds %q, which is not a semantic version", version)
	}

	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(read("package.json"), &pkg); err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Version  string `json:"version"`
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(read("package-lock.json"), &lock); err != nil {
		t.Fatal(err)
	}

	chart := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(string(read("Chart.yaml"))))
	for sc.Scan() {
		if key, value, ok := strings.Cut(sc.Text(), ":"); ok && (key == "version" || key == "appVersion") {
			chart[key] = strings.Trim(strings.TrimSpace(value), `"`)
		}
	}

	for name, got := range map[string]string{
		"web/package.json":                   pkg.Version,
		"web/package-lock.json":              lock.Version,
		`web/package-lock.json packages[""]`: lock.Packages[""].Version,
		"Chart.yaml version":                 chart["version"],
		"Chart.yaml appVersion":              chart["appVersion"],
	} {
		if got != version {
			t.Errorf("%s says %q but VERSION says %q; make release writes them all, so set them back or cut the release with it", name, got, version)
		}
	}
}
