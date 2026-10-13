package auth

import "testing"

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
	a := codeDigest("ada@example.com", "123456")
	if !EqualDigest(a, codeDigest("ada@example.com", "123456")) {
		t.Error("the same code for the same address should digest the same")
	}
	if EqualDigest(a, codeDigest("ben@example.com", "123456")) {
		t.Error("the same code for another address should not")
	}
	if EqualDigest(a, codeDigest("ada@example.com", "654321")) {
		t.Error("nor another code for the same address")
	}
}

func TestARequesterIsNamedByTheLocalPart(t *testing.T) {
	if got := localPart("ada.lovelace@example.com"); got != "ada.lovelace" {
		t.Errorf("localPart = %q", got)
	}
}
