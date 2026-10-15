package desk

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/workflow"
)

// The portal's own shapes, built from the issue once the access checks have
// passed, so what the agents work with stays on their side.

// PortalPerson is somebody on a request as the portal names them.
type PortalPerson struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// AvatarURL is where the picture is served from; empty means initials.
	AvatarURL string `json:"avatarUrl,omitempty"`
}

// PortalStatus is where a request stands; the category says whether it is done.
type PortalStatus struct {
	Name     string                  `json:"name"`
	Category workflow.StatusCategory `json:"category"`
}

// PortalRequest is a request as its customer reads it.
type PortalRequest struct {
	Key             string          `json:"key"`
	Summary         string          `json:"summary"`
	Description     json.RawMessage `json:"description,omitempty"`
	Status          PortalStatus    `json:"status"`
	Priority        issue.Priority  `json:"priority"`
	RequestTypeName string          `json:"requestTypeName,omitempty"`
	// Team is who the request type hands it to, which the customer is told.
	Team *issue.TeamRef `json:"team,omitempty"`
	// Company is the reporter's at the desk, and SharedWithCompany says its
	// other members read the request too.
	Company           *issue.CompanyRef `json:"company,omitempty"`
	SharedWithCompany bool              `json:"sharedWithCompany"`
	Assignee          *PortalPerson     `json:"assignee,omitempty"`
	Reporter          *PortalPerson     `json:"reporter,omitempty"`
	CreatedAt         time.Time         `json:"createdAt"`
	UpdatedAt         time.Time         `json:"updatedAt"`
	ResolvedAt        *time.Time        `json:"resolvedAt,omitempty"`
	// CommentCount counts the public conversation, the part a customer reads.
	CommentCount int `json:"commentCount"`
}

// PortalComment is one reply in a request's public conversation.
type PortalComment struct {
	ID        uuid.UUID       `json:"id"`
	Author    *PortalPerson   `json:"author,omitempty"`
	Body      json.RawMessage `json:"body"`
	CreatedAt time.Time       `json:"createdAt"`
}

// PortalAttachment is a file on a request; the bytes are behind its id.
type PortalAttachment struct {
	ID          uuid.UUID     `json:"id"`
	Uploader    *PortalPerson `json:"uploader,omitempty"`
	FileName    string        `json:"fileName"`
	ContentType string        `json:"contentType"`
	Size        int64         `json:"size"`
	CreatedAt   time.Time     `json:"createdAt"`
}

// PortalFollower is somebody told about a request. Email is there only for
// the reporter, and only for the people they added, whose address they typed.
type PortalFollower struct {
	UserID uuid.UUID `json:"userId"`
	Name   string    `json:"name"`
	Email  string    `json:"email,omitempty"`
}

func portalPerson(ref *issue.UserRef) *PortalPerson {
	if ref == nil {
		return nil
	}
	return &PortalPerson{ID: ref.ID, Name: ref.Name, AvatarURL: ref.AvatarURL}
}

func portalRequest(i *issue.Issue, comments int) PortalRequest {
	return PortalRequest{
		Key:               i.Key,
		Summary:           i.Summary,
		Description:       i.Description,
		Status:            PortalStatus{Name: i.Status.Name, Category: i.Status.Category},
		Priority:          i.Priority,
		RequestTypeName:   i.RequestTypeName,
		Team:              i.Team,
		Company:           i.Company,
		SharedWithCompany: i.SharedWithCompany,
		Assignee:          portalPerson(i.Assignee),
		Reporter:          portalPerson(i.Reporter),
		CreatedAt:         i.CreatedAt,
		UpdatedAt:         i.UpdatedAt,
		ResolvedAt:        i.ResolvedAt,
		CommentCount:      comments,
	}
}

func portalComment(c *issue.Comment) PortalComment {
	return PortalComment{ID: c.ID, Author: portalPerson(c.Author), Body: c.Body, CreatedAt: c.CreatedAt}
}

func portalAttachment(a *attachment.Attachment) PortalAttachment {
	return PortalAttachment{ID: a.ID, Uploader: portalPerson(a.Uploader), FileName: a.FileName, ContentType: a.ContentType, Size: a.Size, CreatedAt: a.CreatedAt}
}

// portalRequests maps a page of requests with one count of their public
// comments, so a list costs one query more and not one per row.
func (s *Service) portalRequests(ctx context.Context, found []issue.Issue) ([]PortalRequest, error) {
	out := make([]PortalRequest, 0, len(found))
	if len(found) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(found))
	for i := range found {
		ids[i] = found[i].ID
	}
	counts := map[uuid.UUID]int{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT issue_id, count(*) FROM issue_comment
			WHERE issue_id = ANY($1) AND NOT is_internal GROUP BY issue_id`, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var (
				id uuid.UUID
				n  int
			)
			if err := rows.Scan(&id, &n); err != nil {
				return err
			}
			counts[id] = n
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	for i := range found {
		out = append(out, portalRequest(&found[i], counts[found[i].ID]))
	}
	return out, nil
}
