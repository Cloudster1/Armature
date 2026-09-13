// Package issue owns issues: the unit of work everything else in the product
// hangs off.
package issue

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/workflow"
)

// Priority is how urgent an issue is.
type Priority string

const (
	PriorityLowest  Priority = "lowest"
	PriorityLow     Priority = "low"
	PriorityMedium  Priority = "medium"
	PriorityHigh    Priority = "high"
	PriorityHighest Priority = "highest"
)

func (p Priority) Valid() bool {
	switch p {
	case PriorityLowest, PriorityLow, PriorityMedium, PriorityHigh, PriorityHighest:
		return true
	}
	return false
}

// UserRef is the shape a person takes when attached to an issue.
// userRef builds the reference a row gives about a person, picture included.
func userRef(id uuid.UUID, name, email, avatar *string) *UserRef {
	ref := &UserRef{ID: id}
	if name != nil {
		ref.Name = *name
	}
	if email != nil {
		ref.Email = *email
	}
	if avatar != nil {
		ref.AvatarURL = *avatar
	}
	return ref
}

type UserRef struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email,omitempty"`
	// AvatarURL is where the picture is served from; empty means initials.
	AvatarURL string `json:"avatarUrl,omitempty"`
}

// TypeRef is an issue type as it appears on an issue.
type TypeRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Icon string    `json:"icon"`
	// Level is where this type sits in the hierarchy; see hierarchy.go.
	Level int `json:"level"`
	// IsSubtask is a reading of the level, not a second opinion about it.
	IsSubtask bool `json:"isSubtask"`
}

// SprintRef is the shape a sprint takes when it hangs off an issue. The issue
// package names it rather than importing the sprint package, so that sprints
// can be built on top of issues without the dependency turning back on itself.
type SprintRef struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	State string    `json:"state"`
}

// MilestoneRef is the shape a milestone takes when it hangs off an issue,
// named here for the same reason SprintRef is.
type MilestoneRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// TeamRef is the shape a team takes when it hangs off an issue, named here for
// the same reason SprintRef is: so that teams can be built on top of issues
// without the dependency turning back on itself.
type TeamRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// ParentRef is the issue above this one, in as much detail as a breadcrumb, a
// card or a header needs without a second request.
type ParentRef struct {
	ID      uuid.UUID `json:"id"`
	Key     string    `json:"key"`
	Summary string    `json:"summary"`
	Type    TypeRef   `json:"type"`
}

