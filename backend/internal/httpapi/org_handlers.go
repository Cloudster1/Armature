package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/calendar"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/project"
)

// The organization's own pages: the audit log, fields every project shares,
// how a project says it is doing, and a project's month.

// auditFilter reads the log's query string; a date is a day, inclusive.
func auditFilter(r *http.Request) (audit.Filter, error) {
	q := r.URL.Query()
	f := audit.Filter{Action: q.Get("action"), Limit: queryInt(r, "limit", 50, 1, audit.MaxPage)}
	if raw := q.Get("actor"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return f, ErrBadRequest("The actor is a user id.")
		}
		f.ActorID = &id
	}
	day := func(name string) (*time.Time, error) {
		raw := q.Get(name)
		if raw == "" {
			return nil, nil
		}
		t, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return nil, ErrBadRequest("Dates are written as YYYY-MM-DD.")
		}
		return &t, nil
	}
	var err error
	if f.From, err = day("from"); err != nil {
		return f, err
	}
	if f.To, err = day("to"); err != nil {
		return f, err
	}
	if f.To != nil {
		end := f.To.AddDate(0, 0, 1)
		f.To = &end
	}
	if raw := q.Get("before"); raw != "" {
		t, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return f, ErrBadRequest("The before cursor is the createdAt of the last row shown.")
		}
		f.Before = &t
	}
	return f, nil
}

func (s *Server) handleAuditLog(w http.ResponseWriter, r *http.Request) {
	f, err := auditFilter(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	rows, err := s.Audit.List(r.Context(), f)
	if err != nil {
		respondError(w, r, err)
		return
	}
	actions, err := s.Audit.Actions(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"entries": rows, "actions": actions})
}

func (s *Server) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	f, err := auditFilter(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	body, err := s.Audit.CSV(r.Context(), f)
	if err != nil {
		respondError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="audit-log.csv"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) handleListOrgFields(w http.ResponseWriter, r *http.Request) {
	fields, err := s.Fields.OrgFields(r.Context())
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"fields": fields})
}

func (s *Server) handleCreateOrgField(w http.ResponseWriter, r *http.Request) {
	var req fieldRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	in := field.Input{Kind: field.Kind(req.Kind)}
	if req.Name != nil {
		in.Name = *req.Name
	}
	if req.Options != nil {
		in.Options = *req.Options
	}
	created, lsn, err := s.Fields.CreateOrg(r.Context(), in)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"field": created})
}

func (s *Server) handlePromoteField(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("fieldID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid field id."))
		return
	}
	promoted, lsn, err := s.Fields.Promote(r.Context(), id)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"field": promoted})
}

type statusUpdateRequest struct {
	Status   string  `json:"status"`
	Note     string  `json:"note,omitempty"`
	TargetOn *string `json:"targetOn,omitempty"`
}

func (s *Server) handleListStatusUpdates(w http.ResponseWriter, r *http.Request) {
	updates, err := s.Projects.Updates(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"updates": updates})
}

func (s *Server) handlePostStatusUpdate(w http.ResponseWriter, r *http.Request) {
	var req statusUpdateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	posted, lsn, err := s.Projects.PostUpdate(r.Context(), r.PathValue("projectKey"), project.StatusInput{
		Status: project.Health(req.Status), Note: req.Note, TargetOn: req.TargetOn,
	}, actorFrom(r).UserID)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"update": posted})
}

func (s *Server) handleCalendarMonth(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("month")
	if raw == "" {
		raw = time.Now().UTC().Format("2006-01")
	}
	year, month, err := calendar.ParseMonth(raw)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	out, err := s.Calendar.Month(r.Context(), r.PathValue("projectKey"), year, month)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"month": out})
}

// administersOrgOrProject is the check behind a field addressed by id: an
// organization field is the organization's administrators' to change.
func (s *Server) administersField(r *http.Request, f *field.Field) bool {
	if f.Org {
		return PermsFrom(r.Context()).CanInOrg(perm.OrgAdminister)
	}
	return PermsFrom(r.Context()).Can(perm.ProjectAdminister, f.ProjectKey)
}
