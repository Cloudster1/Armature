package arrange

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/project"
)

// SaveInProject replaces how this project arranges one issue type. No places
// hands the type back to the organization.
func (s *Service) SaveInProject(ctx context.Context, projectKey string, issueTypeID *uuid.UUID, places []Placement) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var projectID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM project WHERE key = $1`, project.NormalizeKey(projectKey)).Scan(&projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return project.ErrNotFound
		}
		if err != nil {
			return err
		}
		return save(ctx, tx, &projectID, issueTypeID, places)
	})
}

// SaveInOrg replaces what every project follows until it disagrees. No places
// restores the built-in arrangement.
func (s *Service) SaveInOrg(ctx context.Context, issueTypeID *uuid.UUID, places []Placement) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		return save(ctx, tx, nil, issueTypeID, places)
	})
}

// save writes an arrangement whole: what is sent is the arrangement, and
// nothing sent means this scope has no opinion about the type any more.
func save(ctx context.Context, tx db.DBTX, projectID, issueTypeID *uuid.UUID, places []Placement) error {
	if places == nil {
		_, err := tx.Exec(ctx, `
			DELETE FROM issue_arrangement
			WHERE project_id IS NOT DISTINCT FROM $1 AND issue_type_id IS NOT DISTINCT FROM $2`, projectID, issueTypeID)
		return err
	}
	if err := checkPlaces(places); err != nil {
		return err
	}
	if err := checkFields(ctx, tx, projectID, places); err != nil {
		return err
	}

	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO issue_arrangement (org_id, project_id, issue_type_id)
		VALUES (current_org_id(), $1, $2)
		ON CONFLICT (org_id, project_id, issue_type_id) DO UPDATE SET updated_at = now()
		RETURNING id`, projectID, issueTypeID).Scan(&id); err != nil {
		return fmt.Errorf("keep the arrangement: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM issue_arrangement_slot WHERE arrangement_id = $1`, id); err != nil {
		return fmt.Errorf("clear the old places: %w", err)
	}
	for at, place := range places {
		var builtin *string
		if place.Slot != "" {
			word := string(place.Slot)
			builtin = &word
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO issue_arrangement_slot (org_id, arrangement_id, area, position, builtin, custom_field_id)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)`,
			id, string(place.Area), at, builtin, place.FieldID); err != nil {
			return fmt.Errorf("place %s: %w", place.Area, err)
		}
	}
	return nil
}

// checkPlaces refuses an arrangement the page could not draw, in the words the
// person arranging it reads.
func checkPlaces(places []Placement) error {
	seen := map[string]bool{}
	for _, place := range places {
		if !place.Area.Known() {
			return fmt.Errorf("%q is not a part of the page", place.Area)
		}
		named := place.Slot != ""
		if named == (place.FieldID != nil) {
			return errors.New("a place is one of this tracker's own fields or one of the project's, never both")
		}
		if named && !place.Slot.Known() {
			return fmt.Errorf("%q is not a field this tracker knows", place.Slot)
		}
		key := string(place.Slot)
		if place.FieldID != nil {
			key = place.FieldID.String()
		}
		if seen[key] {
			return fmt.Errorf("%s is placed twice; a field is in one place", key)
		}
		seen[key] = true
	}
	return nil
}

// checkFields refuses a field the scope has no business arranging: another
// project's, or a project's own inside the organization's arrangement.
func checkFields(ctx context.Context, tx db.DBTX, projectID *uuid.UUID, places []Placement) error {
	ids := make([]uuid.UUID, 0, len(places))
	for _, place := range places {
		if place.FieldID != nil {
			ids = append(ids, *place.FieldID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	query := `SELECT count(*) FROM custom_field WHERE id = ANY($1) AND project_id IS NULL`
	args := []any{ids}
	if projectID != nil {
		query = `SELECT count(*) FROM custom_field WHERE id = ANY($1) AND (project_id IS NULL OR project_id = $2)`
		args = append(args, *projectID)
	}
	var found int
	if err := tx.QueryRow(ctx, query, args...).Scan(&found); err != nil {
		return err
	}
	if found == len(ids) {
		return nil
	}
	if projectID == nil {
		return errors.New("the organization arranges its own fields; promote a project's field before placing it here")
	}
	return errors.New("a field of another project cannot be placed on this one")
}
