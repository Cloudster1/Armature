package auth

import (
	"testing"

	"github.com/google/uuid"
)

func TestParseAddressTidiesAndRefuses(t *testing.T) {
	if got, err := ParseAddress("  Ada@Example.COM "); err != nil || got != "ada@example.com" {
		t.Errorf("ParseAddress = %q, %v", got, err)
	}
	for _, bad := range []string{"", "ada", "Ada <ada@example.com>", "ada@", "@example.com"} {
		if _, err := ParseAddress(bad); err == nil {
			t.Errorf("%q was accepted as an address", bad)
		}
	}
}

func TestCodeDigestIsBoundToTheAddress(t *testing.T) {
	org := uuid.New()
	a := codeDigest(org, "ada@example.com", "123456")
	if !EqualDigest(a, codeDigest(org, "ada@example.com", "123456")) {
		t.Error("the same code for the same address should digest the same")
	}
	if EqualDigest(a, codeDigest(org, "ben@example.com", "123456")) {
		t.Error("the same code for another address should not")
	}
	if EqualDigest(a, codeDigest(uuid.New(), "ada@example.com", "123456")) {
		t.Error("nor for another organization")
	}
}

func TestARequesterIsNamedByTheLocalPart(t *testing.T) {
	if got := localPart("ada.lovelace@example.com"); got != "ada.lovelace" {
		t.Errorf("localPart = %q", got)
	}
}
