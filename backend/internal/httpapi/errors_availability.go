package httpapi

import (
	"errors"
	"net/http"

	"github.com/armature/armature/backend/internal/availability"
	"github.com/armature/armature/backend/internal/plan"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/tenant"
)

// availabilityAPIError ends moreAPIError's mapping: calendars, weeks, absences
// and what the resource view is set against.
func availabilityAPIError(err error) *APIError {
	switch {
	case errors.Is(err, project.ErrScrumByTeam):
		message := "A scrum project plans by team. Switch the project to kanban to plan by person."
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: message, Fields: map[string]string{"resourceGrouping": message}}
	case errors.Is(err, project.ErrBadPlanning):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: withoutSentinel(err, project.ErrBadPlanning) + "."}
	case errors.Is(err, plan.ErrBadWindow):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: withoutSentinel(err, plan.ErrBadWindow) + "."}
	case errors.Is(err, availability.ErrNotFound):
		return ErrNotFound("That holiday calendar was not found.")
	case errors.Is(err, availability.ErrNameTaken):
		return ErrConflict("This organization already has a holiday calendar by that name. Choose another.")
	case errors.Is(err, availability.ErrDefaultCalendar):
		return ErrConflict("The default calendar stays the default until another one takes its place. Make another calendar the default first.")
	case errors.Is(err, availability.ErrInvalid):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: withoutSentinel(err, availability.ErrInvalid) + "."}
	case errors.Is(err, availability.ErrBadFile):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: withoutSentinel(err, availability.ErrBadFile) + "."}
	case errors.Is(err, availability.ErrNotAMember):
		return ErrNotFound("That person is not a member of this organization.")
	case errors.Is(err, availability.ErrCustomer):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed",
			Message: "A portal customer keeps no working week or absences here. Choose somebody who works in the organization."}
	case errors.Is(err, availability.ErrAbsenceNotFound):
		return ErrNotFound("That absence was not found.")
	case errors.Is(err, availability.ErrOverlap):
		return ErrConflict("That person is already away on some of those days. Change that absence instead of adding another.")
	case errors.Is(err, availability.ErrMayNotRecord):
		return ErrForbidden("Only the person, an administrator, or a scrum master or project administrator of one of their teams records their absences. Ask one of them instead.")
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
