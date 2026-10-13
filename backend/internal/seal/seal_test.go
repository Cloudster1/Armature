package seal

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
)

func newKey(t *testing.T) string {
	t.Helper()
	raw := make([]byte, KeyBytes)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func TestASealedValueOpensWithItsKeyAndBinding(t *testing.T) {
	s, err := Parse(newKey(t))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("an authenticator's secret")
	sealed, err := s.Seal(plain, []byte("user-1"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, plain) {
		t.Fatal("the sealed value carries the plain text")
	}
	again, err := s.Seal(plain, []byte("user-1"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(sealed, again) {
		t.Fatal("sealing twice gave the same bytes; the nonce is not fresh")
	}
	opened, err := s.Open(sealed, []byte("user-1"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(opened, plain) {
		t.Fatalf("opened %q, sealed %q", opened, plain)
	}
	id, err := KeyIDOf(sealed)
	if err != nil || !bytes.Equal(id, s.KeyID()) {
		t.Fatalf("key id %x, %v; want %x", id, err, s.KeyID())
	}
}

func TestASealedValueRefusesAnotherKeyBindingOrEdit(t *testing.T) {
	s, _ := Parse(newKey(t))
	other, _ := Parse(newKey(t))
	sealed, err := s.Seal([]byte("secret"), []byte("user-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Open(sealed, []byte("user-1")); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("another key opened it: %v", err)
	}
	if _, err := s.Open(sealed, []byte("user-2")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("a value moved to another owner opened: %v", err)
	}
	edited := append([]byte(nil), sealed...)
	edited[len(edited)-1] ^= 1
	if _, err := s.Open(edited, []byte("user-1")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("an edited value opened: %v", err)
	}
	// A value relabelled with the other key's id still fails, since the
	// header is authenticated with the rest.
	relabelled := append([]byte(nil), sealed...)
	copy(relabelled[1:], other.KeyID())
	if _, err := other.Open(relabelled, []byte("user-1")); !errors.Is(err, ErrMalformed) {
		t.Fatalf("a relabelled value opened: %v", err)
	}
	if _, err := s.Open([]byte{1, 2}, nil); !errors.Is(err, ErrMalformed) {
		t.Fatalf("a short value: %v", err)
	}
}

func TestAKeyMustBeThirtyTwoBytesOfBase64(t *testing.T) {
	for _, bad := range []string{"", "not base64!", base64.StdEncoding.EncodeToString([]byte("too short"))} {
		if _, err := Parse(bad); !errors.Is(err, ErrBadKey) {
			t.Errorf("Parse(%q) = %v, want ErrBadKey", bad, err)
		}
	}
}
