package arrange

import "github.com/google/uuid"

// Scope says who decided an arrangement, which is the difference between "this
// project was set up that way" and "nobody has said otherwise".
type Scope string

const (
	ScopeBuiltin      Scope = "builtin"
	ScopeOrganization Scope = "organization"
	ScopeProject      Scope = "project"
)

// Origin is where an answer came from. Named is true when the arrangement says
// this issue type by name rather than catching it with its fallback.
type Origin struct {
	Scope Scope `json:"scope"`
	Named bool  `json:"named"`
}

// Placement is one slot in one area. It is a field this tracker knows by name
// or one the project defined, never both.
type Placement struct {
	Area Area `json:"area"`
	Slot Slot `json:"slot,omitempty"`
	// FieldID names one of the project's own fields.
	FieldID *uuid.UUID `json:"fieldId,omitempty"`
}

// Arrangement is how one issue type is drawn here, and who decided it.
type Arrangement struct {
	IssueTypeID   uuid.UUID   `json:"issueTypeId"`
	IssueTypeName string      `json:"issueTypeName"`
	Origin        Origin      `json:"origin"`
	Places        []Placement `json:"places"`
}
