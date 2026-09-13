// Package httpapi is the HTTP surface: routing, middleware and handlers.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/armature/armature/backend/internal/assistant"
	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/automation"
	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/component"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/filter"
	"github.com/armature/armature/backend/internal/git"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/milestone"
	"github.com/armature/armature/backend/internal/nql"
	"github.com/armature/armature/backend/internal/oidc"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/privacy"
	"github.com/armature/armature/backend/internal/profile"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/render"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/team"
	"github.com/armature/armature/backend/internal/template"
	"github.com/armature/armature/backend/internal/tenant"
	"github.com/armature/armature/backend/internal/version"
	"github.com/armature/armature/backend/internal/webhook"
	"github.com/armature/armature/backend/internal/workflow"
)

// APIError is the single error shape every endpoint returns, so that clients
// have exactly one thing to parse.
type APIError struct {
	// Status is the HTTP status code; it is not serialised.
	Status int `json:"-"`
	// Code is a stable machine readable identifier such as "not_found".
	Code string `json:"code"`
	// Message is a human readable, user-safe explanation.
	Message string `json:"message"`
	// Fields carries per-field validation messages keyed by field name.
	Fields map[string]string `json:"fields,omitempty"`
	// Position is the 1-based character a query went wrong at, for bad_query.
	Position *int `json:"position,omitempty"`
	// RequestID lets a user quote something we can find in the logs.
	RequestID string `json:"requestId,omitempty"`

	// cause is logged but never sent to the client.
	cause error
}

func (e *APIError) Error() string {
	if e.cause != nil {
		return e.Code + ": " + e.Message + ": " + e.cause.Error()
	}
	return e.Code + ": " + e.Message
}

func (e *APIError) Unwrap() error { return e.cause }

// Constructors for the errors handlers raise directly.

func ErrBadRequest(message string) *APIError {
	return &APIError{Status: http.StatusBadRequest, Code: "bad_request", Message: message}
}

func ErrValidation(fields map[string]string) *APIError {
	return &APIError{
		Status:  http.StatusUnprocessableEntity,
		Code:    "validation_failed",
		Message: "Some fields need attention.",
		Fields:  fields,
	}
}

func ErrUnauthorized(message string) *APIError {
	if message == "" {
		message = "Sign in to continue."
	}
	return &APIError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: message}
}

// ErrReadOnlyToken is a write refused because the token was made to read only.
func ErrReadOnlyToken() *APIError {
	return &APIError{Status: http.StatusForbidden, Code: "read_only_token",
		Message: "This token can only read. Make a token without the read-only mark to change anything."}
}

// ErrSessionOnly is an act refused to an API key because it must be done by
// somebody signed in.
func ErrSessionOnly() *APIError {
	return &APIError{Status: http.StatusForbidden, Code: "session_only",
		Message: "This can only be done while signed in, not with an API key. Sign in and do it from the browser."}
}

func ErrForbidden(message string) *APIError {
	if message == "" {
		message = "You do not have permission to do that."
	}
	return &APIError{Status: http.StatusForbidden, Code: "forbidden", Message: message}
}

// ErrNotFound is also what a caller gets for a resource that exists in another
// tenant: existence itself is privileged information.
func ErrNotFound(what string) *APIError {
	if what == "" {
		what = "That was not found."
	}
	return &APIError{Status: http.StatusNotFound, Code: "not_found", Message: what}
}

func ErrConflict(message string) *APIError {
	return &APIError{Status: http.StatusConflict, Code: "conflict", Message: message}
}

func ErrInternal(cause error) *APIError {
	return &APIError{
		Status:  http.StatusInternalServerError,
		Code:    "internal_error",
		Message: "Something went wrong on our side.",
		cause:   cause,
	}
}

