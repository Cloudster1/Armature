// Package team owns the groups of people inside a project, and the boards,
// backlogs and sprints that belong to them rather than to the project at large.
package team

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Team is a group of people inside one project.
type Team struct {
	ID          uuid.UUID `json:"id"`
	ProjectID   uuid.UUID `json:"projectId"`
	ProjectKey  string    `json:"projectKey"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Position    int       `json:"position"`

	// Members are filled in when a team is read on its own, and left out of
	// listings, which are read constantly and rarely need them.
	Members []Member `json:"members,omitempty"`
	// MemberCount is always filled in, because "who is on it" is the first
	// question anybody asks of a team.
	MemberCount int `json:"memberCount"`
	// Issues and Boards say what the team is carrying, so a listing can show
	// which teams are real and which are aspirational.
	Issues int `json:"issueCount"`
	Boards int `json:"boardCount"`
	// WeeklyCapacity is how much the team can take on in a week, in the unit
	// issues are estimated in. Nil is "has not said", which the plan reads
	// differently from zero.
	WeeklyCapacity *float64 `json:"weeklyCapacity,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Member is a person on a team.
type Member struct {
	UserID uuid.UUID `json:"userId"`
	Name   string    `json:"name"`
	Email  string    `json:"email,omitempty"`
	// Lead is a person to ask, not a permission. Nothing is refused on it.
	Lead     bool      `json:"lead"`
	JoinedAt time.Time `json:"joinedAt"`
}

// Ref is the shape a team takes when it hangs off an issue or a board.
type Ref struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

var (
	// ErrNotFound is returned for a team that does not exist in the caller's
	// organization, or in the project being asked about.
	ErrNotFound = errors.New("team not found")
	// ErrNameTaken is returned when a project already has a team by that name.
	ErrNameTaken = errors.New("that project already has a team with this name")
	// ErrNegativeCapacity is returned for a weekly capacity below zero.
	ErrNegativeCapacity = errors.New("a team's weekly capacity cannot be negative")
	// ErrNotAMember is returned when adding somebody who is not in the
	// organization, or is only a portal customer. Joining a team is never a way
	// to gain access to something.
	ErrNotAMember = errors.New("that person is not a member of this organization")
	// ErrInUse is returned when deleting a team that still carries work.
	ErrInUse = errors.New("that team still has work")
)
