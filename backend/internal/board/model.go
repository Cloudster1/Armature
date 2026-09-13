// Package board arranges a project's issues into swimlanes.
//
// A swimlane is defined entirely by the ticket states that belong to it: a card
// appears in the swimlane that claims its status. That makes the board a view
// of the workflow rather than a second, parallel notion of progress, and it is
// why dragging a card is a workflow transition rather than a field edit.
package board

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/workflow"
)

// Grouping is the secondary axis a board can be read along, shown as rows
// within each swimlane.
type Grouping string

const (
	GroupNone     Grouping = "none"
	GroupAssignee Grouping = "assignee"
	GroupPriority Grouping = "priority"
	GroupType     Grouping = "type"
)

func (g Grouping) Valid() bool {
	switch g {
	case GroupNone, GroupAssignee, GroupPriority, GroupType:
		return true
	}
	return false
}

// Type is how a board decides which of its work to show.
//
// The type is behaviour, not a label: a scrum board draws the sprint that is
// running in its stream and nothing else, so planning happens in the backlog
// and the board is the commitment. A kanban board draws everything in its scope
// and relies on swimlane limits to say when too much is in flight.
type Type string

const (
	TypeScrum  Type = "scrum"
	TypeKanban Type = "kanban"
)

func (t Type) Valid() bool {
	switch t {
	case TypeScrum, TypeKanban:
		return true
	}
	return false
}

// SprintRef is the sprint a board is showing, with enough of it to head the
// board: the running one on a scrum board, or the one a board was asked for.
// It is nil on a kanban board and on a scrum board between sprints.
type SprintRef struct {
	ID     uuid.UUID  `json:"id"`
	Name   string     `json:"name"`
	Goal   string     `json:"goal,omitempty"`
	EndsOn *time.Time `json:"endsOn,omitempty"`
}

// Board is a project's board.
type Board struct {
	ID          uuid.UUID `json:"id"`
	ProjectID   uuid.UUID `json:"projectId"`
	ProjectKey  string    `json:"projectKey"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Type        Type      `json:"type"`
	GroupBy     Grouping  `json:"groupBy"`
	// Sprint is what the board is showing: the running sprint on a scrum
	// board, or the sprint it was opened for. Nil on a scrum board means
	// nothing is running, which the client says out loud.
	Sprint *SprintRef `json:"sprint,omitempty"`
	// TeamID scopes the board to one team's work. Nil is a board over the whole
	// project, which is what every project starts with.
	TeamID    *uuid.UUID `json:"teamId,omitempty"`
	TeamName  string     `json:"teamName,omitempty"`
	Swimlanes []Swimlane `json:"swimlanes"`
	// Unmapped holds cards whose status no swimlane claims. They would
	// otherwise vanish from the board, which is the worst possible outcome for
	// a misconfigured mapping.
	Unmapped  []Card    `json:"unmapped"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Summary is a board without its cards, for the picker that chooses between
// them. Counting the cards would mean loading them, which is the expensive part.
type Summary struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Type        Type       `json:"type"`
	TeamID      *uuid.UUID `json:"teamId,omitempty"`
	TeamName    string     `json:"teamName,omitempty"`
	Swimlanes   int        `json:"swimlaneCount"`
}

// Swimlane is a named group of statuses, and the cards currently in them.
type Swimlane struct {
	ID       uuid.UUID         `json:"id"`
	Name     string            `json:"name"`
	Position int               `json:"position"`
	WIPLimit int               `json:"wipLimit"`
	Statuses []workflow.Status `json:"statuses"`
	Cards    []Card            `json:"cards"`
}

// OverWIP reports whether the swimlane holds more cards than its limit allows.
// The limit is advisory: a board that refuses to show reality is less useful
// than one that shows the team it has overcommitted.
func (s *Swimlane) OverWIP() bool { return s.WIPLimit > 0 && len(s.Cards) > s.WIPLimit }

// Card is an issue as it appears on a board: enough to render, no more.
type Card struct {
	ID        uuid.UUID       `json:"id"`
	Key       string          `json:"key"`
	Summary   string          `json:"summary"`
	Type      issue.TypeRef   `json:"type"`
	Status    workflow.Status `json:"status"`
	Priority  issue.Priority  `json:"priority"`
	Assignee  *issue.UserRef  `json:"assignee,omitempty"`
	ParentKey string          `json:"parentKey,omitempty"`
	// Parent is the epic or initiative this card belongs to, which is the one
	// piece of context a card is unreadable without on a busy board.
	Parent *issue.ParentRef `json:"parent,omitempty"`
	Labels []issue.LabelRef `json:"labels"`
	Rank   string           `json:"rank"`
	// Group is the value this card is grouped by, when the board groups its
	// rows. Empty when the board does not group.
	Group     string    `json:"group,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

var (
	// ErrNotFound is returned for a board or swimlane that does not exist in
	// the caller's organization.
	ErrNotFound = errors.New("board not found")
	// ErrSwimlaneNotFound is returned for a swimlane that is not on the board.
	ErrSwimlaneNotFound = errors.New("swimlane not found")
	// ErrNotOnThisBoard is returned when a move names a card or a lane that
	// belongs to another project.
	ErrNotOnThisBoard = errors.New("that is not on this board")
	// ErrSprintNotFound is returned when opening the board of a sprint that
	// does not exist in the caller's organization.
	ErrSprintNotFound = errors.New("sprint not found")
	// ErrNameTaken is returned when a project already has a board by that name.
	ErrNameTaken = errors.New("that project already has a board with this name")
	// ErrLastBoard is returned when deleting the only board a project has.
	ErrLastBoard = errors.New("a project keeps at least one board")
	// ErrTeamNotFound is returned for a team that is not in this project.
	ErrTeamNotFound = errors.New("that team is not in this project")
	// ErrBadType is returned for a board type that is neither scrum nor kanban.
	ErrBadType = errors.New("a board is either scrum or kanban")
	// ErrStatusClaimed is returned when assigning a status that another
	// swimlane on the same board already holds.
	ErrStatusClaimed = errors.New("that status is already in another swimlane")
	// ErrNoTransition is returned when a card cannot move to a swimlane
	// because the workflow has no way to get there from where it is.
	ErrNoTransition = errors.New("the workflow does not allow that move")
	// ErrLastSwimlane is returned when deleting the only swimlane, which would
	// leave a board that cannot show anything.
	ErrLastSwimlane = errors.New("a board needs at least one swimlane")
)
