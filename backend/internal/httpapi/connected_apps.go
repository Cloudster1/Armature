package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/armature/armature/backend/internal/oauth"
)

// appWrites are the writes a connected app may make, with the scope each one
// takes. Every other write is refused to an app before any handler, so a route
// added later is closed to apps until it is named here.
var appWrites = map[string]string{
	"POST /projects/{projectKey}/issues":                    oauth.ScopeIssueWrite,
	"POST /issues":                                          oauth.ScopeIssueWrite,
	"PATCH /issues/{issueKey}":                              oauth.ScopeIssueWrite,
	"DELETE /issues/{issueKey}":                             oauth.ScopeIssueWrite,
	"POST /issues/{issueKey}/clone":                         oauth.ScopeIssueWrite,
	"POST /issues/{issueKey}/move":                          oauth.ScopeIssueWrite,
	"POST /issues/bulk":                                     oauth.ScopeIssueWrite,
	"PUT /issues/{issueKey}/parent":                         oauth.ScopeIssueWrite,
	"PUT /issues/{issueKey}/schedule":                       oauth.ScopeIssueWrite,
	"PUT /issues/{issueKey}/estimate":                       oauth.ScopeIssueWrite,
	"PUT /issues/{issueKey}/milestone":                      oauth.ScopeIssueWrite,
	"PUT /issues/{issueKey}/versions":                       oauth.ScopeIssueWrite,
	"PUT /issues/{issueKey}/components":                     oauth.ScopeIssueWrite,
	"PUT /issues/{issueKey}/labels":                         oauth.ScopeIssueWrite,
	"POST /labels":                                          oauth.ScopeIssueWrite,
	"PUT /issues/{issueKey}/fields/{fieldID}":               oauth.ScopeIssueWrite,
	"POST /issues/{issueKey}/links":                         oauth.ScopeIssueWrite,
	"DELETE /issues/{issueKey}/links/{linkID}":              oauth.ScopeIssueWrite,
	"POST /issues/{issueKey}/remote-links":                  oauth.ScopeIssueWrite,
	"DELETE /issues/{issueKey}/remote-links/{remoteLinkID}": oauth.ScopeIssueWrite,
	"POST /issues/{issueKey}/worklogs":                      oauth.ScopeIssueWrite,
	"PATCH /worklogs/{worklogID}":                           oauth.ScopeIssueWrite,
	"DELETE /worklogs/{worklogID}":                          oauth.ScopeIssueWrite,
	"POST /issues/{issueKey}/attachments":                   oauth.ScopeIssueWrite,
	"DELETE /attachments/{attachmentID}":                    oauth.ScopeIssueWrite,
	"POST /issues/{issueKey}/branches":                      oauth.ScopeIssueWrite,
	"POST /issues/{issueKey}/transitions":                   oauth.ScopeIssueTransition,
	"POST /projects/{projectKey}/board/move":                oauth.ScopeIssueTransition,
	"POST /issues/{issueKey}/comments":                      oauth.ScopeCommentWrite,
	"PATCH /issues/{issueKey}/comments/{commentID}":         oauth.ScopeCommentWrite,
	"DELETE /issues/{issueKey}/comments/{commentID}":        oauth.ScopeCommentWrite,
	"POST /issues/{issueKey}/notes":                         oauth.ScopeCommentWrite,
	"POST /issues/{issueKey}/watchers":                      oauth.ScopeCommentWrite,
	"DELETE /issues/{issueKey}/watchers/{userID}":           oauth.ScopeCommentWrite,
}

// appScope refuses a connected app a write its grant does not reach. The
// person's own rights are judged after it, by the narrowed set, as for a key.
func appScope(mux *chi.Mux) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := PrincipalFrom(r.Context())
			if p == nil || p.App == nil || safeMethod(r.Method) || transports[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			pattern := mux.Find(chi.NewRouteContext(), r.Method, r.URL.Path)
			scope, listed := appWrites[r.Method+" "+strings.TrimPrefix(pattern, mcpAPIPrefix)]
			switch {
			case pattern == "":
				next.ServeHTTP(w, r)
			case !listed:
				respondError(w, r, insufficientScope("A connected app cannot change your own settings or Armature's. Do it in Armature.", ""))
			case !p.HasScope(scope):
				respondError(w, r, insufficientScope(oauth.MissingScope(p.App.Name, scope, p.Scopes), scope))
			default:
				next.ServeHTTP(w, r)
			}
		})
	}
}

// insufficientScope is a write refused for what the app was allowed, saying
// in RFC 6750's header which scope would have done.
func insufficientScope(message, scope string) *APIError {
	e := &APIError{Status: http.StatusForbidden, Code: "insufficient_scope", Message: message}
	if scope != "" {
		e.header = map[string]string{"WWW-Authenticate": `Bearer error="insufficient_scope", scope="` + scope + `"`}
	}
	return e
}

// connectedAppError maps what the connected apps refuse, or nil for an error
// of anything else.
func connectedAppError(err error) *APIError {
	var (
		fields  *oauth.ValidationError
		refused *oauth.RefusedError
	)
	switch {
	case errors.Is(err, oauth.ErrNotConnected):
		return &APIError{Status: http.StatusBadRequest, Code: "not_connected", Message: capitalize(err.Error()) + "."}
	case errors.Is(err, oauth.ErrNotFound):
		return ErrNotFound("That connected app was not found.")
	case errors.Is(err, oauth.ErrPublicClient):
		return ErrConflict("That app is a public client, with no secret to rotate. It proves itself with PKCE alone.")
	case errors.As(err, &fields):
		return ErrValidation(fields.Fields)
	case errors.As(err, &refused):
		return ErrForbidden(refused.Sentence)
	}
	return nil
}
