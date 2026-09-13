// Package sprint owns sprints: the named stretches of time a team commits work
// to, and the capacity that says how much work that is.
package sprint

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// State is where a sprint is in its life. A sprint only ever moves forwards.
type State string

const (
	// StateFuture is a sprint being planned. Work can be moved in and out
	// freely and it has no effect on anything yet.
	StateFuture State = "future"
	// StateActive is the one sprint a team is working in. There is at most one
	// per project, so "the sprint" is never ambiguous.
	StateActive State = "active"
	// StateClosed is a sprint that has been completed and reported on.
	StateClosed State = "closed"
)

func (s State) Valid() bool {
	switch s {
	case StateFuture, StateActive, StateClosed:
		return true
	}
	return false
}

// Sprint is a stretch of time with work committed to it.
type Sprint struct {
	ID         uuid.UUID `json:"id"`
	ProjectID  uuid.UUID `json:"projectId"`
	ProjectKey string    `json:"projectKey"`
	// TeamID is whose sprint this is. Nil is the project's own, for work no
	// team has taken on.
	TeamID   *uuid.UUID `json:"teamId,omitempty"`
	TeamName string     `json:"teamName,omitempty"`

	Name  string `json:"name"`
	Goal  string `json:"goal,omitempty"`
	State State  `json:"state"`

	// StartsOn and EndsOn are optional while a sprint is being sketched, and
	// required before it can be started.
	StartsOn *time.Time `json:"startsOn,omitempty"`
	EndsOn   *time.Time `json:"endsOn,omitempty"`

	// Capacity is how much the team can take on, in the same unit as an issue's
	// estimate. Nil means nobody has said, which reads differently from zero.
	Capacity *float64 `json:"capacity,omitempty"`
	Position int      `json:"position"`

	StartedAt   *time.Time `json:"startedAt,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`

	// Committed and Completed are what the sprint turned out to be, written
	// once when it closes so that later edits cannot rewrite its history.
	// Finished and Carried count its issues the same way: done, and moved on.
	Committed *float64 `json:"committed,omitempty"`
	Completed *float64 `json:"completed,omitempty"`
	Finished  *int     `json:"finished,omitempty"`
	Carried   *int     `json:"carried,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Running reports whether this is the sprint the team is working in.
func (s *Sprint) Running() bool { return s.State == StateActive }

// Planned reports whether work can still be moved in and out freely.
func (s *Sprint) Planned() bool { return s.State == StateFuture }

// Ref is the shape a sprint takes when it hangs off an issue.
type Ref struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	State State     `json:"state"`
}

var (
	// ErrNotFound is returned for a sprint that does not exist in the caller's
	// organization.
	ErrNotFound = errors.New("sprint not found")
	// ErrNotStartable is returned when a sprint cannot begin: it has no dates,
	// it has already run, or another sprint is running in the same project.
	ErrNotStartable = errors.New("that sprint cannot be started")
	// ErrNotRunning is returned when completing a sprint that is not running.
	ErrNotRunning = errors.New("that sprint is not running")
	// ErrClosed is returned when changing a sprint that has already been
	// completed. Its report has been written and will not be rewritten.
	ErrClosed = errors.New("that sprint is over")
	// ErrTeamNotFound is returned for a team that is not in this project.
	ErrTeamNotFound = errors.New("that team is not in this project")
)
