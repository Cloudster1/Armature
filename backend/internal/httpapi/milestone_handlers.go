package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/milestone"
)

func (s *Server) handleListMilestones(w http.ResponseWriter, r *http.Request) {
	includeClosed := r.URL.Query().Get("closed") == "true"
	found, err := s.Milestones.List(r.Context(), r.PathValue("projectKey"), includeClosed)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"milestones": found})
}

// milestoneRequest is a milestone as the client describes it. The date takes an
// explicit null to clear it, which an omitted field cannot express.
type milestoneRequest struct {
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	DueOn       datePatch `json:"dueOn"`
}

func (s *Server) handleCreateMilestone(w http.ResponseWriter, r *http.Request) {
	var req milestoneRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	in := milestone.Input{Name: req.Name}
	if req.Description != nil {
		in.Description = *req.Description
	}
	if req.DueOn.Set {
		in.DueOn = req.DueOn.Value
	}

	created, lsn, err := s.Milestones.Create(r.Context(), r.PathValue("projectKey"), in, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"milestone": created})
}

func milestoneID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("milestoneID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid milestone id."))
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) handleUpdateMilestone(w http.ResponseWriter, r *http.Request) {
	id, ok := milestoneID(w, r)
	if !ok {
		return
	}
	var req milestoneRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	var in milestone.UpdateInput
	if req.Name != "" {
		in.Name = &req.Name
	}
	in.Description = req.Description
	if req.DueOn.Set {
		value := req.DueOn.Value
		in.DueOn = &value
	}

	updated, lsn, err := s.Milestones.Update(r.Context(), id, in, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"milestone": updated})
}

func (s *Server) handleCloseMilestone(w http.ResponseWriter, r *http.Request) {
	id, ok := milestoneID(w, r)
	if !ok {
		return
	}
	closed, lsn, err := s.Milestones.Close(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"milestone": closed})
}

func (s *Server) handleReopenMilestone(w http.ResponseWriter, r *http.Request) {
	id, ok := milestoneID(w, r)
	if !ok {
		return
	}
	reopened, lsn, err := s.Milestones.Reopen(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"milestone": reopened})
}

func (s *Server) handleDeleteMilestone(w http.ResponseWriter, r *http.Request) {
	id, ok := milestoneID(w, r)
	if !ok {
		return
	}
	lsn, err := s.Milestones.Delete(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

type setIssueMilestoneRequest struct {
	MilestoneID *uuid.UUID `json:"milestoneId"`
}

// handleSetIssueMilestone makes an issue count towards a milestone. A null
// milestone is not an omission: it is the issue counting towards none.
func (s *Server) handleSetIssueMilestone(w http.ResponseWriter, r *http.Request) {
	var req setIssueMilestoneRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Issues.SetMilestone(r.Context(), r.PathValue("issueKey"), req.MilestoneID, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"issue": updated})
}
