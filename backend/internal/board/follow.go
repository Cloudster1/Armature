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
	for _, status := range added {
		if err := appendLaneToBoards(ctx, tx, projects, status); err != nil {
			return err
		}
	}
	return nil
}

// appendLaneToBoards gives each board of these projects that has no lane for
// the status one at the end, named after it, in two statements for all of them.
func appendLaneToBoards(ctx context.Context, tx db.DBTX, projects []uuid.UUID, status workflow.Status) error {
	if len(projects) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		WITH lacking AS (
		    SELECT b.id FROM board b
		    WHERE b.project_id = ANY($1)
		      AND NOT EXISTS (SELECT 1 FROM board_swimlane_status ls WHERE ls.board_id = b.id AND ls.status_id = $2)
		), lanes AS (
		    INSERT INTO board_swimlane (board_id, name, position)
		    SELECT l.id, $3, (SELECT COALESCE(max(position) + 1, 0) FROM board_swimlane WHERE board_id = l.id)
		    FROM lacking l
		    RETURNING id, board_id
		)
		INSERT INTO board_swimlane_status (board_id, swimlane_id, status_id)
		SELECT board_id, id, $2 FROM lanes`, projects, status.ID, status.Name); err != nil {
		return fmt.Errorf("add a lane for %q: %w", status.Name, err)
	}
	return nil
}
