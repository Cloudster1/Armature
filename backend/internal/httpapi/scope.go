package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/project"
)

// objectKind is something a route addresses by id, and the statement that
// finds its project; a null key is an object of the organization.
type objectKind struct {
	param   string
	lookup  string
	missing string
}

// viaIssue finds the project of a row that hangs off an issue.
func viaIssue(table string) string {
	return `SELECT p.key FROM ` + table + ` t JOIN issue i ON i.id = t.issue_id JOIN project p ON p.id = i.project_id WHERE t.id = $1`
}

// ofProject finds the project of a row that names it.
func ofProject(table string) string {
	return `SELECT p.key FROM ` + table + ` t JOIN project p ON p.id = t.project_id WHERE t.id = $1`
}

// ofDashboard finds the project of a row that hangs off a dashboard.
func ofDashboard(table string) string {
	return `SELECT p.key FROM ` + table + ` t JOIN dashboard d ON d.id = t.dashboard_id JOIN project p ON p.id = d.project_id WHERE t.id = $1`
}

var objectKinds = []objectKind{
	{"sprintID", ofProject("sprint"), "That sprint was not found."},
	{"milestoneID", ofProject("milestone"), "That milestone was not found."},
	{"versionID", ofProject("version"), "That version was not found."},
	{"componentID", ofProject("component"), "That component was not found."},
	{"teamID", ofProject("team"), "That team was not found."},
	{"boardID", ofProject("board"), "That board was not found."},
	{"swimlaneID", `SELECT p.key FROM board_swimlane t JOIN board b ON b.id = t.board_id JOIN project p ON p.id = b.project_id WHERE t.id = $1`, "That swimlane was not found."},
	{"repositoryID", ofProject("git_repository"), "That repository was not found."},
	{"dashboardID", ofProject("dashboard"), "That dashboard was not found."},
	{"widgetID", ofDashboard("dashboard_widget"), "That widget was not found."},
	{"shareID", ofDashboard("dashboard_share"), "That link was not found."},
	{"articleID", ofProject("kb_article"), "That article was not found."},
	{"responseID", ofProject("canned_response"), "That canned response was not found."},
	{"requestTypeID", ofProject("request_type"), "That request type was not found."},
	{"policyID", ofProject("sla_policy"), "That policy was not found."},
	{"fieldID", `SELECT p.key FROM custom_field t LEFT JOIN project p ON p.id = t.project_id WHERE t.id = $1`, "That field was not found."},
	{"attachmentID", viaIssue("attachment"), "That attachment was not found."},
	{"ruleID", `SELECT p.key FROM automation_rule t LEFT JOIN project p ON p.id = t.project_id WHERE t.id = $1`, "That rule was not found."},
	{"worklogID", viaIssue("issue_worklog"), "That worklog was not found."},
	{"commentID", viaIssue("issue_comment"), "That comment was not found."},
}

// ctxObjectProject carries the project projectScope found for the object a
// route names, for requireObjectPerm to ask about.
const ctxObjectProject ctxKey = 100

type objectProject struct {
	key string
}

// projectScope answers "not found" for a project, issue or object the caller may
// not read, since existence is privileged, and for a path mixing two projects.
func (s *Server) projectScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The portal keeps customers to their own requests by its own rule.
		if strings.HasPrefix(r.URL.Path, "/api/v1/portal/") {
			next.ServeHTTP(w, r)
			return
		}
		perms := PermsFrom(r.Context())
		named := ""
		if raw := chi.URLParam(r, "projectKey"); raw != "" {
			named = project.NormalizeKey(raw)
			if !perms.Can(perm.Read, named) {
				respondError(w, r, ErrNotFound("That project was not found."))
				return
			}
		}
		if raw := chi.URLParam(r, "issueKey"); raw != "" {
			if key, _, err := issue.ParseKey(raw); err == nil {
				if !perms.Can(perm.Read, key) {
					respondError(w, r, ErrNotFound("That issue was not found."))
					return
				}
				named = key
			}
		}
		ctx := r.Context()
		for _, kind := range objectKinds {
			raw := chi.URLParam(r, kind.param)
			id, err := uuid.Parse(raw)
			if raw == "" || err != nil {
				continue
			}
			key, found, err := s.projectOf(ctx, kind, id)
			if err != nil {
				respondError(w, r, err)
				return
			}
			if !found {
				continue
			}
			if key != "" && (!perms.Can(perm.Read, key) || (named != "" && key != named)) {
				respondError(w, r, ErrNotFound(kind.missing))
				return
			}
			if key != "" {
				named = key
			}
			ctx = context.WithValue(ctx, ctxObjectProject, objectProject{key: key})
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// projectOf finds the project an object belongs to. An unknown id is not an
// error here: the handler answers for it the way it always has.
func (s *Server) projectOf(ctx context.Context, kind objectKind, id uuid.UUID) (string, bool, error) {
	var key *string
	err := s.DB.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, kind.lookup, id).Scan(&key)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if key == nil {
		return "", true, nil
	}
	return *key, true, nil
}

// requireObjectPerm asks for a permission in the project of the object a route
// names; an organization's object, or a missing one, is the handler's to judge.
func requireObjectPerm(required perm.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := PrincipalFrom(r.Context())
			if p == nil || !p.InOrg() {
				respondError(w, r, ErrUnauthorized(""))
				return
			}
			perms := PermsFrom(r.Context())
			allowed := perms.CanSomewhere(required)
			if found, ok := r.Context().Value(ctxObjectProject).(objectProject); ok && found.key != "" {
				allowed = perms.Can(required, found.key)
			}
			if !allowed {
				respondError(w, r, forbidden(required))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// readableProjects drops from a listing the projects the caller may not see.
func readableProjects(r *http.Request, projects []project.Project) []project.Project {
	perms := PermsFrom(r.Context())
	if all, _ := perms.Readable(); all {
		return projects
	}
	kept := make([]project.Project, 0, len(projects))
	for _, p := range projects {
		if perms.Can(perm.Read, p.Key) {
			kept = append(kept, p)
		}
	}
	return kept
}

// readableFilter narrows a list of issues to the projects the caller may read.
func readableFilter(r *http.Request, f issue.Filter) issue.Filter {
	if all, keys := PermsFrom(r.Context()).Readable(); !all {
		f.Scoped = true
		f.Within = keys
	}
	return f
}
