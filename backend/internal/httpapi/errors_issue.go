package httpapi

import (
	"errors"
	"net/http"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/issuetype"
)

// issueAPIError continues toAPIError's mapping with the refusals of an issue
// edit, then hands on to availabilityAPIError.
func issueAPIError(err error) *APIError {
	if errors.Is(err, issue.ErrStale) {
		return &APIError{Status: http.StatusConflict, Code: "stale_edit",
			Message: "Somebody changed this issue since you opened it. Reload it and apply your edit again."}
	}
	if e := projectTypeError(err); e != nil {
		return e
	}
	return availabilityAPIError(err)
}

// projectTypeError answers a type a project does not use, a retired one, or a
// set of a project's types the service refuses; the database's backstops for
// the same read alike, since an edit can race the check.
func projectTypeError(err error) *APIError {
	var (
		notAllowed *issue.TypeNotAllowedError
		retired    *issue.TypeRetiredError
		stillUsed  *issuetype.StillUsedError
		only       *issuetype.OnlyStandardError
		taken      *issuetype.TakenInUseError
	)
	switch {
	case errors.As(err, &notAllowed):
		return &APIError{Status: http.StatusConflict, Code: "type_not_allowed", Message: capitalize(err.Error()) + "."}
	case errors.As(err, &retired):
		return &APIError{Status: http.StatusConflict, Code: "type_retired", Message: capitalize(err.Error()) + "."}
	case errors.As(err, &stillUsed), errors.As(err, &only), errors.As(err, &taken):
		return ErrConflict(err.Error() + ".")
	case errors.Is(err, issue.ErrNoSuchType):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: "That issue type does not exist. Pick it from the list again."}
	case errors.Is(err, issue.ErrNoStandardType):
		return &APIError{Status: http.StatusConflict, Code: "no_standard_type", Message: capitalize(err.Error()) + "."}
	case errors.Is(err, issuetype.ErrNoTypes), errors.Is(err, issuetype.ErrNoStandardType), errors.Is(err, issuetype.ErrRepeated):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: capitalize(err.Error()) + "."}
	}
	switch constraint, _ := issue.IsTypeRule(err); constraint {
	case issue.ProjectTypeConstraint, issue.RequestTypeTypeConstraint:
		return &APIError{Status: http.StatusConflict, Code: "type_not_allowed", cause: err,
			Message: "That project does not use that issue type; ask its administrator to allow it or change the type first."}
	case issue.RetiredTypeConstraint:
		return &APIError{Status: http.StatusConflict, Code: "type_retired", cause: err,
			Message: "That issue type is retired and offered for nothing new; pick another type."}
	case issue.StandardTypeConstraint:
		return &APIError{Status: http.StatusConflict, Code: "no_standard_type", cause: err,
			Message: "A project would be left without a standard issue type that is not retired. Allow another standard type there first."}
	}
	return nil
}
