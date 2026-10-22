package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/armature/armature/backend/internal/netguard"
	"github.com/armature/armature/backend/internal/oauth"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/webhook"
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
	"POST /issues/{issueKey}/pull-requests":                 oauth.ScopeIssueWrite,
	"POST /issues/{issueKey}/transitions":                   oauth.ScopeIssueTransition,
	"POST /projects/{projectKey}/board/move":                oauth.ScopeIssueTransition,
	"POST /issues/{issueKey}/comments":                      oauth.ScopeCommentWrite,
	"PATCH /issues/{issueKey}/comments/{commentID}":         oauth.ScopeCommentWrite,
	"DELETE /issues/{issueKey}/comments/{commentID}":        oauth.ScopeCommentWrite,
	"POST /issues/{issueKey}/notes":                         oauth.ScopeCommentWrite,
	"POST /issues/{issueKey}/watchers":                      oauth.ScopeCommentWrite,
	"DELETE /issues/{issueKey}/watchers/{userID}":           oauth.ScopeCommentWrite,
	"POST /webhooks/partner":                                oauth.ScopeWebhooks,
	"PATCH /webhooks/partner":                               oauth.ScopeWebhooks,
	"DELETE /webhooks/partner":                              oauth.ScopeWebhooks,
	"POST /webhooks/partner/rotate-secret":                  oauth.ScopeWebhooks,
	"POST /webhooks/partner/test":                           oauth.ScopeWebhooks,
}

// appScope refuses a connected app a write its grant does not reach. The
// person's own rights are judged after it, by the narrowed set, as for a key.
func appScope(mux *chi.Mux) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := PrincipalFrom(r.Context())
			if p == nil || p.App == nil || safeMethod(r.Method) || transports[r.URL.Path] || gitTransfer(r.URL.Path) {
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

// personPermsFrom is what the caller's person may do before a key or an app
// narrowed it: a partner manages its webhook only while its person administers.
func personPermsFrom(ctx context.Context) perm.Set {
	set, _ := ctx.Value(ctxPersonPerms).(perm.Set)
	return set
}

// requirePartner keeps the partner webhook routes to a connected app holding
// webhooks.partner for a person who administers Armature at the time of the call.
func requirePartner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := PrincipalFrom(r.Context())
		switch {
		case p == nil:
			respondError(w, r, ErrUnauthorized(""))
		case p.App == nil:
			respondError(w, r, ErrForbidden("Only a connected app manages its own webhook here. Manage webhooks under Settings, Webhooks."))
		case !p.HasScope(oauth.ScopeWebhooks):
			respondError(w, r, insufficientScope(p.App.Name+" was not allowed to manage its webhook. An Armature administrator connects it again.", oauth.ScopeWebhooks))
		case !personPermsFrom(r.Context()).CanInOrg(perm.OrgAdminister):
			respondError(w, r, ErrForbidden(p.User.Name+" is no longer an Armature administrator. An administrator connects "+p.App.Name+" again to manage its webhook."))
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// connectedAppError maps what the connected apps and their webhooks refuse,
// or nil for an error of anything else.
func connectedAppError(err error) *APIError {
	var (
		fields   *oauth.ValidationError
		clash    *oauth.NameClashError
		refused  *oauth.RefusedError
		owned    *webhook.PartnerOwnedError
		taken    *webhook.NameTakenError
		field    *webhook.FieldError
		outbound *netguard.RefusedError
		unknown  *netguard.UnresolvedError
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
	case errors.As(err, &clash):
		return ErrConflict(capitalize(clash.Error()) + ". Rename that webhook in Settings, Webhooks, or name the app differently.")
	case errors.As(err, &refused):
		return ErrForbidden(refused.Sentence)
	case errors.As(err, &owned):
		return &APIError{Status: http.StatusConflict, Code: "partner_owned", Message: owned.Error() + "."}
	case errors.As(err, &taken):
		return ErrConflict(taken.Error())
	case errors.As(err, &field):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: field.Sentence, Fields: map[string]string{field.Field: field.Sentence}}
	case errors.As(err, &outbound):
		allow := "Add " + outbound.Host + " to network.outboundAllow (" + netguard.AllowEnv + ")"
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "outbound_refused",
			Message: "Armature may not reach " + outbound.Host + ", which is inside its own network. " + allow + " and try again.",
			Fields:  map[string]string{"url": allow + "."}}
	case errors.As(err, &unknown):
		sentence := unknown.Host + " does not resolve from Armature. Check the address, or Armature's DNS."
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "invalid", Message: sentence, Fields: map[string]string{"url": sentence}}
	}
	return nil
}
