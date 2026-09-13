package httpapi

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/report"
)

// pathUUID reads an id out of the route, naming what was wrong in the words
// the reader used rather than the route's own parameter name.
func pathUUID(r *http.Request, name, noun string) (uuid.UUID, *APIError) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.Nil, ErrBadRequest("Invalid " + noun + " id.")
	}
	return id, nil
}

// queryUUID reads an optional id off the query string; absent is not an error.
func queryUUID(query url.Values, key, noun string) (*uuid.UUID, error) {
	raw := query.Get(key)
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, ErrBadRequest("Invalid " + noun + " id.")
	}
	return &id, nil
}

func (s *Server) handleListDashboards(w http.ResponseWriter, r *http.Request) {
	found, err := s.Reports.Dashboards(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"dashboards": found})
}

// handleReportKinds offers the reports a project of this kind can show.
func (s *Server) handleReportKinds(w http.ResponseWriter, r *http.Request) {
	p, err := s.Projects.ByKey(r.Context(), r.PathValue("projectKey"))
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"kinds": report.Kinds(project.Kind(p.Kind))})
}

// reportParams is what a widget asks a report with, all of it off the query
// string, which is how a widget passes its stored settings along.
func reportParams(r *http.Request) (report.Params, error) {
	query := r.URL.Query()
	var params report.Params
	if days := query.Get("days"); days != "" {
		params.Days, _ = strconv.Atoi(days)
	}
	var err error
	if params.TeamID, err = queryUUID(query, "team", "team"); err != nil {
		return params, err
	}
	if params.SprintID, err = queryUUID(query, "sprint", "sprint"); err != nil {
		return params, err
	}
	if params.VersionID, err = queryUUID(query, "version", "version"); err != nil {
		return params, err
	}
	if params.MilestoneID, err = queryUUID(query, "milestone", "milestone"); err != nil {
		return params, err
	}
	params.GroupBy, params.SplitBy, params.Measure = query.Get("groupBy"), query.Get("splitBy"), query.Get("measure")
	params.Shape, params.Interval, params.Series = query.Get("shape"), query.Get("interval"), query.Get("series")
	if q := query.Get("q"); q != "" {
		if params.Narrow, err = compileQuery(q, userFrom(r)); err != nil {
			return params, err
		}
	}
	return params, nil
}

// handleReport computes one report.
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	params, err := reportParams(r)
	if err != nil {
		respondError(w, r, err)
		return
	}
	out, err := s.Reports.Report(r.Context(), r.PathValue("projectKey"), report.Kind(r.PathValue("kind")), params)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	respondJSON(w, r, http.StatusOK, out)
}

type createDashboardRequest struct {
	Name string `json:"name"`
	// Template is a built-in key or a saved template's id; empty starts blank.
	Template string `json:"template,omitempty"`
	// MilestoneID makes the dashboard about one milestone: the template's
	// filter and milestones widget are pinned to it.
	MilestoneID *uuid.UUID `json:"milestoneId,omitempty"`
}

func (s *Server) handleCreateDashboard(w http.ResponseWriter, r *http.Request) {
	var req createDashboardRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	created, lsn, err := s.Reports.CreateDashboardFrom(r.Context(), r.PathValue("projectKey"), report.CreateInput{Name: req.Name, Template: req.Template, MilestoneID: req.MilestoneID}, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"dashboard": created})
}

type renameDashboardRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleRenameDashboard(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "dashboardID", "dashboard")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req renameDashboardRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	lsn, err := s.Reports.RenameDashboard(r.Context(), id, req.Name)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

func (s *Server) handleDeleteDashboard(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "dashboardID", "dashboard")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Reports.DeleteDashboard(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

type widgetRequest struct {
	Kind   string         `json:"kind"`
	Title  *string        `json:"title"`
	Width  *int           `json:"width"`
	Config *report.Params `json:"config"`
}

func (s *Server) handleAddWidget(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "dashboardID", "dashboard")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req widgetRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	in := report.WidgetInput{Kind: report.Kind(req.Kind)}
	if req.Title != nil {
		in.Title = *req.Title
	}
	if req.Width != nil {
		in.Width = *req.Width
	}
	if req.Config != nil {
		in.Config = *req.Config
	}
	added, lsn, err := s.Reports.AddWidget(r.Context(), id, in)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"widget": added})
}

func (s *Server) handleUpdateWidget(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "widgetID", "widget")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req widgetRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	updated, lsn, err := s.Reports.UpdateWidget(r.Context(), id, report.UpdateWidgetInput{Title: req.Title, Width: req.Width, Config: req.Config})
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusOK, map[string]any{"widget": updated})
}

func (s *Server) handleRemoveWidget(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "widgetID", "widget")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Reports.RemoveWidget(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

type reorderWidgetsRequest struct {
	Order []uuid.UUID `json:"order"`
}

func (s *Server) handleReorderWidgets(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "dashboardID", "dashboard")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req reorderWidgetsRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	lsn, err := s.Reports.ReorderWidgets(r.Context(), id, req.Order)
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}

// handleListDashboardTemplates offers what a dashboard may start from: the
// built-ins that suit the project, then the organization's own.
func (s *Server) handleListDashboardTemplates(w http.ResponseWriter, r *http.Request) {
	kind := project.KindSoftware
	if key := r.URL.Query().Get("project"); key != "" {
		p, err := s.Projects.ByKey(r.Context(), key)
		if err != nil {
			respondError(w, r, err)
			return
		}
		kind = project.Kind(p.Kind)
	}
	templates, err := s.Reports.Templates(r.Context(), kind)
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, map[string]any{"templates": templates})
}

type saveTemplateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// handleSaveDashboardTemplate keeps a dashboard's arrangement for the
// organization, under a name of its own.
func (s *Server) handleSaveDashboardTemplate(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "dashboardID", "dashboard")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	var req saveTemplateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		respondError(w, r, err)
		return
	}
	saved, lsn, err := s.Reports.SaveTemplate(r.Context(), id, req.Name, req.Description, userFrom(r))
	if err != nil {
		respondError(w, r, asValidationError(err))
		return
	}
	NoteWrite(r.Context(), lsn)
	respondJSON(w, r, http.StatusCreated, map[string]any{"template": saved})
}

func (s *Server) handleDeleteDashboardTemplate(w http.ResponseWriter, r *http.Request) {
	id, apiErr := pathUUID(r, "templateID", "template")
	if apiErr != nil {
		respondError(w, r, apiErr)
		return
	}
	lsn, err := s.Reports.DeleteTemplate(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}
	NoteWrite(r.Context(), lsn)
	respondNoContent(w)
}
