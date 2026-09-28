//go:build integration

package test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/theme"
)

// A theme leaves as one file with its pictures inside, and comes back as a
// theme of the importer's own: the files get new ids, and everything that
// named them, the spec and the CSS alike, names the new ones.
func TestAThemeTravelsAsOneFile(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "porter")

	made := want(t, owner.post("/api/v1/themes", map[string]any{"name": "Traveller", "spec": map[string]any{"colors": map[string]any{"light": map[string]string{"accent": "#123456"}}, "shape": map[string]any{"radiusControl": 0}}}), http.StatusCreated, "make a theme")
	themeID := obj(t, made, "theme")["id"].(string)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M2 2h12v12H2z"/></svg>`)
	uploaded := owner.upload("/api/v1/themes/"+themeID+"/assets", "file", "block.svg", "image/svg+xml", svg)
	if uploaded.Status != http.StatusCreated {
		t.Fatalf("upload: %d %s", uploaded.Status, uploaded.Raw)
	}
	assetID := obj(t, uploaded, "asset")["id"].(string)
	want(t, owner.patch("/api/v1/themes/"+themeID, map[string]any{"spec": map[string]any{
		"colors": map[string]any{"light": map[string]string{"accent": "#123456"}},
		"shape":  map[string]any{"radiusControl": 0},
		"icons":  map[string]any{"plus": map[string]any{"assetId": assetID}},
		"css":    ".x { background: url(/api/v1/themes/" + themeID + "/assets/" + assetID + ") }",
	}}), http.StatusOK, "name the file")
	h.waitForPrimary(t)

	resp, body := owner.download("/api/v1/themes/" + themeID + "/export")
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Disposition"), "Traveller.armature-theme.json") {
		t.Fatalf("export: %d %s %s", resp.StatusCode, resp.Header.Get("Content-Disposition"), body)
	}
	var pkg theme.Package
	if err := json.Unmarshal(body, &pkg); err != nil {
		t.Fatalf("the export is not JSON: %v", err)
	}
	if pkg.Format != theme.PackageFormat || pkg.Name != "Traveller" || len(pkg.Assets) != 1 || string(pkg.Assets[0].Data) != string(svg) {
		t.Fatalf("package = %+v", pkg)
	}

	t.Run("a stranger cannot export it", func(t *testing.T) {
		other := api.client(t)
		other.signup(t, h, "porter-other")
		if resp, _ := other.download("/api/v1/themes/" + themeID + "/export"); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("a private theme was exported by a stranger: %d", resp.StatusCode)
		}
	})

	t.Run("imported, it is a theme of one's own with files of its own", func(t *testing.T) {
		imported := owner.upload("/api/v1/themes/import", "file", "Traveller.armature-theme.json", "application/json", body)
		if imported.Status != http.StatusCreated {
			t.Fatalf("import: %d %s", imported.Status, imported.Raw)
		}
		back := obj(t, imported, "theme")
		if back["name"] != "Traveller (2)" || back["id"] == themeID {
			t.Fatalf("the import is not a second theme with a numbered name: %s", imported.Raw)
		}
		assets := back["assets"].([]any)
		if len(assets) != 1 || assets[0].(map[string]any)["id"] == assetID {
			t.Fatalf("the file did not get an id of its own: %s", imported.Raw)
		}
		newAsset := assets[0].(map[string]any)["id"].(string)
		newID := back["id"].(string)
		spec := back["spec"].(map[string]any)
		if spec["icons"].(map[string]any)["plus"].(map[string]any)["assetId"] != newAsset {
			t.Fatalf("the icon still names the old file: %s", imported.Raw)
		}
		if css := spec["css"].(string); !strings.Contains(css, "/api/v1/themes/"+newID+"/assets/"+newAsset) || strings.Contains(css, assetID) {
			t.Fatalf("the CSS still names the old file: %s", css)
		}
		h.waitForPrimary(t)
		if resp, data := owner.download("/api/v1/themes/" + newID + "/assets/" + newAsset); resp.StatusCode != http.StatusOK || string(data) != string(svg) {
			t.Fatalf("the imported file does not read back: %d", resp.StatusCode)
		}
	})

	t.Run("what is not a theme file is refused, and leaves nothing behind", func(t *testing.T) {
		before := len(list(t, want(t, owner.get("/api/v1/themes"), http.StatusOK, "themes"), "themes"))
		if got := owner.upload("/api/v1/themes/import", "file", "notes.json", "application/json", []byte(`{"hello": "world"}`)); got.Status != http.StatusUnprocessableEntity {
			t.Fatalf("a stray file was imported: %d %s", got.Status, got.Raw)
		}
		broken := `{"format":"armature-theme/1","name":"Broken","spec":{"backdrop":{"assetId":"00000000-0000-0000-0000-000000000001","fit":"cover"}},"assets":[]}`
		if got := owner.upload("/api/v1/themes/import", "file", "broken.json", "application/json", []byte(broken)); got.Status != http.StatusUnprocessableEntity {
			t.Fatalf("a theme naming a file it does not carry was imported: %d %s", got.Status, got.Raw)
		}
		h.waitForPrimary(t)
		if after := len(list(t, want(t, owner.get("/api/v1/themes"), http.StatusOK, "themes"), "themes")); after != before {
			t.Fatalf("a refused import left a theme behind: %d -> %d", before, after)
		}
	})
}