// toAPIError maps a domain error onto the wire shape. Keeping the mapping in
// one place is what stops handlers from leaking internals into responses by
// accident.
func toAPIError(err error) *APIError {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}

	var queryErr *nql.Error
	if errors.As(err, &queryErr) {
		pos := queryErr.Pos
		return &APIError{Status: http.StatusBadRequest, Code: "bad_query", Message: queryErr.Msg, Position: &pos}
	}

	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		return &APIError{Status: http.StatusUnauthorized, Code: "invalid_credentials", Message: auth.ErrInvalidCredentials.Error()}
	case errors.Is(err, auth.ErrInvalidToken):
		return ErrUnauthorized("Your session has expired. Sign in again.")
	case errors.Is(err, auth.ErrUserInactive):
		return ErrForbidden("This account has been deactivated.")
	case errors.Is(err, auth.ErrEmailTaken):
		return &APIError{Status: http.StatusConflict, Code: "email_taken", Message: auth.ErrEmailTaken.Error()}
	case errors.Is(err, auth.ErrSlugTaken):
		return &APIError{Status: http.StatusConflict, Code: "slug_taken", Message: auth.ErrSlugTaken.Error()}
	case errors.Is(err, auth.ErrNotAMember):
		return ErrNotFound("That organization was not found.")
	case errors.Is(err, auth.ErrInviteInvalid):
		return &APIError{Status: http.StatusGone, Code: "invite_invalid", Message: auth.ErrInviteInvalid.Error()}
	case errors.Is(err, auth.ErrSignInToAccept):
		return &APIError{Status: http.StatusConflict, Code: "sign_in_to_accept",
			Message: "An account already uses this address. Sign in with it, then open the invitation again."}
	case errors.Is(err, auth.ErrInviteForSomeoneElse):
		return &APIError{Status: http.StatusForbidden, Code: "invite_for_someone_else",
			Message: "This invitation is for another address. Sign out and open it again, or ask for one of your own."}
	case errors.Is(err, auth.ErrAlreadyMember):
		return &APIError{Status: http.StatusConflict, Code: "already_member",
			Message: "That person is already a member here. Change what they may do under Access instead."}
	case errors.Is(err, auth.ErrSessionStaysHome):
		return sessionBound(http.StatusConflict)
	case errors.Is(err, issue.ErrNotFound):
		return ErrNotFound("That issue was not found.")
	case errors.Is(err, issue.ErrLinkCycle):
		return ErrConflict(err.Error())
	case errors.Is(err, issue.ErrInvalidKey):
		return ErrBadRequest("That is not an issue key.")
	case errors.Is(err, project.ErrNotFound):
		return ErrNotFound("That project was not found.")
	case errors.Is(err, project.ErrKeyTaken):
		return ErrConflict(project.ErrKeyTaken.Error())
	case errors.Is(err, project.ErrArchived):
		return ErrConflict("That project is archived.")
	case errors.Is(err, project.ErrNotADesk):
		return ErrConflict("Only a service desk has a door; make the project a service desk first.")
	case errors.Is(err, project.ErrBadDomain):
		return ErrValidation(map[string]string{"trustedDomains": "Write a domain such as acme.test, without the @."})
	case errors.Is(err, project.ErrDomainNotTrusted):
		return domainNotTrusted(err)
	case errors.Is(err, privacy.ErrLastOwner):
		return &APIError{Status: http.StatusConflict, Code: "last_owner", Message: lastOwner(err)}
	case errors.Is(err, privacy.ErrNotAMember):
		return ErrNotFound("That person is not a member of this organization.")
	case errors.Is(err, privacy.ErrNotFound):
		return ErrNotFound("That person was not found.")
	case errors.Is(err, project.ErrBadFeature):
		return ErrValidation(map[string]string{"features": "Name pages the project has: board, sprints, plan and the rest."})
	case errors.Is(err, project.ErrFeatureOff):
		return featureOff(err)
	case errors.Is(err, board.ErrNotFound), errors.Is(err, board.ErrSwimlaneNotFound):
		return ErrNotFound("That board was not found.")
	case errors.Is(err, board.ErrNotOnThisBoard):
		return ErrNotFound("That card is not on this board.")
	case errors.Is(err, privacy.ErrOwnerRemovesOwner):
		return ErrForbidden(capitalize(privacy.ErrOwnerRemovesOwner.Error()) + ".")
	case errors.Is(err, perm.ErrNotFound):
		return ErrNotFound("That was not found.")
	case errors.Is(err, perm.ErrNotAMember):
		return ErrBadRequest(perm.ErrNotAMember.Error())
	case errors.Is(err, perm.ErrNameTaken):
		return ErrConflict(perm.ErrNameTaken.Error())
	case errors.Is(err, perm.ErrScope):
		return ErrBadRequest(withoutSentinel(err, perm.ErrScope))
	case errors.Is(err, perm.ErrManagedElsewhere):
		return ErrConflict(withoutSentinel(err, perm.ErrManagedElsewhere))
	case errors.Is(err, team.ErrNotFound):
		return ErrNotFound("That team was not found.")
	case errors.Is(err, team.ErrNegativeCapacity):
		return ErrBadRequest(capitalize(team.ErrNegativeCapacity.Error()) + ".")
	case errors.Is(err, team.ErrNameTaken):
		return ErrConflict(team.ErrNameTaken.Error())
	case errors.Is(err, team.ErrNotAMember):
		return ErrBadRequest(team.ErrNotAMember.Error())
	case errors.Is(err, team.ErrInUse):
		return ErrConflict(withoutSentinel(err, team.ErrInUse))
	case errors.Is(err, issue.ErrTeamNotFound), errors.Is(err, board.ErrTeamNotFound),
		errors.Is(err, sprint.ErrTeamNotFound):
		return ErrNotFound("That team is not in this project.")
	case errors.Is(err, board.ErrNameTaken):
		return ErrConflict(board.ErrNameTaken.Error())
	case errors.Is(err, board.ErrBadType):
		return ErrBadRequest("A board is either scrum or kanban.")
	case errors.Is(err, template.ErrUnknown):
		return ErrBadRequest("There is no project template by that name.")
	case errors.Is(err, auth.ErrBadAddress), errors.Is(err, auth.ErrCodeTooSoon), errors.Is(err, auth.ErrCodeInvalid), errors.Is(err, auth.ErrHasAccount), errors.Is(err, auth.ErrDoorAsksForCode):
		return portalEntryError(err)
	case errors.Is(err, issue.ErrLinkNotFound):
		return ErrNotFound("That link was not found.")
	case errors.Is(err, issue.ErrNotWatching):
		return ErrNotFound("That person is not watching this issue.")
	case errors.Is(err, desk.ErrNotYourRequest):
		return &APIError{Status: http.StatusForbidden, Code: "not_your_request", Message: "Only the person who raised the request may do that."}
	case errors.Is(err, desk.ErrNotYourFile):
		return &APIError{Status: http.StatusForbidden, Code: "not_your_file", Message: "Only the person who attached a file may remove it."}
	case errors.Is(err, auth.ErrBadName), errors.Is(err, auth.ErrBadTimezone), errors.Is(err, auth.ErrBadLocale):
		return ErrBadRequest(capitalize(err.Error()) + ".")
	case errors.Is(err, profile.ErrNotAnImage), errors.Is(err, profile.ErrTooLarge):
		return ErrBadRequest(capitalize(err.Error()) + ".")
	case errors.Is(err, desk.ErrNotFound):
		return ErrNotFound("That was not found.")
	case errors.Is(err, desk.ErrNameTaken):
		return ErrConflict(desk.ErrNameTaken.Error())
	case errors.Is(err, desk.ErrNotAServiceProject):
		return ErrBadRequest("That project is not a service desk.")
	case errors.Is(err, report.ErrNotFound):
		return ErrNotFound("That dashboard was not found.")
	case errors.Is(err, report.ErrNameTaken):
		return ErrConflict(report.ErrNameTaken.Error())
	case errors.Is(err, report.ErrLastDashboard):
		return ErrConflict(report.ErrLastDashboard.Error())
	case errors.Is(err, report.ErrBadKind):
		return ErrBadRequest("That is not a kind of report.")
	case errors.Is(err, report.ErrNoReport):
		return ErrBadRequest(withoutSentinel(err, report.ErrNoReport) + ".")
	case errors.Is(err, report.ErrOneFilter):
		return ErrConflict("This dashboard already has a filter. Change that one instead.")
	case errors.Is(err, report.ErrBadChart):
		return ErrBadRequest(withoutSentinel(err, report.ErrBadChart))
	default:
		return moreAPIError(err)
	}
}

