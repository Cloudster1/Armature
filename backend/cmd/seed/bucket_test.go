package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/attachment"
)

const (
	testRetry     = 20 * time.Millisecond
	testWait      = 5 * time.Second
	testGiveUp    = 200 * time.Millisecond
	testStartsAt  = 150 * time.Millisecond
	testBucketKey = "armature-attachments"
)

// A free address with nothing listening on it yet, so a dial is refused.
func refusingAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

func storeAt(t *testing.T, addr string) *attachment.S3Store {
	t.Helper()
	store, err := attachment.NewS3(attachment.S3Config{Endpoint: addr, Bucket: testBucketKey, AccessKey: "key", SecretKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// The install failure of #314: the gateway refused the seed's one try while
// SeaweedFS was still starting, and the seed gave up at once.
func TestTheSeedWaitsForAnObjectStoreThatIsStillStarting(t *testing.T) {
	addr := refusingAddress(t)
	var heads atomic.Int32
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead && strings.HasSuffix(r.URL.Path, "/"+testBucketKey) {
			heads.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	})}
	go func() {
		time.Sleep(testStartsAt)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			t.Errorf("listen on %s once the store has started: %v", addr, err)
			return
		}
		_ = server.Serve(l)
	}()
	t.Cleanup(func() { server.Close() })

	if err := waitForBucket(context.Background(), storeAt(t, addr), testWait, testRetry, quiet); err != nil {
		t.Fatalf("waitForBucket = %v, want it to find the bucket once the store answers", err)
	}
	if heads.Load() == 0 {
		t.Fatal("the store answered no request for the bucket; the wait returned without asking it")
	}
}

func TestTheSeedGivesUpOnAnObjectStoreThatNeverAnswers(t *testing.T) {
	addr := refusingAddress(t)
	started := time.Now()
	err := waitForBucket(context.Background(), storeAt(t, addr), testGiveUp, testRetry, quiet)
	if err == nil {
		t.Fatal("waitForBucket = nil, want the refused connection reported")
	}
	if !strings.Contains(err.Error(), "still failing after") || !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("waitForBucket = %q, want it to say how long it waited and why it failed", err)
	}
	if elapsed := time.Since(started); elapsed < testGiveUp {
		t.Errorf("gave up after %s, before the %s it was given", elapsed, testGiveUp)
	}
}
