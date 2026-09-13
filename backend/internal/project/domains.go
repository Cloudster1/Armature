package project

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// A desk trusts domains, not addresses: one list decides who may ask for a
// code, walk through an open door, be offered the desk, and reply by mail.

var (
	// ErrBadDomain is returned for a trusted domain that is not written as one.
	ErrBadDomain = errors.New("that is not a domain")
	// ErrDomainNotTrusted is what errors.Is matches; the value is a *NotTrustedError.
	ErrDomainNotTrusted = errors.New("this desk does not take requests from that domain")
)

// NotTrustedError says which domains would have been let in, so the refusal
// can name them.
type NotTrustedError struct {
	Domains []string
}

func (e *NotTrustedError) Error() string {
	return ErrDomainNotTrusted.Error() + ": " + strings.Join(e.Domains, ", ")
}

func (e *NotTrustedError) Is(target error) bool { return target == ErrDomainNotTrusted }

// domainShape is the database's rule, kept identical so the service and the
// constraint refuse the same spellings.
var domainShape = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// NormalizeDomains lowercases, strips a leading @, drops blanks and doubles,
// and sorts, so the stored list is the one spelling of itself.
func NormalizeDomains(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	for _, each := range raw {
		domain := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(each), "@"))
		if domain == "" {
			continue
		}
		if !domainShape.MatchString(domain) {
			return nil, fmt.Errorf("%w: %q", ErrBadDomain, each)
		}
		if !slices.Contains(out, domain) {
			out = append(out, domain)
		}
	}
	slices.Sort(out)
	return out, nil
}

// DomainOf is the part of an address after its last @, lowercased; "" when
// there is none.
func DomainOf(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[at+1:]))
}

// Trusts says whether an address is at one of the domains; an empty list
// trusts everyone.
func Trusts(domains []string, email string) bool {
	return len(domains) == 0 || slices.Contains(domains, DomainOf(email))
}
