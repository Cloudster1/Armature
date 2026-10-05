package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/availability"
)

// Holiday calendars are read by every agent, since planning shows whose days
// are off; only an administrator changes them, or anybody's working week.

func (s *Server) handleListHolidayCalendars(w http.ResponseWriter, r *http.Request) {
	found, err := s.Availability.ListCalendars(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"calendars": found})
}

func (s *Server) handleGetHolidayCalendar(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "calendarID", "calendar")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	found, err := s.Availability.Calendar(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"calendar": found})
}

type holidayCalendarRequest struct {
	Name *string `json:"name"`
	// Default true makes this the calendar of everybody not given another.
	Default *bool `json:"default"`
}

func (s *Server) handleCreateHolidayCalendar(w http.ResponseWriter, r *http.Request) {
	var req holidayCalendarRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	created, lsn, err := s.Availability.CreateCalendar(r.Context(), availability.CalendarInput{Name: req.Name, Default: req.Default}, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"calendar": created})
}

func (s *Server) handleUpdateHolidayCalendar(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "calendarID", "calendar")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req holidayCalendarRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Availability.UpdateCalendar(r.Context(), id, availability.CalendarInput{Name: req.Name, Default: req.Default}, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"calendar": updated})
}

func (s *Server) handleDeleteHolidayCalendar(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "calendarID", "calendar")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Availability.DeleteCalendar(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

type holidayDaysRequest struct {
	Days []availability.Holiday `json:"days"`
}

func (s *Server) handleSetHolidays(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "calendarID", "calendar")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req holidayDaysRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Availability.ReplaceDays(r.Context(), id, req.Days, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"calendar": updated})
}

// handleImportHolidays adds the days of an .ics file to a calendar.
func (s *Server) handleImportHolidays(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "calendarID", "calendar")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, availability.MaxICSBytes+uploadSlack)
	reader, err := r.MultipartReader()
	if err != nil {
		respondError(w, r, ErrBadRequest("Send the calendar as multipart form data in a part named file."))
		return
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			respondError(w, r, ErrBadRequest("The upload has no part named file."))
			return
		}
		if err != nil {
			respondError(w, r, ErrBadRequest("The upload could not be read. Export the calendar again and upload the new copy."))
			return
		}
		if part.FormName() != "file" {
			continue
		}
		updated, imported, lsn, err := s.Availability.Import(r.Context(), id, part, userFrom(r))
		if err != nil {
			respondError(w, r, err)
			return
		}
		NoteWrite(r.Context(), lsn)
		respondJSON(w, r, http.StatusOK, map[string]any{"calendar": updated, "imported": imported})
		return
	}
}

// handleGetWorkingWeek answers a person about themselves, and an administrator
// about anybody: hours worked are a person's own business.
func (s *Server) handleGetWorkingWeek(w http.ResponseWriter, r *http.Request) {
	userID, apiErr := pathUUID(r, "userID", "user")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	if userID != userFrom(r) && !s.administers(r) {
		respondError(w, r, ErrForbidden("Only an administrator reads somebody else's working week. Ask them, or an administrator, instead."))
		return
	}
	found, err := s.Availability.WorkingWeek(r.Context(), userID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"week": found})
}

type workingWeekRequest struct {
	// CalendarID null follows the organization's default calendar.
	CalendarID *uuid.UUID `json:"calendarId"`
	// Minutes per weekday, mon to sun; a day left out is not worked, and none at all is the standard week.
	Minutes map[string]int `json:"minutes"`
}

func (s *Server) handleSetWorkingWeek(w http.ResponseWriter, r *http.Request) {
	userID, apiErr := pathUUID(r, "userID", "user")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req workingWeekRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	saved, lsn, err := s.Availability.SaveWorkingWeek(r.Context(), userID, availability.WeekInput{CalendarID: req.CalendarID, Minutes: req.Minutes}, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"week": saved})
}
