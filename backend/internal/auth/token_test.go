package auth

import (
	"strings"
	"testing"
)

func TestGenerateTokenIsUniqueAndHashed(t *testing.T) {
	seen := make(map[string]bool)
	for range 100 {
		secret, digest, err := GenerateToken()
		if err != nil {
			t.Fatalf("GenerateToken: %v", err)
		}
		if seen[secret] {
			t.Fatal("GenerateToken returned a duplicate secret")
		}
		seen[secret] = true

		if len(digest) != 32 {
			t.Fatalf("digest length = %d, want 32", len(digest))
		}
		if strings.Contains(string(digest), secret) {
			t.Fatal("digest contains the secret verbatim")
		}
		if !EqualDigest(digest, HashToken(secret)) {
			t.Fatal("HashToken does not reproduce the digest returned by GenerateToken")
		}
	}
}

func TestAPITokenIsRecognisable(t *testing.T) {
	secret, digest, err := GenerateAPIToken()
	if err != nil {
		t.Fatalf("GenerateAPIToken: %v", err)
	}
	if !IsAPIToken(secret) {
		t.Errorf("token %q is not recognised as a personal access token", secret)
	}
	if !EqualDigest(digest, HashToken(secret)) {
		t.Error("digest does not match the full prefixed secret")
	}

	sessionToken, _, err := GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if IsAPIToken(sessionToken) {
		t.Error("a session token was misidentified as a personal access token")
	}
}

func TestEqualDigest(t *testing.T) {
	a := HashToken("one")
	b := HashToken("two")
	if !EqualDigest(a, HashToken("one")) {
		t.Error("identical digests compared unequal")
	}
	if EqualDigest(a, b) {
		t.Error("different digests compared equal")
	}
	if EqualDigest(a, a[:16]) {
		t.Error("digests of different lengths compared equal")
	}
}
