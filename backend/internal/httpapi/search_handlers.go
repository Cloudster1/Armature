package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/nql"
	"github.com/armature/armature/backend/internal/workflow"
)

// compileQuery turns the q parameter into SQL for the person asking. A bad
// query is an *nql.Error, which the error mapping answers with its position.
func compileQuery(text string, userID uuid.UUID) (*nql.Compiled, error) {
	parsed, err := nql.Parse(text)
	if err != nil {
		return nil, err
	}
	return parsed.Compile(nql.Env{UserID: userID, Now: time.Now()})
}

// handleListIssues serves both the project issue list and a cross-project
// search, depending on whether the path carries a project key.
func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	p := PrincipalFrom(r.Context())

	filter := issue.Filter{
		ProjectKey: r.PathValue("projectKey"),
		Text:       query.Get("text"),
	}
	if filter.ProjectKey == "" {
		filter.ProjectKey = query.Get("project")
	}
	filter = readableFilter(r, filter)

	for _, raw := range query["status"] {
		id, err := uuid.Parse(raw)
		if err != nil {
			respondError(w, r, ErrBadRequest("Invalid status id: "+raw))
			return
		}
		filter.StatusIDs = append(filter.StatusIDs, id)
	}
	for _, raw := range query["category"] {
		category := workflow.StatusCategory(raw)
		if !category.Valid() {
			respondError(w, r, ErrBadRequest("Unknown status category: "+raw))
			return
		}
		filter.Categories = append(filter.Categories, category)
	}
	for _, raw := range query["type"] {
		id, err := uuid.Parse(raw)
		if err != nil {
			respondError(w, r, ErrBadRequest("Invalid issue type id: "+raw))
			return
		}
		filter.TypeIDs = append(filter.TypeIDs, id)
	}
	if raw := query.Get("milestone"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			respondError(w, r, ErrBadRequest("Invalid milestone id: "+raw))
			return
		}
		filter.MilestoneID = &id
	}
	for _, raw := range query["label"] {
		id, err := uuid.Parse(raw)
		if err != nil {
			respondError(w, r, ErrBadRequest("Invalid label id: "+raw))
			return
		}
		filter.LabelIDs = append(filter.LabelIDs, id)
	}
	for _, raw := range query["priority"] {
		priority := issue.Priority(raw)
		if !priority.Valid() {
			respondError(w, r, ErrBadRequest("Unknown priority: "+raw))
			return
		}
		filter.Priorities = append(filter.Priorities, priority)
	}

	switch assignee := query.Get("assignee"); assignee {
	case "":
	case "me":
		filter.AssigneeID = &p.User.ID
	case "none", "unassigned":
		filter.Unassigned = true
	default:
		id, err := uuid.Parse(assignee)
		if err != nil {
			respondError(w, r, ErrBadRequest("Invalid assignee: "+assignee))
			return
		}
		filter.AssigneeID = &id
	}

	if reporter := query.Get("reporter"); reporter != "" {
		if reporter == "me" {
			filter.ReporterID = &p.User.ID
		} else {
			id, err := uuid.Parse(reporter)
			if err != nil {
				respondError(w, r, ErrBadRequest("Invalid reporter: "+reporter))
				return
			}
			filter.ReporterID = &id
		}
	}

	if q := strings.TrimSpace(query.Get("q")); q != "" {
		compiled, err := compileQuery(q, p.User.ID)
		if err != nil {
			respondError(w, r, err)
			return
		}
		filter.Query = compiled
	}

	page := issue.Page{
		Limit:   queryInt(r, "limit", 50, 1, 200),
		Offset:  queryInt(r, "offset", 0, 0, 1_000_000),
		OrderBy: query.Get("orderBy"),
		Desc:    query.Get("order") != "asc",
	}

	result, err := s.Issues.List(r.Context(), filter, page)
	if err != nil {
		respondError(w, r, err)
		return
	}
	if result.Issues == nil {
		result.Issues = []issue.Issue{}
	}
	respondJSON(w, r, http.StatusOK, result)
}

// handleSuggest answers the search bar as it is typed: the words the query
// language takes at the caret, and the issues the words so far find.
func (s *Server) handleSuggest(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	at := len([]rune(query.Get("q")))
	if raw := query.Get("at"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			respondError(w, r, ErrBadRequest("at is the caret's position in q, a whole number."))
			return
		}
		at = n
	}
	all, within := PermsFrom(r.Context()).Readable()
	found, err := s.Issues.Suggest(r.Context(), issue.SuggestInput{
		Query:      query.Get("q"),
		At:         at,
		ProjectKey: query.Get("project"),
		Viewer:     actorFrom(r),
		Limit:      queryInt(r, "limit", suggestLimit, 1, 20),
		Within:     within,
		Scoped:     !all,
	})
	if err != nil {
		respondError(w, r, err)
		return
	}
	respondJSON(w, r, http.StatusOK, found)
}

// suggestLimit is how many words and how many issues the bar offers: enough
// to pick from, few enough to read at a glance.
const suggestLimit = 6
