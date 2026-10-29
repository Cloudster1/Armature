package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/auth"
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

// maxLibraryOffset bounds how far into the workflows or schemes a page may start.
const maxLibraryOffset = 1 << 20

// libraryFilter reads the search and the page a workflow or scheme listing asks for.
func libraryFilter(r *http.Request) workflow.ListFilter {
	return workflow.ListFilter{
		Text:   r.URL.Query().Get("q"),
		Limit:  queryInt(r, "limit", workflow.DefaultPage, 1, workflow.MaxPage),
		Offset: queryInt(r, "offset", 0, 0, maxLibraryOffset),
	}
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	var page workflow.SummaryPage
	err := s.DB.Read(r.Context(), func(ctx context.Context, tx db.DBTX) error {
		var err error
		page, err = s.Workflow.Store.List(ctx, tx, libraryFilter(r))
		return err
	})
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, page)
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

// maxPeopleOffset bounds how far into the people a page may start, as for issues.
const maxPeopleOffset = 1 << 20

// peopleFilter reads the search and the page a list of people is asked for.
func peopleFilter(r *http.Request) auth.PeopleFilter {
	return auth.PeopleFilter{
		Text:   r.URL.Query().Get("q"),
		Staff:  r.URL.Query().Get("staff") == "true",
		Limit:  queryInt(r, "limit", auth.DefaultPeoplePage, 1, auth.MaxPeoplePage),
		Offset: queryInt(r, "offset", 0, 0, maxPeopleOffset),
	}
}

// handleListMembers backs every person picker, a page of matches at a time.
func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	page, err := s.Auth.Members(r.Context(), peopleFilter(r))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, page)
}
