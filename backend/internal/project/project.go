// Package project owns projects: the containers issues live in, and the
// configuration they inherit.
package project

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// Kind separates the three product shapes a project can take. It decides which
// features a project offers, not how its issues behave.
type Kind string

const (
	KindSoftware Kind = "software"
	KindService  Kind = "service"
	KindBusiness Kind = "business"
)

func (k Kind) Valid() bool {
	switch k {
	case KindSoftware, KindService, KindBusiness:
		return true
	}
	return false
}

// Project is a container for issues.
type Project struct {
	ID          uuid.UUID  `json:"id"`
	Key         string     `json:"key"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Kind        Kind       `json:"kind"`
	LeadID      *uuid.UUID `json:"leadId,omitempty"`
	LeadName    string     `json:"leadName,omitempty"`
	// WorkflowSchemeID is the project's own scheme, or nil when it takes the
	// organization's. Nil is the ordinary case and the meaningful one: it says
	// the project has no opinion rather than that it happens to agree.
	WorkflowSchemeID *uuid.UUID `json:"workflowSchemeId,omitempty"`
	// Template is the key of the template the project was made from, or empty.
	// It says how the project began, not how it must stay.
	Template       string `json:"template,omitempty"`
	IssueCount     int    `json:"issueCount"`
	OpenIssueCount int    `json:"openIssueCount"`
	// Status is the latest status update, or nil when nobody has posted one.
	Status *StatusUpdate `json:"status,omitempty"`
	// PortalVerifies says whether the desk's door asks for a code by mail
	// before letting somebody in. Always true for anything but a desk.
	PortalVerifies bool `json:"portalVerifies"`
	// TrustedDomains are the address domains the desk takes requests from,
	// at the door and by mail. Empty means everyone; only a desk has any.
	TrustedDomains []string `json:"trustedDomains"`
	// Features are the pages the project has, decided by its template and
	// changed by its administrators. Issues, workflow, fields and settings
	// are not features: every project has them.
	Features   []Feature  `json:"features"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`
}

// IsArchived reports whether the project has been archived.
func (p *Project) IsArchived() bool { return p.ArchivedAt != nil }

var (
	// ErrNotFound is returned for a project that does not exist in the caller's
	// organization. A project in another organization is also not found:
	// existence itself is privileged.
	ErrNotFound = errors.New("project not found")
	// ErrKeyTaken is returned when the key is already in use.
	ErrKeyTaken = errors.New("that project key is already in use")
	// ErrArchived is returned when writing to an archived project.
	ErrArchived = errors.New("this project is archived")
	// ErrNotADesk is returned when a software project is told to leave its door open.
	ErrNotADesk = errors.New("only a service desk has a door to leave open")
)

// keyShape matches the database constraint: two to ten characters, starting
// with a letter, upper case letters and digits only. Short, because it prefixes
// every issue key a person will ever type or read.
var keyShape = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)

// ValidKey reports whether a key is usable.
func ValidKey(key string) bool { return keyShape.MatchString(key) }

// SuggestKey derives a project key from a name, the way a person would:
// initials for a multi-word name, the leading letters for a single word.
//
// It is a suggestion only. The caller checks availability, and the user can
// always type their own.
func SuggestKey(name string) string {
	words := strings.FieldsFunc(strings.ToUpper(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	// Keep only plain ASCII letters and digits, since the key constraint allows
	// nothing else.
	clean := make([]string, 0, len(words))
	for _, w := range words {
		var b strings.Builder
		for _, r := range w {
			if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			}
		}
		if b.Len() > 0 {
			clean = append(clean, b.String())
		}
	}
	if len(clean) == 0 {
		return ""
	}

	var key string
	if len(clean) == 1 {
		key = clean[0]
	} else {
		var b strings.Builder
		for _, w := range clean {
			b.WriteByte(w[0])
		}
		key = b.String()
	}

	if len(key) > 10 {
		key = key[:10]
	}
	// A key must start with a letter, so a name beginning with a digit needs
	// the user to choose one themselves.
	if key == "" || key[0] < 'A' || key[0] > 'Z' {
		return ""
	}
	// Two characters is the minimum; pad a single letter rather than returning
	// something the constraint would reject.
	if len(key) == 1 {
		key += key
	}
	return key
}

// NormalizeKey upper-cases and trims a key the user typed.
func NormalizeKey(key string) string { return strings.ToUpper(strings.TrimSpace(key)) }
