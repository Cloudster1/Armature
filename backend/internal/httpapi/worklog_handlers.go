package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
)

// Worklogs: the time people spent, entry by entry.

func (s *Server) handleListWorklogs(w http.ResponseWriter, r *http.Request) {
	found, err := s.Issues.Worklogs(r.Context(), r.PathValue("issueKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"worklogs": found})
}

type worklogRequest struct {
	Minutes int `json:"minutes"`
	// StartedOn is the day the work was done, YYYY-MM-DD; today when absent.
	StartedOn datePatch `json:"startedOn,omitempty"`
	Note      string    `json:"note,omitempty"`
}

func (req worklogRequest) input() issue.WorklogInput {
	return issue.WorklogInput{Minutes: req.Minutes, StartedOn: req.StartedOn.Value, Note: req.Note}
}

func (s *Server) handleLogWork(w http.ResponseWriter, r *http.Request) {
	var req worklogRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	logged, lsn, err := s.Issues.LogWork(r.Context(), r.PathValue("issueKey"), req.input(), actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"worklog": logged})
}

func (s *Server) handleUpdateWorklog(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("worklogID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid worklog id."))
		return
	}
	var req worklogRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Issues.UpdateWorklog(r.Context(), id, req.input(), actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"worklog": updated})
}

func (s *Server) handleDeleteWorklog(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("worklogID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid worklog id."))
		return
	}
	lsn, err := s.Issues.DeleteWorklog(r.Context(), id, actorFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
