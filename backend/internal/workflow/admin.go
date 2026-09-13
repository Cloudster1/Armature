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

// Admin writes workflow configuration: the graphs themselves, the schemes that
// map issue types onto them, and which scheme the organization answers with.
type Admin struct {
	db       *db.Cluster
	store    *Store
	registry *Registry
	observer Observer
}

// Observer is told, inside the saving transaction, which statuses a workflow
// has just gained. Boards live above workflows and cannot be imported from
// here, so the board package hands one in and follows the workflow itself.
type Observer interface {
	WorkflowSaved(ctx context.Context, tx db.DBTX, workflowID uuid.UUID, added []Status) error
}

// WithObserver names who is told when a workflow gains statuses.
func (a *Admin) WithObserver(o Observer) *Admin {
	a.observer = o
	return a
}

// StatusInput describes a status to coin.
type StatusInput struct {
	Name        string
	Category    StatusCategory
	Description string
}

// CreateStatus coins a status for the organization. Statuses are shared by
// every workflow, so the name is the organization's to keep unique; the
// category is decided here and never changed, since every count of what is
// done reads it.
func (a *Admin) CreateStatus(ctx context.Context, in StatusInput, actor uuid.UUID) (*Status, db.LSN, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" {
		return nil, 0, invalid("a status needs a name")
	}
	if !in.Category.Valid() {
		return nil, 0, invalid("%q is not a status category; use todo, in_progress or done", in.Category)
	}
	var out *Status
	lsn, err := a.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		id, err := InsertStatus(ctx, tx, in)
		if isUniqueViolation(err) {
			return invalid("this organization already has a status called %s", in.Name)
		}
		if err != nil {
			return err
		}
		out = &Status{ID: id, Name: in.Name, Category: in.Category, Description: in.Description}
		if err := tx.QueryRow(ctx, `SELECT position FROM issue_status WHERE id = $1`, id).Scan(&out.Position); err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "status.created", map[string]any{
			"statusId": id, "name": in.Name, "category": in.Category, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// InsertStatus adds a status after everything the organization already has, so
// a board built from the statuses reads in the order they were made.
func InsertStatus(ctx context.Context, tx db.DBTX, in StatusInput) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO issue_status (org_id, name, category, description, position)
		VALUES (current_org_id(), $1, $2, $3, (SELECT COALESCE(max(position) + 1, 0) FROM issue_status))
		RETURNING id`, in.Name, string(in.Category), in.Description).Scan(&id)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create status %q: %w", in.Name, err)
	}
	return id, nil
}

// NewAdmin holds saved rules against the built-in registry. An engine with a
// registry of its own hands it over with WithRegistry.
func NewAdmin(cluster *db.Cluster, store *Store) *Admin {
	if store == nil {
		store = NewStore()
	}
	return &Admin{db: cluster, store: store, registry: NewDefaultRegistry()}
}

// WithRegistry makes the admin refuse rules the engine could not run.
func (a *Admin) WithRegistry(registry *Registry) *Admin {
	a.registry = registry
	return a
}

// CreateWorkflow builds a new workflow from an editor's description of it.
func (a *Admin) CreateWorkflow(ctx context.Context, in GraphInput, actor uuid.UUID) (*Workflow, db.LSN, error) {
	var out *Workflow
	lsn, err := a.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := a.checkGraph(ctx, tx, &in); err != nil {
			return err
		}

		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO workflow (org_id, name, description)
			VALUES (current_org_id(), $1, $2) RETURNING id`,
			in.Name, in.Description,
		).Scan(&id)
		if isUniqueViolation(err) {
			return invalid("this organization already has a workflow called %s", in.Name)
		}
		if err != nil {
			return fmt.Errorf("create workflow: %w", err)
		}

		if err := a.writeGraph(ctx, tx, id, in); err != nil {
			return err
		}
		out, err = a.store.Load(ctx, tx, id)
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "workflow.created", map[string]any{
			"workflowId": id, "name": in.Name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// SaveWorkflow replaces a workflow's graph.
//
// It reconciles rather than rewrites: a status or transition that is still
// there keeps its row, and with it the rules hanging off that transition, which
// a delete and re-insert would silently throw away.
func (a *Admin) SaveWorkflow(ctx context.Context, id uuid.UUID, in GraphInput, actor uuid.UUID) (*Workflow, db.LSN, error) {
	var out *Workflow
	lsn, err := a.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := a.checkGraph(ctx, tx, &in); err != nil {
			return err
		}

		tag, err := tx.Exec(ctx, `
			UPDATE workflow SET name = $2, description = $3 WHERE id = $1`,
			id, in.Name, in.Description)
		if isUniqueViolation(err) {
			return invalid("this organization already has a workflow called %s", in.Name)
		}
		if err != nil {
			return fmt.Errorf("rename workflow: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}

		before, err := stepsByStatus(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := a.writeGraph(ctx, tx, id, in); err != nil {
			return err
		}
		out, err = a.store.Load(ctx, tx, id)
		if err != nil {
			return err
		}
		// Whoever follows workflows is told which statuses are new to this
		// one, so a board built on it can grow a lane before a card lands there.
		if a.observer != nil {
			var added []Status
			for _, step := range out.Steps {
				if _, had := before[step.Status.ID]; !had {
					added = append(added, step.Status)
				}
			}
			if len(added) > 0 {
				if err := a.observer.WorkflowSaved(ctx, tx, id, added); err != nil {
					return err
				}
			}
		}
		return events.EmitInTenant(ctx, tx, "workflow.updated", map[string]any{
			"workflowId": id, "name": in.Name, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// CopyWorkflow duplicates a workflow, rules included. Copying the organization's
// workflow and changing one thing is how a project override usually starts.
func (a *Admin) CopyWorkflow(ctx context.Context, id uuid.UUID, name string, actor uuid.UUID) (*Workflow, db.LSN, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, 0, invalid("the copy needs a name of its own")
	}

	var out *Workflow
	lsn, err := a.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		source, err := a.store.Load(ctx, tx, id)
		if err != nil {
			return err
		}

		var copyID uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO workflow (org_id, name, description)
			VALUES (current_org_id(), $1, $2) RETURNING id`,
			name, source.Description,
		).Scan(&copyID)
		if isUniqueViolation(err) {
			return invalid("this organization already has a workflow called %s", name)
		}
		if err != nil {
			return fmt.Errorf("copy workflow: %w", err)
		}

		steps := map[uuid.UUID]uuid.UUID{}
		for _, step := range source.Steps {
			x, y := coordinates(step.Layout)
			var newID uuid.UUID
			err := tx.QueryRow(ctx, `
				INSERT INTO workflow_step
				    (org_id, workflow_id, status_id, is_initial, position, layout_x, layout_y)
				VALUES (current_org_id(), $1, $2, $3, $4, $5, $6) RETURNING id`,
				copyID, step.Status.ID, step.IsInitial, step.Position, x, y,
			).Scan(&newID)
			if err != nil {
				return fmt.Errorf("copy step: %w", err)
			}
			steps[step.ID] = newID
		}

		for _, t := range source.Transitions {
			var from *uuid.UUID
			if !t.IsGlobal() {
				mapped := steps[*t.FromStepID]
				from = &mapped
			}
			var newID uuid.UUID
			err := tx.QueryRow(ctx, `
				INSERT INTO workflow_transition
				    (org_id, workflow_id, name, from_step_id, to_step_id, description, position)
				VALUES (current_org_id(), $1, $2, $3, $4, $5, $6) RETURNING id`,
				copyID, t.Name, from, steps[t.ToStepID], t.Description, t.Position,
			).Scan(&newID)
			if err != nil {
				return fmt.Errorf("copy transition: %w", err)
			}
			for _, rule := range t.Rules {
				if _, err := tx.Exec(ctx, `
					INSERT INTO transition_rule (org_id, transition_id, kind, rule_type, config, position)
					VALUES (current_org_id(), $1, $2, $3, $4, $5)`,
					newID, rule.Kind, rule.Type, rule.Config, rule.Position,
				); err != nil {
					return fmt.Errorf("copy rule: %w", err)
				}
			}
		}

		out, err = a.store.Load(ctx, tx, copyID)
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, "workflow.created", map[string]any{
			"workflowId": copyID, "name": name, "copiedFrom": id, "actorId": actor,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// DeleteWorkflow removes a workflow no scheme maps.
func (a *Admin) DeleteWorkflow(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return a.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var schemes []string
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(array_agg(DISTINCT sc.name), '{}')
			FROM workflow_scheme_item i
			JOIN workflow_scheme sc ON sc.id = i.scheme_id
			WHERE i.workflow_id = $1`, id).Scan(&schemes); err != nil {
			return fmt.Errorf("check workflow use: %w", err)
		}
		if len(schemes) > 0 {
			return fmt.Errorf("%w: %s still maps this workflow", ErrInUse, strings.Join(schemes, ", "))
		}

		tag, err := tx.Exec(ctx, `DELETE FROM workflow WHERE id = $1`, id)
		if err != nil {
			return fmt.Errorf("delete workflow: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// checkGraph tidies the input and holds it against the organization's statuses.
func (a *Admin) checkGraph(ctx context.Context, tx db.DBTX, in *GraphInput) error {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	for i := range in.Transitions {
		in.Transitions[i].Name = strings.TrimSpace(in.Transitions[i].Name)
		in.Transitions[i].Description = strings.TrimSpace(in.Transitions[i].Description)
	}

	names, err := namesOf(ctx, tx, `SELECT id, name FROM issue_status`)
	if err != nil {
		return err
	}
	if err := ValidateGraph(*in, names); err != nil {
		return err
	}
	return ValidateRules(in.Transitions, a.registry)
}

// coordinates splits a point for two nullable columns.
func coordinates(p *Point) (x, y *int) {
	if p == nil {
		return nil, nil
	}
	return &p.X, &p.Y
}

// writeGraph reconciles the stored steps and transitions with the input.
func (a *Admin) writeGraph(ctx context.Context, tx db.DBTX, workflowID uuid.UUID, in GraphInput) error {
	steps, err := stepsByStatus(ctx, tx, workflowID)
	if err != nil {
		return err
	}

	keep := make([]uuid.UUID, 0, len(in.Steps))
	for _, step := range in.Steps {
		keep = append(keep, step.StatusID)
	}
	// Removing a status takes the transitions that touched it with it, which is
	// the only sensible reading of "this status is no longer part of the flow".
	if _, err := tx.Exec(ctx, `
		DELETE FROM workflow_step WHERE workflow_id = $1 AND status_id <> ALL($2)`,
		workflowID, keep); err != nil {
		return fmt.Errorf("remove statuses: %w", err)
	}

	// One initial step at a time, so moving it never has both marked at once.
	if _, err := tx.Exec(ctx, `
		UPDATE workflow_step SET is_initial = false WHERE workflow_id = $1 AND is_initial`,
		workflowID); err != nil {
		return fmt.Errorf("clear the initial status: %w", err)
	}

	for position, step := range in.Steps {
		x, y := coordinates(step.Layout)
		existing, ok := steps[step.StatusID]
		if ok {
			if _, err := tx.Exec(ctx, `
				UPDATE workflow_step
				   SET is_initial = $2, position = $3, layout_x = $4, layout_y = $5
				 WHERE id = $1`,
				existing, step.IsInitial, position, x, y); err != nil {
				return fmt.Errorf("move status: %w", err)
			}
			continue
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO workflow_step
			    (org_id, workflow_id, status_id, is_initial, position, layout_x, layout_y)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6) RETURNING id`,
			workflowID, step.StatusID, step.IsInitial, position, x, y,
		).Scan(&id); err != nil {
			return fmt.Errorf("add status: %w", err)
		}
		steps[step.StatusID] = id
	}

	return a.writeTransitions(ctx, tx, workflowID, in.Transitions, steps)
}

func (a *Admin) writeTransitions(ctx context.Context, tx db.DBTX, workflowID uuid.UUID, wanted []TransitionInput, steps map[uuid.UUID]uuid.UUID) error {
	// A transition whose status was just removed is already gone by cascade, so
	// what survives is read back rather than assumed.
	surviving, err := idSet(ctx, tx, `SELECT id FROM workflow_transition WHERE workflow_id = $1`, workflowID)
	if err != nil {
		return err
	}

	keep := make([]uuid.UUID, 0, len(wanted))
	for _, t := range wanted {
		if surviving[t.ID] {
			keep = append(keep, t.ID)
		}
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM workflow_transition WHERE workflow_id = $1 AND id <> ALL($2)`,
		workflowID, keep); err != nil {
		return fmt.Errorf("remove transitions: %w", err)
	}

	for position, t := range wanted {
		var from *uuid.UUID
		if !t.FromAnywhere() {
			mapped := steps[*t.FromStatusID]
			from = &mapped
		}
		to := steps[t.ToStatusID]

		if surviving[t.ID] {
			if _, err := tx.Exec(ctx, `
				UPDATE workflow_transition
				   SET name = $2, from_step_id = $3, to_step_id = $4, description = $5, position = $6
				 WHERE id = $1`,
				t.ID, t.Name, from, to, t.Description, position); err != nil {
				return fmt.Errorf("update transition %q: %w", t.Name, err)
			}
			// The editor either sent the rules it wants or said nothing about
			// them; only the former is a reason to touch what is there.
			if t.Rules == nil {
				continue
			}
			if _, err := tx.Exec(ctx, `DELETE FROM transition_rule WHERE transition_id = $1`, t.ID); err != nil {
				return fmt.Errorf("replace rules of %q: %w", t.Name, err)
			}
			if err := writeRules(ctx, tx, t.ID, t); err != nil {
				return err
			}
			continue
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO workflow_transition
			    (org_id, workflow_id, name, from_step_id, to_step_id, description, position)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6) RETURNING id`,
			workflowID, t.Name, from, to, t.Description, position).Scan(&id); err != nil {
			return fmt.Errorf("add transition %q: %w", t.Name, err)
		}
		if err := writeRules(ctx, tx, id, t); err != nil {
			return err
		}
	}
	return nil
}

func writeRules(ctx context.Context, tx db.DBTX, transitionID uuid.UUID, t TransitionInput) error {
	for j, rule := range t.Rules {
		config := rule.Config
		if config == "" {
			config = "{}"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO transition_rule (org_id, transition_id, kind, rule_type, config, position)
			VALUES (current_org_id(), $1, $2, $3, $4, $5)`,
			transitionID, string(rule.Kind), rule.Type, config, j); err != nil {
			return fmt.Errorf("add rule %q to %q: %w", rule.Type, t.Name, err)
		}
	}
	return nil
}

// EnsureWorkflow returns the organization's workflow by the input's name,
// building it from the graph when there is none yet.
//
// It runs in the caller's transaction, so a project made from a template and
// the workflow that template brings commit together. Applying the same template
// twice finds the workflow the first application made rather than failing on
// the name or making a second one.
func (a *Admin) EnsureWorkflow(ctx context.Context, tx db.DBTX, in GraphInput) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM workflow WHERE lower(name) = lower($1)`, in.Name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("look for workflow %q: %w", in.Name, err)
	}

	if err := a.checkGraph(ctx, tx, &in); err != nil {
		return uuid.Nil, err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO workflow (org_id, name, description)
		VALUES (current_org_id(), $1, $2) RETURNING id`,
		in.Name, in.Description,
	).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("create workflow %q: %w", in.Name, err)
	}
	if err := a.writeGraph(ctx, tx, id, in); err != nil {
		return uuid.Nil, err
	}
	return id, events.EmitInTenant(ctx, tx, "workflow.created", map[string]any{
		"workflowId": id, "name": in.Name,
	})
}

// EnsureScheme returns the organization's scheme by the input's name, creating
// it with the given mappings when there is none. See EnsureWorkflow.
func (a *Admin) EnsureScheme(ctx context.Context, tx db.DBTX, in SchemeInput) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM workflow_scheme WHERE lower(name) = lower($1)`, in.Name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, fmt.Errorf("look for scheme %q: %w", in.Name, err)
	}

	if err := a.checkScheme(ctx, tx, &in, false); err != nil {
		return uuid.Nil, err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO workflow_scheme (org_id, name) VALUES (current_org_id(), $1) RETURNING id`,
		in.Name,
	).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("create scheme %q: %w", in.Name, err)
	}
	if err := writeSchemeItems(ctx, tx, id, in.Items); err != nil {
		return uuid.Nil, err
	}
	return id, events.EmitInTenant(ctx, tx, "workflow.scheme.created", map[string]any{
		"schemeId": id, "name": in.Name,
	})
}

func stepsByStatus(ctx context.Context, tx db.DBTX, workflowID uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `
		SELECT status_id, id FROM workflow_step WHERE workflow_id = $1`, workflowID)
	if err != nil {
		return nil, fmt.Errorf("read steps: %w", err)
	}
	defer rows.Close()

	out := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var statusID, stepID uuid.UUID
		if err := rows.Scan(&statusID, &stepID); err != nil {
			return nil, err
		}
		out[statusID] = stepID
	}
	return out, rows.Err()
}

func idSet(ctx context.Context, tx db.DBTX, query string, args ...any) (map[uuid.UUID]bool, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("read ids: %w", err)
	}
	defer rows.Close()

	out := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func namesOf(ctx context.Context, tx db.DBTX, query string) (map[uuid.UUID]string, error) {
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("read names: %w", err)
	}
	defer rows.Close()

	out := map[uuid.UUID]string{}
	for rows.Next() {
		var (
			id   uuid.UUID
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
