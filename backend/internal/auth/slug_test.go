package auth

import "testing"

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Acme Inc", "acme-inc"},
		{"  Globex  Corporation  ", "globex-corporation"},
		{"Foo & Bar, Ltd.", "foo-bar-ltd"},
		{"already-a-slug", "already-a-slug"},
		{"UPPER CASE", "upper-case"},
		{"---leading and trailing---", "leading-and-trailing"},
		{"multiple   spaces", "multiple-spaces"},
		{"a very long organization name that runs past the forty character limit", "a-very-long-organization-name-that-runs"},
	}
	for _, tt := range tests {
		if got := Slugify(tt.in); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Slugify must never produce something the database constraint would reject,
// because that turns a user's odd company name into a 500.
func TestSlugifyOutputSatisfiesConstraint(t *testing.T) {
	for _, in := range []string{
		"Acme Inc",
		"Foo & Bar, Ltd.",
		"a very long organization name that runs past the forty character limit",
		"Ünïcödé Nämes",
		"123 Numbers",
	} {
		got := Slugify(in)
		if got == "" {
			continue // caller must ask for an explicit slug
		}
		if !ValidSlug(got) {
			t.Errorf("Slugify(%q) = %q, which the org_slug_shape constraint rejects", in, got)
		}
	}
}

func TestValidSlug(t *testing.T) {
	valid := []string{"acme", "acme-inc", "a1b", "x-y-z", "a234567890123456789012345678901234567890"}
	invalid := []string{"", "ab", "-acme", "acme-", "Acme", "acme inc", "acme_inc", "a2345678901234567890123456789012345678901"}
	for _, s := range valid {
		if !ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = false, want true", s)
		}
	}
	for _, s := range invalid {
		if ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = true, want false", s)
		}
	}
}
