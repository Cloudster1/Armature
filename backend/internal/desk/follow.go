package desk

import (
	"context"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
)

// A request's followers, from the portal's side. The request has to be the
// customer's, as the reporter or as a follower; a request that is not theirs
// is not found, since its existence is privileged.

// Followers lists who is told about a customer's request.
func (s *Service) Followers(ctx context.Context, key string, customer uuid.UUID) ([]issue.Watcher, error) {
	if _, err := s.Request(ctx, key, customer); err != nil {
		return nil, err
	}
	return s.issues.Watchers(ctx, key)
}

// MayFollow says whether a customer may add a follower to a request: only the
// person who raised it may widen who hears about it.
func (s *Service) MayFollow(ctx context.Context, key string, customer uuid.UUID) error {
	found, err := s.Request(ctx, key, customer)
	if err != nil {
		return err
	}
	if found.Issue.Reporter == nil || found.Issue.Reporter.ID != customer {
		return ErrNotYourRequest
	}
	return nil
}

// Unfollow removes a follower: the reporter may remove anyone from their
// request, and a follower may remove themselves.
func (s *Service) Unfollow(ctx context.Context, key string, userID uuid.UUID, customer issue.Actor) (db.LSN, error) {
	found, err := s.Request(ctx, key, customer.UserID)
	if err != nil {
		return 0, err
	}
	reporter := found.Issue.Reporter != nil && found.Issue.Reporter.ID == customer.UserID
	if !reporter && userID != customer.UserID {
		return 0, ErrNotYourRequest
	}
	return s.issues.RemoveWatcher(ctx, key, userID, customer)
}
