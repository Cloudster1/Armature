//go:build integration

package test

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"net/http"
	"strings"
	"testing"
)

// pictureOf makes a small PNG in memory, the way a test can without a file.
func pictureOf(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for x := 0; x < side; x++ {
		img.Set(x, x%side, color.RGBA{R: 200, G: 90, B: 40, A: 255})
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func gifOf(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := gif.Encode(&out, image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White}), nil); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// A person changes their own name, time zone and language, and puts a face to
// their account; the face is seen by anyone in an organization with them and
// by nobody else.
func TestAPersonChangesTheirProfileAndTheirPicture(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	me := api.client(t)
	me.signup(t, h, "profiled")

	t.Run("name, time zone and language", func(t *testing.T) {
		changed := want(t, me.patch("/api/v1/auth/me", map[string]any{"name": "Ada Lovelace", "timezone": "Europe/Berlin", "locale": "de-DE"}), http.StatusOK, "profile")
		if principalField(t, changed, "principal", "user", "timezone") != "Europe/Berlin" || principalField(t, changed, "principal", "user", "locale") != "de-DE" {
			t.Errorf("principal = %v", changed.Body)
		}
		again := want(t, me.get("/api/v1/auth/me"), http.StatusOK, "me")
		if principalField(t, again, "principal", "user", "name") != "Ada Lovelace" {
			t.Errorf("the name did not stick: %v", again.Body)
		}
		want(t, me.patch("/api/v1/auth/me", map[string]any{"timezone": "Mars/Olympus"}), http.StatusBadRequest, "a zone nobody keeps")
		want(t, me.patch("/api/v1/auth/me", map[string]any{"locale": "tlh"}), http.StatusBadRequest, "a language not offered")
		want(t, me.patch("/api/v1/auth/me", map[string]any{"name": "   "}), http.StatusBadRequest, "no name")
	})

	var pictureURL string
	t.Run("a picture goes up and comes back", func(t *testing.T) {
		picture := pictureOf(t, 64)
		set := want(t, me.upload("/api/v1/auth/me/avatar", "file", "face.png", "image/png", picture), http.StatusOK, "set the picture")
		pictureURL, _ = principalField(t, set, "principal", "user", "avatarUrl").(string)
		if !strings.HasPrefix(pictureURL, "/api/v1/users/") || !strings.Contains(pictureURL, "/avatar?v=") {
			t.Fatalf("avatarUrl = %q", pictureURL)
		}
		got, data := me.download(pictureURL)
		if got.StatusCode != http.StatusOK || !bytes.Equal(data, picture) {
			t.Errorf("the picture came back as %d with %d bytes, want the %d sent", got.StatusCode, len(data), len(picture))
		}
		if !strings.Contains(got.Header.Get("Cache-Control"), "immutable") || got.Header.Get("Content-Type") != "image/png" {
			t.Errorf("headers = %v", got.Header)
		}
	})

	t.Run("what is not a small picture is refused", func(t *testing.T) {
		want(t, me.upload("/api/v1/auth/me/avatar", "file", "face.gif", "image/gif", gifOf(t)), http.StatusBadRequest, "a gif")
		want(t, me.upload("/api/v1/auth/me/avatar", "file", "notes.txt", "text/plain", []byte("not a picture")), http.StatusBadRequest, "text")
		want(t, me.upload("/api/v1/auth/me/avatar", "file", "wall.png", "image/png", pictureOf(t, 2100)), http.StatusBadRequest, "too wide")
	})

	t.Run("a colleague sees it, a stranger does not", func(t *testing.T) {
		colleagueEmail := h.email(t, "colleague")
		invited := want(t, me.post("/api/v1/invites", map[string]any{"email": colleagueEmail, "role": "member"}), http.StatusCreated, "invite")
		colleague := api.client(t)
		want(t, colleague.post("/api/v1/auth/invites/accept", map[string]any{"token": invited.Body["token"], "name": "Col League", "password": testPassword}), http.StatusOK, "joins")
		if got, _ := colleague.download(pictureURL); got.StatusCode != http.StatusOK {
			t.Errorf("a colleague got %d", got.StatusCode)
		}
		members := list(t, want(t, colleague.get("/api/v1/members"), http.StatusOK, "members"), "members")
		found := false
		for _, m := range members {
			if m.(map[string]any)["avatarUrl"] == pictureURL {
				found = true
			}
		}
		if !found {
			t.Errorf("the member list does not carry the picture: %v", members)
		}
		stranger := api.client(t)
		stranger.signup(t, h, "stranger")
		if got, _ := stranger.download(pictureURL); got.StatusCode != http.StatusNotFound {
			t.Errorf("a stranger got %d", got.StatusCode)
		}
	})

	t.Run("the picture can be taken away", func(t *testing.T) {
		want(t, me.delete("/api/v1/auth/me/avatar"), http.StatusNoContent, "remove")
		if got, _ := me.download(pictureURL); got.StatusCode != http.StatusNotFound {
			t.Errorf("after removal the picture still answers %d", got.StatusCode)
		}
		again := want(t, me.get("/api/v1/auth/me"), http.StatusOK, "me")
		if _, has := obj(t, again, "principal", "user")["avatarUrl"]; has {
			t.Errorf("avatarUrl still set: %v", again.Body)
		}
	})
}
