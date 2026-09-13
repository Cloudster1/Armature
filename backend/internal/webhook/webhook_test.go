package webhook

import (
	"testing"
	"time"
)

func TestSignAndVerify(t *testing.T) {
	body := []byte(`{"id":"x"}`)
	sig := Sign(body, "secret")
	if len(sig) != len("sha256=")+64 || sig[:7] != "sha256=" {
		t.Fatalf("signature = %q", sig)
	}
	if !Verify(body, "secret", sig) || Verify(body, "other", sig) || Verify([]byte(`{}`), "secret", sig) {
		t.Fatal("verify disagrees with sign")
	}
}

func TestBackoffCoversEveryRetry(t *testing.T) {
	if len(Backoff) != MaxAttempts-1 {
		t.Fatalf("%d waits for %d attempts", len(Backoff), MaxAttempts)
	}
	for i := 1; i < len(Backoff); i++ {
		if Backoff[i] <= Backoff[i-1] || Backoff[i] > 24*time.Hour {
			t.Errorf("backoff %d = %s", i, Backoff[i])
		}
	}
}
