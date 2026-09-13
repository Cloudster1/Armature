package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
)

// Locales is what a person may choose to have dates written in. A short list
// rather than every BCP 47 tag, so the picker can be a select and the API
// never has to decide what "en-XX" means.
var Locales = []string{"en-GB", "en-US", "de-DE", "fr-FR", "es-ES", "nl-NL", "it-IT", "pt-BR"}

// NameMaxLength is the longest name a person may give themselves.
const NameMaxLength = 120

var (
	// ErrBadName is returned for an empty or overlong name.
	ErrBadName = errors.New("give yourself a name of up to 120 characters")
	// ErrBadTimezone is returned for a zone the database of zones does not know.
	ErrBadTimezone = errors.New("choose a time zone from the list")
	// ErrBadLocale is returned for a language that is not offered.
	ErrBadLocale = errors.New("choose a language from the list")
)

// ProfileInput is what a person may change about themselves. Nil leaves a
// field alone.
type ProfileInput struct {
	Name     *string
	Timezone *string
	Locale   *string
}

// UpdateProfile changes the name, the time zone or the language of an account.
// The zone is checked against the zone database the binary carries, so the
// container needs no zoneinfo of its own.
func (s *Service) UpdateProfile(ctx context.Context, userID uuid.UUID, in ProfileInput) error {
	if in.Name != nil {
		trimmed := strings.TrimSpace(*in.Name)
		if trimmed == "" || len([]rune(trimmed)) > NameMaxLength {
			return ErrBadName
		}
		in.Name = &trimmed
	}
	if in.Timezone != nil {
		zone := strings.TrimSpace(*in.Timezone)
		if _, err := time.LoadLocation(zone); err != nil || zone == "" {
			return ErrBadTimezone
		}
		in.Timezone = &zone
	}
	if in.Locale != nil {
		found := false
		for _, l := range Locales {
			if l == *in.Locale {
				found = true
			}
		}
		if !found {
			return ErrBadLocale
		}
	}
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			UPDATE app_user
			SET name = COALESCE($2, name), timezone = COALESCE($3, timezone), locale = COALESCE($4, locale)
			WHERE id = $1`, userID, in.Name, in.Timezone, in.Locale)
		if err != nil {
			return fmt.Errorf("update profile: %w", err)
		}
		return nil
	})
	return err
}

// SetAvatarURL records where a person's picture is served from; nil takes it away.
func (s *Service) SetAvatarURL(ctx context.Context, userID uuid.UUID, url *string) error {
	_, err := s.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE app_user SET avatar_url = $2 WHERE id = $1`, userID, url)
		return err
	})
	return err
}

// SharesOrganization reports whether two people are members of the same
// organization, which is what lets one see the other's picture.
func (s *Service) SharesOrganization(ctx context.Context, a, b uuid.UUID) (bool, error) {
	var shared bool
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM org_member x JOIN org_member y ON y.org_id = x.org_id
				WHERE x.user_id = $1 AND y.user_id = $2)`, a, b).Scan(&shared)
	})
	return shared, err
}
