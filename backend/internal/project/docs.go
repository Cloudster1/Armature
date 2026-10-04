package project

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// The bounds of a documentation link, which the database holds as well.
const (
	MaxDocsURL   = 2048
	MaxDocsLabel = 60
)

var (
	// ErrBadDocsURL is returned for a documentation address that is not a web page.
	ErrBadDocsURL = errors.New("that is not a web address")
	// ErrBadDocsLabel is returned for a label too long, or one without an address.
	ErrBadDocsLabel = errors.New("that label cannot be used")
)

// NormalizeDocsLink trims the address and label and checks them. An empty
// address clears the link, label and all; an empty label leaves it unnamed.
func NormalizeDocsLink(rawURL, rawLabel string) (docsURL, label *string, err error) {
	u, l := strings.TrimSpace(rawURL), strings.TrimSpace(rawLabel)
	if u == "" {
		if l != "" {
			return nil, nil, fmt.Errorf("%w: give the documentation's address first; the label names it", ErrBadDocsLabel)
		}
		return nil, nil, nil
	}
	if len([]rune(u)) > MaxDocsURL {
		return nil, nil, fmt.Errorf("%w: shorten the address to %d characters or fewer", ErrBadDocsURL, MaxDocsURL)
	}
	parsed, perr := url.Parse(u)
	if perr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || strings.ContainsAny(u, " \t\r\n") {
		return nil, nil, fmt.Errorf("%w: write an address starting with http:// or https://", ErrBadDocsURL)
	}
	if len([]rune(l)) > MaxDocsLabel {
		return nil, nil, fmt.Errorf("%w: shorten the label to %d characters or fewer", ErrBadDocsLabel, MaxDocsLabel)
	}
	if l == "" {
		return &u, nil, nil
	}
	return &u, &l, nil
}
