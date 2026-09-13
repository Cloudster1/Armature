// Package bootstrap gives a brand new organization the configuration it needs
// to be usable.
//
// An organization with no statuses, issue types or workflow cannot have a
// project, and a project is the first thing anybody wants. Rather than make the
// user assemble that by hand, every organization is created with a working set
// in the same transaction as the signup itself, so there is no window in which
// an account exists but cannot do anything.
package bootstrap

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/workflow"
)

// Names of the defaults. They are referenced by the seed command and by tests,
// and users can rename them afterwards.
const (
	StatusToDo       = "To Do"
	StatusInProgress = "In Progress"
	StatusInReview   = "In Review"
	StatusDone       = "Done"

	TypeTask       = "Task"
	TypeBug        = "Bug"
	TypeStory      = "Story"
	TypeEpic       = "Epic"
	TypeSubtask    = "Subtask"
	TypeInitiative = "Initiative"

	DefaultWorkflowName = "Default software workflow"
	DefaultSchemeName   = "Default workflow scheme"

	LinkBlocks     = "Blocks"
	LinkRelates    = "Relates"
	LinkDuplicates = "Duplicates"
)

// The hierarchy levels the default types occupy. They are duplicated from the
// issue package rather than imported: bootstrap runs underneath issues, and a
// dependency the other way would only exist to share four integers.
const (
	LevelSubtask    = -1
	LevelStandard   = 0
	LevelEpic       = 1
	LevelInitiative = 2
)

// Result reports what was created, so the caller can reference it without
// querying again.
type Result struct {
	WorkflowSchemeID uuid.UUID
	WorkflowID       uuid.UUID
	StatusIDs        map[string]uuid.UUID
	IssueTypeIDs     map[string]uuid.UUID
}

