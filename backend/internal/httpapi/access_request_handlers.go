package httpapi

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/access"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/perm"
)

// AccessRequestsPerWindow brakes asking for access per account: plenty for a
// person, and too few to probe for keys.
const AccessRequestsPerWindow = 20

var accessRequests = newThrottle(AccessRequestsPerWindow, DoorWindow)

type accessRequestBody struct {
	// Message is a note to the project's administrators, up to 1000 characters.
	Message string `json:"message,omitempty"`
}

// accessReceipt is the one answer to every ask, so it tells nothing.
type accessReceipt struct {
	Received bool `json:"received"`
}

// handleAskForAccess records an ask for an issue the caller may not see. Every
// well-formed ask is answered alike, whatever the key names (#279).
func (s *Server) handleAskForAccess(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("issueKey")
	if _, _, err := issue.ParseKey(key); err != nil {
		respondError(w, r, ErrValidation(map[string]string{"issueKey": "Give an issue key such as PROJ-12."}))
		return
	}
	// The note is optional, so an ask may come with no body at all.
	var req accessRequestBody
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &req); err != nil {
			respondError(w, r, err)
			return
		}
	}
	message, err := access.CheckMessage(req.Message)
	if err != nil {
		respondError(w, r, ErrValidation(map[string]string{"message": "Keep the message to 1000 characters."}))
		return
	}
	if !accessRequests.gateKey(w, r, "account:"+userFrom(r).String(), "Too many requests for access. Wait a few minutes and ask again.") {
		return
	}
	perms := PermsFrom(r.Context())
	mayRead := func(projectKey string) bool { return perms.Can(perm.Read, projectKey) }
	if err := s.Access.Ask(r.Context(), userFrom(r), key, message, mayRead); err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusAccepted, accessReceipt{Received: true})
}

// accessRequestID reads the request a path names; a malformed id is one that
// does not exist.
func accessRequestID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("requestID"))
	if err != nil {
		respondError(w, r, ErrNotFound(accessRequestMissing))
		return uuid.Nil, false
	}
	return id, true
}

const accessRequestMissing = "That access request was not found."

func administersProject(r *http.Request) func(projectKey string) bool {
	perms := PermsFrom(r.Context())
	return func(projectKey string) bool { return perms.Can(perm.ProjectAdminister, projectKey) }
}

func (s *Server) handleGetAccessRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := accessRequestID(w, r)
	if !ok {
		return
	}
	req, err := s.Access.Get(r.Context(), id, administersProject(r))
	if errors.Is(err, access.ErrNotFound) {
		err = ErrNotFound(accessRequestMissing)
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"accessRequest": req})
}

// handleGrantAccessRequest gives the requester the reader role on the project,
// which only somebody who administers it may do.
func (s *Server) handleGrantAccessRequest(w http.ResponseWriter, r *http.Request) {
	id, ok := accessRequestID(w, r)
	if !ok {
		return
	}
	req, lsn, err := s.Access.Grant(r.Context(), id, userFrom(r), administersProject(r))
	if errors.Is(err, access.ErrNotFound) {
		err = ErrNotFound(accessRequestMissing)
	}
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"accessRequest": req})
}
