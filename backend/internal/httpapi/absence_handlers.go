package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/availability"
)

// Every agent reads who is away and when, never why; the service decides who
// may write one down, since that depends on whose teams the person is on.

func (s *Server) recorder(r *http.Request) availability.Recorder {
	return availability.Recorder{ID: userFrom(r), Perms: PermsFrom(r.Context())}
}

func (s *Server) handleListAbsences(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	userID, err := queryUUID(query, "userId", "user")
	if err != nil {
		respondError(w, r, err)
		return
	}
	from, to, err := availability.AbsenceWindow(query.Get("from"), query.Get("to"), time.Now().UTC())
	if err != nil {
		respondError(w, r, err)
		return
	}
	found, err := s.Availability.Absences(r.Context(), from, to, userID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{
		"absences": found, "from": from.Format(availability.DateLayout), "to": to.Format(availability.DateLayout),
	})
}

type absenceRequest struct {
	// UserID is who is away; left out, it is the caller.
	UserID *uuid.UUID `json:"userId,omitempty"`
	// StartsOn and EndsOn are YYYY-MM-DD, both days away; EndsOn left out of a new absence is StartsOn.
	StartsOn *string `json:"startsOn,omitempty"`
	EndsOn   *string `json:"endsOn,omitempty"`
	// HalfDay is half of a single day away.
	HalfDay *bool `json:"halfDay,omitempty"`
}

func (req absenceRequest) input() availability.AbsenceInput {
	return availability.AbsenceInput{StartsOn: req.StartsOn, EndsOn: req.EndsOn, HalfDay: req.HalfDay}
}

func (s *Server) handleRecordAbsence(w http.ResponseWriter, r *http.Request) {
	var req absenceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	userID := userFrom(r)
	if req.UserID != nil {
		userID = *req.UserID
	}
	created, lsn, err := s.Availability.RecordAbsence(r.Context(), s.recorder(r), userID, req.input())
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"absence": created})
}

func (s *Server) handleUpdateAbsence(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "absenceID", "absence")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req absenceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.UserID != nil {
		respondError(w, r, ErrBadRequest("An absence stays with the person it was recorded for. Remove it and record one for the other person instead."))
		return
	}
	updated, lsn, err := s.Availability.UpdateAbsence(r.Context(), s.recorder(r), id, req.input())
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"absence": updated})
}

func (s *Server) handleRemoveAbsence(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "absenceID", "absence")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Availability.RemoveAbsence(r.Context(), s.recorder(r), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
