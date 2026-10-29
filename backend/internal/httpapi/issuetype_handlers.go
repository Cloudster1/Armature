package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issuetype"
)

// Issue types: the library every project picks from. Anyone signed in reads
// it, since every picker needs it; its administrators keep it.

func (s *Server) handleListIssueTypes(w http.ResponseWriter, r *http.Request) {
	types, err := s.IssueTypes.List(r.Context(), r.URL.Query().Get("retired") == "true")
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"issueTypes": types})
}

type issueTypeRequest struct {
	Name        *string         `json:"name,omitempty"`
	Description *string         `json:"description,omitempty"`
	Icon        *issuetype.Icon `json:"icon,omitempty"`
	// Level is where the type sits in the hierarchy: -1 under another issue, 0 the working level, above that epics and up.
	Level *int `json:"level,omitempty"`
}

func issueTypeActor(r *http.Request) issuetype.Actor {
	return issuetype.Actor{UserID: PrincipalFrom(r.Context()).User.ID, IP: clientIP(r)}
}

func issueTypeID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("typeID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("That is not an issue type id. Pick the type from the list again."))
		return uuid.Nil, false
	}
	return id, true
}

func (s *Server) handleCreateIssueType(w http.ResponseWriter, r *http.Request) {
	var req issueTypeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	in := issuetype.Input{}
	if req.Name != nil {
		in.Name = *req.Name
	}
	if req.Description != nil {
		in.Description = *req.Description
	}
	if req.Icon != nil {
		in.Icon = *req.Icon
	}
	if req.Level != nil {
		in.Level = *req.Level
	}
	made, lsn, err := s.IssueTypes.Create(r.Context(), in, issueTypeActor(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"issueType": made})
}

func (s *Server) handleUpdateIssueType(w http.ResponseWriter, r *http.Request) {
	id, ok := issueTypeID(w, r)
	if !ok {
		return
	}
	var req issueTypeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.IssueTypes.Update(r.Context(), id, issuetype.UpdateInput{
		Name: req.Name, Description: req.Description, Icon: req.Icon, Level: req.Level,
	}, issueTypeActor(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"issueType": updated})
}

func (s *Server) handleRetireIssueType(w http.ResponseWriter, r *http.Request) {
	s.setIssueTypeRetired(w, r, s.IssueTypes.Retire)
}

func (s *Server) handleReinstateIssueType(w http.ResponseWriter, r *http.Request) {
	s.setIssueTypeRetired(w, r, s.IssueTypes.Reinstate)
}

func (s *Server) setIssueTypeRetired(w http.ResponseWriter, r *http.Request, set func(ctx context.Context, id uuid.UUID, actor issuetype.Actor) (*issuetype.IssueType, db.LSN, error)) {
	id, ok := issueTypeID(w, r)
	if !ok {
		return
	}
	out, lsn, err := set(r.Context(), id, issueTypeActor(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"issueType": out})
}

func (s *Server) handleDeleteIssueType(w http.ResponseWriter, r *http.Request) {
	id, ok := issueTypeID(w, r)
	if !ok {
		return
	}
	lsn, err := s.IssueTypes.Delete(r.Context(), id, issueTypeActor(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
