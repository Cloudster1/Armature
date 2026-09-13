package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/label"
)

// Labels: the organization's words, and which of them an issue carries.

func (s *Server) handleListLabels(w http.ResponseWriter, r *http.Request) {
	found, err := s.Labels.Labels(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"labels": found})
}

type labelRequest struct {
	Name  *string `json:"name,omitempty"`
	Color *string `json:"color,omitempty"`
}

func (s *Server) handleCreateLabel(w http.ResponseWriter, r *http.Request) {
	var req labelRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	in := label.Input{}
	if req.Name != nil {
		in.Name = *req.Name
	}
	if req.Color != nil {
		in.Color = *req.Color
	}
	created, lsn, err := s.Labels.Create(r.Context(), in)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"label": created})
}

func (s *Server) handleUpdateLabel(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("labelID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid label id."))
		return
	}
	var req labelRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Labels.Update(r.Context(), id, label.UpdateInput{Name: req.Name, Color: req.Color})
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"label": updated})
}

func (s *Server) handleDeleteLabel(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("labelID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid label id."))
		return
	}
	lsn, err := s.Labels.Delete(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

type setIssueLabelsRequest struct {
	// Labels are names; one the organization lacks is made on the spot.
	Labels []string `json:"labels"`
}

func (s *Server) handleSetIssueLabels(w http.ResponseWriter, r *http.Request) {
	var req setIssueLabelsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	labels, lsn, err := s.Labels.SetIssueLabels(r.Context(), r.PathValue("issueKey"), req.Labels, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"labels": labels})
}
