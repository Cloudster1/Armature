package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
)

// ErrAddressHasAccount refuses an import an address somebody already holds.
var ErrAddressHasAccount = errors.New("an account already uses that address; name the member instead of making one")

// ImportedMember stands in for somebody who worked in the tracker being
// imported and has no account here: no password, not active, but a member.
func (s *Service) ImportedMember(ctx context.Context, raw, name string) (uuid.UUID, error) {
	email, err := ParseAddress(raw)
	if err != nil {
		return uuid.Nil, err
	}
	if name == "" {
		name = localPart(email)
	}
	var id uuid.UUID
	_, err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		// An address already in this organization is the one an earlier run
		// made; one outside it belongs to somebody who did not ask to be here.
		var (
			found  uuid.UUID
			member bool
		)
		err := tx.QueryRow(ctx, `
			SELECT u.id, EXISTS (SELECT 1 FROM org_member m WHERE m.user_id = u.id AND m.org_id = current_org_id())
			FROM app_user u WHERE u.email = $1`, email).Scan(&found, &member)
		switch {
		case err == nil && member:
			id = found
			return nil
		case err == nil:
			return fmt.Errorf("%w: %s", ErrAddressHasAccount, email)
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO app_user (email, name, password_hash, is_active)
			VALUES ($1, $2, NULL, false) RETURNING id`, email, name).Scan(&id); err != nil {
			return fmt.Errorf("make an account for %s: %w", email, err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO org_member (org_id, user_id, org_role)
			VALUES (current_org_id(), $1, 'member') ON CONFLICT DO NOTHING`, id)
		return err
	})
	return id, err
}
