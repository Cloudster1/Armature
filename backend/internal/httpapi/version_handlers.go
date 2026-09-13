package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/component"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/version"
)

// Versions: what a project ships. Planning them is the scrum master's job,
// like sprints and milestones; naming one on an issue is editing the issue.

// versionRequest is a version as the page sends it. Dates are days; a null
// clears one on an edit.
type versionRequest struct {
	Name        *string    `json:"name,omitempty"`
	Description *string    `json:"description,omitempty"`
	StartOn     *time.Time `json:"startOn,omitempty"`
	ReleaseOn   *time.Time `json:"releaseOn,omitempty"`
	// ClearStart and ClearRelease take a date away, since absent means untouched.
	ClearStart   bool `json:"clearStart,omitempty"`
	ClearRelease bool `json:"clearRelease,omitempty"`
}

func (r versionRequest) input() version.Input {
	in := version.Input{Name: r.Name, Description: r.Description}
	if r.StartOn != nil || r.ClearStart {
		in.StartOn = &r.StartOn
	}
	if r.ReleaseOn != nil || r.ClearRelease {
		in.ReleaseOn = &r.ReleaseOn
	}
	return in
}

func (s *Server) handleListVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := s.Versions.List(r.Context(), r.PathValue("projectKey"), r.URL.Query().Get("archived") == "true")
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"versions": versions})
}

func (s *Server) handleCreateVersion(w http.ResponseWriter, r *http.Request) {
	var req versionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Versions.Create(r.Context(), r.PathValue("projectKey"), req.input(), userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"version": made})
}

func (s *Server) handleUpdateVersion(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "versionID", "version")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req versionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Versions.Update(r.Context(), id, req.input(), userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"version": updated})
}

func (s *Server) versionAction(w http.ResponseWriter, r *http.Request, act func(uuid.UUID) (*version.Version, db.LSN, error)) {
	id, apiErr := pathUUID(r, "versionID", "version")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	v, lsn, err := act(id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"version": v})
}

func (s *Server) handleReleaseVersion(w http.ResponseWriter, r *http.Request) {
	s.versionAction(w, r, func(id uuid.UUID) (*version.Version, db.LSN, error) {
		return s.Versions.Release(r.Context(), id, userFrom(r))
	})
}

func (s *Server) handleUnreleaseVersion(w http.ResponseWriter, r *http.Request) {
	s.versionAction(w, r, func(id uuid.UUID) (*version.Version, db.LSN, error) {
		return s.Versions.Unrelease(r.Context(), id, userFrom(r))
	})
}

func (s *Server) handleArchiveVersion(w http.ResponseWriter, r *http.Request) {
	s.versionAction(w, r, func(id uuid.UUID) (*version.Version, db.LSN, error) {
		return s.Versions.Archive(r.Context(), id, userFrom(r))
	})
}

func (s *Server) handleDeleteVersion(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "versionID", "version")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Versions.Delete(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleReleaseNotes(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "versionID", "version")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	notes, err := s.Versions.ReleaseNotes(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"notes": notes})
}

// setIssueVersionsRequest names both sets in full; an absent list is empty.
type setIssueVersionsRequest struct {
	Fix     []uuid.UUID `json:"fix"`
	Affects []uuid.UUID `json:"affects"`
}

func (s *Server) handleSetIssueVersions(w http.ResponseWriter, r *http.Request) {
	var req setIssueVersionsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Issues.SetVersions(r.Context(), r.PathValue("issueKey"), req.Fix, req.Affects, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"issue": updated})
}

// Components: a project's parts. Configuring them is administering the project.

type componentRequest struct {
	Name              *string    `json:"name,omitempty"`
	Description       *string    `json:"description,omitempty"`
	LeadID            *uuid.UUID `json:"leadId,omitempty"`
	DefaultAssigneeID *uuid.UUID `json:"defaultAssigneeId,omitempty"`
	ClearLead         bool       `json:"clearLead,omitempty"`
	ClearDefault      bool       `json:"clearDefaultAssignee,omitempty"`
}

func (r componentRequest) input() component.Input {
	in := component.Input{Name: r.Name, Description: r.Description}
	if r.LeadID != nil || r.ClearLead {
		in.LeadID = &r.LeadID
	}
	if r.DefaultAssigneeID != nil || r.ClearDefault {
		in.DefaultAssigneeID = &r.DefaultAssigneeID
	}
	return in
}

func (s *Server) handleListComponents(w http.ResponseWriter, r *http.Request) {
	components, err := s.Components.List(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"components": components})
}

func (s *Server) handleCreateComponent(w http.ResponseWriter, r *http.Request) {
	var req componentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	made, lsn, err := s.Components.Create(r.Context(), r.PathValue("projectKey"), req.input())
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"component": made})
}

func (s *Server) handleUpdateComponent(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "componentID", "component")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req componentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Components.Update(r.Context(), id, req.input())
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"component": updated})
}

func (s *Server) handleDeleteComponent(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "componentID", "component")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Components.Delete(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

type setIssueComponentsRequest struct {
	Components []uuid.UUID `json:"components"`
}

func (s *Server) handleSetIssueComponents(w http.ResponseWriter, r *http.Request) {
	var req setIssueComponentsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Issues.SetComponents(r.Context(), r.PathValue("issueKey"), req.Components, actorFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"issue": updated})
}
