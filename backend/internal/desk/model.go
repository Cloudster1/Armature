// Package desk is the service desk: what customers raise through the portal,
// how agents answer it, and how long that takes against what was promised.
//
// A request is an ordinary issue in a service project, raised by a customer
// through a request type. Everything the tracker already does to an issue
// applies; what the desk adds is the customer's view of it, the notes the
// customer does not see, and the clocks.
package desk

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
)

// RequestType is what a customer chooses from: a name in their words that
// becomes an issue of a type in ours, at a priority the desk decided.
type RequestType struct {
	ID            uuid.UUID      `json:"id"`
	ProjectID     uuid.UUID      `json:"projectId"`
	ProjectKey    string         `json:"projectKey"`
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	IssueTypeID   uuid.UUID      `json:"issueTypeId"`
	IssueTypeName string         `json:"issueTypeName"`
	Priority      issue.Priority `json:"priority"`
	Position      int            `json:"position"`
	// Category is the heading the type is offered under; empty is the desk's
	// general list.
	Category string `json:"category"`
	// DetailsTemplate is what the requester starts their details from.
	DetailsTemplate string `json:"detailsTemplate"`
	// TeamID is who the request lands with. Nil leaves it with the project.
	TeamID   *uuid.UUID `json:"teamId,omitempty"`
	TeamName string     `json:"teamName,omitempty"`
}

// Metric is what a policy measures.
type Metric string

const (
	// FirstResponse runs until an agent answers the customer.
	FirstResponse Metric = "first_response"
	// Resolution runs until the request reaches a done status.
	Resolution Metric = "resolution"
)

func (m Metric) Valid() bool { return m == FirstResponse || m == Resolution }

// Policy is a goal per priority for one metric in one project.
type Policy struct {
	ID         uuid.UUID `json:"id"`
	ProjectID  uuid.UUID `json:"projectId"`
	ProjectKey string    `json:"projectKey"`
	Name       string    `json:"name"`
	Metric     Metric    `json:"metric"`
	// Goals are minutes, keyed by priority. A priority with no goal is not
	// measured.
	Goals map[issue.Priority]int `json:"goals"`
	// PauseStatusIDs are the statuses in which the clock stops.
	PauseStatusIDs []uuid.UUID `json:"pauseStatusIds"`
	PauseStatuses  []string    `json:"pauseStatuses"`
	// UseCalendar makes the goal count only the project's business hours.
	UseCalendar bool `json:"useCalendar"`
}

