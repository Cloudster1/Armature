package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/project"
)

type createProjectRequest struct {
	Key         string     `json:"key,omitempty"`
	Name        string     `json:"name"`
	Description string     `json:"description,omitempty"`
	Kind        string     `json:"kind,omitempty"`
	LeadID      *uuid.UUID `json:"leadId,omitempty"`
	SchemeID    *uuid.UUID `json:"workflowSchemeId,omitempty"`
	// Template is a built-in's key or a stored template's id to set the project
	// up from. Empty is the default one.
	Template string `json:"template,omitempty"`
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

// maxProjectOffset bounds how far into the projects a page may start, as for issues.
const maxProjectOffset = 1 << 20

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := project.ListFilter{
		IncludeArchived: q.Get("archived") == "true",
		Text:            q.Get("q"),
		Limit:           queryInt(r, "limit", project.DefaultProjectPage, 1, project.MaxProjectPage),
		Offset:          queryInt(r, "offset", 0, 0, maxProjectOffset),
	}
	if all, keys := PermsFrom(r.Context()).Readable(); !all {
		f.Scoped = true
		f.Within = keys
	}

	page, err := s.Projects.List(r.Context(), f)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, page)
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
	// RevealsIssues is whether somebody who may not see the project is told
	// that an issue of it exists, with 403 not_permitted, instead of 404.
	RevealsIssues *bool `json:"revealsIssues,omitempty"`
	// TrustedDomains replaces the desk's list of address domains; empty means everyone.
	TrustedDomains *[]string `json:"trustedDomains,omitempty"`
	// MailRequestTypeID is what a new mail to the desk becomes; null stops
	// mail raising requests.
	MailRequestTypeID uuidPatch `json:"mailRequestTypeId,omitempty"`
	// Features replaces the list of pages the project has.
	Features *[]string `json:"features,omitempty"`
	// DocsURL is where the project's documentation lives; an empty string
	// clears the link. DocsLabel names it in the sidebar.
	DocsURL   *string `json:"docsUrl,omitempty"`
	DocsLabel *string `json:"docsLabel,omitempty"`
	// PlanningMethod is scrum or kanban; turning to scrum takes the grouping
	// back to teams. ResourceGrouping is team, or person on a kanban project.
	PlanningMethod   *project.PlanningMethod   `json:"planningMethod,omitempty"`
	ResourceGrouping *project.ResourceGrouping `json:"resourceGrouping,omitempty"`
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

	in := project.UpdateInput{Name: req.Name, Description: req.Description, PortalVerifies: req.PortalVerifies, RevealsIssues: req.RevealsIssues, TrustedDomains: req.TrustedDomains, Features: req.Features, DocsURL: req.DocsURL, DocsLabel: req.DocsLabel}
	if req.Kind != nil {
		kind := project.Kind(*req.Kind)
		in.Kind = &kind
	}
	if req.PlanningMethod != nil {
		method := string(*req.PlanningMethod)
		in.PlanningMethod = &method
	}
	if req.ResourceGrouping != nil {
		grouping := string(*req.ResourceGrouping)
		in.ResourceGrouping = &grouping
	}
	if req.LeadID.Set {
		value := req.LeadID.Value
		in.LeadID = &value
	}
	if req.MailRequestTypeID.Set {
		value := req.MailRequestTypeID.Value
		in.MailRequestTypeID = &value
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
	lsn, err := s.Projects.Archive(r.Context(), r.PathValue("projectKey"), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleRestoreProject(w http.ResponseWriter, r *http.Request) {
	lsn, err := s.Projects.Restore(r.Context(), r.PathValue("projectKey"), userFrom(r))
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
