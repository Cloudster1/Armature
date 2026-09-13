package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/perm"
)

// Custom fields: defined per project by its administrators, answered per issue
// by anyone who may edit it.

func (s *Server) handleFieldKinds(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]any{"kinds": field.Kinds()})
}

func (s *Server) handleListFields(w http.ResponseWriter, r *http.Request) {
	fields, err := s.Fields.Fields(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"fields": fields})
}

type fieldRequest struct {
	Name    *string   `json:"name,omitempty"`
	Kind    string    `json:"kind,omitempty"`
	Options *[]string `json:"options,omitempty"`
}

func (s *Server) handleCreateField(w http.ResponseWriter, r *http.Request) {
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
	created, lsn, err := s.Fields.Create(r.Context(), r.PathValue("projectKey"), in)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"field": created})
}

// administeredField loads a field addressed by id and checks the caller
// administers the project it belongs to. The route only knows the caller holds
// the permission somewhere; this is where "somewhere" becomes "here".
func (s *Server) administeredField(w http.ResponseWriter, r *http.Request) (*field.Field, bool) {
	id, err := uuid.Parse(r.PathValue("fieldID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid field id."))
		return nil, false
	}
	found, err := s.Fields.Field(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return nil, false
	}
	if !s.administersField(r, found) {
		respondError(w, r, forbidden(perm.ProjectAdminister))
		return nil, false
	}
	return found, true
}

func (s *Server) handleUpdateField(w http.ResponseWriter, r *http.Request) {
	found, ok := s.administeredField(w, r)
	if !ok {
		return
	}
	var req fieldRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Fields.Update(r.Context(), found.ID, field.UpdateInput{Name: req.Name, Options: req.Options})
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"field": updated})
}

func (s *Server) handleDeleteField(w http.ResponseWriter, r *http.Request) {
	found, ok := s.administeredField(w, r)
	if !ok {
		return
	}
	lsn, err := s.Fields.Delete(r.Context(), found.ID)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleIssueFields(w http.ResponseWriter, r *http.Request) {
	values, err := s.Fields.Values(r.Context(), r.PathValue("issueKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"values": values})
}

type setIssueFieldRequest struct {
	Value json.RawMessage `json:"value"`
}

func (s *Server) handleSetIssueField(w http.ResponseWriter, r *http.Request) {
	fieldID, err := uuid.Parse(r.PathValue("fieldID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid field id."))
		return
	}
	var req setIssueFieldRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	value, lsn, err := s.Fields.Set(r.Context(), r.PathValue("issueKey"), fieldID, req.Value, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"value": value})
}
