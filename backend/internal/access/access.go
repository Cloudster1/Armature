// Package access lets a person ask for access to an issue they may not see,
// without learning whether it exists, and a project's administrators grant it.
package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/perm"
)

// MaxMessage is how long the note to the administrators may be; the database
// holds it to the same.
const MaxMessage = 1000

// FoldWindow is how long asking again for the same project adds nothing: the
// administrators were told once, and a second ask is the same question.
const FoldWindow = 24 * time.Hour

// GrantedRole is what granting a request gives: seeing the project, no more.
const GrantedRole = perm.Reader

// ErrNotFound is a request that does not exist, or one the caller may not act on.
var ErrNotFound = errors.New("there is no such access request")

// ErrTooLong is a message over MaxMessage.
var ErrTooLong = fmt.Errorf("keep the message to %d characters", MaxMessage)

// Person is who asked, or who granted.
type Person struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

// Request is one ask, as the project's administrators see it.
type Request struct {
	ID          uuid.UUID  `json:"id"`
	ProjectKey  string     `json:"projectKey"`
	ProjectName string     `json:"projectName"`
	IssueKey    string     `json:"issueKey"`
	Message     string     `json:"message"`
	Requester   Person     `json:"requester"`
	CreatedAt   time.Time  `json:"createdAt"`
	GrantedAt   *time.Time `json:"grantedAt,omitempty"`
	GrantedBy   *Person    `json:"grantedBy,omitempty"`
}

// Issues finds an issue by key, a moved one by its old key too.
type Issues interface {
	ByKey(ctx context.Context, key string) (*issue.Issue, error)
}

// Service records asks and grants them.
type Service struct {
	db     *db.Cluster
	issues Issues
	perms  *perm.Store
}

func NewService(cluster *db.Cluster, issues Issues, perms *perm.Store) *Service {
	return &Service{db: cluster, issues: issues, perms: perms}
}

// CheckMessage refuses a note too long to keep. It is judged before anything
// is looked up, so the answer says nothing about the issue.
func CheckMessage(message string) (string, error) {
	message = strings.TrimSpace(message)
	if utf8.RuneCountInString(message) > MaxMessage {
		return "", ErrTooLong
	}
	return message, nil
}

// Ask records that the requester would like to see the issue. A missing issue,
// a readable one and a hidden one all end the same way, with nil.
func (s *Service) Ask(ctx context.Context, requester uuid.UUID, key, message string, mayRead func(projectKey string) bool) error {
	found, err := s.issues.ByKey(ctx, key)
	if errors.Is(err, issue.ErrNotFound) || errors.Is(err, issue.ErrInvalidKey) {
		return nil
	}
	if err != nil {
		return err
	}
	if mayRead(found.ProjectKey) {
		return nil
	}
	_, err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var recent bool
		err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM access_request
			               WHERE project_id = $1 AND requester_id = $2 AND created_at > now() - make_interval(secs => $3))`,
			found.ProjectID, requester, FoldWindow.Seconds()).Scan(&recent)
		if err != nil || recent {
			return err
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO access_request (project_id, requester_id, issue_key, message)
			VALUES ($1, $2, $3, $4) RETURNING id`, found.ProjectID, requester, found.Key, message).Scan(&id)
		if err != nil {
			return err
		}
		return events.Emit(ctx, tx, events.TopicAccessRequested, map[string]any{"requestId": id, "projectKey": found.ProjectKey})
	})
	return err
}

const selectRequest = `
SELECT r.id, p.key, p.name, r.issue_key, r.message, u.id, u.name, u.email, r.created_at,
       r.granted_at, g.id, g.name, g.email
FROM access_request r
JOIN project p ON p.id = r.project_id
JOIN app_user u ON u.id = r.requester_id
LEFT JOIN app_user g ON g.id = r.granted_by
WHERE r.id = $1`

func scanRequest(row pgx.Row) (*Request, error) {
	var (
		r                         Request
		grantedID                 *uuid.UUID
		grantedName, grantedEmail *string
	)
	err := row.Scan(&r.ID, &r.ProjectKey, &r.ProjectName, &r.IssueKey, &r.Message, &r.Requester.ID, &r.Requester.Name, &r.Requester.Email,
		&r.CreatedAt, &r.GrantedAt, &grantedID, &grantedName, &grantedEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if grantedID != nil {
		r.GrantedBy = &Person{ID: *grantedID, Name: deref(grantedName), Email: deref(grantedEmail)}
	}
	return &r, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Get reads a request for somebody who may administer its project; to anyone
// else it does not exist.
func (s *Service) Get(ctx context.Context, id uuid.UUID, administers func(projectKey string) bool) (*Request, error) {
	var out *Request
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scanRequest(tx.QueryRow(ctx, selectRequest, id))
		return err
	})
	if err != nil {
		return nil, err
	}
	if !administers(out.ProjectKey) {
		return nil, ErrNotFound
	}
	return out, nil
}

// Grant gives the requester the reader role on the request's project. Granting
// twice is the state asked for, not an error, and keeps who granted it first.
func (s *Service) Grant(ctx context.Context, id uuid.UUID, actor uuid.UUID, administers func(projectKey string) bool) (*Request, db.LSN, error) {
	req, err := s.Get(db.PinPrimary(ctx), id, administers)
	if err != nil {
		return nil, 0, err
	}
	requester := req.Requester.ID
	if _, _, err := s.perms.Grant(ctx, perm.GrantInput{Role: GrantedRole, ProjectKey: req.ProjectKey, UserID: &requester}, actor); err != nil {
		return nil, 0, err
	}
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE access_request SET granted_at = now(), granted_by = $2 WHERE id = $1 AND granted_at IS NULL`, id, actor)
		if err != nil {
			return err
		}
		req, err = scanRequest(tx.QueryRow(ctx, selectRequest, id))
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return req, lsn, nil
}
