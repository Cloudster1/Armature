package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/armature/armature/backend/internal/desk"
)

// companyAPIError ends sprintAPIError's mapping: a desk's companies, and a
// request shared with one.
func companyAPIError(err error) *APIError {
	if taken := (*desk.DomainTakenError)(nil); errors.As(err, &taken) {
		return ErrConflict(fmt.Sprintf("%s already takes %s, and a customer joins one company of a desk. Take the domain off %s first.", taken.Company, taken.Domain, taken.Company))
	}
	switch {
	case errors.Is(err, desk.ErrCompanyNotFound):
		return ErrNotFound("That company was not found.")
	case errors.Is(err, desk.ErrCompanyNameTaken):
		return ErrConflict("This desk already has a company by that name. Choose another.")
	case errors.Is(err, desk.ErrCompanyName):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: "Give the company a name of at most 100 characters.",
			Fields: map[string]string{"name": "Give the company a name of at most 100 characters."}}
	case errors.Is(err, desk.ErrNoCompany):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "validation_failed",
			Message: "You are in no company at this desk, so there is nobody to share the request with. Send it without sharing."}
	case errors.Is(err, desk.ErrMemberNotACustomer):
		return ErrConflict("That address belongs to somebody who works here, and sees every request already. Add a customer instead.")
	case errors.Is(err, desk.ErrInAnotherCompany):
		return ErrConflict("That customer is in another company of this desk. Take them out of it first.")
	case errors.Is(err, desk.ErrNotAMember):
		return ErrNotFound("That customer is not in this company.")
	default:
		return ErrInternal(err)
	}
}
