package workflow

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

// SchemeInput is a scheme as an administrator describes it.
type SchemeInput struct {
	Name  string
	Items []SchemeItemInput
}

// SchemeItemInput maps one issue type onto a workflow. A nil issue type is the
// scheme's fallback.
type SchemeItemInput struct {
	IssueTypeID *uuid.UUID
	WorkflowID  uuid.UUID
}

// ValidateScheme checks a scheme before it is written. A scheme that answers
// for the whole organization must have a fallback; one a project names need
// only cover the types it disagrees about, and the rest fall through.
func ValidateScheme(in SchemeInput, isDefault bool, typeNames map[uuid.UUID]string) error {
	if in.Name == "" {
		return invalid("a workflow scheme needs a name")
	}
	if len(in.Items) == 0 {
		return invalid("a workflow scheme with no mappings decides nothing")
	}

	fallbacks := 0
	seen := map[uuid.UUID]bool{}
	for _, item := range in.Items {
		if item.IssueTypeID == nil {
			fallbacks++
			continue
		}
		name, known := typeNames[*item.IssueTypeID]
		if !known {
			return invalid("this organization has no such issue type")
		}
		if seen[*item.IssueTypeID] {
			return invalid("%s is mapped twice", name)
		}
		seen[*item.IssueTypeID] = true
	}
	if fallbacks > 1 {
		return invalid("a scheme can only have one fallback")
	}
	if isDefault && fallbacks == 0 {
		return invalid("the organization's scheme needs a fallback, or an issue type nobody mapped would have no workflow")
	}
	return nil
}

// CreateScheme adds a scheme. It is not the default until it is promoted.
func (a *Admin) CreateScheme(ctx context.Context, in SchemeInput, actor uuid.UUID) (*Scheme, db.LSN, error) {
	var out *Scheme
	lsn, err := a.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := a.checkScheme(ctx, tx, &in, false); err != nil {
			return err
		}

		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO workflow_scheme (org_id, name) VALUES (current_org_id(), $1) RETURNING id`,
			in.Name,
		).Scan(&id)
		if isUniqueViolation(err) {
			return invalid("this organization already has a scheme called %s", in.Name)
		}
		if err != nil {
			return fmt.Errorf("create scheme: %w", err)
		}

		if err := writeSchemeItems(ctx, tx, id, in.Items); err != nil {
			return err
		}
		out, err = loadScheme(ctx, tx, id)
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "workflow.scheme.created", map[string]any{
			"schemeId": id, "name": in.Name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// SaveScheme replaces a scheme's name and mappings.
func (a *Admin) SaveScheme(ctx context.Context, id uuid.UUID, in SchemeInput, actor uuid.UUID) (*Scheme, db.LSN, error) {
	var out *Scheme
	lsn, err := a.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var isDefault bool
		err := tx.QueryRow(ctx, `SELECT is_default FROM workflow_scheme WHERE id = $1`, id).Scan(&isDefault)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read scheme: %w", err)
		}
		if err := a.checkScheme(ctx, tx, &in, isDefault); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `UPDATE workflow_scheme SET name = $2 WHERE id = $1`, id, in.Name); isUniqueViolation(err) {
			return invalid("this organization already has a scheme called %s", in.Name)
		} else if err != nil {
			return fmt.Errorf("rename scheme: %w", err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM workflow_scheme_item WHERE scheme_id = $1`, id); err != nil {
			return fmt.Errorf("clear scheme: %w", err)
		}
		if err := writeSchemeItems(ctx, tx, id, in.Items); err != nil {
			return err
		}
		out, err = loadScheme(ctx, tx, id)
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "workflow.scheme.updated", map[string]any{
			"schemeId": id, "name": in.Name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// SetDefaultScheme makes one scheme the answer for every project that has not
// named its own.
func (a *Admin) SetDefaultScheme(ctx context.Context, id uuid.UUID, actor uuid.UUID) (db.LSN, error) {
	return a.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var hasFallback bool
		err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM workflow_scheme_item
			                WHERE scheme_id = $1 AND issue_type_id IS NULL)
			FROM workflow_scheme WHERE id = $1`, id).Scan(&hasFallback)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read scheme: %w", err)
		}
		if !hasFallback {
			return invalid("the organization's scheme needs a fallback, or an issue type nobody mapped would have no workflow")
		}

		// Clearing first keeps the one-default index satisfied at every step.
		if _, err := tx.Exec(ctx, `
			UPDATE workflow_scheme SET is_default = false WHERE is_default AND id <> $1`, id); err != nil {
			return fmt.Errorf("stand down the old default: %w", err)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE workflow_scheme SET is_default = true WHERE id = $1`, id); err != nil {
			return fmt.Errorf("promote scheme: %w", err)
		}
		return events.EmitInTenant(ctx, tx, "workflow.scheme.promoted", map[string]any{
			"schemeId": id, "actorId": actor,
		})
	})
}

