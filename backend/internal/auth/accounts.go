package auth

import (
	"context"
	"fmt"

	"github.com/armature/armature/backend/internal/db"
)

// UserForAddress finds the person behind a mail address, or makes one with no
// password. A watcher named by address becomes a user this way, so that "mail
// or account" is one column and the person can enter the portal later with a
// code and find what they were added to.
func (s *Service) UserForAddress(ctx context.Context, raw string) (User, error) {
	email, err := ParseAddress(raw)
	if err != nil {
		return User{}, err
	}
	var user User
	_, err = s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		user, err = upsertUserByAddress(ctx, tx, email)
		return err
	})
	return user, err
}

// upsertUserByAddress is the one statement that makes an account-less user:
// an address that already has an account is returned as it is.
func upsertUserByAddress(ctx context.Context, tx db.DBTX, email string) (User, error) {
	var user User
	err := tx.QueryRow(ctx, `
		INSERT INTO app_user (email, name, password_hash)
		VALUES ($1, $2, NULL)
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
		RETURNING id, email, name, timezone, locale, is_active, COALESCE(avatar_url, '')`,
		email, localPart(email)).Scan(&user.ID, &user.Email, &user.Name, &user.Timezone, &user.Locale, &user.IsActive, &user.AvatarURL)
	if err != nil {
		return User{}, fmt.Errorf("find or make the person behind %s: %w", email, err)
	}
	return user, nil
}
