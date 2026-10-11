package httpapi

import (
	"errors"
	"net/http"

	"github.com/armature/armature/backend/internal/issue"
)

// issueAPIError continues toAPIError's mapping with the refusals of an issue
// edit, then hands on to availabilityAPIError.
func issueAPIError(err error) *APIError {
	if errors.Is(err, issue.ErrStale) {
		return &APIError{Status: http.StatusConflict, Code: "stale_edit",
			Message: "Somebody changed this issue since you opened it. Reload it and apply your edit again."}
	}
	return availabilityAPIError(err)
}
