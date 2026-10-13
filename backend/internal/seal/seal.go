// Package seal encrypts the few secrets the server has to read back, such as
// an authenticator's key, so a copy of the database alone does not hold them.
package seal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const (
	// KeyBytes is the length of an AES-256 key.
	KeyBytes = 32
	// version leads every sealed value, so a later format can be told apart.
	version byte = 1
	// keyIDBytes names the key a value was sealed with, so a key can be rotated
	// by keeping the old one around to open what it sealed.
	keyIDBytes  = 4
	headerBytes = 1 + keyIDBytes
)

var (
	// ErrBadKey is a key that is not 32 bytes of base64.
	ErrBadKey = errors.New("the key must be 32 random bytes in base64, such as `openssl rand -base64 32` prints")
	// ErrWrongKey is a value sealed with another key than the one in hand.
	ErrWrongKey = errors.New("the value was sealed with another key")
	// ErrMalformed is a value that is not a sealed value, or was changed.
	ErrMalformed = errors.New("the sealed value is damaged")
)

// Sealer seals and opens values with one key.
type Sealer struct {
	id   [keyIDBytes]byte
	aead cipher.AEAD
}

// Parse reads a key the way the configuration carries it, as base64.
func Parse(encoded string) (*Sealer, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(key) != KeyBytes {
		return nil, ErrBadKey
	}
	return New(key)
}

// New makes a sealer from the raw key.
func New(key []byte) (*Sealer, error) {
	if len(key) != KeyBytes {
		return nil, ErrBadKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("seal: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("seal: %w", err)
	}
	s := &Sealer{aead: aead}
	// The id is a digest of the key, not the key: it may be read off any row.
	sum := sha256.Sum256(append([]byte("armature seal key id\x00"), key...))
	copy(s.id[:], sum[:keyIDBytes])
	return s, nil
}

// KeyID names this sealer's key in the values it seals.
func (s *Sealer) KeyID() []byte { return append([]byte(nil), s.id[:]...) }

// Seal encrypts plain. What it is bound to, such as the owner's id, has to be
// given again to open it, so a sealed value moved to another row does not open.
func (s *Sealer) Seal(plain, boundTo []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("seal: %w", err)
	}
	out := make([]byte, 0, headerBytes+len(nonce)+len(plain)+s.aead.Overhead())
	out = append(out, version)
	out = append(out, s.id[:]...)
	out = append(out, nonce...)
	return s.aead.Seal(out, nonce, plain, s.additional(boundTo)), nil
}

// Open decrypts a value Seal made with the same key and the same binding.
func (s *Sealer) Open(sealed, boundTo []byte) ([]byte, error) {
	nonceSize := s.aead.NonceSize()
	if len(sealed) < headerBytes+nonceSize+s.aead.Overhead() || sealed[0] != version {
		return nil, ErrMalformed
	}
	if string(sealed[1:headerBytes]) != string(s.id[:]) {
		return nil, ErrWrongKey
	}
	nonce := sealed[headerBytes : headerBytes+nonceSize]
	plain, err := s.aead.Open(nil, nonce, sealed[headerBytes+nonceSize:], s.additional(boundTo))
	if err != nil {
		return nil, ErrMalformed
	}
	return plain, nil
}

// KeyIDOf reads which key sealed a value, without opening it.
func KeyIDOf(sealed []byte) ([]byte, error) {
	if len(sealed) < headerBytes || sealed[0] != version {
		return nil, ErrMalformed
	}
	return append([]byte(nil), sealed[1:headerBytes]...), nil
}

// additional covers the header too, so a value cannot be relabelled.
func (s *Sealer) additional(boundTo []byte) []byte {
	out := make([]byte, 0, headerBytes+len(boundTo))
	out = append(out, version)
	out = append(out, s.id[:]...)
	return append(out, boundTo...)
}
