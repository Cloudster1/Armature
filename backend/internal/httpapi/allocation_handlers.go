package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// A project's people read the shares it plans with; its administrators set
// them, since a share says how much of somebody the project may count on.

func (s *Server) handleListAllocations(w http.ResponseWriter, r *http.Request) {
	found, err := s.Availability.Allocations(r.Context(), chi.URLParam(r, "projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"allocations": found})
}

type allocationRequest struct {
	// Percent is the project's share of the person's week, 0 to 100.
	Percent *int `json:"percent"`
}

func (s *Server) handleSetAllocation(w http.ResponseWriter, r *http.Request) {
	userID, apiErr := pathUUID(r, "userID", "user")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req allocationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.Percent == nil {
		respondError(w, r, &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed",
			Message: "Give the share of the week as a percent from 0 to 100.", Fields: map[string]string{"percent": "Give a percent from 0 to 100."}})
		return
	}
	set, lsn, err := s.Availability.SetAllocation(r.Context(), chi.URLParam(r, "projectKey"), userID, *req.Percent, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"allocation": set})
}
