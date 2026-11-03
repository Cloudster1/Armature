package workflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// EnsureStatus returns the status by the input's name, coining it when there
// is none. Somebody coining the same name at once makes this wait and use theirs.
func EnsureStatus(ctx context.Context, tx db.DBTX, in StatusInput) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO issue_status (name, category, description, position)
		VALUES ($1, $2, $3, (SELECT COALESCE(max(position) + 1, 0) FROM issue_status))
		ON CONFLICT (lower(name)) DO NOTHING
		RETURNING id`, in.Name, string(in.Category), in.Description).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("create status %q: %w", in.Name, err)
	}
	if err := tx.QueryRow(ctx, `SELECT id FROM issue_status WHERE lower(name) = lower($1)`, in.Name).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("look for status %q: %w", in.Name, err)
	}
	return id, nil
}

// EnsureWorkflow returns the workflow by the input's name, building it in the caller's
// transaction when there is none; a template applied twice, even at once, shares one.
func (a *Admin) EnsureWorkflow(ctx context.Context, tx db.DBTX, in GraphInput) (uuid.UUID, error) {
	var id uuid.UUID
	lookup := func() (uuid.UUID, error) {
		err := tx.QueryRow(ctx, `SELECT id FROM workflow WHERE lower(name) = lower($1)`, in.Name).Scan(&id)
		return id, err
	}
	if _, err := lookup(); err == nil {
		return id, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("look for workflow %q: %w", in.Name, err)
	}

	if err := a.checkGraph(ctx, tx, &in); err != nil {
		return uuid.Nil, err
	}
	err := tx.QueryRow(ctx, `
		INSERT INTO workflow (name, description)
		VALUES ($1, $2) ON CONFLICT (lower(name)) DO NOTHING RETURNING id`,
		in.Name, in.Description,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// Made at the same moment by another transaction, which committed its
		// graph with it, so theirs is whole.
		if _, err := lookup(); err != nil {
			return uuid.Nil, fmt.Errorf("look for workflow %q: %w", in.Name, err)
		}
		return id, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("create workflow %q: %w", in.Name, err)
	}
	if err := a.writeGraph(ctx, tx, id, in); err != nil {
		return uuid.Nil, err
	}
	return id, events.Emit(ctx, tx, "workflow.created", map[string]any{
		"workflowId": id, "name": in.Name,
	})
}

// EnsureScheme returns the organization's scheme by the input's name, creating
// it with the given mappings when there is none. See EnsureWorkflow.
func (a *Admin) EnsureScheme(ctx context.Context, tx db.DBTX, in SchemeInput) (uuid.UUID, error) {
	var id uuid.UUID
	lookup := func() (uuid.UUID, error) {
		err := tx.QueryRow(ctx, `SELECT id FROM workflow_scheme WHERE lower(name) = lower($1)`, in.Name).Scan(&id)
		return id, err
	}
	if _, err := lookup(); err == nil {
		return id, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("look for scheme %q: %w", in.Name, err)
	}

	if err := a.checkScheme(ctx, tx, &in, false); err != nil {
		return uuid.Nil, err
	}
	err := tx.QueryRow(ctx, `
		INSERT INTO workflow_scheme (name) VALUES ($1) ON CONFLICT (lower(name)) DO NOTHING RETURNING id`,
		in.Name,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := lookup(); err != nil {
			return uuid.Nil, fmt.Errorf("look for scheme %q: %w", in.Name, err)
		}
		return id, nil
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("create scheme %q: %w", in.Name, err)
	}
	if err := writeSchemeItems(ctx, tx, id, in.Items); err != nil {
		return uuid.Nil, err
	}
	return id, events.Emit(ctx, tx, "workflow.scheme.created", map[string]any{
		"schemeId": id, "name": in.Name,
	})
}
