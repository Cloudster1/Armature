package attachment

import (
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
)

// MaxSize is the largest file an issue takes. Screenshots, logs and documents
// fit; a database dump does not belong on a ticket.
const MaxSize int64 = 25 << 20

// Attachment is what the tracker knows about a file on an issue.
type Attachment struct {
	ID          uuid.UUID      `json:"id"`
	IssueID     uuid.UUID      `json:"issueId"`
	IssueKey    string         `json:"issueKey"`
	Uploader    *issue.UserRef `json:"uploader,omitempty"`
	FileName    string         `json:"fileName"`
	ContentType string         `json:"contentType"`
	Size        int64          `json:"size"`
	CreatedAt   time.Time      `json:"createdAt"`
}

var (
	// ErrNotFound is returned for an attachment that is not in the caller's
	// organization.
	ErrNotFound = errors.New("attachment not found")
	// ErrTooLarge is returned for a file over MaxSize.
	ErrTooLarge = errors.New("that file is too large")
	// ErrEmpty is returned for an upload with no bytes in it.
	ErrEmpty = errors.New("that file is empty")
)
