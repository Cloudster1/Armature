package httpapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/nql"
	"github.com/armature/armature/backend/internal/perm"
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

// alignRequest is the plan's query, if any; an empty object aligns the whole plan.
type alignRequest struct {
	Query string `json:"q,omitempty"`
}

// handleAlignPlan proposes the moves that start each blocked issue after its
// blockers, and saves nothing.
func (s *Server) handleAlignPlan(w http.ResponseWriter, r *http.Request) {
	var req alignRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	var query *nql.Compiled
	if q := strings.TrimSpace(req.Query); q != "" {
		compiled, err := compileQuery(q, PrincipalFrom(r.Context()).User.ID)
		if err != nil {
			respondError(w, r, err)
			return
		}
		query = compiled
	}
	perms := PermsFrom(r.Context())
	mayRead := func(projectKey string) bool { return perms.Can(perm.Read, projectKey) }
	found, err := s.Plans.Align(r.Context(), r.PathValue("projectKey"), query, mayRead)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, found)
}

// handleGetResources reads the project's resource view; from and to are days,
// taken to their whole weeks.
func (s *Server) handleGetResources(w http.ResponseWriter, r *http.Request) {
	p, err := s.Projects.ByKey(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	query := r.URL.Query()
	found, err := s.Plans.Resources(r.Context(), p, query.Get("from"), query.Get("to"))
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

// planMoveRequest is one issue's new range and the range it is expected to
// have now. All four days are required, null where a day is unset.
type planMoveRequest struct {
	Key      string          `json:"key"`
	Expected scheduleRequest `json:"expected"`
	Start    datePatch       `json:"startDate"`
	Due      datePatch       `json:"dueDate"`
}

type planScheduleRequest struct {
	Moves []planMoveRequest `json:"moves"`
}

// handleSchedulePlan moves several of a project's issues at once, or none of
// them when any has moved since the dates the moves were worked out from.
func (s *Server) handleSchedulePlan(w http.ResponseWriter, r *http.Request) {
	var req planScheduleRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	switch {
	case len(req.Moves) == 0:
		respondError(w, r, ErrBadRequest("Give at least one move."))
		return
	case len(req.Moves) > issue.MaxScheduleMoves:
		respondError(w, r, ErrBadRequest(fmt.Sprintf("Give at most %d moves at once, and send the rest in another request.", issue.MaxScheduleMoves)))
		return
	}
	moves := make([]issue.ScheduleMove, 0, len(req.Moves))
	for _, m := range req.Moves {
		if strings.TrimSpace(m.Key) == "" || !m.Expected.Start.Set || !m.Expected.Due.Set || !m.Start.Set || !m.Due.Set {
			respondError(w, r, ErrBadRequest("Give each move its key, its new startDate and dueDate, and the expected ones it has now, with null for a day that is unset."))
			return
		}
		moves = append(moves, issue.ScheduleMove{
			Key:           strings.TrimSpace(m.Key),
			ExpectedStart: m.Expected.Start.Value, ExpectedDue: m.Expected.Due.Value,
			Start: m.Start.Value, Due: m.Due.Value,
		})
	}
	updated, lsn, err := s.Issues.ScheduleMany(r.Context(), r.PathValue("projectKey"), moves, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"issues": updated})
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