// moreAPIError continues toAPIError's mapping in the same order; one switch
// would be too long to read.
func moreAPIError(err error) *APIError {
	switch {
	case errors.Is(err, assistant.ErrUnavailable):
		return &APIError{Status: http.StatusServiceUnavailable, Code: "assistant_unavailable", Message: "Ask is not set up on this deployment. Ask whoever runs it to name a model provider."}
	case errors.Is(err, assistant.ErrProvider):
		return &APIError{Status: http.StatusBadGateway, Code: "assistant_failed", Message: "The assistant did not answer. Try again in a moment, or ask the guide with fewer words."}
	case errors.Is(err, render.ErrUnavailable):
		return &APIError{Status: http.StatusServiceUnavailable, Code: "render_unavailable", Message: "PDF export is not set up on this deployment. Ask whoever runs it to start the render service."}
	case errors.Is(err, report.ErrShareGone):
		return ErrNotFound(withoutSentinel(err, report.ErrShareGone) + ".")
	case errors.Is(err, report.ErrShareName):
		return ErrBadRequest("Give the link a name.")
	case errors.Is(err, report.ErrShareExpiry):
		return ErrBadRequest(withoutSentinel(err, report.ErrShareExpiry) + ".")
	case errors.Is(err, report.ErrNoTemplate):
		return ErrBadRequest(withoutSentinel(err, report.ErrNoTemplate) + ".")
	case errors.Is(err, report.ErrTemplateName):
		return ErrBadRequest("Give the template a name.")
	case errors.Is(err, report.ErrTemplateNameTaken):
		return ErrConflict("This organization already has a template by that name. Choose another.")
	case errors.Is(err, field.ErrNotFound):
		return ErrNotFound("That field was not found.")
	case errors.Is(err, field.ErrNameTaken):
		return ErrConflict(field.ErrNameTaken.Error())
	case errors.Is(err, field.ErrBadKind):
		return ErrBadRequest("That is not a kind of field.")
	case errors.Is(err, field.ErrBadValue):
		return ErrBadRequest(withoutSentinel(err, field.ErrBadValue))
	case errors.Is(err, field.ErrOtherProject):
		return ErrBadRequest("That field belongs to another project.")
	case errors.Is(err, field.ErrKindMismatch):
		return ErrConflict(capitalize(field.ErrKindMismatch.Error()) + ".")
	case errors.Is(err, label.ErrNotFound):
		return ErrNotFound("That label was not found.")
	case errors.Is(err, label.ErrNameTaken):
		return ErrConflict(label.ErrNameTaken.Error())
	case errors.Is(err, label.ErrBadName):
		return ErrBadRequest(capitalize(label.ErrBadName.Error()) + ".")
	case errors.Is(err, label.ErrBadColor):
		return ErrBadRequest(capitalize(label.ErrBadColor.Error()) + ".")
	case errors.Is(err, issue.ErrWorklogNotFound):
		return ErrNotFound("That worklog was not found.")
	case errors.Is(err, issue.ErrNotYourWorklog):
		return ErrForbidden(capitalize(issue.ErrNotYourWorklog.Error()) + ".")
	case errors.Is(err, issue.ErrBadDuration):
		return ErrBadRequest(capitalize(withoutSentinel(err, issue.ErrBadDuration)) + ".")
	case errors.Is(err, attachment.ErrNotFound):
		return ErrNotFound("That attachment was not found.")
	case errors.Is(err, attachment.ErrTooLarge):
		return &APIError{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: capitalize(withoutSentinel(err, attachment.ErrTooLarge)) + "."}
	case errors.Is(err, attachment.ErrEmpty):
		return ErrBadRequest("That file is empty.")
	case errors.Is(err, attachment.ErrUnavailable):
		return &APIError{Status: http.StatusServiceUnavailable, Code: "attachments_unavailable", Message: capitalize(attachment.ErrUnavailable.Error()) + "."}
	case errors.Is(err, attachment.ErrNoObject):
		return &APIError{Status: http.StatusBadGateway, Code: "object_missing", Message: capitalize(attachment.ErrNoObject.Error()) + "."}
	case errors.Is(err, oidc.ErrNotConfigured):
		// One answer for "no such organization" and "no provider", so the
		// sign-in page cannot be used to enumerate slugs.
		return ErrNotFound("Single sign-on is not set up for that organization.")
	case errors.Is(err, git.ErrNotFound):
		return ErrNotFound("That repository was not found.")
	case errors.Is(err, git.ErrNameTaken):
		return ErrConflict(git.ErrNameTaken.Error())
	case errors.Is(err, git.ErrBadHost):
		return ErrBadRequest("The host must be github, gitlab or gitea.")
	case errors.Is(err, git.ErrBadAddress):
		return ErrBadRequest("A repository's address and its API address are http or https URLs.")
	case errors.Is(err, git.ErrNoToken):
		return ErrConflict(git.ErrNoToken.Error())
	case errors.Is(err, git.ErrBranchExists):
		return ErrConflict("The host already has a branch of that name. Pick another, or push to the one that is there.")
	case errors.Is(err, git.ErrBadBranchName):
		return ErrBadRequest("That is not a name git accepts for a branch. Use letters, digits, dots, dashes and slashes, with no spaces.")
	case errors.Is(err, git.ErrHost):
		return &APIError{Status: http.StatusBadGateway, Code: "host_refused", Message: capitalize(withoutSentinel(err, git.ErrHost))}
	case errors.Is(err, board.ErrLastBoard):
		return ErrConflict(board.ErrLastBoard.Error())
	case errors.Is(err, sprint.ErrNotFound), errors.Is(err, board.ErrSprintNotFound):
		return ErrNotFound("That sprint was not found.")
	case errors.Is(err, sprint.ErrNotStartable):
		return ErrConflict(withoutSentinel(err, sprint.ErrNotStartable))
	case errors.Is(err, sprint.ErrNotRunning):
		return ErrConflict(withoutSentinel(err, sprint.ErrNotRunning))
	case errors.Is(err, sprint.ErrClosed), errors.Is(err, issue.ErrSprintClosed):
		return ErrConflict(withoutSentinel(err, sprint.ErrClosed))
	case errors.Is(err, issue.ErrSprintNotFound):
		return ErrNotFound("That sprint was not found.")
	case errors.Is(err, desk.ErrArticleNotFound), errors.Is(err, desk.ErrResponseNotFound):
		return ErrNotFound(capitalize(err.Error()) + ".")
	case errors.Is(err, desk.ErrRatingNotFound):
		return ErrNotFound("That rating link is not one we sent. Open it from the mail again.")
	case errors.Is(err, desk.ErrAlreadyRated):
		return ErrConflict("This request was already rated. Thank you.")
	case errors.Is(err, desk.ErrNoCalendar):
		return ErrConflict("This project has no business hours yet. Set them under Business hours first.")
	case errors.Is(err, desk.ErrNotAService):
		return ErrConflict("Articles belong to a service desk project.")
	case errors.Is(err, filter.ErrNotFound):
		return ErrNotFound("That saved filter was not found.")
	case errors.Is(err, filter.ErrDuplicateName):
		return ErrConflict(capitalize(err.Error()) + ".")
	case errors.Is(err, filter.ErrNotYours):
		return ErrForbidden(capitalize(err.Error()) + ".")
	case errors.Is(err, issue.ErrMoveNeedsStatus):
		return ErrConflict(capitalize(err.Error()) + ".")
	case errors.Is(err, version.ErrNotFound):
		return ErrNotFound("That version was not found.")
	case errors.Is(err, version.ErrDuplicateName), errors.Is(err, component.ErrDuplicateName):
		return ErrConflict(capitalize(err.Error()) + ".")
	case errors.Is(err, version.ErrArchived), errors.Is(err, version.ErrNotReleased):
		return ErrConflict(capitalize(err.Error()) + ".")
	case errors.Is(err, component.ErrNotFound):
		return ErrNotFound("That component was not found.")
	case errors.Is(err, issue.ErrVersionNotFound), errors.Is(err, issue.ErrComponentNotFound):
		return ErrNotFound(capitalize(err.Error()) + ".")
	case errors.Is(err, automation.ErrNotFound):
		return ErrNotFound("That rule was not found.")
	case errors.Is(err, automation.ErrOutsideRule):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "outside_rule",
			Message: capitalize(withoutSentinel(err, automation.ErrOutsideRule))}
	case errors.Is(err, webhook.ErrNotFound):
		return ErrNotFound("That webhook was not found.")
	case errors.Is(err, milestone.ErrNotFound), errors.Is(err, issue.ErrMilestoneNotFound):
		return ErrNotFound("That milestone was not found.")
	case errors.Is(err, milestone.ErrClosed), errors.Is(err, issue.ErrMilestoneClosed):
		return ErrConflict(withoutSentinel(err, milestone.ErrClosed))
	case errors.Is(err, milestone.ErrOpen):
		return ErrConflict(withoutSentinel(err, milestone.ErrOpen))
	case errors.Is(err, milestone.ErrDuplicateName):
		return ErrConflict(withoutSentinel(err, milestone.ErrDuplicateName))
	case errors.Is(err, workflow.ErrNotFound):
		return ErrNotFound("That workflow was not found.")
	case errors.Is(err, workflow.ErrInvalid):
		return ErrBadRequest(withoutSentinel(err, workflow.ErrInvalid))
	case errors.Is(err, workflow.ErrInUse):
		return ErrConflict(withoutSentinel(err, workflow.ErrInUse))
	case errors.Is(err, tenant.ErrNoTenant):
		return &APIError{
			Status:  http.StatusBadRequest,
			Code:    "no_organization",
			Message: "Select an organization first.",
		}
	default:
		return ErrInternal(err)
	}
}

