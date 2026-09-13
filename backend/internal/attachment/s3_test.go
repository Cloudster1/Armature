package attachment

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The GET Object example from the S3 signature documentation, which pins the
// canonicalisation and the key derivation to a published answer.
func TestSignatureMatchesTheDocumentedExample(t *testing.T) {
	headers := map[string]string{
		"host":                 "examplebucket.s3.amazonaws.com",
		"range":                "bytes=0-9",
		"x-amz-content-sha256": emptyPayloadHash,
		"x-amz-date":           "20130524T000000Z",
	}
	got := authorization("AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1",
		http.MethodGet, "/test.txt", "", headers, emptyPayloadHash)
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got != want {
		t.Fatalf("authorization\n got %s\nwant %s", got, want)
	}
}

func TestKeysAreEncodedSegmentBySegment(t *testing.T) {
	got := encodePath("org/abc/issue/x y/notes (final).md")
	want := "org/abc/issue/x%20y/notes%20%28final%29.md"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if encodeSegment("ü") != "%C3%BC" {
		t.Fatalf("multi byte characters are encoded byte by byte, got %s", encodeSegment("ü"))
	}
}

// fakeS3 is enough of a bucket to see the requests the store makes: paths,
// signatures present, bodies stored and returned.
type fakeS3 struct {
	mu      sync.Mutex
	bucket  string
	objects map[string][]byte
	seen    []string
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, r.Method+" "+r.URL.EscapedPath())
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=") ||
		r.Header.Get("X-Amz-Content-Sha256") == "" || r.Header.Get("X-Amz-Date") == "" {
		http.Error(w, "<Error><Code>AccessDenied</Code><Message>unsigned</Message></Error>", http.StatusForbidden)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(path, "/")
	if bucket != f.bucket {
		http.Error(w, "<Error><Code>NoSuchBucket</Code><Message>no</Message></Error>", http.StatusNotFound)
		return
	}
	switch {
	case key == "" && r.Method == http.MethodHead:
		if f.objects == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	case key == "" && r.Method == http.MethodPut:
		f.objects = map[string][]byte{}
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodPut:
		data, _ := io.ReadAll(r.Body)
		f.objects[key] = data
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet:
		data, ok := f.objects[key]
		if !ok {
			http.Error(w, "<Error><Code>NoSuchKey</Code><Message>gone</Message></Error>", http.StatusNotFound)
			return
		}
		_, _ = w.Write(data)
	case r.Method == http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func TestS3StoreAgainstAFakeBucket(t *testing.T) {
	fake := &fakeS3{bucket: "att"}
	server := httptest.NewServer(fake)
	defer server.Close()

	store, err := NewS3(S3Config{Endpoint: server.URL, Bucket: "att", AccessKey: "k", SecretKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC) }
	ctx := context.Background()

	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatalf("ensure bucket: %v", err)
	}
	if err := store.EnsureBucket(ctx); err != nil {
		t.Fatalf("ensure bucket again: %v", err)
	}
	key := "org/1/issue/2/3/notes (final).md"
	if err := store.Put(ctx, key, strings.NewReader("hello"), 5, "text/markdown"); err != nil {
		t.Fatalf("put: %v", err)
	}
	body, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	data, _ := io.ReadAll(body)
	body.Close()
	if string(data) != "hello" {
		t.Fatalf("got %q", data)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.Get(ctx, key); err != ErrNoObject {
		t.Fatalf("want ErrNoObject after delete, got %v", err)
	}

	want := []string{"HEAD /att", "PUT /att", "HEAD /att",
		"PUT /att/org/1/issue/2/3/notes%20%28final%29.md",
		"GET /att/org/1/issue/2/3/notes%20%28final%29.md",
		"DELETE /att/org/1/issue/2/3/notes%20%28final%29.md",
		"GET /att/org/1/issue/2/3/notes%20%28final%29.md"}
	if strings.Join(fake.seen, "|") != strings.Join(want, "|") {
		t.Fatalf("requests\n got %v\nwant %v", fake.seen, want)
	}
}

func TestEndpointForms(t *testing.T) {
	for _, in := range []string{"seaweedfs:8333", "http://seaweedfs:8333/", "https://s3.eu-central-1.amazonaws.com"} {
		if _, err := NewS3(S3Config{Endpoint: in, Bucket: "b"}); err != nil {
			t.Errorf("%q should be accepted: %v", in, err)
		}
	}
	if _, err := NewS3(S3Config{Endpoint: "seaweedfs:8333"}); err == nil {
		t.Error("a missing bucket should be refused")
	}
}
