package board

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/workflow"
)

// Follower keeps boards in step with the workflows their projects follow: when
// a workflow gains a status, every board of a project that now reaches that
// status grows a lane for it, so no card can land where no lane is. It runs in
// the workflow's own transaction and costs a read per project and a write per
// board, which an administrator saving a workflow can afford.
type Follower struct{}

// WorkflowSaved appends a lane for each added status to each board that lacks
// one, in every project whose statuses now include it.
func (Follower) WorkflowSaved(ctx context.Context, tx db.DBTX, workflowID uuid.UUID, added []workflow.Status) error {
	rows, err := tx.Query(ctx, `SELECT id FROM project WHERE archived_at IS NULL ORDER BY key`)
	if err != nil {
		return fmt.Errorf("read projects: %w", err)
	}
	var projects []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		projects = append(projects, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	store := workflow.NewStore()
	for _, projectID := range projects {
		reachable, err := store.ProjectStatuses(ctx, tx, projectID)
		if err != nil {
			return err
		}
		for _, status := range added {
			if !contains(reachable, status.ID) {
				continue
			}
			if err := appendLaneToBoards(ctx, tx, projectID, status); err != nil {
				return err
			}
		}
	}
	return nil
}

func contains(statuses []workflow.Status, id uuid.UUID) bool {
	for _, s := range statuses {
		if s.ID == id {
			return true
		}
	}
	return false
}

// appendLaneToBoards gives each of the project's boards without a lane for the
// status one at the end, named after it.
func appendLaneToBoards(ctx context.Context, tx db.DBTX, projectID uuid.UUID, status workflow.Status) error {
	rows, err := tx.Query(ctx, `
		SELECT b.id FROM board b
		WHERE b.project_id = $1
		  AND NOT EXISTS (SELECT 1 FROM board_swimlane_status ls WHERE ls.board_id = b.id AND ls.status_id = $2)
		ORDER BY b.created_at`, projectID, status.ID)
	if err != nil {
		return fmt.Errorf("find boards without %q: %w", status.Name, err)
	}
	var boards []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		boards = append(boards, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, boardID := range boards {
		var laneID uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO board_swimlane (org_id, board_id, name, position)
			VALUES (current_org_id(), $1, $2, (SELECT COALESCE(max(position) + 1, 0) FROM board_swimlane WHERE board_id = $1))
			RETURNING id`, boardID, status.Name).Scan(&laneID); err != nil {
			return fmt.Errorf("add a lane for %q: %w", status.Name, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO board_swimlane_status (org_id, board_id, swimlane_id, status_id)
			VALUES (current_org_id(), $1, $2, $3)`, boardID, laneID, status.ID); err != nil {
			return fmt.Errorf("map %q into its new lane: %w", status.Name, err)
		}
	}
	return nil
}
