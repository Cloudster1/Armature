package httpapi

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/nql"
)

func (s *Server) handleGetPlan(w http.ResponseWriter, r *http.Request) {
	var query *nql.Compiled
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		compiled, err := compileQuery(q, PrincipalFrom(r.Context()).User.ID)
		if err != nil {
			respondError(w, r, err)
			return
		}
		query = compiled
	}
	found, err := s.Plans.ForProjectMatching(r.Context(), r.PathValue("projectKey"), query)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, found)
}

// scheduleRequest carries the two ends of an issue's range. Each accepts an
// explicit null to clear that end, which an omitted field cannot express.
type scheduleRequest struct {
	Start datePatch `json:"startDate"`
	Due   datePatch `json:"dueDate"`
}

func (s *Server) handleScheduleIssue(w http.ResponseWriter, r *http.Request) {
	var req scheduleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if !req.Start.Set && !req.Due.Set {
		respondError(w, r, ErrBadRequest("Give a start date, a due date, or both."))
		return
	}

	var in issue.ScheduleInput
	if req.Start.Set {
		value := req.Start.Value
		in.Start = &value
	}
	if req.Due.Set {
		value := req.Due.Value
		in.Due = &value
	}

	updated, lsn, err := s.Issues.Schedule(r.Context(), r.PathValue("issueKey"), in, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"issue": updated})
}

type linkRequest struct {
	TypeID    *uuid.UUID `json:"typeId,omitempty"`
	TypeName  string     `json:"type,omitempty"`
	TargetKey string     `json:"targetKey"`
}

func (s *Server) handleAddLink(w http.ResponseWriter, r *http.Request) {
	var req linkRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.TargetKey == "" {
		respondError(w, r, ErrBadRequest("Which issue should this be linked to?"))
		return
	}

	in := issue.LinkInput{TypeName: req.TypeName, TargetKey: req.TargetKey}
	if req.TypeID != nil {
		in.TypeID = *req.TypeID
	}

	created, lsn, err := s.Issues.AddLink(r.Context(), r.PathValue("issueKey"), in, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"link": created})
}

func (s *Server) handleListLinks(w http.ResponseWriter, r *http.Request) {
	links, err := s.Issues.Links(r.Context(), r.PathValue("issueKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"links": links})
}

func (s *Server) handleDeleteLink(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("linkID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid link id."))
		return
	}
	lsn, err := s.Issues.RemoveLink(r.Context(), r.PathValue("issueKey"), id)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleListLinkTypes(w http.ResponseWriter, r *http.Request) {
	types, err := s.Issues.ListLinkTypes(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"linkTypes": types})
}
