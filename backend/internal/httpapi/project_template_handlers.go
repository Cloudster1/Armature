package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/template"
)

// Project templates: the built-ins in code and the ones administrators keep.
// Anyone who may make a project reads them; administrators keep the stored ones.

// handleListTemplates offers the ways a project can be set up.
func (s *Server) handleListTemplates(w http.ResponseWriter, r *http.Request) {
	archived := r.URL.Query().Get("archived") == "true"
	if archived && !PermsFrom(r.Context()).CanInOrg(perm.OrgAdminister) {
		respondError(w, r, ErrForbidden("Only an administrator sees archived project templates. Ask one to restore the template you need."))
		return
	}
	templates, err := s.Templates.List(r.Context(), archived)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"templates": templates})
}

type projectTemplateRequest struct {
	// From is a built-in key or a stored template's id to start as a copy of; only when making one.
	From           *string                 `json:"from,omitempty"`
	Name           *string                 `json:"name,omitempty"`
	Description    *string                 `json:"description,omitempty"`
	Kind           *project.Kind           `json:"kind,omitempty"`
	BoardType      *board.Type             `json:"boardType,omitempty"`
	PlanningMethod *project.PlanningMethod `json:"planningMethod,omitempty"`
	Features       []string                `json:"features,omitempty"`
	SchemeID       *uuid.UUID              `json:"workflowSchemeId,omitempty"`
	// DefaultScheme true has projects made from it follow everyone's default scheme.
	DefaultScheme bool  `json:"defaultWorkflowScheme,omitempty"`
	Desk          *bool `json:"desk,omitempty"`
	// IssueTypeIDs are the types its projects allow, in order.
	IssueTypeIDs []uuid.UUID `json:"issueTypeIds,omitempty"`
}

func (req projectTemplateRequest) input() (template.Input, error) {
	in := template.Input{
		Name: req.Name, Description: req.Description, Kind: req.Kind, BoardType: req.BoardType, PlanningMethod: req.PlanningMethod,
		SchemeID: req.SchemeID, DefaultScheme: req.DefaultScheme, Desk: req.Desk, IssueTypeIDs: req.IssueTypeIDs,
	}
	if req.Features != nil {
		features, err := project.NormalizeFeatures(req.Features)
		if err != nil {
			return in, err
		}
		in.Features = features
	}
	return in, nil
}

func templateActor(r *http.Request) template.Actor {
	return template.Actor{UserID: PrincipalFrom(r.Context()).User.ID, IP: clientIP(r)}
}

// templateID reads a stored template's id; a built-in's key is refused for what it is.
func templateID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := r.PathValue("templateID")
	id, err := uuid.Parse(raw)
	if err == nil {
		return id, true
	}
	if _, builtin := template.Find(raw); builtin && raw != "" {
		respondError(w, r, toAPIError(template.ErrBuiltin))
	} else {
		respondError(w, r, toAPIError(template.ErrNotFound))
	}
	return uuid.Nil, false
}

func (s *Server) handleCreateProjectTemplate(w http.ResponseWriter, r *http.Request) {
	var req projectTemplateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	in, err := req.input()
	if err != nil {
		respondError(w, r, err)
		return
	}
	from := ""
	if req.From != nil {
		from = *req.From
	}
	made, lsn, err := s.Templates.Make(r.Context(), from, in, templateActor(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"template": made})
}

func (s *Server) handleUpdateProjectTemplate(w http.ResponseWriter, r *http.Request) {
	id, ok := templateID(w, r)
	if !ok {
		return
	}
	var req projectTemplateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	if req.From != nil {
		respondError(w, r, ErrBadRequest("A template is copied when it is made. Leave out from, or duplicate the one you mean."))
		return
	}
	in, err := req.input()
	if err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Templates.Update(r.Context(), id, in, templateActor(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"template": updated})
}

func (s *Server) handleArchiveProjectTemplate(w http.ResponseWriter, r *http.Request) {
	s.setProjectTemplateArchived(w, r, s.Templates.Archive)
}

func (s *Server) handleRestoreProjectTemplate(w http.ResponseWriter, r *http.Request) {
	s.setProjectTemplateArchived(w, r, s.Templates.Restore)
}

func (s *Server) setProjectTemplateArchived(w http.ResponseWriter, r *http.Request, set func(ctx context.Context, id uuid.UUID, actor template.Actor) (*template.ProjectTemplate, db.LSN, error)) {
	id, ok := templateID(w, r)
	if !ok {
		return
	}
	out, lsn, err := set(r.Context(), id, templateActor(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"template": out})
}

// projectTemplateError words the template service's refusals, and the
// database's backstop for a template's standard type.
func projectTemplateError(err error) *APIError {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, template.ErrUnknown):
		return ErrBadRequest("There is no project template by that name. Pick one from the list again.")
	case errors.Is(err, template.ErrNotFound):
		return ErrNotFound("That project template was not found. Pick it from the list again.")
	case errors.Is(err, template.ErrArchived), errors.Is(err, template.ErrBuiltin), errors.Is(err, template.ErrNameTaken):
		return ErrConflict(capitalize(err.Error()) + ".")
	case errors.Is(err, template.ErrInvalid):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: withoutSentinel(err, template.ErrInvalid) + "."}
	case errors.As(err, &pgErr) && pgErr.ConstraintName == template.StandardTypeConstraint:
		return &APIError{Status: http.StatusConflict, Code: "no_standard_type", cause: err,
			Message: "A project template would be left without a standard issue type that is not retired. Add another standard type to it first."}
	}
	return nil
}
