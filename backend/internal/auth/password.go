// Package auth handles identity: password verification, opaque session tokens,
// API tokens, and the organization a request is acting within.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// ErrInvalidCredentials is returned for both an unknown email and a wrong
// password. Callers must not distinguish between the two: doing so turns the
// login form into an account enumeration oracle.
var ErrInvalidCredentials = errors.New("invalid email or password")

// PasswordParams are the argon2id cost parameters. They are stored inside every
// hash, so raising them later does not invalidate existing passwords: an old
// hash still verifies with its own parameters and is transparently rehashed on
// the next successful login.
type PasswordParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// DefaultPasswordParams follows the OWASP argon2id recommendation of 64 MiB
// with three iterations.
func DefaultPasswordParams() PasswordParams {
	return PasswordParams{
		MemoryKiB:   64 * 1024,
		Iterations:  3,
		Parallelism: 4,
		SaltLength:  16,
		KeyLength:   32,
	}
}

// HashConcurrency is how many passwords are hashed at once. Argon2 is meant to
// be expensive, so unbounded callers would be a way to exhaust the machine.
const HashConcurrency = 4

var hashers = make(chan struct{}, HashConcurrency)

func hashing() func() {
	hashers <- struct{}{}
	return func() { <-hashers }
}

// HashPassword returns an argon2id hash in the PHC string format, which carries
// its own parameters: $argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>.
func HashPassword(password string, p PasswordParams) (string, error) {
	if password == "" {
		return "", errors.New("password must not be empty")
	}
	salt := make([]byte, p.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	defer hashing()()
	key := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Parallelism, p.KeyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Iterations, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches encoded, and whether the hash
// should be upgraded because it was produced with weaker parameters than the
// ones now in force.
func VerifyPassword(password, encoded string, current PasswordParams) (ok bool, needsRehash bool, err error) {
	p, salt, want, err := decodeHash(encoded)
	if err != nil {
		return false, false, err
	}
	release := hashing()
	got := argon2.IDKey([]byte(password), salt, p.Iterations, p.MemoryKiB, p.Parallelism, uint32(len(want)))
	release()
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, false, nil
	}
	weaker := p.MemoryKiB < current.MemoryKiB ||
		p.Iterations < current.Iterations ||
		p.Parallelism < current.Parallelism ||
		uint32(len(want)) < current.KeyLength
	return true, weaker, nil
}

func decodeHash(encoded string) (PasswordParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// "", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash
	if len(parts) != 6 || parts[0] != "" {
		return PasswordParams{}, nil, nil, errors.New("malformed password hash")
	}
	if parts[1] != "argon2id" {
		return PasswordParams{}, nil, nil, fmt.Errorf("unsupported password hash algorithm %q", parts[1])
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return PasswordParams{}, nil, nil, errors.New("malformed password hash version")
	}
	if version != argon2.Version {
		return PasswordParams{}, nil, nil, fmt.Errorf("unsupported argon2 version %d", version)
	}

	var p PasswordParams
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Iterations, &p.Parallelism); err != nil {
		return PasswordParams{}, nil, nil, errors.New("malformed password hash parameters")
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil {
		return PasswordParams{}, nil, nil, errors.New("malformed password hash salt")
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil {
		return PasswordParams{}, nil, nil, errors.New("malformed password hash digest")
	}
	p.SaltLength = uint32(len(salt))
	p.KeyLength = uint32(len(key))
	return p, salt, key, nil
}
