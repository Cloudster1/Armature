package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
)

// Watchers: who is told about an issue. Agents add colleagues or an address on
// the issue page; a requester adds an address on their request; anyone a mail
// reached can stop with the token it carried.

func (s *Server) handleListWatchers(w http.ResponseWriter, r *http.Request) {
	watchers, err := s.Issues.Watchers(r.Context(), r.PathValue("issueKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"watchers": watchers})
}

// addWatcherRequest names a person by account or by address; leaving both out
// means the caller themselves.
type addWatcherRequest struct {
	UserID *uuid.UUID `json:"userId"`
	Email  string     `json:"email"`
}

// watcherFrom resolves who a request names, making a passwordless user for an
// address nobody has yet.
func (s *Server) watcherFrom(r *http.Request, req addWatcherRequest) (uuid.UUID, error) {
	switch {
	case req.UserID != nil:
		return *req.UserID, nil
	case req.Email != "":
		user, err := s.Auth.UserForAddress(r.Context(), req.Email)
		if err != nil {
			return uuid.Nil, err
		}
		return user.ID, nil
	default:
		return userFrom(r), nil
	}
}

func (s *Server) handleAddWatcher(w http.ResponseWriter, r *http.Request) {
	var req addWatcherRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	userID, err := s.watcherFrom(r, req)
	if err != nil {
		respondError(w, r, err)
		return
	}
	added, _, lsn, err := s.Issues.AddWatcher(r.Context(), r.PathValue("issueKey"), userID, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"watcher": added})
}

func (s *Server) handleRemoveWatcher(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(r.PathValue("userID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid user id."))
		return
	}
	lsn, err := s.Issues.RemoveWatcher(r.Context(), r.PathValue("issueKey"), userID, actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// The portal's side: a requester's request has followers, and the requester
// decides who they are.

func (s *Server) handlePortalWatchers(w http.ResponseWriter, r *http.Request) {
	watchers, err := s.Desk.Followers(r.Context(), r.PathValue("issueKey"), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"watchers": watchers})
}

type portalFollowRequest struct {
	Email string `json:"email"`
}

func (s *Server) handlePortalFollow(w http.ResponseWriter, r *http.Request) {
	var req portalFollowRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	key := r.PathValue("issueKey")
	if err := s.Desk.MayFollow(r.Context(), key, userFrom(r)); err != nil {
		respondError(w, r, err)
		return
	}
	user, err := s.Auth.UserForAddress(r.Context(), req.Email)
	if err != nil {
		respondError(w, r, err)
		return
	}
	added, _, lsn, err := s.Issues.AddWatcher(r.Context(), key, user.ID, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"watcher": added})
}

func (s *Server) handlePortalUnfollow(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(r.PathValue("userID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid user id."))
		return
	}
	lsn, err := s.Desk.Unfollow(r.Context(), r.PathValue("issueKey"), userID, actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// The way out a mail carries: no session, just the token.

type unwatchRequest struct {
	Token string `json:"token"`
}

func (s *Server) handleUnwatch(w http.ResponseWriter, r *http.Request) {
	var req unwatchRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.Token == "" {
		respondError(w, r, ErrBadRequest("The link is missing its token. Open it from the mail again."))
		return
	}
	lsn, err := s.Issues.Unwatch(r.Context(), req.Token)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

var _ = issue.ErrNotWatching
