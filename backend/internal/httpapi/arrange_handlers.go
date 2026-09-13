package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/arrange"
	"github.com/armature/armature/backend/internal/db"
)

// A project reads how it arranges an issue's fields; the organization reads
// what every project follows until it says otherwise.

func (s *Server) handleProjectArrangement(w http.ResponseWriter, r *http.Request) {
	arrangements, err := s.Arrange.InProject(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"arrangements": arrangements})
}

func (s *Server) handleOrgArrangement(w http.ResponseWriter, r *http.Request) {
	arrangements, err := s.Arrange.InOrg(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"arrangements": arrangements})
}

type arrangementRequest struct {
	// IssueTypeID is the type being arranged; absent arranges every type that
	// is not named by one of its own.
	IssueTypeID *uuid.UUID `json:"issueTypeId,omitempty"`
	// Places is the whole arrangement. Null hands the type back to whoever
	// answered before: the organization, or the built-in arrangement.
	Places *[]arrange.Placement `json:"places,omitempty"`
}

func (s *Server) handleSetProjectArrangement(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeArrangement(w, r)
	if !ok {
		return
	}
	key := r.PathValue("projectKey")
	lsn, err := s.Arrange.SaveInProject(r.Context(), key, req.IssueTypeID, placesOf(req))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	// Read back what every type now answers with, so the page that arranged
	// one of them redraws from this answer alone.
	arrangements, err := s.Arrange.InProject(db.PinPrimary(r.Context()), key)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"arrangements": arrangements})
}

func (s *Server) handleSetOrgArrangement(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeArrangement(w, r)
	if !ok {
		return
	}
	lsn, err := s.Arrange.SaveInOrg(r.Context(), req.IssueTypeID, placesOf(req))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	arrangements, err := s.Arrange.InOrg(db.PinPrimary(r.Context()))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"arrangements": arrangements})
}

func decodeArrangement(w http.ResponseWriter, r *http.Request) (arrangementRequest, bool) {
	var req arrangementRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return req, false
	}
	return req, true
}

// placesOf keeps the difference between "arrange it this way" and "say nothing
// about it", which an empty list and an absent one are.
func placesOf(req arrangementRequest) []arrange.Placement {
	if req.Places == nil {
		return nil
	}
	return *req.Places
}
