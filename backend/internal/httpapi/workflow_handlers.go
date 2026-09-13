package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/workflow"
)

// Workflows are configured at two levels. The organization keeps one scheme
// that answers for every project; a project may name its own, which is
// consulted first and need only cover the issue types it disagrees about.

func userFrom(r *http.Request) uuid.UUID {
	if p := PrincipalFrom(r.Context()); p != nil {
		return p.User.ID
	}
	return uuid.Nil
}

// graphRequest is a workflow as the editor sends it: statuses, and the
// transitions between them.
type graphRequest struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Steps       []nodeRequest `json:"steps"`
	Transitions []edgeRequest `json:"transitions"`
}

type nodeRequest struct {
	StatusID  uuid.UUID `json:"statusId"`
	IsInitial bool      `json:"isInitial"`
	// Layout is where the designer drew the status; absent leaves it unplaced.
	Layout *workflow.Point `json:"layout,omitempty"`
}

type edgeRequest struct {
	ID           uuid.UUID  `json:"id"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	FromStatusID *uuid.UUID `json:"fromStatusId"`
	ToStatusID   uuid.UUID  `json:"toStatusId"`
	// Rules, when present, replace the transition's rules; left out, an
	// existing transition keeps the ones it has.
	Rules *[]ruleRequest `json:"rules,omitempty"`
}

type ruleRequest struct {
	Kind   workflow.RuleKind `json:"kind"`
	Type   string            `json:"type"`
	Config json.RawMessage   `json:"config,omitempty"`
}

func (g graphRequest) input() workflow.GraphInput {
	in := workflow.GraphInput{Name: g.Name, Description: g.Description}
	for _, step := range g.Steps {
		in.Steps = append(in.Steps, workflow.StepInput{
			StatusID: step.StatusID, IsInitial: step.IsInitial, Layout: step.Layout,
		})
	}
	for _, t := range g.Transitions {
		var rules []workflow.RuleInput
		if t.Rules != nil {
			rules = make([]workflow.RuleInput, 0, len(*t.Rules))
			for _, rule := range *t.Rules {
				rules = append(rules, workflow.RuleInput{Kind: rule.Kind, Type: rule.Type, Config: string(rule.Config)})
			}
		}
		in.Transitions = append(in.Transitions, workflow.TransitionInput{
			ID:           t.ID,
			Name:         t.Name,
			Description:  t.Description,
			FromStatusID: t.FromStatusID,
			ToStatusID:   t.ToStatusID,
			Rules:        rules,
		})
	}
	return in
}

type createStatusRequest struct {
	Name        string `json:"name"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

// handleCreateStatus coins a status for the organization, which every workflow
// may then be built from.
func (s *Server) handleCreateStatus(w http.ResponseWriter, r *http.Request) {
	var req createStatusRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	created, lsn, err := s.Workflow.Admin.CreateStatus(r.Context(), workflow.StatusInput{
		Name: req.Name, Category: workflow.StatusCategory(req.Category), Description: req.Description,
	}, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"status": created})
}

func (s *Server) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	var req graphRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	created, lsn, err := s.Workflow.Admin.CreateWorkflow(r.Context(), req.input(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"workflow": created})
}

func (s *Server) handleSaveWorkflow(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("workflowID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid workflow id."))
		return
	}
	var req graphRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	saved, lsn, err := s.Workflow.Admin.SaveWorkflow(r.Context(), id, req.input(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"workflow": saved})
}

type copyWorkflowRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleCopyWorkflow(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("workflowID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid workflow id."))
		return
	}
	var req copyWorkflowRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	copied, lsn, err := s.Workflow.Admin.CopyWorkflow(r.Context(), id, req.Name, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"workflow": copied})
}

func (s *Server) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("workflowID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid workflow id."))
		return
	}
	lsn, err := s.Workflow.Admin.DeleteWorkflow(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// handleListSchemes returns every scheme with its mappings, and says which one
// the organization answers with.
func (s *Server) handleListSchemes(w http.ResponseWriter, r *http.Request) {
	schemes := []workflow.Scheme{}
	err := s.DB.Read(r.Context(), func(ctx context.Context, tx db.DBTX) error {
		found, err := s.Workflow.Store.ListSchemes(ctx, tx)
		if err != nil {
			return err
		}
		if found != nil {
			schemes = found
		}
		return nil
	})
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"schemes": schemes})
}

