package workflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
)

// ErrNotFound is returned when a workflow, status or scheme does not exist in
// the caller's organization.
var ErrNotFound = errors.New("not found")

// Store loads and saves workflows. It takes a DBTX rather than a pool so that
// callers can read a workflow inside the same transaction as the change they
// are about to make.
type Store struct{}

func NewStore() *Store { return &Store{} }

// Load reads one workflow with its steps, transitions and rules.
//
// Three queries rather than one join: a workflow has a handful of steps, each
// with a handful of transitions, each with a handful of rules, and joining all
// three multiplies the rows by a factor that has to be de-duplicated in Go
// anyway.
func (s *Store) Load(ctx context.Context, tx db.DBTX, workflowID uuid.UUID) (*Workflow, error) {
	var w Workflow
	err := tx.QueryRow(ctx, `
		SELECT id, name, description FROM workflow WHERE id = $1`, workflowID,
	).Scan(&w.ID, &w.Name, &w.Description)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load workflow: %w", err)
	}

	if err := s.loadSteps(ctx, tx, &w); err != nil {
		return nil, err
	}
	if err := s.loadTransitions(ctx, tx, &w); err != nil {
		return nil, err
	}
	return &w, nil
}

func (s *Store) loadSteps(ctx context.Context, tx db.DBTX, w *Workflow) error {
	rows, err := tx.Query(ctx, `
		SELECT st.id, st.is_initial, st.position, st.layout_x, st.layout_y,
		       s.id, s.name, s.category, s.description, s.position
		FROM workflow_step st
		JOIN issue_status s ON s.id = st.status_id
		WHERE st.workflow_id = $1
		ORDER BY st.position, s.name`, w.ID)
	if err != nil {
		return fmt.Errorf("load steps: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			step Step
			x, y *int
		)
		if err := rows.Scan(
			&step.ID, &step.IsInitial, &step.Position, &x, &y,
			&step.Status.ID, &step.Status.Name, &step.Status.Category,
			&step.Status.Description, &step.Status.Position,
		); err != nil {
			return err
		}
		if x != nil && y != nil {
			step.Layout = &Point{X: *x, Y: *y}
		}
		w.Steps = append(w.Steps, step)
	}
	return rows.Err()
}

func (s *Store) loadTransitions(ctx context.Context, tx db.DBTX, w *Workflow) error {
	rows, err := tx.Query(ctx, `
		SELECT t.id, t.name, t.from_step_id, t.to_step_id, t.description, t.position,
		       r.id, r.kind, r.rule_type, r.config, r.position
		FROM workflow_transition t
		LEFT JOIN transition_rule r ON r.transition_id = t.id
		WHERE t.workflow_id = $1
		ORDER BY t.position, t.name, r.kind, r.position`, w.ID)
	if err != nil {
		return fmt.Errorf("load transitions: %w", err)
	}
	defer rows.Close()

	// The left join repeats the transition once per rule, so collapse as we go.
	byID := map[uuid.UUID]*Transition{}
	var order []uuid.UUID

	for rows.Next() {
		var (
			t        Transition
			ruleID   *uuid.UUID
			ruleKind *RuleKind
			ruleType *string
			ruleCfg  []byte
			rulePos  *int
		)
		if err := rows.Scan(
			&t.ID, &t.Name, &t.FromStepID, &t.ToStepID, &t.Description, &t.Position,
			&ruleID, &ruleKind, &ruleType, &ruleCfg, &rulePos,
		); err != nil {
			return err
		}

		existing, ok := byID[t.ID]
		if !ok {
			copied := t
			byID[t.ID] = &copied
			order = append(order, t.ID)
			existing = &copied
		}

		if ruleID != nil {
			existing.Rules = append(existing.Rules, Rule{
				ID:       *ruleID,
				Kind:     *ruleKind,
				Type:     *ruleType,
				Config:   ruleCfg,
				Position: *rulePos,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, id := range order {
		w.Transitions = append(w.Transitions, *byID[id])
	}
	return nil
}

// Summary is a workflow without its graph, for listings.
type Summary struct {
	ID              uuid.UUID `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description,omitempty"`
	StepCount       int       `json:"stepCount"`
	TransitionCount int       `json:"transitionCount"`
	// Schemes is how many schemes map this workflow. A workflow no scheme maps
	// is one nothing uses, and the only kind that can safely be deleted.
	Schemes int `json:"schemeCount"`
	// InDefault marks a workflow the organization's own scheme maps, so the
	// listing can say which workflows every project inherits.
	InDefault bool `json:"inDefault"`
}

// List returns every workflow in the organization.
func (s *Store) List(ctx context.Context, tx db.DBTX) ([]Summary, error) {
	rows, err := tx.Query(ctx, `
		SELECT w.id, w.name, w.description,
		       (SELECT count(*) FROM workflow_step st WHERE st.workflow_id = w.id),
		       (SELECT count(*) FROM workflow_transition t WHERE t.workflow_id = w.id),
		       (SELECT count(DISTINCT i.scheme_id) FROM workflow_scheme_item i
		         WHERE i.workflow_id = w.id),
		       EXISTS (SELECT 1 FROM workflow_scheme_item i
		                 JOIN workflow_scheme sc ON sc.id = i.scheme_id
		                WHERE i.workflow_id = w.id AND sc.is_default)
		FROM workflow w
		ORDER BY w.name`)
	if err != nil {
		return nil, fmt.Errorf("list workflows: %w", err)
	}
	defer rows.Close()

	var out []Summary
	for rows.Next() {
		var s Summary
		if err := rows.Scan(&s.ID, &s.Name, &s.Description,
			&s.StepCount, &s.TransitionCount, &s.Schemes, &s.InDefault); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListStatuses returns every status in the organization.
func (s *Store) ListStatuses(ctx context.Context, tx db.DBTX) ([]Status, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, name, category, description, position
		FROM issue_status ORDER BY position, name`)
	if err != nil {
		return nil, fmt.Errorf("list statuses: %w", err)
	}
	defer rows.Close()

	var out []Status
	for rows.Next() {
		var s Status
		if err := rows.Scan(&s.ID, &s.Name, &s.Category, &s.Description, &s.Position); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
