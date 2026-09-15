//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/perm"
)

// A theme is its maker's, shared when they say so, chosen per person, and
// walled by the tenant like everything else.
func TestThemesOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)

	owner := api.client(t)
	signedUp := owner.signup(t, h, "themes")
	orgID := principalField(t, signedUp, "principal", "org", "id").(string)
	ownerID := principalField(t, signedUp, "principal", "user", "id").(string)

	spec := map[string]any{
		"colors": map[string]any{"light": map[string]string{"accent": "#ff0066"}, "dark": map[string]string{"accent": "#00ffcc"}},
		"shape":  map[string]any{"radiusControl": 2},
		"css":    "[data-rail] { opacity: .95 }",
	}
	made := want(t, owner.post("/api/v1/themes", map[string]any{"name": "Magenta", "spec": spec}), http.StatusCreated, "make a theme")
	theme := obj(t, made, "theme")
	themeID := theme["id"].(string)
	if theme["shared"] != false || theme["active"] != false || theme["inUse"].(float64) != 0 {
		t.Fatalf("a new theme is private, unchosen and unused: %s", made.Raw)
	}
	if obj(t, made, "theme", "spec", "colors", "light")["accent"] != "#ff0066" {
		t.Fatalf("the spec did not come back: %s", made.Raw)
	}

	t.Run("what is refused on the way in", func(t *testing.T) {
		refuse := func(what string, got response, status int) {
			t.Helper()
			if got.Status != status {
				t.Errorf("%s: got %d, want %d: %s", what, got.Status, status, got.Raw)
			}
		}
		refuse("no name", owner.post("/api/v1/themes", map[string]any{"spec": spec}), http.StatusUnprocessableEntity)
		refuse("the same name twice", owner.post("/api/v1/themes", map[string]any{"name": "magenta"}), http.StatusConflict)
		refuse("a colour that is a word", owner.post("/api/v1/themes", map[string]any{"name": "Words", "spec": map[string]any{"colors": map[string]any{"light": map[string]string{"accent": "red"}}}}), http.StatusUnprocessableEntity)
		refuse("css that imports", owner.post("/api/v1/themes", map[string]any{"name": "Importer", "spec": map[string]any{"css": "@import url(http://x)"}}), http.StatusUnprocessableEntity)
		refuse("css that loads elsewhere", owner.patch("/api/v1/themes/"+themeID, map[string]any{"spec": map[string]any{"css": ".x{background:url(https://evil/p.png)}"}}), http.StatusUnprocessableEntity)
		refuse("a file the theme does not have", owner.patch("/api/v1/themes/"+themeID, map[string]any{"spec": map[string]any{"backdrop": map[string]any{"assetId": "00000000-0000-0000-0000-000000000001", "fit": "cover"}}}), http.StatusUnprocessableEntity)
		refuse("choosing a theme that is not there", owner.put("/api/v1/themes/active", map[string]any{"themeId": "00000000-0000-0000-0000-000000000001"}), http.StatusNotFound)
		refuse("a stranger's theme", owner.get("/api/v1/themes/00000000-0000-0000-0000-000000000001"), http.StatusNotFound)
		refuse("nobody signed in", api.client(t).get("/api/v1/themes"), http.StatusUnauthorized)
		refuse("nobody signed in asking what is active", api.client(t).get("/api/v1/themes/active"), http.StatusUnauthorized)
	})

	t.Run("choosing, and the active answer following", func(t *testing.T) {
		none := want(t, owner.get("/api/v1/themes/active"), http.StatusOK, "nothing chosen")
		if none.Body["theme"] != nil {
			t.Fatalf("a fresh account has a theme: %s", none.Raw)
		}
		chosen := want(t, owner.put("/api/v1/themes/active", map[string]any{"themeId": themeID}), http.StatusOK, "choose")
		if obj(t, chosen, "theme")["active"] != true {
			t.Fatalf("chosen but not active: %s", chosen.Raw)
		}
		active := want(t, owner.get("/api/v1/themes/active"), http.StatusOK, "read the active theme")
		if obj(t, active, "theme")["id"] != themeID || obj(t, active, "theme")["inUse"].(float64) != 1 {
			t.Fatalf("the active theme is not the chosen one: %s", active.Raw)
		}
		back := want(t, owner.put("/api/v1/themes/active", map[string]any{"themeId": nil}), http.StatusOK, "back to the built-in")
		if back.Body["theme"] != nil {
			t.Fatalf("null did not return to the built-in theme: %s", back.Raw)
		}
		want(t, owner.put("/api/v1/themes/active", map[string]any{"themeId": themeID}), http.StatusOK, "choose again")
	})

	other := api.asRole(t, h, owner, "", perm.User)
	t.Run("a private theme is nobody else's until shared, and then not theirs to change", func(t *testing.T) {
		if got := other.get("/api/v1/themes/" + themeID); got.Status != http.StatusNotFound {
			t.Fatalf("a private theme was found by somebody else: %d", got.Status)
		}
		if seen := list(t, want(t, other.get("/api/v1/themes"), http.StatusOK, "list as another"), "themes"); len(seen) != 0 {
			t.Fatalf("somebody else sees %d themes before any is shared", len(seen))
		}
		want(t, owner.patch("/api/v1/themes/"+themeID, map[string]any{"shared": true}), http.StatusOK, "share")
		h.waitForPrimary(t)
		seen := list(t, want(t, other.get("/api/v1/themes"), http.StatusOK, "list as another"), "themes")
		if len(seen) != 1 || seen[0].(map[string]any)["ownerId"] != ownerID {
			t.Fatalf("the shared theme is not seen: %s", seen)
		}
		if got := other.patch("/api/v1/themes/"+themeID, map[string]any{"name": "Mine now"}); got.Status != http.StatusForbidden {
			t.Fatalf("somebody else changed a shared theme: %d %s", got.Status, got.Raw)
		}
		if got := other.delete("/api/v1/themes/" + themeID); got.Status != http.StatusForbidden {
			t.Fatalf("somebody else deleted a shared theme: %d %s", got.Status, got.Raw)
		}
		want(t, other.put("/api/v1/themes/active", map[string]any{"themeId": themeID}), http.StatusOK, "they use it")
		h.waitForPrimary(t)
		if obj(t, want(t, owner.get("/api/v1/themes/"+themeID), http.StatusOK, "read"), "theme")["inUse"].(float64) != 2 {
			t.Error("two people use it now")
		}
	})

	t.Run("files: a safe SVG is kept and served as a download, an unsafe one is refused", func(t *testing.T) {
		safe := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M2 8h12"/></svg>`)
		added := want(t, owner.upload("/api/v1/themes/"+themeID+"/assets", "file", "glyph.svg", "image/svg+xml", safe), http.StatusCreated, "upload an svg")
		asset := obj(t, added, "asset")
		if asset["contentType"] != "image/svg+xml" {
			t.Fatalf("type = %v", asset["contentType"])
		}
		assetID := asset["id"].(string)

		got, data := owner.download("/api/v1/themes/" + themeID + "/assets/" + assetID)
		if got.StatusCode != http.StatusOK || string(data) != string(safe) {
			t.Fatalf("the file came back as %d with %d bytes", got.StatusCode, len(data))
		}
		if got.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(got.Header.Get("Content-Disposition"), "attachment") || !strings.Contains(got.Header.Get("Cache-Control"), "immutable") {
			t.Errorf("headers = %v", got.Header)
		}
		if !strings.Contains(got.Header.Get("Content-Security-Policy"), "default-src 'none'") {
			t.Errorf("an svg served without a policy that keeps it inert: %v", got.Header)
		}

		want(t, owner.upload("/api/v1/themes/"+themeID+"/assets", "file", "evil.svg", "image/svg+xml", []byte(`<svg onload="alert(1)"><script>1</script></svg>`)), http.StatusBadRequest, "a scripted svg")
		want(t, owner.upload("/api/v1/themes/"+themeID+"/assets", "file", "notes.txt", "text/plain", []byte("words")), http.StatusBadRequest, "text")
		if got := other.upload("/api/v1/themes/"+themeID+"/assets", "file", "glyph.svg", "image/svg+xml", safe); got.Status != http.StatusForbidden {
			t.Errorf("somebody else added a file: %d", got.Status)
		}

		// The file can be named by the theme, and then not removed.
		want(t, owner.patch("/api/v1/themes/"+themeID, map[string]any{"spec": map[string]any{"icons": map[string]any{"home": map[string]any{"assetId": assetID}}}}), http.StatusOK, "use the file as an icon")
		if got := owner.delete("/api/v1/themes/" + themeID + "/assets/" + assetID); got.Status != http.StatusConflict {
			t.Errorf("a file in use was removed: %d %s", got.Status, got.Raw)
		}
		want(t, owner.patch("/api/v1/themes/"+themeID, map[string]any{"spec": map[string]any{}}), http.StatusOK, "stop using the file")
		want(t, owner.delete("/api/v1/themes/"+themeID+"/assets/"+assetID), http.StatusNoContent, "remove the file")
		if got, _ := owner.download("/api/v1/themes/" + themeID + "/assets/" + assetID); got.StatusCode != http.StatusNotFound {
			t.Errorf("a removed file still serves: %d", got.StatusCode)
		}
		if got := owner.delete("/api/v1/themes/" + themeID + "/assets/" + assetID); got.Status != http.StatusNotFound {
			t.Errorf("removing a removed file: %d", got.Status)
		}
	})

	t.Run("a customer cannot share, and SQL refuses it too", func(t *testing.T) {
		var customerID string
		if err := h.super.QueryRow(context.Background(), `INSERT INTO app_user (email, name) VALUES ($1, 'Customer') RETURNING id`, h.email(t, "customer")).Scan(&customerID); err != nil {
			t.Fatal(err)
		}
		if _, err := h.super.Exec(context.Background(), `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'customer')`, orgID, customerID); err != nil {
			t.Fatal(err)
		}
		_, err := h.super.Exec(context.Background(), `INSERT INTO theme (org_id, owner_id, name, shared) VALUES ($1, $2, 'Theirs', true)`, orgID, customerID)
		if err == nil || !strings.Contains(err.Error(), "customer cannot share") {
			t.Fatalf("SQL let a customer share: %v", err)
		}
	})

	t.Run("another organization sees nothing, over the API and in SQL", func(t *testing.T) {
		stranger := api.client(t)
		stranger.signup(t, h, "themes-stranger")
		if got := stranger.get("/api/v1/themes/" + themeID); got.Status != http.StatusNotFound {
			t.Errorf("another organization found the theme: %d", got.Status)
		}
		if got := stranger.put("/api/v1/themes/active", map[string]any{"themeId": themeID}); got.Status != http.StatusNotFound {
			t.Errorf("another organization chose the theme: %d", got.Status)
		}
		_, ctxB := h.makeOrg(t, h.orgSlug(t, "themes-b"))
		var n int
		err := h.cluster.ReadPrimary(ctxB, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM theme`).Scan(&n)
		})
		if err != nil || n != 0 {
			t.Errorf("another tenant counts %d themes (err %v), want 0", n, err)
		}
	})

	t.Run("deleting returns everybody to the built-in theme", func(t *testing.T) {
		want(t, owner.delete("/api/v1/themes/"+themeID), http.StatusNoContent, "delete")
		h.waitForPrimary(t)
		if got := want(t, other.get("/api/v1/themes/active"), http.StatusOK, "the other's active theme"); got.Body["theme"] != nil {
			t.Errorf("a deleted theme is still somebody's: %s", got.Raw)
		}
		if got := owner.get("/api/v1/themes/" + themeID); got.Status != http.StatusNotFound {
			t.Errorf("a deleted theme is still found: %d", got.Status)
		}
	})
}
