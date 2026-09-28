package attachment

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFSStoreRoundTripsAndTidiesAfterItself(t *testing.T) {
	ctx := context.Background()
	store, err := NewFS(filepath.Join(t.TempDir(), "files"))
	if err != nil {
		t.Fatal(err)
	}
	key := "org/a1/issue/b2/notes (final).md"
	if err := store.Put(ctx, key, strings.NewReader("hello"), 5, "text/markdown"); err != nil {
		t.Fatalf("put: %v", err)
	}
	body, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(body)
	_ = body.Close()
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
	if _, err := store.Get(ctx, "org/a1/nothing"); !errors.Is(err, ErrNoObject) {
		t.Fatalf("a missing object is ErrNoObject, got %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("deleting twice is fine, got %v", err)
	}
	entries, _ := os.ReadDir(store.Root())
	if len(entries) != 0 {
		t.Fatalf("the directories the object needed were not removed with it: %v", entries)
	}
}

func TestFSStoreRefusesAKeyThatLeavesTheDirectory(t *testing.T) {
	ctx := context.Background()
	store, err := NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "..", "../x", "a/../../x", "/etc/passwd", "a/.tmp-b", "a\x00b"} {
		if err := store.Put(ctx, key, strings.NewReader("x"), 1, ""); err == nil {
			t.Errorf("put %q was allowed", key)
		}
		if _, err := store.Get(ctx, key); err == nil || errors.Is(err, ErrNoObject) {
			t.Errorf("get %q was allowed: %v", key, err)
		}
	}
}

func TestFSStoreWritesWholeObjectsOrNothing(t *testing.T) {
	ctx := context.Background()
	store, err := NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A body shorter than it said it was is refused and leaves nothing behind.
	if err := store.Put(ctx, "short", strings.NewReader("abc"), 10, ""); err == nil {
		t.Fatal("a short body was accepted")
	}
	if _, err := store.Get(ctx, "short"); !errors.Is(err, ErrNoObject) {
		t.Fatalf("a refused put left an object: %v", err)
	}
	var objects []Object
	if err := store.Walk(ctx, func(o Object) error { objects = append(objects, o); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(objects) != 0 {
		t.Fatalf("a temporary file is listed as an object: %v", objects)
	}
}

func TestFSStoreWalksEveryObjectInOrder(t *testing.T) {
	ctx := context.Background()
	store, err := NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"b/two", "a/one", "a/three"} {
		if err := store.Put(ctx, key, strings.NewReader(key), int64(len(key)), ""); err != nil {
			t.Fatal(err)
		}
	}
	var keys []string
	var bytes int64
	if err := store.Walk(ctx, func(o Object) error { keys = append(keys, o.Key); bytes += o.Size; return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(keys, ",") != "a/one,a/three,b/two" {
		t.Fatalf("walked %v", keys)
	}
	if bytes != int64(len("b/two")+len("a/one")+len("a/three")) {
		t.Fatalf("sizes add up to %d", bytes)
	}
}
