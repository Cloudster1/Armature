//go:build integration

package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/theme"
)

// Themes between Armature, Exciter and Stator (#295): a file another of the three
// exported imports here and leaves again the same, compared as Exciter compares.

var anyThemeID = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

// travelled is a theme file with the ids each installation gave it left out: files
// are numbered in the order the file carries them, and any other id is the theme's own.
type travelled struct {
	Name  string
	Spec  string
	Files []string
}

func travel(t *testing.T, raw []byte) travelled {
	t.Helper()
	var pkg theme.Package
	if err := json.Unmarshal(raw, &pkg); err != nil {
		t.Fatalf("not a theme file: %v", err)
	}
	if pkg.Format != theme.PackageFormat {
		t.Fatalf("a file of format %q", pkg.Format)
	}
	numbered := map[string]string{}
	out := travelled{Name: pkg.Name}
	for i, a := range pkg.Assets {
		numbered[a.ID.String()] = fmt.Sprintf("file-%d", i+1)
		out.Files = append(out.Files, fmt.Sprintf("%s %s %x", a.Name, a.ContentType, a.Data))
	}
	// All three products trim a theme's CSS as they save it, so a last newline is not a change.
	pkg.Spec.CSS = strings.TrimSpace(pkg.Spec.CSS)
	spec, err := json.Marshal(pkg.Spec)
	if err != nil {
		t.Fatal(err)
	}
	out.Spec = anyThemeID.ReplaceAllStringFunc(string(spec), func(id string) string {
		if n, ok := numbered[id]; ok {
			return n
		}
		return "theme"
	})
	return out
}

// sameTravel names where two files part ways, rather than printing both whole.
func sameTravel(t *testing.T, what string, got, want travelled) {
	t.Helper()
	if got.Name != want.Name {
		t.Errorf("%s: the name became %q, want %q", what, got.Name, want.Name)
	}
	if got.Spec != want.Spec {
		at := 0
		for at < len(got.Spec) && at < len(want.Spec) && got.Spec[at] == want.Spec[at] {
			at++
		}
		from := max(at-80, 0)
		t.Errorf("%s: the spec parts ways at %d:\n got  ...%s\n want ...%s", what, at, got.Spec[from:min(at+120, len(got.Spec))], want.Spec[from:min(at+120, len(want.Spec))])
	}
	if fmt.Sprint(got.Files) != fmt.Sprint(want.Files) {
		t.Errorf("%s: the files are %d, want %d, or their bytes differ", what, len(got.Files), len(want.Files))
	}
}

func partnerThemeFile(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "internal", "theme", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// roundTrip imports a file as the owner and answers what exporting it again gives.
func roundTrip(t *testing.T, owner *client, name string, body []byte) []byte {
	t.Helper()
	imported := want(t, owner.upload("/api/v1/themes/import", "file", name, "application/json", body), http.StatusCreated, "import "+name)
	resp, out := owner.download("/api/v1/themes/" + obj(t, imported, "theme")["id"].(string) + "/export")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export %s: %d %s", name, resp.StatusCode, out)
	}
	return out
}

func TestExcitersThemeTravelsThroughArmatureUnchanged(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signIn(t, h.person(t, "theme-traveller", auth.RoleOwner))
	body := partnerThemeFile(t, "exciter.armature-theme.json")

	t.Run("Exciter's own export imports and leaves again the same", func(t *testing.T) {
		got := travel(t, roundTrip(t, owner, "exciter.armature-theme.json", body))
		if len(got.Files) == 0 {
			t.Fatal("Exciter's file carries a cursor; the export lost it")
		}
		sameTravel(t, "Exciter's export on its way through Armature", got, travel(t, body))
	})

	t.Run("a token Armature does not know travels through it, and one the file leaves out stays out", func(t *testing.T) {
		var pkg map[string]any
		if err := json.Unmarshal(body, &pkg); err != nil {
			t.Fatal(err)
		}
		light := pkg["spec"].(map[string]any)["colors"].(map[string]any)["light"].(map[string]any)
		light["coverage-full"] = "#15803d"
		pkg["name"] = "Workshop, with a later token"
		changed, _ := json.Marshal(pkg)
		var back theme.Package
		if err := json.Unmarshal(roundTrip(t, owner, "later.armature-theme.json", changed), &back); err != nil {
			t.Fatal(err)
		}
		if back.Spec.Colors.Light["coverage-full"] != "#15803d" {
			t.Errorf("the unknown token was lost: %v", back.Spec.Colors.Light)
		}
		if _, named := back.Spec.Colors.Light["accent"]; named {
			t.Error("a token the file leaves out came back with a value")
		}
	})
}
