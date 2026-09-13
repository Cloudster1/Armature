// Package milestone owns milestones: the named points a project works towards,
// each reached when the issues assigned to it are done.
package milestone

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Milestone is a point a project is working towards.
type Milestone struct {
	ID         uuid.UUID `json:"id"`
	ProjectID  uuid.UUID `json:"projectId"`
	ProjectKey string    `json:"projectKey"`

	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// DueOn is when the milestone should be reached; nil while it is an
	// intention rather than a date.
	DueOn *time.Time `json:"dueOn,omitempty"`
	// ClosedAt is set once the milestone has been declared reached or dropped.
	ClosedAt *time.Time `json:"closedAt,omitempty"`
	Position int        `json:"position"`

	// Progress is read off the assigned issues every time, never stored.
	Progress Progress `json:"progress"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Open reports whether work can still be assigned to the milestone.
func (m *Milestone) Open() bool { return m.ClosedAt == nil }

// Progress is how far a milestone has got, counted by the status category of
// the issues assigned to it. Boards and reports key on the same categories, so
// "done" here means the same thing it means everywhere else.
type Progress struct {
	Issues     int `json:"issues"`
	Done       int `json:"done"`
	InProgress int `json:"inProgress"`
	Todo       int `json:"todo"`
	// Percent is done over everything, rounded down. A milestone with nothing
	// assigned is at zero: nothing has been reached until something was asked.
	Percent int `json:"percent"`
}

// Measure derives the progress from the three counts.
func Measure(done, inProgress, todo int) Progress {
	issues := done + inProgress + todo
	p := Progress{Issues: issues, Done: done, InProgress: inProgress, Todo: todo}
	if issues > 0 {
		p.Percent = done * 100 / issues
	}
	return p
}

// Ref is the shape a milestone takes when it hangs off an issue.
type Ref struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

var (
	// ErrNotFound is returned for a milestone that does not exist in the
	// caller's organization.
	ErrNotFound = errors.New("milestone not found")
	// ErrClosed is returned when changing a milestone that has been closed.
	ErrClosed = errors.New("that milestone is closed")
	// ErrOpen is returned when reopening a milestone that is not closed.
	ErrOpen = errors.New("that milestone is not closed")
	// ErrDuplicateName is returned when a project already has a milestone by
	// that name.
	ErrDuplicateName = errors.New("this project already has a milestone by that name")
)
