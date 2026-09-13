package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/sprint"
)

func (s *Server) handleListSprints(w http.ResponseWriter, r *http.Request) {
	includeClosed := r.URL.Query().Get("closed") == "true"

	found, err := s.Sprints.List(r.Context(), r.PathValue("projectKey"), includeClosed)
	if err != nil {
		respondError(w, r, err)
		return
	}

	// A team's own planning page asks for its sprints; the project's plan asks
	// for all of them.
	if raw := r.URL.Query().Get("team"); raw != "" {
		teamID, err := uuid.Parse(raw)
		if err != nil {
			respondError(w, r, ErrBadRequest("Invalid team id."))
			return
		}
		found = onlyFor(found, &teamID)
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"sprints": found})
}

// sprintRequest is a sprint as the client describes it. Dates and capacity each
// accept an explicit null to clear them, which an omitted field cannot express.
type sprintRequest struct {
	Name     string      `json:"name"`
	Goal     *string     `json:"goal"`
	StartsOn datePatch   `json:"startsOn"`
	EndsOn   datePatch   `json:"endsOn"`
	Capacity numberPatch `json:"capacity"`
	// TeamID says whose sprint this is, and is only read when one is created:
	// moving a sprint between teams would move everything committed to it.
	TeamID uuidPatch `json:"teamId"`
}

// onlyFor keeps the sprints belonging to one team.
func onlyFor(sprints []sprint.Sprint, teamID *uuid.UUID) []sprint.Sprint {
	out := []sprint.Sprint{}
	for _, each := range sprints {
		if each.TeamID != nil && *each.TeamID == *teamID {
			out = append(out, each)
		}
	}
	return out
}

func (s *Server) handleCreateSprint(w http.ResponseWriter, r *http.Request) {
	var req sprintRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	in := sprint.CreateInput{Name: req.Name}
	if req.Goal != nil {
		in.Goal = *req.Goal
	}
	if req.StartsOn.Set {
		in.StartsOn = req.StartsOn.Value
	}
	if req.EndsOn.Set {
		in.EndsOn = req.EndsOn.Value
	}
	if req.Capacity.Set {
		in.Capacity = req.Capacity.Value
	}
	if req.TeamID.Set {
		in.TeamID = req.TeamID.Value
	}

	created, lsn, err := s.Sprints.Create(r.Context(), r.PathValue("projectKey"), in, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"sprint": created})
}

func (s *Server) handleUpdateSprint(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("sprintID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid sprint id."))
		return
	}
	var req sprintRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	var in sprint.UpdateInput
	if req.Name != "" {
		in.Name = &req.Name
	}
	in.Goal = req.Goal
	if req.StartsOn.Set {
		value := req.StartsOn.Value
		in.StartsOn = &value
	}
	if req.EndsOn.Set {
		value := req.EndsOn.Value
		in.EndsOn = &value
	}
	if req.Capacity.Set {
		value := req.Capacity.Value
		in.Capacity = &value
	}

	updated, lsn, err := s.Sprints.Update(r.Context(), id, in, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"sprint": updated})
}

func (s *Server) handleStartSprint(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("sprintID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid sprint id."))
		return
	}

	started, lsn, err := s.Sprints.Start(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"sprint": started})
}

type completeSprintRequest struct {
	MoveTo *uuid.UUID `json:"moveTo"`
}

func (s *Server) handleCompleteSprint(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("sprintID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid sprint id."))
		return
	}
	var req completeSprintRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	report, lsn, err := s.Sprints.Complete(r.Context(), id, sprint.CompleteInput{MoveTo: req.MoveTo}, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"report": report})
}

func (s *Server) handleDeleteSprint(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("sprintID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid sprint id."))
		return
	}
	lsn, err := s.Sprints.Delete(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

type setIssueSprintRequest struct {
	SprintID *uuid.UUID `json:"sprintId"`
}

// handleSetIssueSprint commits an issue to a sprint. A null sprint is not an
// omission: it is the issue going back to the backlog.
func (s *Server) handleSetIssueSprint(w http.ResponseWriter, r *http.Request) {
	var req setIssueSprintRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	updated, lsn, err := s.Issues.SetSprint(r.Context(), r.PathValue("issueKey"), req.SprintID, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"issue": updated})
}

type setIssueEstimateRequest struct {
	Estimate *float64 `json:"estimate"`
}

// handleSetIssueEstimate sizes an issue. A null estimate clears it, which says
// nobody has decided rather than that there is no work.
func (s *Server) handleSetIssueEstimate(w http.ResponseWriter, r *http.Request) {
	var req setIssueEstimateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	updated, lsn, err := s.Issues.SetEstimate(r.Context(), r.PathValue("issueKey"), req.Estimate, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"issue": updated})
}
