package auth

import (
	"regexp"
	"strings"
	"unicode"
)

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify turns an organization name into a candidate URL slug matching the
// org_slug_shape constraint in the schema: lowercase alphanumerics and internal
// hyphens, 3 to 40 characters.
func Slugify(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r < unicode.MaxASCII:
			b.WriteRune(r)
		default:
			// Transliterating every script properly is out of scope; drop what
			// we cannot represent and let the caller supply an explicit slug.
			b.WriteRune('-')
		}
	}
	s := nonSlug.ReplaceAllString(b.String(), "-")
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	return s
}

// ValidSlug reports whether s satisfies the database constraint, so that a bad
// slug is a 400 from the handler rather than a constraint violation from
// Postgres.
var slugShape = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)

func ValidSlug(s string) bool { return slugShape.MatchString(s) }
