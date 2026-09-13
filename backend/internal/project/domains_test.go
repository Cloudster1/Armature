package project

import (
	"errors"
	"slices"
	"testing"
)

func TestNormalizeDomainsMakesOneSpellingOfEachDomain(t *testing.T) {
	got, err := NormalizeDomains([]string{" @Acme.TEST ", "acme.test", "", "beta.example.org"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"acme.test", "beta.example.org"}) {
		t.Fatalf("got %v", got)
	}
	for _, bad := range []string{"not a domain", "acme", "-acme.test", "acme.test.", "a@b.test"} {
		if _, err := NormalizeDomains([]string{bad}); !errors.Is(err, ErrBadDomain) {
			t.Errorf("%q was accepted: %v", bad, err)
		}
	}
	if got, err := NormalizeDomains(nil); err != nil || len(got) != 0 {
		t.Fatalf("nil = %v %v", got, err)
	}
}

func TestTrustsIsEveryoneUntilAListSaysOtherwise(t *testing.T) {
	if !Trusts(nil, "anyone@anywhere.test") {
		t.Fatal("an empty list should trust everyone")
	}
	list := []string{"acme.test"}
	if !Trusts(list, "Ann@ACME.test") || Trusts(list, "bob@other.test") || Trusts(list, "no-at-sign") {
		t.Fatal("the list was not followed")
	}
	if DomainOf("a@b@c.test") != "c.test" || DomainOf("plain") != "" {
		t.Fatal("DomainOf")
	}
	var err error = &NotTrustedError{Domains: list}
	if !errors.Is(err, ErrDomainNotTrusted) {
		t.Fatal("NotTrustedError should match ErrDomainNotTrusted")
	}
}
