package board

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/workflow"
)

// Follower grows a lane on every board of a project following a workflow that
// gains a status, inside the save, so no card can land where no lane is.
type Follower struct{}

// WorkflowSaved appends a lane for each added status to each board that lacks
// one, in every project that follows the workflow; only those can reach it.
func (Follower) WorkflowSaved(ctx context.Context, tx db.DBTX, workflowID uuid.UUID, added []workflow.Status) error {
	if len(added) == 0 {
		return nil
	}
	projects, err := workflow.NewStore().ProjectsFollowing(ctx, tx, workflowID)
	if err != nil {
		return err
	}
	for _, projectID := range projects {
		for _, status := range added {
			if err := appendLaneToBoards(ctx, tx, projectID, status); err != nil {
				return err
			}
		}
	}
	return nil
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
			INSERT INTO board_swimlane (board_id, name, position)
			VALUES ($1, $2, (SELECT COALESCE(max(position) + 1, 0) FROM board_swimlane WHERE board_id = $1))
			RETURNING id`, boardID, status.Name).Scan(&laneID); err != nil {
			return fmt.Errorf("add a lane for %q: %w", status.Name, err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO board_swimlane_status (board_id, swimlane_id, status_id)
			VALUES ($1, $2, $3)`, boardID, laneID, status.ID); err != nil {
			return fmt.Errorf("map %q into its new lane: %w", status.Name, err)
		}
	}
	return nil
}