// withoutSentinel drops the sentinel's own words from a wrapped error, leaving
// the part that was written for a person to read. "that sprint cannot be
// started: Sprint 2 is already running" is a sentence with a stutter in it.
func withoutSentinel(err, sentinel error) string {
	message := strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
	if message == "" {
		return sentinel.Error()
	}
	return strings.ToUpper(message[:1]) + message[1:]
}

// respondError writes err as JSON and logs the underlying cause when there is
// one worth seeing.
func respondError(w http.ResponseWriter, r *http.Request, err error) {
	apiErr := toAPIError(err)
	apiErr.RequestID = RequestIDFrom(r.Context())

	log := loggerFrom(r.Context())
	if apiErr.Status >= 500 {
		log.Error("request failed", "code", apiErr.Code, "error", err)
	} else {
		log.Debug("request rejected", "code", apiErr.Code, "status", apiErr.Status, "message", apiErr.Message)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(apiErr.Status)
	if encErr := json.NewEncoder(w).Encode(map[string]any{"error": apiErr}); encErr != nil {
		log.Error("failed to write error response", "error", encErr)
	}
}

// respondJSON writes a successful response.
func respondJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		// The status line is already sent, so there is nothing to do but note it.
		loggerFrom(r.Context()).Error("failed to write response body", "error", err)
	}
}

