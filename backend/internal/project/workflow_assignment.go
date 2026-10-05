package project

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/workflow"
)

// SetWorkflowAssignment decides which workflow one issue type follows in a
// project, or hands that type back to the organization with a nil workflow.
func (s *Service) SetWorkflowAssignment(ctx context.Context, key string, issueTypeID uuid.UUID, workflowID *uuid.UUID, actor uuid.UUID) (*Project, db.LSN, error) {
	var p *Project
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		current, err := scanProject(tx.QueryRow(ctx, selectProject+` WHERE p.key = $1 AND p.archived_at IS NULL`, NormalizeKey(key)))
		if err != nil {
			return err
		}
		var typeExists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM issue_type WHERE id = $1)`, issueTypeID).Scan(&typeExists); err != nil {
			return err
		}
		if !typeExists {
			return fmt.Errorf("%w: this organization has no such issue type.", workflow.ErrInvalid)
		}

		// The scheme is bookkeeping: a project needs one of its own to disagree in.
		schemeID, created, err := workflow.EnsureOwnScheme(ctx, tx, current.ID, current.Name, current.Key, current.WorkflowSchemeID)
		if err != nil {
			return err
		}
		if err := workflow.SetSchemeItem(ctx, tx, schemeID, issueTypeID, workflowID); err != nil {
			return err
		}

		next := &schemeID
		empty, err := workflow.SchemeIsEmpty(ctx, tx, schemeID)
		if err != nil {
			return err
		}
		if empty {
			// A project that disagrees about nothing follows the organization
			// again, and the scheme that said nothing goes with it.
			next = nil
			if _, err := tx.Exec(ctx, `UPDATE project SET workflow_scheme_id = NULL WHERE id = $1`, current.ID); err != nil {
				return fmt.Errorf("hand the decision back: %w", err)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM workflow_scheme WHERE id = $1`, schemeID); err != nil {
				return fmt.Errorf("drop the empty scheme: %w", err)
			}
		} else if created {
			if _, err := tx.Exec(ctx, `UPDATE project SET workflow_scheme_id = $2 WHERE id = $1`, current.ID, schemeID); err != nil {
				return fmt.Errorf("set the project's workflow scheme: %w", err)
			}
		}

		p, err = scanProject(tx.QueryRow(ctx, selectProject+` WHERE p.key = $1`, current.Key))
		if err != nil {
			return err
		}
		if err := events.EmitInTenant(ctx, tx, "project.workflow_assignment_changed", map[string]any{
			"projectId": p.ID, "key": p.Key, "issueTypeId": issueTypeID, "workflowId": workflowID, "schemeId": next, "actorId": actor,
		}); err != nil {
			return err
		}
		if (current.WorkflowSchemeID == nil) != (next == nil) || (next != nil && current.WorkflowSchemeID != nil && *next != *current.WorkflowSchemeID) {
			return events.EmitInTenant(ctx, tx, "project.workflow_scheme_changed", map[string]any{
				"projectId": p.ID, "key": p.Key, "schemeId": next, "actorId": actor,
			})
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return p, lsn, nil
}
