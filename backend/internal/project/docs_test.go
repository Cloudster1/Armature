package project

import (
	"errors"
	"strings"
	"testing"
)

func TestADocsLinkIsAWebAddressWithAnOptionalLabel(t *testing.T) {
	u, l, err := NormalizeDocsLink("  https://wiki.example.com/spaces/ENG ", " Engineering wiki ")
	if err != nil || u == nil || *u != "https://wiki.example.com/spaces/ENG" || l == nil || *l != "Engineering wiki" {
		t.Fatalf("got %v %v %v", u, l, err)
	}
	if u, l, err := NormalizeDocsLink("http://docs.test", ""); err != nil || u == nil || l != nil {
		t.Fatalf("an unnamed link: %v %v %v", u, l, err)
	}
	if u, l, err := NormalizeDocsLink(" ", ""); err != nil || u != nil || l != nil {
		t.Fatalf("an empty address should clear the link: %v %v %v", u, l, err)
	}

	for raw, want := range map[string]error{
		"javascript:alert(1)":             ErrBadDocsURL,
		"ftp://files.example.com":         ErrBadDocsURL,
		"wiki.example.com":                ErrBadDocsURL,
		"https://":                        ErrBadDocsURL,
		"https://wiki.example.com/a page": ErrBadDocsURL,
		"https://x.test/" + strings.Repeat("a", MaxDocsURL): ErrBadDocsURL,
	} {
		if _, _, err := NormalizeDocsLink(raw, ""); !errors.Is(err, want) {
			t.Errorf("%q: got %v, want %v", raw, err, want)
		}
	}
	if _, _, err := NormalizeDocsLink("", "Docs"); !errors.Is(err, ErrBadDocsLabel) {
		t.Errorf("a label without an address: %v", err)
	}
	if _, _, err := NormalizeDocsLink("https://docs.test", strings.Repeat("x", MaxDocsLabel+1)); !errors.Is(err, ErrBadDocsLabel) {
		t.Errorf("a long label: %v", err)
	}
}