// DeleteScheme removes a scheme no project names and that is not the default.
func (a *Admin) DeleteScheme(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return a.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			isDefault bool
			projects  []string
		)
		err := tx.QueryRow(ctx, `
			SELECT sc.is_default,
			       COALESCE(array_agg(p.key ORDER BY p.key) FILTER (WHERE p.key IS NOT NULL), '{}')
			FROM workflow_scheme sc
			LEFT JOIN project p ON p.workflow_scheme_id = sc.id
			WHERE sc.id = $1
			GROUP BY sc.is_default`, id).Scan(&isDefault, &projects)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read scheme: %w", err)
		}
		if isDefault {
			return fmt.Errorf("%w: it is the organization's own scheme, so promote another one first", ErrInUse)
		}
		if len(projects) > 0 {
			return fmt.Errorf("%w: %s still uses it", ErrInUse, strings.Join(projects, ", "))
		}

		if _, err := tx.Exec(ctx, `DELETE FROM workflow_scheme WHERE id = $1`, id); err != nil {
			return fmt.Errorf("delete scheme: %w", err)
		}
		return nil
	})
}

func (a *Admin) checkScheme(ctx context.Context, tx db.DBTX, in *SchemeInput, isDefault bool) error {
	in.Name = strings.TrimSpace(in.Name)
	names, err := namesOf(ctx, tx, `SELECT id, name FROM issue_type`)
	if err != nil {
		return err
	}
	return ValidateScheme(*in, isDefault, names)
}

func writeSchemeItems(ctx context.Context, tx db.DBTX, schemeID uuid.UUID, items []SchemeItemInput) error {
	for _, item := range items {
		_, err := tx.Exec(ctx, `
			INSERT INTO workflow_scheme_item (org_id, scheme_id, issue_type_id, workflow_id)
			VALUES (current_org_id(), $1, $2, $3)`,
			schemeID, item.IssueTypeID, item.WorkflowID)
		if isForeignKeyViolation(err) {
			return invalid("that workflow does not exist in this organization")
		}
		if err != nil {
			return fmt.Errorf("map workflow: %w", err)
		}
	}
	return nil
}

func loadScheme(ctx context.Context, tx db.DBTX, id uuid.UUID) (*Scheme, error) {
	var sc Scheme
	err := tx.QueryRow(ctx, `
		SELECT sc.id, sc.name, sc.is_default,
		       COALESCE(array_agg(p.key ORDER BY p.key) FILTER (WHERE p.key IS NOT NULL), '{}')
		FROM workflow_scheme sc
		LEFT JOIN project p ON p.workflow_scheme_id = sc.id
		WHERE sc.id = $1
		GROUP BY sc.id, sc.name, sc.is_default`, id,
	).Scan(&sc.ID, &sc.Name, &sc.IsDefault, &sc.Projects)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load scheme: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT i.issue_type_id, COALESCE(t.name, ''), i.workflow_id, w.name
		FROM workflow_scheme_item i
		JOIN workflow w ON w.id = i.workflow_id
		LEFT JOIN issue_type t ON t.id = i.issue_type_id
		WHERE i.scheme_id = $1
		ORDER BY (i.issue_type_id IS NULL), t.hierarchy_level DESC, t.position, t.name`, id)
	if err != nil {
		return nil, fmt.Errorf("load scheme items: %w", err)
	}
	defer rows.Close()

	sc.Items = []SchemeItem{}
	for rows.Next() {
		var item SchemeItem
		if err := rows.Scan(&item.IssueTypeID, &item.IssueTypeName, &item.WorkflowID, &item.WorkflowName); err != nil {
			return nil, err
		}
		sc.Items = append(sc.Items, item)
	}
	return &sc, rows.Err()
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
