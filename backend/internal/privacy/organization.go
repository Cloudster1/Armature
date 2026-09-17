package privacy

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
)

var (
	// ErrNotOwner is returned when somebody who does not own the organization
	// tries to delete it.
	ErrNotOwner = errors.New("only an owner can delete the organization")
	// ErrConfirmation is returned when the organization's address was not typed
	// back, which is what stands between a slip and losing everything.
	ErrConfirmation = errors.New("the confirmation does not name this organization")
)

// DeleteOrganization removes an organization and everything in it: projects,
// issues, members, roles, the audit log. Accounts stay, for their other
// organizations. The files in the bucket are left to the reaper, marked in the
// same transaction, because a bucket cannot take part in one.
//
// It is refused unless the caller owns the organization and has typed its
// address back as confirm.
func (s *Service) DeleteOrganization(ctx context.Context, orgID, actor uuid.UUID, confirm string) (db.LSN, error) {
	return s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var slug string
		err := tx.QueryRow(ctx, `SELECT slug FROM org WHERE id = $1 FOR UPDATE`, orgID).Scan(&slug)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotAMember
		}
		if err != nil {
			return err
		}

		var role string
		err = tx.QueryRow(ctx, `SELECT org_role FROM org_member WHERE org_id = $1 AND user_id = $2`, orgID, actor).Scan(&role)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotAMember
		}
		if err != nil {
			return err
		}
		if role != "owner" {
			return ErrNotOwner
		}
		if confirm != slug {
			return ErrConfirmation
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO attachment_tombstone (object_key, org_id)
			SELECT object_key, org_id FROM attachment WHERE org_id = $1
			ON CONFLICT (object_key) DO NOTHING`, orgID); err != nil {
			return fmt.Errorf("mark the organization's files for removal: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM org WHERE id = $1`, orgID); err != nil {
			return fmt.Errorf("delete the organization: %w", err)
		}
		return nil
	})
}