// respondNoContent ends a request that has nothing to return.
func respondNoContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// jsonUnmarshal is exposed for the small custom unmarshallers in this package.
func jsonUnmarshal(data []byte, into any) error { return json.Unmarshal(data, into) }

var _ = slog.Default

// domainNotTrusted names the domains a desk takes, since the refusal is a
// policy the desk publishes, not a fact about a person.
// lastOwner names the organization that would be left with nobody.
func lastOwner(err error) string {
	var last *privacy.LastOwnerError
	if errors.As(err, &last) {
		return fmt.Sprintf("%s would be left without an owner. Make somebody else an owner first, under Access.", last.Org)
	}
	return "An organization would be left without an owner. Make somebody else an owner first, under Access."
}

// featureOff names the page and where to turn it on.
func featureOff(err error) *APIError {
	var off *project.FeatureOffError
	message := "This project does not use that feature. Turn it on under the project's settings."
	if errors.As(err, &off) {
		message = fmt.Sprintf("%s does not use %s. Turn it on under the project's settings.", off.Key, off.Feature.Word())
	}
	return &APIError{Status: http.StatusConflict, Code: "feature_off", Message: message}
}

func domainNotTrusted(err error) *APIError {
	var refused *project.NotTrustedError
	message := "This desk does not take requests from addresses at that domain."
	if errors.As(err, &refused) && len(refused.Domains) > 0 {
		message = fmt.Sprintf("This desk takes requests from addresses at %s only.", joinDomains(refused.Domains))
	}
	return &APIError{Status: http.StatusForbidden, Code: "domain_not_trusted", Message: message}
}

// joinDomains reads as a sentence: one, "a or b", "a, b or c".
func joinDomains(domains []string) string {
	switch len(domains) {
	case 0:
		return ""
	case 1:
		return domains[0]
	}
	return strings.Join(domains[:len(domains)-1], ", ") + " or " + domains[len(domains)-1]
}