// Timer is one policy applied to one request, and where its clock stands.
type Timer struct {
	ID          uuid.UUID `json:"id"`
	IssueID     uuid.UUID `json:"issueId"`
	PolicyID    uuid.UUID `json:"policyId"`
	PolicyName  string    `json:"policyName"`
	Metric      Metric    `json:"metric"`
	GoalMinutes int       `json:"goalMinutes"`
	StartedAt   time.Time `json:"startedAt"`
	// elapsedSeconds is what has been banked by pauses; RunningSince is set
	// while the clock is going. calendar is the project's hours when the
	// policy counts only those.
	elapsedSeconds int64
	calendar       *Calendar
	RunningSince   *time.Time `json:"runningSince,omitempty"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
	BreachedAt     *time.Time `json:"breachedAt,omitempty"`

	// BusinessHours says the goal counts only the desk's open hours.
	BusinessHours bool `json:"businessHours"`
	// The reading, taken when the timer was loaded.
	ElapsedSeconds   int64 `json:"elapsedSeconds"`
	RemainingSeconds int64 `json:"remainingSeconds"`
	Breached         bool  `json:"breached"`
	Paused           bool  `json:"paused"`
}

// Clock is the pure arithmetic of a timer, kept apart from the rows so it can
// be tested without a database. With a Calendar, the running stretch counts
// only the hours the desk is open.
type Clock struct {
	GoalMinutes    int
	ElapsedSeconds int64
	RunningSince   *time.Time
	CompletedAt    *time.Time
	Calendar       *Calendar
}

// Elapsed is how much of the goal has been used by now.
func (c Clock) Elapsed(now time.Time) time.Duration {
	elapsed := time.Duration(c.ElapsedSeconds) * time.Second
	if c.RunningSince != nil && c.CompletedAt == nil {
		elapsed += c.running(*c.RunningSince, now)
	}
	return elapsed
}

// running is how much of a stretch counts: all of it, or the open part.
func (c Clock) running(from, to time.Time) time.Duration {
	if !to.After(from) {
		return 0
	}
	if c.Calendar != nil {
		return c.Calendar.WorkingDuration(from, to)
	}
	return to.Sub(from)
}

// Remaining is what is left of the goal, negative once it is breached.
func (c Clock) Remaining(now time.Time) time.Duration {
	return time.Duration(c.GoalMinutes)*time.Minute - c.Elapsed(now)
}

// Breached reports whether the goal has been used up.
func (c Clock) Breached(now time.Time) bool { return c.Remaining(now) < 0 }

// Paused reports whether the clock is stopped without being finished.
func (c Clock) Paused() bool { return c.RunningSince == nil && c.CompletedAt == nil }

// clock is the timer's arithmetic, with the project's calendar when its
// policy counts working hours.
func (t *Timer) clock() Clock {
	return Clock{GoalMinutes: t.GoalMinutes, ElapsedSeconds: t.elapsedSeconds, RunningSince: t.RunningSince, CompletedAt: t.CompletedAt, Calendar: t.calendar}
}

// read fills the timer's reading from its clock.
func (t *Timer) read(now time.Time) {
	clock := t.clock()
	t.ElapsedSeconds = int64(clock.Elapsed(now) / time.Second)
	t.RemainingSeconds = int64(clock.Remaining(now) / time.Second)
	t.Breached = t.BreachedAt != nil || (t.CompletedAt == nil && clock.Breached(now))
	t.Paused = clock.Paused()
}

// QueueRow is one request as an agent's queue shows it: the issue and its
// clocks, the most pressing first.
type QueueRow struct {
	Issue  issue.Issue `json:"issue"`
	Timers []Timer     `json:"timers"`
	// RequestTypeName is empty for an issue an agent made directly.
	RequestTypeName string `json:"requestTypeName,omitempty"`
	// Csat is what the customer said of it, once resolved, 1 to 5.
	Csat *int `json:"csat,omitempty"`
}

// Request is a request as its customer sees it: the issue with only what a
// customer is told, and the conversation without the notes.
type Request struct {
	Issue    issue.Issue     `json:"issue"`
	Comments []issue.Comment `json:"comments"`
	// RequestTypeName is what they chose when raising it.
	RequestTypeName string `json:"requestTypeName,omitempty"`
}

// Desk is a service project as the portal offers it.
type Desk struct {
	ProjectID    uuid.UUID     `json:"projectId"`
	ProjectKey   string        `json:"projectKey"`
	Name         string        `json:"name"`
	Description  string        `json:"description,omitempty"`
	RequestTypes []RequestType `json:"requestTypes"`
	// RepliesByMail says the desk reads answers to its mails.
	RepliesByMail bool `json:"repliesByMail"`
}

var (
	// ErrNotFound is returned for a request type, policy or request that is
	// not in the caller's organization, or not the customer's to see.
	ErrNotFound = errors.New("not found")
	// ErrNameTaken is returned when the project already has that request type.
	ErrNameTaken = errors.New("this project already has a request type by that name")
	// ErrNotAServiceProject is returned when a desk operation names a project
	// that is not a service project.
	ErrNotAServiceProject = errors.New("that project is not a service desk")
	// ErrNotACustomer is returned when the portal is used by somebody who is
	// not a customer, which the agent side exists for.
	ErrNotACustomer = errors.New("the portal is for customers")
	// ErrNotYourRequest is returned when a follower tries what only the
	// reporter may do to a request.
	ErrNotYourRequest = errors.New("only the person who raised the request may do that")
	// ErrNotYourFile is returned when a customer removes a file somebody
	// else put on the request.
	ErrNotYourFile = errors.New("only the person who attached a file may remove it")
	// ErrTeamElsewhere is returned when a request type names a team of another
	// project.
	ErrTeamElsewhere = errors.New("That team belongs to another project. Pick one of this project's teams.")
)