type schemeRequest struct {
	Name  string `json:"name"`
	Items []struct {
		IssueTypeID *uuid.UUID `json:"issueTypeId"`
		WorkflowID  uuid.UUID  `json:"workflowId"`
	} `json:"items"`
}

func (s schemeRequest) input() workflow.SchemeInput {
	in := workflow.SchemeInput{Name: s.Name}
	for _, item := range s.Items {
		in.Items = append(in.Items, workflow.SchemeItemInput{
			IssueTypeID: item.IssueTypeID,
			WorkflowID:  item.WorkflowID,
		})
	}
	return in
}

func (s *Server) handleCreateScheme(w http.ResponseWriter, r *http.Request) {
	var req schemeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	created, lsn, err := s.Workflow.Admin.CreateScheme(r.Context(), req.input(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"scheme": created})
}

func (s *Server) handleSaveScheme(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("schemeID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid scheme id."))
		return
	}
	var req schemeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	saved, lsn, err := s.Workflow.Admin.SaveScheme(r.Context(), id, req.input(), userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"scheme": saved})
}

// handleSetDefaultScheme promotes a scheme to be the organization's own.
func (s *Server) handleSetDefaultScheme(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("schemeID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid scheme id."))
		return
	}
	lsn, err := s.Workflow.Admin.SetDefaultScheme(r.Context(), id, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleDeleteScheme(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("schemeID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid scheme id."))
		return
	}
	lsn, err := s.Workflow.Admin.DeleteScheme(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// handleProjectWorkflows answers the only question a project administrator
// actually has: which workflow does each issue type use here, and did this
// project decide that or did the organization?
func (s *Server) handleProjectWorkflows(w http.ResponseWriter, r *http.Request) {
	found, err := s.Projects.ByKey(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}

	assignments := []workflow.Assignment{}
	err = s.DB.Read(r.Context(), func(ctx context.Context, tx db.DBTX) error {
		resolved, err := s.Workflow.Store.Assignments(ctx, tx, found.ID)
		if err != nil {
			return err
		}
		if resolved != nil {
			assignments = resolved
		}
		return nil
	})
	if err != nil {
		respondError(w, r, err)
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]any{
		"projectKey":  found.Key,
		"schemeId":    found.WorkflowSchemeID,
		"assignments": assignments,
	})
}

type setProjectSchemeRequest struct {
	SchemeID *uuid.UUID `json:"schemeId"`
}

// handleSetProjectScheme gives a project a scheme of its own, or takes it away.
// A null scheme is not an omission: it is the project saying it has no opinion.
func (s *Server) handleSetProjectScheme(w http.ResponseWriter, r *http.Request) {
	var req setProjectSchemeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	updated, lsn, err := s.Projects.SetWorkflowScheme(r.Context(), r.PathValue("projectKey"), req.SchemeID, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"project": updated})
}

type setAssignmentRequest struct {
	// WorkflowID is the workflow the issue type follows here; null hands the
	// type back to the organization.
	WorkflowID *uuid.UUID `json:"workflowId"`
}

// handleSetProjectAssignment lets a project administrator decide one issue
// type without naming a scheme; the scheme is made and kept behind the row.
func (s *Server) handleSetProjectAssignment(w http.ResponseWriter, r *http.Request) {
	issueTypeID, err := uuid.Parse(r.PathValue("issueTypeID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid issue type id."))
		return
	}
	var req setAssignmentRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}

	updated, lsn, err := s.Projects.SetWorkflowAssignment(r.Context(), r.PathValue("projectKey"), issueTypeID, req.WorkflowID, userFrom(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)

	// The answer is read from the primary: the write is a moment old and the
	// page redraws from this response alone.
	assignments := []workflow.Assignment{}
	err = s.DB.ReadPrimary(r.Context(), func(ctx context.Context, tx db.DBTX) error {
		resolved, err := s.Workflow.Store.Assignments(ctx, tx, updated.ID)
		if err != nil {
			return err
		}
		if resolved != nil {
			assignments = resolved
		}
		return nil
	})
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{
		"projectKey":  updated.Key,
		"schemeId":    updated.WorkflowSchemeID,
		"assignments": assignments,
	})
}
