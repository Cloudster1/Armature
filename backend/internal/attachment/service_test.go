package attachment

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"screenshot.png":                  "screenshot.png",
		"  notes.md ":                     "notes.md",
		"../../etc/passwd":                "passwd",
		`C:\Users\me\report.pdf`:          "report.pdf",
		"":                                "file",
		"..":                              "file",
		"bad\"quote\x00.txt":              "badquote.txt",
		strings.Repeat("a", 300) + ".txt": strings.Repeat("a", MaxNameLength),
	}
	for in, want := range cases {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContentTypeIsSniffedWhenTheClientSaysNothingUseful(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	if got := contentTypeFor("", png); got != "image/png" {
		t.Errorf("sniffed %q, want image/png", got)
	}
	if got := contentTypeFor("application/octet-stream", png); got != "image/png" {
		t.Errorf("a generic declaration should be sniffed past, got %q", got)
	}
	if got := contentTypeFor("text/markdown", png); got != "text/markdown" {
		t.Errorf("a specific declaration is kept, got %q", got)
	}
	if got := contentTypeFor("text/plain\r\nX-Injected: yes", png); got != "image/png" {
		t.Errorf("a declaration with a line break is not trusted, got %q", got)
	}
}

func TestObjectKeyIsLaidOutByTenantAndIssue(t *testing.T) {
	org, iss, id := uuid.New(), uuid.New(), uuid.New()
	key := objectKey(org, iss, id, "a.txt")
	want := "org/" + org.String() + "/issue/" + iss.String() + "/" + id.String() + "/a.txt"
	if key != want {
		t.Fatalf("got %s want %s", key, want)
	}
}

func TestMemoryStoreRoundTrips(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	if err := store.Put(ctx, "k", strings.NewReader("hello"), 5, "text/plain"); err != nil {
		t.Fatal(err)
	}
	body, err := store.Get(ctx, "k")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(body)
	if string(data) != "hello" {
		t.Fatalf("got %q", data)
	}
	if err := store.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, "k"); err != ErrNoObject {
		t.Fatalf("want ErrNoObject after delete, got %v", err)
	}
}

func TestUnavailableStoreNamesTheSetting(t *testing.T) {
	store, err := FromConfig(S3Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "k", strings.NewReader("x"), 1, ""); err != ErrUnavailable {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if !strings.Contains(ErrUnavailable.Error(), "ARMATURE_S3_ENDPOINT") {
		t.Fatal("the error should say which setting to fix")
	}
}
