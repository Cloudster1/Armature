package httpapi

import (
	"errors"
	"net/http"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/sprint"
)

// sprintAPIError continues availabilityAPIError's mapping: work refused for being
// another team's, which the person resolves by handing it over or picking again.
func sprintAPIError(err error) *APIError {
	switch {
	case errors.Is(err, issue.ErrSprintOtherTeam):
		return ErrConflict("That sprint is planned for another team. Hand the issue to that team first, or pick a sprint of its own team.")
	case errors.Is(err, sprint.ErrOtherTeam):
		return ErrConflict("Unfinished work stays with its own team. Carry it into one of that team's sprints, or send it back to the backlog.")
	case errors.Is(err, sprint.ErrIntoItself):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed",
			Message: "A sprint cannot carry its work into itself. Pick another sprint or the backlog."}
	default:
		return companyAPIError(err)
	}
}
