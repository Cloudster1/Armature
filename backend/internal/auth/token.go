package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Token secrets are never stored. Only their SHA-256 digest goes to the
// database, so a database disclosure does not hand an attacker working
// credentials. The digest is a plain hash rather than argon2 because the secret
// is 256 bits of entropy we generated ourselves: there is nothing to brute
// force, and session lookup happens on every single request.

const (
	// tokenBytes is the raw entropy behind a token, before encoding.
	tokenBytes = 32

	// APITokenPrefix marks personal access tokens so that secret scanners, our
	// own logs redaction, and support staff can recognise one on sight.
	APITokenPrefix = "armature_pat_"
)

// ErrInvalidToken covers an unparseable, unknown, revoked or expired token.
var ErrInvalidToken = errors.New("invalid or expired token")

// GenerateToken returns a new opaque secret and the digest to store for it.
func GenerateToken() (secret string, digest []byte, err error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	secret = base64.RawURLEncoding.EncodeToString(raw)
	return secret, HashToken(secret), nil
}

// GenerateAPIToken returns a prefixed personal access token.
func GenerateAPIToken() (secret string, digest []byte, err error) {
	s, _, err := GenerateToken()
	if err != nil {
		return "", nil, err
	}
	secret = APITokenPrefix + s
	return secret, HashToken(secret), nil
}

// HashToken returns the digest stored for a token secret.
func HashToken(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

// EqualDigest compares two digests in constant time.
func EqualDigest(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}

// IsAPIToken reports whether a presented credential is a personal access token
// rather than a session token.
func IsAPIToken(secret string) bool {
	return strings.HasPrefix(secret, APITokenPrefix)
}
