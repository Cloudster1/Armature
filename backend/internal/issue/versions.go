package issue

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// ErrVersionNotFound is returned for a version that is not this project's.
var ErrVersionNotFound = errors.New("that version is not one of this project's")

// ErrComponentNotFound is returned for a component that is not this project's.
var ErrComponentNotFound = errors.New("that component is not one of this project's")

// SetVersions names what fixes the issue and what it affects, replacing both
// sets, and writes each change to the history.
func (s *Service) SetVersions(ctx context.Context, key string, fix, affects []uuid.UUID, actor Actor) (*Issue, db.LSN, error) {
	var updated *Issue
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		id, _, err := s.idAndKey(ctx, tx, key)
		if err != nil {
			return err
		}
		before, err := scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1 FOR UPDATE OF i`, id))
		if err != nil {
			return err
		}
		var changes []Change
		for _, set := range []struct {
			role string
			want []uuid.UUID
			had  []VersionRef
		}{{"fix", fix, before.FixVersions}, {"affects", affects, before.AffectsVersions}} {
			if _, err := tx.Exec(ctx, `DELETE FROM issue_version WHERE issue_id = $1 AND role = $2`, id, set.role); err != nil {
				return err
			}
			var names []string
			for _, v := range dedupe(set.want) {
				var name string
				err := tx.QueryRow(ctx, `SELECT name FROM version WHERE id = $1 AND project_id = $2`, v, before.ProjectID).Scan(&name)
				if err != nil {
					return ErrVersionNotFound
				}
				if _, err := tx.Exec(ctx, `INSERT INTO issue_version (org_id, issue_id, version_id, role) VALUES (current_org_id(), $1, $2, $3)`, id, v, set.role); err != nil {
					if isCheckViolation(err) {
						return fmt.Errorf("%s is archived and cannot be named", name)
					}
					return err
				}
				names = append(names, name)
			}
			from := versionNames(set.had)
			to := strings.Join(names, ", ")
			if from != to {
				field := "fixVersion"
				if set.role == "affects" {
					field = "affectsVersion"
				}
				changes = append(changes, Change{Field: field, From: from, To: to})
			}
		}
		if len(changes) == 0 {
			updated = before
			return nil
		}
		if err := s.recordHistory(ctx, tx, id, actor.UserID, changes); err != nil {
			return err
		}
		updated, err = scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, id))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, events.TopicIssueUpdated, map[string]any{
			"issueId": id, "key": updated.Key, "changes": changes, "actorId": actor.UserID,
		})
	})
	return updated, lsn, err
}

// SetComponents puts the issue in these parts of the project and no others.
func (s *Service) SetComponents(ctx context.Context, key string, ids []uuid.UUID, actor Actor) (*Issue, db.LSN, error) {
	var updated *Issue
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		id, _, err := s.idAndKey(ctx, tx, key)
		if err != nil {
			return err
		}
		before, err := scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1 FOR UPDATE OF i`, id))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM issue_component WHERE issue_id = $1`, id); err != nil {
			return err
		}
		if err := s.putComponents(ctx, tx, id, ids); err != nil {
			return err
		}
		updated, err = scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, id))
		if err != nil {
			return err
		}
		from, to := componentNames(before.Components), componentNames(updated.Components)
		if from == to {
			return nil
		}
		changes := []Change{{Field: "component", From: from, To: to}}
		if err := s.recordHistory(ctx, tx, id, actor.UserID, changes); err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, events.TopicIssueUpdated, map[string]any{
			"issueId": id, "key": updated.Key, "changes": changes, "actorId": actor.UserID,
		})
	})
	return updated, lsn, err
}

// putComponents inserts the component rows; the trigger refuses another
// project's, which is answered as not found.
func (s *Service) putComponents(ctx context.Context, tx db.DBTX, issueID uuid.UUID, ids []uuid.UUID) error {
	for _, c := range dedupe(ids) {
		if _, err := tx.Exec(ctx, `INSERT INTO issue_component (org_id, issue_id, component_id) VALUES (current_org_id(), $1, $2)`, issueID, c); err != nil {
			if isCheckViolation(err) || isForeignKey(err) {
				return ErrComponentNotFound
			}
			return err
		}
	}
	return nil
}

// componentAssignee is who takes a new issue when nobody was named: the first
// of its components with a default assignee, or nobody.
func (s *Service) componentAssignee(ctx context.Context, tx db.DBTX, given *uuid.UUID, components []uuid.UUID, projectID uuid.UUID) (*uuid.UUID, error) {
	if given != nil || len(components) == 0 {
		return given, nil
	}
	var who *uuid.UUID
	err := tx.QueryRow(ctx, `
		SELECT c.default_assignee_id FROM component c
		JOIN unnest($1::uuid[]) WITH ORDINALITY AS wanted(id, ord) ON wanted.id = c.id
		WHERE c.project_id = $2 AND c.default_assignee_id IS NOT NULL
		ORDER BY wanted.ord LIMIT 1`, components, projectID).Scan(&who)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return who, err
}

func versionNames(refs []VersionRef) string {
	var names []string
	for _, r := range refs {
		names = append(names, r.Name)
	}
	return strings.Join(names, ", ")
}

func componentNames(refs []ComponentRef) string {
	var names []string
	for _, r := range refs {
		names = append(names, r.Name)
	}
	return strings.Join(names, ", ")
}

func dedupe(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func isForeignKey(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