// Issue is one unit of work.
type Issue struct {
	ID   uuid.UUID `json:"id"`
	Key  string    `json:"key"`
	Type TypeRef   `json:"type"`

	ProjectID  uuid.UUID `json:"projectId"`
	ProjectKey string    `json:"projectKey"`

	Summary     string          `json:"summary"`
	Description json.RawMessage `json:"description,omitempty"`
	Status      workflow.Status `json:"status"`
	Priority    Priority        `json:"priority"`

	Assignee *UserRef `json:"assignee,omitempty"`
	Reporter *UserRef `json:"reporter,omitempty"`

	ParentID  *uuid.UUID `json:"parentId,omitempty"`
	ParentKey string     `json:"parentKey,omitempty"`
	Parent    *ParentRef `json:"parent,omitempty"`

	// StartDate and DueDate are the two ends of the stretch of time an issue
	// occupies. Either may be missing while a plan is still a sketch.
	StartDate *time.Time `json:"startDate,omitempty"`
	DueDate   *time.Time `json:"dueDate,omitempty"`

	// SprintID is the sprint this is committed to; nil is the backlog, which is
	// work that exists but nobody has taken on yet.
	SprintID *uuid.UUID `json:"sprintId,omitempty"`
	Sprint   *SprintRef `json:"sprint,omitempty"`
	// TeamID is the team carrying this, and nil is work the project has not
	// handed to anyone in particular.
	TeamID *uuid.UUID `json:"teamId,omitempty"`
	Team   *TeamRef   `json:"team,omitempty"`
	// MilestoneID is the milestone this counts towards; nil is work no
	// milestone is waiting on.
	MilestoneID *uuid.UUID    `json:"milestoneId,omitempty"`
	Milestone   *MilestoneRef `json:"milestone,omitempty"`
	// RequestTypeID is set on an issue raised through the customer portal.
	RequestTypeID   *uuid.UUID `json:"requestTypeId,omitempty"`
	RequestTypeName string     `json:"requestTypeName,omitempty"`
	// Estimate is how much work this is, in whatever unit the team counts in.
	// Nil is unestimated, which is not the same as an estimate of zero.
	Estimate *float64 `json:"estimate,omitempty"`

	// TimeEstimateMinutes is how long this was thought to take, and
	// TimeRemainingMinutes how long is thought to remain; TimeSpentMinutes is
	// the sum of the work logged and is never stored on its own.
	TimeEstimateMinutes  *int `json:"timeEstimateMinutes,omitempty"`
	TimeRemainingMinutes *int `json:"timeRemainingMinutes,omitempty"`
	TimeSpentMinutes     int  `json:"timeSpentMinutes"`

	// Labels are the organization's words attached to this issue.
	Labels []LabelRef `json:"labels"`
	// FixVersions is what ships this; AffectsVersions is where it was found.
	FixVersions     []VersionRef `json:"fixVersions"`
	AffectsVersions []VersionRef `json:"affectsVersions"`
	// Components are the project's parts this belongs to.
	Components []ComponentRef `json:"components"`

	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`

	// ChildCount and CommentCount are filled in on the detail view only.
	ChildCount   int `json:"childCount,omitempty"`
	CommentCount int `json:"commentCount,omitempty"`
}

// LabelRef is a label as it appears on an issue.
type LabelRef struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Color string    `json:"color"`
}

// Worklog is one stretch of somebody's time on an issue.
type Worklog struct {
	ID        uuid.UUID `json:"id"`
	IssueID   uuid.UUID `json:"issueId"`
	Author    *UserRef  `json:"author,omitempty"`
	Minutes   int       `json:"minutes"`
	StartedOn time.Time `json:"startedOn"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// IsDone reports whether the issue sits in a done status.
func (i *Issue) IsDone() bool { return i.Status.Category == workflow.CategoryDone }

// Comment is a note on an issue.
type Comment struct {
	ID      uuid.UUID       `json:"id"`
	IssueID uuid.UUID       `json:"issueId"`
	Author  *UserRef        `json:"author,omitempty"`
	Body    json.RawMessage `json:"body"`
	// Internal marks a note between agents, which a customer is never shown.
	Internal  bool       `json:"internal"`
	CreatedAt time.Time  `json:"createdAt"`
	EditedAt  *time.Time `json:"editedAt,omitempty"`
}

// Change is one field's movement, as recorded in the changelog.
type Change struct {
	Field string `json:"field"`
	// From and To are display strings: the changelog is read by people, and
	// resolving ids to names months later is both slow and often impossible
	// once the referenced row is gone.
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// HistoryEntry is one recorded moment in an issue's life.
type HistoryEntry struct {
	ID        uuid.UUID `json:"id"`
	IssueID   uuid.UUID `json:"issueId"`
	Actor     *UserRef  `json:"actor,omitempty"`
	Changes   []Change  `json:"changes"`
	CreatedAt time.Time `json:"createdAt"`
}

// Link is a relationship between two issues, seen from one end.
type Link struct {
	ID uuid.UUID `json:"id"`
	// Direction says whether this issue is the source ("outward") or the
	// target ("inward") of the relationship, which decides which phrase to use.
	Direction string    `json:"direction"`
	TypeID    uuid.UUID `json:"typeId"`
	TypeName  string    `json:"typeName"`
	// Phrase is the wording for this end, such as "blocks" or "is blocked by".
	Phrase string `json:"phrase"`
	Issue  Issue  `json:"issue"`
}

var (
	// ErrNotFound is returned for an issue that does not exist in the caller's
	// organization.
	ErrNotFound = errors.New("issue not found")
	// ErrInvalidKey is returned when a key is not of the form PROJ-123.
	ErrInvalidKey = errors.New("that is not an issue key")
	// ErrNeedsParent is returned when a type below the standard level is used
	// with no parent to sit under.
	ErrNeedsParent = errors.New("this issue type only exists underneath another issue")
	// ErrParentLevel is returned when the proposed parent is not exactly one
	// level above the child.
	ErrParentLevel = errors.New("that issue is at the wrong level to be a parent of this one")
	// ErrParentOtherProject is returned for a parent in another project.
	ErrParentOtherProject = errors.New("a parent has to be in the same project as its child")
	// ErrParentIsSelf is returned for an issue offered as its own parent.
	ErrParentIsSelf = errors.New("an issue cannot be its own parent")
	// ErrChildrenInTheWay is returned when a change would leave an issue's
	// children hanging off something that cannot hold them.
	ErrChildrenInTheWay = errors.New("this issue has children that would be stranded")
	// ErrBackwardsRange is returned for a schedule that ends before it starts.
	ErrBackwardsRange = errors.New("a scheduled range cannot end before it starts")
	// ErrLinkNotFound is returned for a link that is not in this organization.
	ErrLinkNotFound = errors.New("link not found")
	// ErrLinkToSelf is returned when both ends of a link are the same issue.
	ErrLinkToSelf = errors.New("an issue cannot be linked to itself")
	// ErrLinkCycle is returned for a blocks link that would make an issue wait
	// on itself through the others.
	ErrLinkCycle = errors.New("that would make an issue block itself")
	// ErrWorklogNotFound is returned for a worklog that is not in this
	// organization.
	ErrWorklogNotFound = errors.New("worklog not found")
	// ErrNotYourWorklog is returned when somebody edits time they did not log.
	ErrNotYourWorklog = errors.New("only the person who logged this time, or an administrator, can change it")
	// ErrBadDuration is returned for time that is zero, negative or absurd.
	ErrBadDuration = errors.New("that is not an amount of time")
)

// MaxWorklogMinutes is the most one entry may claim: a week of days, which
// nobody works in one go; longer stretches are several entries.
const MaxWorklogMinutes = 7 * 24 * 60

// FormatMinutes writes minutes the way people say them: 2h 30m, 3d 4h, 45m.
// A day is eight hours, which is what "a day of work" means on a ticket.
func FormatMinutes(minutes int) string {
	if minutes <= 0 {
		return "0m"
	}
	days, rest := minutes/(8*60), minutes%(8*60)
	hours, mins := rest/60, rest%60
	var parts []string
	if days > 0 {
		parts = append(parts, strconv.Itoa(days)+"d")
	}
	if hours > 0 {
		parts = append(parts, strconv.Itoa(hours)+"h")
	}
	if mins > 0 {
		parts = append(parts, strconv.Itoa(mins)+"m")
	}
	return strings.Join(parts, " ")
}

// keyPattern matches an issue key: the project key, a hyphen, then the number.
var keyPattern = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,9})-([1-9][0-9]{0,17})$`)

// ParseKey splits an issue key into its project key and number.
func ParseKey(key string) (projectKey string, num int64, err error) {
	m := keyPattern.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(key)))
	if m == nil {
		return "", 0, fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	n, convErr := strconv.ParseInt(m[2], 10, 64)
	if convErr != nil {
		return "", 0, fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	return m[1], n, nil
}

// FormatKey joins a project key and an issue number.
func FormatKey(projectKey string, num int64) string {
	return projectKey + "-" + strconv.FormatInt(num, 10)
}

// VersionRef is a version as it hangs off an issue.
type VersionRef struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Released bool      `json:"released"`
}

// ComponentRef is a component as it hangs off an issue.
type ComponentRef struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}
