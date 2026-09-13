package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/board"
)

func (s *Server) handleGetBoard(w http.ResponseWriter, r *http.Request) {
	found, err := s.Boards.ForProject(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"board": found})
}

type moveCardRequest struct {
	IssueKey   string    `json:"issueKey"`
	SwimlaneID uuid.UUID `json:"swimlaneId"`
	// AfterKey and BeforeKey name the cards the dropped card lands between.
	AfterKey  string `json:"afterKey,omitempty"`
	BeforeKey string `json:"beforeKey,omitempty"`
}

// handleMoveCard is the drag and drop endpoint. Moving a card into a swimlane
// that holds different states is a workflow transition, so this can legitimately
// fail with the workflow's own reason.
func (s *Server) handleMoveCard(w http.ResponseWriter, r *http.Request) {
	var req moveCardRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.IssueKey == "" || req.SwimlaneID == uuid.Nil {
		respondError(w, r, ErrBadRequest("Which card, and into which swimlane?"))
		return
	}

	result, lsn, err := s.Boards.Move(r.Context(), r.PathValue("projectKey"), board.MoveInput{
		IssueKey:   req.IssueKey,
		SwimlaneID: req.SwimlaneID,
		AfterKey:   req.AfterKey,
		BeforeKey:  req.BeforeKey,
	}, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, result)
}

type swimlaneRequest struct {
	Name      string      `json:"name"`
	StatusIDs []uuid.UUID `json:"statusIds,omitempty"`
	WIPLimit  int         `json:"wipLimit,omitempty"`
}

func (s *Server) handleAddSwimlane(w http.ResponseWriter, r *http.Request) {
	var req swimlaneRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	lane, lsn, err := s.Boards.AddSwimlane(r.Context(), r.PathValue("projectKey"), board.AddSwimlaneInput{
		Name:      req.Name,
		StatusIDs: req.StatusIDs,
		WIPLimit:  req.WIPLimit,
	})
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"swimlane": lane})
}

type updateSwimlaneRequest struct {
	Name      *string      `json:"name,omitempty"`
	WIPLimit  *int         `json:"wipLimit,omitempty"`
	StatusIDs *[]uuid.UUID `json:"statusIds,omitempty"`
}

func (s *Server) handleUpdateSwimlane(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("swimlaneID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid swimlane id."))
		return
	}

	var req updateSwimlaneRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	lsn, err := s.Boards.UpdateSwimlane(r.Context(), r.PathValue("projectKey"), id, board.UpdateSwimlaneInput{
		Name:      req.Name,
		WIPLimit:  req.WIPLimit,
		StatusIDs: req.StatusIDs,
	})
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleDeleteSwimlane(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("swimlaneID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid swimlane id."))
		return
	}
	lsn, err := s.Boards.DeleteSwimlane(r.Context(), r.PathValue("projectKey"), id)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

type reorderSwimlanesRequest struct {
	// Order is the full left to right order, so two people reordering at once
	// cannot interleave into an arrangement neither asked for.
	Order []uuid.UUID `json:"order"`
}

func (s *Server) handleReorderSwimlanes(w http.ResponseWriter, r *http.Request) {
	var req reorderSwimlanesRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	lsn, err := s.Boards.ReorderSwimlanes(r.Context(), r.PathValue("projectKey"), req.Order)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

type boardSettingsRequest struct {
	GroupBy string `json:"groupBy"`
}

func (s *Server) handleUpdateBoard(w http.ResponseWriter, r *http.Request) {
	var req boardSettingsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	lsn, err := s.Boards.SetGrouping(r.Context(), r.PathValue("projectKey"), board.Grouping(req.GroupBy))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// handleGetSprintBoard is a sprint's own board: its stream's board showing only
// what is committed to it, whether or not it is the sprint that is running.
func (s *Server) handleGetSprintBoard(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("sprintID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid sprint id."))
		return
	}
	found, err := s.Boards.ForSprint(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"board": found})
}
