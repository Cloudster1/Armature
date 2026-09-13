package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/template"
)

type createProjectRequest struct {
	Key         string     `json:"key,omitempty"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Kind        string     `json:"kind,omitempty"`
	LeadID      *uuid.UUID `json:"leadId,omitempty"`
	SchemeID    *uuid.UUID `json:"workflowSchemeId,omitempty"`
	// Template is which of the templates to set the project up from. Empty is
	// the default one.
	Template string `json:"template,omitempty"`
}

// handleListTemplates offers the ways a project can be set up.
func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]any{"templates": template.All()})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	p := PrincipalFrom(r.Context())

	in := project.CreateInput{
		Key:         req.Key,
		Name:        req.Name,
		Description: req.Description,
		Kind:        project.Kind(req.Kind),
		LeadID:      req.LeadID,
	}
	if req.SchemeID != nil {
		in.SchemeID = *req.SchemeID
	}
	// A project with no stated lead belongs to whoever made it.
	if in.LeadID == nil {
		in.LeadID = &p.User.ID
	}

	created, lsn, err := s.Templates.Create(r.Context(), req.Template, in, p.User.ID)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"project": created})
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	includeArchived := r.URL.Query().Get("archived") == "true"

	projects, err := s.Projects.List(r.Context(), includeArchived)
	if err != nil {
		respondError(w, r, err)
		return
	}
	projects = readableProjects(r, projects)
	if projects == nil {
		projects = []project.Project{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"projects": projects})
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	found, err := s.Projects.ByKey(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"project": found})
}

type updateProjectRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Kind        *string `json:"kind,omitempty"`
	// LeadID is a double pointer so that an explicit null clears the lead,
	// distinctly from the field being absent.
	LeadID leadPatch `json:"leadId,omitempty"`
	// PortalVerifies is whether the desk's door asks for a code by mail.
	PortalVerifies *bool `json:"portalVerifies,omitempty"`
	// TrustedDomains replaces the desk's list of address domains; empty means everyone.
	TrustedDomains *[]string `json:"trustedDomains,omitempty"`
	// Features replaces the list of pages the project has.
	Features *[]string `json:"features,omitempty"`
}

// leadPatch distinguishes an absent field from an explicit null.
type leadPatch struct {
	Set   bool
	Value *uuid.UUID
}

func (l *leadPatch) UnmarshalJSON(data []byte) error {
	l.Set = true
	if string(data) == "null" {
		l.Value = nil
		return nil
	}
	var id uuid.UUID
	if err := jsonUnmarshal(data, &id); err != nil {
		return err
	}
	l.Value = &id
	return nil
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	var req updateProjectRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	in := project.UpdateInput{Name: req.Name, Description: req.Description, PortalVerifies: req.PortalVerifies, TrustedDomains: req.TrustedDomains, Features: req.Features}
	if req.Kind != nil {
		kind := project.Kind(*req.Kind)
		in.Kind = &kind
	}
	if req.LeadID.Set {
		value := req.LeadID.Value
		in.LeadID = &value
	}

	updated, lsn, err := s.Projects.Update(r.Context(), r.PathValue("projectKey"), in)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"project": updated})
}

func (s *Server) handleArchiveProject(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Projects.Archive(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleRestoreProject(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Projects.Restore(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// handleCheckProjectKey answers whether a key is free, so the creation form can
// say so before the user submits.
func (s *Server) handleCheckProjectKey(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		// With no key to check, suggest one from the name instead.
		suggestion := project.SuggestKey(r.URL.Query().Get("name"))
		respondJSON(w, r, http.StatusOK, map[string]any{"suggestion": suggestion})
		return
	}

	available, err := s.Projects.KeyAvailable(r.Context(), key)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{
		"key":       project.NormalizeKey(key),
		"valid":     project.ValidKey(project.NormalizeKey(key)),
		"available": available,
	})
}

// queryInt reads a bounded integer from the query string.
func queryInt(r *http.Request, name string, def, min, max int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	if n < min {
		return min
	}
	if n > max {
		return max
	}
	return n
}

var _ = errors.Is
