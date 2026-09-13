package workflow

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// ownSchemeNames is how many names are tried for a project's scheme before
// giving up; the key makes the second unique, so the rest are a safety net.
const ownSchemeNames = 4

// EnsureOwnScheme returns a scheme the project may edit without reaching any
// other project, copying what it follows today when the current one is shared.
func EnsureOwnScheme(ctx context.Context, tx db.DBTX, projectID uuid.UUID, projectName, projectKey string, current *uuid.UUID) (uuid.UUID, bool, error) {
	if current != nil {
		var isDefault bool
		var others int
		err := tx.QueryRow(ctx, `
			SELECT s.is_default, (SELECT count(*) FROM project WHERE workflow_scheme_id = s.id AND id <> $2)
			FROM workflow_scheme s WHERE s.id = $1`, *current, projectID).Scan(&isDefault, &others)
		if err != nil {
			return uuid.Nil, false, fmt.Errorf("look at the project's scheme: %w", err)
		}
		if !isDefault && others == 0 {
			return *current, false, nil
		}
	}

	name, err := freeSchemeName(ctx, tx, projectName, projectKey)
	if err != nil {
		return uuid.Nil, false, err
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO workflow_scheme (org_id, name) VALUES (current_org_id(), $1) RETURNING id`, name,
	).Scan(&id); err != nil {
		return uuid.Nil, false, fmt.Errorf("create scheme %q: %w", name, err)
	}
	if current != nil {
		// The project followed a shared scheme until now; it keeps behaving the
		// same until somebody changes a row, so the shared items are copied.
		if _, err := tx.Exec(ctx, `
			INSERT INTO workflow_scheme_item (org_id, scheme_id, issue_type_id, workflow_id)
			SELECT current_org_id(), $1, issue_type_id, workflow_id
			FROM workflow_scheme_item WHERE scheme_id = $2`, id, *current); err != nil {
			return uuid.Nil, false, fmt.Errorf("copy the shared scheme: %w", err)
		}
	}
	return id, true, events.EmitInTenant(ctx, tx, "workflow.scheme.created", map[string]any{
		"schemeId": id, "name": name, "projectId": projectID,
	})
}

func freeSchemeName(ctx context.Context, tx db.DBTX, projectName, projectKey string) (string, error) {
	candidates := []string{
		fmt.Sprintf("%s scheme", projectName),
		fmt.Sprintf("%s scheme (%s)", projectName, projectKey),
	}
	for i := 3; i <= ownSchemeNames; i++ {
		candidates = append(candidates, fmt.Sprintf("%s scheme (%s %d)", projectName, projectKey, i))
	}
	for _, name := range candidates {
		var taken bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM workflow_scheme WHERE lower(name) = lower($1))`, name).Scan(&taken); err != nil {
			return "", fmt.Errorf("look for scheme %q: %w", name, err)
		}
		if !taken {
			return name, nil
		}
	}
	return "", invalid("rename or delete the schemes already named after this project under Workflows, then try again")
}

// SetSchemeItem decides one issue type in a scheme: a workflow, or nil to let
// the type fall through to the organization again.
func SetSchemeItem(ctx context.Context, tx db.DBTX, schemeID, issueTypeID uuid.UUID, workflowID *uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		DELETE FROM workflow_scheme_item WHERE scheme_id = $1 AND issue_type_id = $2`, schemeID, issueTypeID); err != nil {
		return fmt.Errorf("unmap the issue type: %w", err)
	}
	if workflowID == nil {
		return nil
	}
	// Under row level security a workflow of another organization is simply
	// absent, so the check is made here where the answer can be a sentence.
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workflow WHERE id = $1)`, *workflowID).Scan(&exists); err != nil {
		return fmt.Errorf("look for the workflow: %w", err)
	}
	if !exists {
		return invalid("choose one of this organization's workflows.")
	}
	return writeSchemeItems(ctx, tx, schemeID, []SchemeItemInput{{IssueTypeID: &issueTypeID, WorkflowID: *workflowID}})
}

// SchemeIsEmpty says whether a scheme decides nothing any more.
func SchemeIsEmpty(ctx context.Context, tx db.DBTX, schemeID uuid.UUID) (bool, error) {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM workflow_scheme_item WHERE scheme_id = $1`, schemeID).Scan(&count); err != nil {
		return false, fmt.Errorf("count the scheme's items: %w", err)
	}
	return count == 0, nil
}
