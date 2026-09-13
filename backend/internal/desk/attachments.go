package desk

import (
	"context"
	"io"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
)

// A customer's files go through the same rule as the request itself: a file on
// a request that is not theirs is not found, since existence is privileged.

// Attachments lists the files on a customer's request, the desk's included.
func (s *Service) Attachments(ctx context.Context, key string, customer uuid.UUID) ([]attachment.Attachment, error) {
	if s.files == nil {
		return nil, attachment.ErrUnavailable
	}
	if _, err := s.Request(ctx, key, customer); err != nil {
		return nil, err
	}
	return s.files.List(ctx, key)
}

// Attach puts a file on the customer's own request.
func (s *Service) Attach(ctx context.Context, key string, in attachment.UploadInput, customer issue.Actor) (*attachment.Attachment, db.LSN, error) {
	if s.files == nil {
		return nil, 0, attachment.ErrUnavailable
	}
	if _, err := s.Request(ctx, key, customer.UserID); err != nil {
		return nil, 0, err
	}
	return s.files.Upload(ctx, key, in, customer)
}

// OpenAttachment reads a file on the customer's request and its bytes. The
// caller closes the reader.
func (s *Service) OpenAttachment(ctx context.Context, id uuid.UUID, customer uuid.UUID) (*attachment.Attachment, io.ReadCloser, error) {
	if _, err := s.customersFile(ctx, id, customer); err != nil {
		return nil, nil, err
	}
	return s.files.Open(ctx, id)
}

// Detach removes a file the customer put on the request themselves. The
// desk's files are the desk's to take back.
func (s *Service) Detach(ctx context.Context, id uuid.UUID, customer issue.Actor) (db.LSN, error) {
	found, err := s.customersFile(ctx, id, customer.UserID)
	if err != nil {
		return 0, err
	}
	if found.Uploader == nil || found.Uploader.ID != customer.UserID {
		return 0, ErrNotYourFile
	}
	return s.files.Delete(ctx, id, customer)
}

// customersFile finds a file and proves the request it is on is the customer's.
func (s *Service) customersFile(ctx context.Context, id uuid.UUID, customer uuid.UUID) (*attachment.Attachment, error) {
	if s.files == nil {
		return nil, attachment.ErrUnavailable
	}
	found, err := s.files.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.Request(ctx, found.IssueKey, customer); err != nil {
		return nil, err
	}
	return found, nil
}
