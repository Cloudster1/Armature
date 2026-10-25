package httpapi

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/perm"
)

// lastAdministratorError answers the service's refusal and the database trigger's alike, so
// erasing the last administrator's account reads the same as revoking their role.
func lastAdministratorError(err error) *APIError {
	var pgErr *pgconn.PgError
	if !errors.Is(err, perm.ErrLastGlobalAdministrator) &&
		(!errors.As(err, &pgErr) || pgErr.ConstraintName != perm.KeepsAnAdministrator) {
		return nil
	}
	return &APIError{Status: http.StatusConflict, Code: "last_administrator",
		Message: "This is the last global administrator. Make someone else a global administrator first."}
}
