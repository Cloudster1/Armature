package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/perm"
)

func TestTheLastGlobalAdministratorIsAConflictWhicheverPathRemovesIt(t *testing.T) {
	const sentence = "This is the last global administrator. Make someone else a global administrator first."
	for name, err := range map[string]error{
		"the service's revoke": fmt.Errorf("revoke: %w", perm.ErrLastGlobalAdministrator),
		"the database trigger": fmt.Errorf("erase: %w", &pgconn.PgError{Code: "23514", ConstraintName: perm.KeepsAnAdministrator}),
	} {
		got := toAPIError(err)
		if got.Status != http.StatusConflict || got.Code != "last_administrator" || got.Message != sentence {
			t.Errorf("%s: got %d %s %q, want 409 last_administrator %q", name, got.Status, got.Code, got.Message, sentence)
		}
	}
}

func TestAnotherCheckViolationIsNotTakenForTheLastAdministrator(t *testing.T) {
	got := toAPIError(&pgconn.PgError{Code: "23514", ConstraintName: "issue_dates_in_order"})
	if got.Code == "last_administrator" {
		t.Fatal("a check violation from another constraint read as the last administrator")
	}
}
