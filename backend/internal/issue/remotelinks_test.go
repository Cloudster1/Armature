package issue

import (
	"errors"
	"strings"
	"testing"
)

func TestAPageLinkIsCheckedBeforeItIsWritten(t *testing.T) {
	good := RemoteLinkInput{URL: "https://wiki.example.com/pages/42", Title: "Checkout spec", Source: "Stator"}

	refused := map[string]RemoteLinkInput{
		"no address":              {Title: "Spec", Source: "Stator"},
		"a relative address":      {URL: "/pages/42", Title: "Spec", Source: "Stator"},
		"a script address":        {URL: "javascript:alert(1)", Title: "Spec", Source: "Stator"},
		"a file address":          {URL: "file:///etc/passwd", Title: "Spec", Source: "Stator"},
		"an address with no host": {URL: "https://", Title: "Spec", Source: "Stator"},
		"a space in the address":  {URL: "https://wiki.example.com/a page", Title: "Spec", Source: "Stator"},
		"an address too long":     {URL: "https://wiki.example.com/" + strings.Repeat("a", MaxRemoteLinkURL), Title: "Spec", Source: "Stator"},
		"no title":                {URL: good.URL, Title: "   ", Source: "Stator"},
		"a title too long":        {URL: good.URL, Title: strings.Repeat("t", MaxRemoteLinkTitle+1), Source: "Stator"},
		"no source":               {URL: good.URL, Title: "Spec"},
		"a source too long":       {URL: good.URL, Title: "Spec", Source: strings.Repeat("s", MaxRemoteLinkSource+1)},
		"an icon not on the web":  {URL: good.URL, Title: "Spec", Source: "Stator", IconURL: "data:image/png;base64,AAAA"},
	}
	for what, in := range refused {
		if _, err := checkRemoteLink(in); !errors.Is(err, ErrBadRemoteLink) {
			t.Errorf("%s: got %v, want ErrBadRemoteLink", what, err)
		}
	}

	in, err := checkRemoteLink(RemoteLinkInput{URL: "  " + good.URL + " ", Title: " Checkout spec ", Source: " Stator ", IconURL: "http://wiki.example.com/icon.png"})
	if err != nil {
		t.Fatal(err)
	}
	if in.URL != good.URL || in.Title != good.Title || in.Source != good.Source {
		t.Errorf("the input should be trimmed, got %+v", in)
	}

	// The longest title allowed is counted in characters, not bytes.
	if _, err := checkRemoteLink(RemoteLinkInput{URL: good.URL, Title: strings.Repeat("ü", MaxRemoteLinkTitle), Source: "Stator"}); err != nil {
		t.Errorf("a title of %d characters should be taken: %v", MaxRemoteLinkTitle, err)
	}
}

func TestAPageLinkRefusalSaysWhatToFix(t *testing.T) {
	_, err := checkRemoteLink(RemoteLinkInput{URL: "ftp://files.example.com/spec", Title: "Spec", Source: "Stator"})
	if err == nil || !strings.Contains(err.Error(), "starting with http:// or https://") {
		t.Errorf("got %v, want a sentence naming the schemes taken", err)
	}
}
