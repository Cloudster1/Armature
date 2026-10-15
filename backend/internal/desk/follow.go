package desk

import (
	"context"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
)

// A request's followers, from the portal's side. A request that is not the
// customer's, as reporter or follower, is not found: its existence is privileged.

// Followers lists who is told about a customer's request, to its reporter and
// followers: the reporter chose them, and a colleague has no need to know who.
func (s *Service) Followers(ctx context.Context, key string, customer uuid.UUID) ([]PortalFollower, error) {
	found, throughCompany, err := s.reach(ctx, key, customer)
	if err != nil {
		return nil, err
	}
	if throughCompany {
		return nil, ErrNotFound
	}
	watchers, err := s.issues.Watchers(ctx, key)
	if err != nil {
		return nil, err
	}
	reporter := found.Reporter != nil && found.Reporter.ID == customer
	out := make([]PortalFollower, 0, len(watchers))
	for _, w := range watchers {
		out = append(out, portalFollower(w, reporter && w.AddedBy != nil && *w.AddedBy == customer))
	}
	return out, nil
}

// Follow adds somebody the reporter named by address, and answers with them
// as the reporter typed them.
func (s *Service) Follow(ctx context.Context, key string, userID uuid.UUID, customer issue.Actor) (*PortalFollower, db.LSN, error) {
	if err := s.MayFollow(ctx, key, customer.UserID); err != nil {
		return nil, 0, err
	}
	added, _, lsn, err := s.issues.AddWatcher(ctx, key, userID, customer)
	if err != nil {
		return nil, 0, err
	}
	out := portalFollower(*added, true)
	return &out, lsn, nil
}

func portalFollower(w issue.Watcher, withAddress bool) PortalFollower {
	out := PortalFollower{UserID: w.UserID, Name: w.Name}
	if withAddress {
		out.Email = w.Email
	}
	return out
}

// MayFollow says whether a customer may add a follower to a request: only the
// person who raised it may widen who hears about it.
func (s *Service) MayFollow(ctx context.Context, key string, customer uuid.UUID) error {
	found, _, err := s.reach(ctx, key, customer)
	if err != nil {
		return err
	}
	if found.Reporter == nil || found.Reporter.ID != customer {
		return ErrNotYourRequest
	}
	return nil
}

// Unfollow removes a follower: the reporter may remove anyone from their
// request, and a follower may remove themselves.
func (s *Service) Unfollow(ctx context.Context, key string, userID uuid.UUID, customer issue.Actor) (db.LSN, error) {
	found, _, err := s.reach(ctx, key, customer.UserID)
	if err != nil {
		return 0, err
	}
	reporter := found.Reporter != nil && found.Reporter.ID == customer.UserID
	if !reporter && userID != customer.UserID {
		return 0, ErrNotYourRequest
	}
	return s.issues.RemoveWatcher(ctx, key, userID, customer)
}