// Org creates the default statuses, issue types, workflow, workflow scheme and
// link types for an organization.
//
// It must run in the same transaction as the organization's own creation. It is
// not idempotent: calling it twice for one organization violates the uniqueness
// constraints on names, which is the correct outcome.
func Org(ctx context.Context, tx db.DBTX, orgID uuid.UUID) (*Result, error) {
	result := &Result{
		StatusIDs:    map[string]uuid.UUID{},
		IssueTypeIDs: map[string]uuid.UUID{},
	}

	statuses := []struct {
		name     string
		category workflow.StatusCategory
		desc     string
	}{
		{StatusToDo, workflow.CategoryTodo, "Not started yet."},
		{StatusInProgress, workflow.CategoryInProgress, "Somebody is working on this."},
		{StatusInReview, workflow.CategoryInProgress, "Waiting on review."},
		{StatusDone, workflow.CategoryDone, "Finished."},
	}
	for i, s := range statuses {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO issue_status (org_id, name, category, description, position)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			orgID, s.name, string(s.category), s.desc, i,
		).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("create status %q: %w", s.name, err)
		}
		result.StatusIDs[s.name] = id
	}

	// The level is where the type sits in the hierarchy, counted from the
	// ordinary working level at zero. Task comes first at that level because
	// the first one there is what an issue created without a type becomes.
	issueTypes := []struct {
		name  string
		icon  string
		desc  string
		level int
	}{
		{TypeTask, "task", "A piece of work.", LevelStandard},
		{TypeBug, "bug", "Something is broken.", LevelStandard},
		{TypeStory, "story", "A unit of value for a user.", LevelStandard},
		{TypeEpic, "epic", "A large body of work made of smaller issues.", LevelEpic},
		{TypeSubtask, "subtask", "A step within another issue.", LevelSubtask},
		{TypeInitiative, "initiative", "A goal several epics add up to.", LevelInitiative},
	}
	for i, t := range issueTypes {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO issue_type (org_id, name, description, icon, hierarchy_level, position)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			orgID, t.name, t.desc, t.icon, t.level, i,
		).Scan(&id)
		if err != nil {
			return nil, fmt.Errorf("create issue type %q: %w", t.name, err)
		}
		result.IssueTypeIDs[t.name] = id
	}

	workflowID, err := defaultWorkflow(ctx, tx, orgID, result.StatusIDs)
	if err != nil {
		return nil, err
	}
	result.WorkflowID = workflowID

	// The organization's own scheme: the answer for every project that does not
	// name one of its own.
	var schemeID uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO workflow_scheme (org_id, name, is_default)
		VALUES ($1, $2, true) RETURNING id`,
		orgID, DefaultSchemeName,
	).Scan(&schemeID)
	if err != nil {
		return nil, fmt.Errorf("create workflow scheme: %w", err)
	}
	result.WorkflowSchemeID = schemeID

	// A null issue type is the fallback, so every type gets this workflow until
	// somebody maps one explicitly.
	if _, err := tx.Exec(ctx, `
		INSERT INTO workflow_scheme_item (org_id, scheme_id, issue_type_id, workflow_id)
		VALUES ($1, $2, NULL, $3)`,
		orgID, schemeID, workflowID,
	); err != nil {
		return nil, fmt.Errorf("map workflow into scheme: %w", err)
	}

	linkTypes := []struct{ name, outward, inward string }{
		{LinkBlocks, "blocks", "is blocked by"},
		{LinkRelates, "relates to", "relates to"},
		{LinkDuplicates, "duplicates", "is duplicated by"},
	}
	for _, l := range linkTypes {
		if _, err := tx.Exec(ctx, `
			INSERT INTO issue_link_type (org_id, name, outward, inward)
			VALUES ($1, $2, $3, $4)`,
			orgID, l.name, l.outward, l.inward,
		); err != nil {
			return nil, fmt.Errorf("create link type %q: %w", l.name, err)
		}
	}

	// The automation account: the actor of every rule's act, an inactive
	// member that never signs in and is never listed among the people.
	if _, err := tx.Exec(ctx, `
		WITH made AS (
			INSERT INTO app_user (email, name, is_active)
			VALUES ('automation+' || $1::uuid::text || '@armature.invalid', 'Automation', false)
			RETURNING id
		), joined AS (
			INSERT INTO org_member (org_id, user_id, org_role) SELECT $1::uuid, id, 'member' FROM made
		)
		UPDATE org SET automation_user_id = (SELECT id FROM made) WHERE id = $1::uuid`, orgID); err != nil {
		return nil, fmt.Errorf("create the automation account: %w", err)
	}

	return result, nil
}

// defaultWorkflow builds the graph every new project starts with:
//
//	To Do ──Start──> In Progress ──Ready for review──> In Review ──Approve──> Done
//	  ^                   ^                                │                    │
//	  │                   └────────Request changes─────────┘                    │
//	  └───────────────────────────Reopen───────────────────────────────────────┘
//
// plus a global Close, because in practice something always needs closing from
// wherever it happens to be.
func defaultWorkflow(ctx context.Context, tx db.DBTX, orgID uuid.UUID, statuses map[string]uuid.UUID) (uuid.UUID, error) {
	var workflowID uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO workflow (org_id, name, description)
		VALUES ($1, $2, $3) RETURNING id`,
		orgID, DefaultWorkflowName, "To Do, In Progress, In Review and Done, with a review loop.",
	).Scan(&workflowID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("create workflow: %w", err)
	}

	steps := map[string]uuid.UUID{}
	order := []string{StatusToDo, StatusInProgress, StatusInReview, StatusDone}
	for i, name := range order {
		var stepID uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO workflow_step (org_id, workflow_id, status_id, is_initial, position)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`,
			orgID, workflowID, statuses[name], name == StatusToDo, i,
		).Scan(&stepID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("add step %q: %w", name, err)
		}
		steps[name] = stepID
	}

	type ruleSpec struct {
		kind     workflow.RuleKind
		ruleType string
		config   string
	}
	transitions := []struct {
		name  string
		from  string // empty means available from anywhere
		to    string
		rules []ruleSpec
	}{
		{
			name: "Start progress", from: StatusToDo, to: StatusInProgress,
			// Picking work up assigns it to you, which is what everybody means
			// by starting something.
			rules: []ruleSpec{{workflow.KindPostFunction, workflow.PostAssignToActor, ""}},
		},
		{name: "Stop progress", from: StatusInProgress, to: StatusToDo},
		{
			name: "Ready for review", from: StatusInProgress, to: StatusInReview,
			rules: []ruleSpec{{workflow.KindValidator, workflow.ValidatorAssigneeRequired, ""}},
		},
		{name: "Request changes", from: StatusInReview, to: StatusInProgress},
		{
			name: "Approve", from: StatusInReview, to: StatusDone,
			rules: []ruleSpec{{workflow.KindPostFunction, workflow.PostSetResolved, ""}},
		},
		{
			name: "Close", from: "", to: StatusDone,
			rules: []ruleSpec{{workflow.KindPostFunction, workflow.PostSetResolved, ""}},
		},
		{
			name: "Reopen", from: StatusDone, to: StatusToDo,
			rules: []ruleSpec{{workflow.KindPostFunction, workflow.PostClearResolved, ""}},
		},
	}

	for i, t := range transitions {
		var fromStep *uuid.UUID
		if t.from != "" {
			id := steps[t.from]
			fromStep = &id
		}

		var transitionID uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO workflow_transition (org_id, workflow_id, name, from_step_id, to_step_id, position)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			orgID, workflowID, t.name, fromStep, steps[t.to], i,
		).Scan(&transitionID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("add transition %q: %w", t.name, err)
		}

		for j, r := range t.rules {
			config := r.config
			if config == "" {
				config = "{}"
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO transition_rule (org_id, transition_id, kind, rule_type, config, position)
				VALUES ($1, $2, $3, $4, $5, $6)`,
				orgID, transitionID, string(r.kind), r.ruleType, config, j,
			); err != nil {
				return uuid.Nil, fmt.Errorf("add rule %q to %q: %w", r.ruleType, t.name, err)
			}
		}
	}

	return workflowID, nil
}
