package webhook

import (
	"strconv"
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

// The timestamped signature binds the body to the moment it was sent: another
// time, another secret or another body is refused, and so is an old delivery.
func TestATimestampedSignatureRefusesAReplay(t *testing.T) {
	body := []byte(`{"id":"x"}`)
	sent := time.Unix(1_790_000_000, 0)
	stamp := strconv.FormatInt(sent.Unix(), 10)
	sig := SignTimestamped(body, "secret", sent)
	window := 5 * time.Minute

	if sig == Sign(body, "secret") {
		t.Fatal("the timestamped signature does not depend on the time")
	}
	if !VerifyTimestamped(body, "secret", stamp, sig, window, sent.Add(time.Minute)) {
		t.Fatal("a fresh delivery was refused")
	}
	for name, ok := range map[string]bool{
		"another secret":       VerifyTimestamped(body, "other", stamp, sig, window, sent),
		"another body":         VerifyTimestamped([]byte(`{}`), "secret", stamp, sig, window, sent),
		"a moved timestamp":    VerifyTimestamped(body, "secret", strconv.FormatInt(sent.Unix()+1, 10), sig, window, sent),
		"a padded timestamp":   VerifyTimestamped(body, "secret", "0"+stamp, sig, window, sent),
		"no timestamp":         VerifyTimestamped(body, "secret", "", sig, window, sent),
		"a replay much later":  VerifyTimestamped(body, "secret", stamp, sig, window, sent.Add(window+time.Second)),
		"a time in the future": VerifyTimestamped(body, "secret", stamp, sig, window, sent.Add(-window-time.Second)),
	} {
		if ok {
			t.Errorf("%s was accepted", name)
		}
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
