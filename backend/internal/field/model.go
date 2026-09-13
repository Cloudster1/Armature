// Package field owns custom fields: what a project wants to record about its
// issues beyond what every tracker records.
package field

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Kind is what shape a field's values take. It is text in the database so that
// adding a kind is code, not a migration; the service refuses one it does not
// know.
type Kind string

const (
	Text     Kind = "text"
	Number   Kind = "number"
	Date     Kind = "date"
	Select   Kind = "select"
	Checkbox Kind = "checkbox"
	URL      Kind = "url"
)

// KindInfo describes a kind for the client, which draws the control from it.
type KindInfo struct {
	Kind        Kind   `json:"kind"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// HasOptions says the field is defined with a list of choices.
	HasOptions bool `json:"hasOptions"`
}

// kinds is the one list every kind lives in: the chooser, the validator and the
// tests all read it.
var kinds = []KindInfo{
	{Text, "Text", "A line of text, such as a customer's name or a release.", false},
	{Number, "Number", "A number, such as a cost or a count.", false},
	{Date, "Date", "A calendar day.", false},
	{Select, "Select", "One choice from a list this field defines.", true},
	{Checkbox, "Checkbox", "Yes or no.", false},
	{URL, "Link", "An address, shown as a link.", false},
}

// Kinds lists what a field can be.
func Kinds() []KindInfo { return append([]KindInfo(nil), kinds...) }

// Valid reports whether the kind is one the service knows.
func (k Kind) Valid() bool {
	for _, info := range kinds {
		if info.Kind == k {
			return true
		}
	}
	return false
}

// Field is one custom field, defined for one project or, with no project, for
// every project of the organization.
type Field struct {
	ID         uuid.UUID  `json:"id"`
	ProjectID  *uuid.UUID `json:"projectId,omitempty"`
	ProjectKey string     `json:"projectKey,omitempty"`
	// Org says the field belongs to the whole organization.
	Org  bool   `json:"org"`
	Name string `json:"name"`
	Kind Kind   `json:"kind"`
	// Options are the choices a select field offers, in the order shown.
	Options   []string  `json:"options"`
	Position  int       `json:"position"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Value is one issue's answer to one field, alongside the field so that a
// client can draw the control without a second request. A nil Value is unset.
type Value struct {
	Field Field           `json:"field"`
	Value json.RawMessage `json:"value,omitempty"`
	// Display is the value as the changelog and a table would show it.
	Display string `json:"display,omitempty"`
}

var (
	// ErrNotFound is returned for a field that is not in the caller's
	// organization.
	ErrNotFound = errors.New("field not found")
	// ErrNameTaken is returned when the project already has a field by that
	// name.
	ErrNameTaken = errors.New("a field by that name already exists here; a name means one field across the organization")
	// ErrBadKind is returned for a kind the service does not know.
	ErrBadKind = errors.New("that is not a kind of field")
	// ErrBadValue is returned when a value does not fit the field's kind. It is
	// wrapped with what would have fit.
	ErrBadValue = errors.New("that value does not fit this field")
	// ErrOtherProject is returned when a field is set on an issue in a project
	// the field was not defined for.
	ErrOtherProject = errors.New("that field belongs to another project")
	// ErrKindMismatch is a promotion that would merge fields of different kinds.
	ErrKindMismatch = errors.New("another project has a field by that name of a different kind; rename one of them first")
)
