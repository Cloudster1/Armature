package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/workflow"
)

// The metadata endpoints back the pickers in the client: which issue types
// exist, which statuses, which workflows. They change rarely and are read
// constantly, so they are plain reads with no filtering.

func (s *Server) handleListStatuses(w http.ResponseWriter, r *http.Request) {
	var statuses []workflow.Status
	err := s.DB.Read(r.Context(), func(ctx context.Context, tx db.DBTX) error {
		var err error
		statuses, err = s.Workflow.Store.ListStatuses(ctx, tx)
		return err
	})
	if err != nil {
		respondError(w, r, err)
		return
	}
	if statuses == nil {
		statuses = []workflow.Status{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"statuses": statuses})
}

// issueTypeView is an issue type as the client sees it.
type issueTypeView struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Icon        string    `json:"icon"`
	Level       int       `json:"level"`
	IsSubtask   bool      `json:"isSubtask"`
	Position    int       `json:"position"`
}

func (s *Server) handleListIssueTypes(w http.ResponseWriter, r *http.Request) {
	types := []issueTypeView{}
	err := s.DB.Read(r.Context(), func(ctx context.Context, tx db.DBTX) error {
		// Highest level first, so a picker reads down the hierarchy the way the
		// tree is drawn rather than in whatever order the types were created.
		rows, err := tx.Query(ctx, `
			SELECT id, name, description, icon, hierarchy_level, is_subtask, position
			FROM issue_type ORDER BY hierarchy_level DESC, position, name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t issueTypeView
			if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.Icon, &t.Level, &t.IsSubtask, &t.Position); err != nil {
				return err
			}
			types = append(types, t)
		}
		return rows.Err()
	})
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"issueTypes": types})
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	var summaries []workflow.Summary
	err := s.DB.Read(r.Context(), func(ctx context.Context, tx db.DBTX) error {
		var err error
		summaries, err = s.Workflow.Store.List(ctx, tx)
		return err
	})
	if err != nil {
		respondError(w, r, err)
		return
	}
	if summaries == nil {
		summaries = []workflow.Summary{}
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"workflows": summaries})
}

// handleGetWorkflow returns one workflow with its whole graph, which is what
// the workflow editor and the "why can I not move this" question both need.
func (s *Server) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("workflowID"))
	if err != nil {
		respondError(w, r, ErrBadRequest("Invalid workflow id."))
		return
	}

	var wf *workflow.Workflow
	err = s.DB.Read(r.Context(), func(ctx context.Context, tx db.DBTX) error {
		var err error
		wf, err = s.Workflow.Store.Load(ctx, tx, id)
		return err
	})
	if err != nil {
		respondError(w, r, err)
		return
	}

	// The rules are not part of the Workflow JSON shape, so send them
	// alongside: an administrator needs to see why a transition is restricted.
	rules := map[string][]workflow.Rule{}
	for _, t := range wf.Transitions {
		if len(t.Rules) > 0 {
			rules[t.ID.String()] = t.Rules
		}
	}

	respondJSON(w, r, http.StatusOK, map[string]any{
		"workflow": wf,
		"rules":    rules,
	})
}

// handleWorkflowRuleTypes lists what a transition can be guarded and followed
// by, described for the designer's rule picker.
func (s *Server) handleWorkflowRuleTypes(w http.ResponseWriter, r *http.Request) {
	respondJSON(w, r, http.StatusOK, map[string]any{
		"ruleTypes": s.Workflow.Engine.Registry().Catalogue(),
	})
}

// memberView is a person as the assignee picker sees them.
type memberView struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	AvatarURL string    `json:"avatarUrl,omitempty"`
}

// handleListMembers backs the assignee picker.
func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	members := []memberView{}
	err := s.DB.Read(r.Context(), func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT u.id, u.name, u.email, m.org_role, COALESCE(u.avatar_url, '')
			FROM org_member m
			JOIN app_user u ON u.id = m.user_id
			WHERE u.is_active
			ORDER BY u.name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var m memberView
			if err := rows.Scan(&m.ID, &m.Name, &m.Email, &m.Role, &m.AvatarURL); err != nil {
				return err
			}
			members = append(members, m)
		}
		return rows.Err()
	})
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"members": members})
}
