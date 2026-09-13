package board

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/rank"
	"github.com/armature/armature/backend/internal/workflow"
)

// workflowTransition is the engine's transition type, named locally so the
// signatures below read as board code rather than workflow plumbing.
type workflowTransition = workflow.Transition

// MoveInput describes a card being dragged.
//
// The destination is given as neighbours rather than an index, because by the
// time the request arrives the board may have moved under the user. "Between
// these two cards" still means something when a third card has appeared; "at
// position 4" does not.
type MoveInput struct {
	IssueKey   string
	SwimlaneID uuid.UUID
	// AfterKey and BeforeKey are the cards the dropped card should land
	// between. Both empty means the end of the swimlane.
	AfterKey  string
	BeforeKey string
}

// MoveResult reports what the move actually did.
type MoveResult struct {
	Issue *issue.Issue `json:"issue"`
	// Transition names the workflow transition that was taken, empty when the
	// card only moved within its own swimlane.
	Transition string `json:"transition,omitempty"`
	Rank       string `json:"rank"`
}

// Move drops a card into a swimlane, at a position within it.
//
// A swimlane is a set of statuses, so moving between swimlanes is a change of
// status, and a change of status is a workflow transition. This does not write
// the status field directly: it finds a transition the workflow actually offers
// and takes it, so conditions, validators and post-functions all apply exactly
// as they would from the issue view. A drag the workflow forbids is refused.
func (s *Service) Move(ctx context.Context, projectKey string, in MoveInput, actor issue.Actor) (*MoveResult, db.LSN, error) {
	current, err := s.issues.ByKey(ctx, in.IssueKey)
	if err != nil {
		return nil, 0, err
	}
	// The board is the path's project; a card and a lane from elsewhere are
	// not on it, however the request names them.
	if !strings.EqualFold(current.ProjectKey, strings.TrimSpace(projectKey)) {
		return nil, 0, ErrNotOnThisBoard
	}
	if err := s.laneIsHere(ctx, projectKey, in.SwimlaneID); err != nil {
		return nil, 0, err
	}

	target, err := s.swimlaneStatuses(ctx, in.SwimlaneID)
	if err != nil {
		return nil, 0, err
	}

	result := &MoveResult{Issue: current}

	// Moving within the swimlane the card is already in is a reorder, not a
	// transition, so the workflow is not involved at all.
	if !containsStatus(target.statuses, current.Status.ID) {
		moved, transitionName, err := s.transitionInto(ctx, current, target, actor)
		if err != nil {
			return nil, 0, err
		}
		result.Issue = moved
		result.Transition = transitionName
	}

	newRank, lsn, err := s.reorder(ctx, result.Issue.ID, in)
	if err != nil {
		return nil, 0, err
	}
	result.Rank = newRank

	return result, lsn, nil
}

// laneIsHere refuses a swimlane belonging to another project's board.
func (s *Service) laneIsHere(ctx context.Context, projectKey string, swimlaneID uuid.UUID) error {
	return s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var here bool
		err := tx.QueryRow(ctx, `
			SELECT upper(p.key) = upper($2)
			FROM board_swimlane l JOIN board b ON b.id = l.board_id JOIN project p ON p.id = b.project_id
			WHERE l.id = $1`, swimlaneID, strings.TrimSpace(projectKey)).Scan(&here)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSwimlaneNotFound
		}
		if err != nil {
			return err
		}
		if !here {
			return ErrNotOnThisBoard
		}
		return nil
	})
}

// swimlaneTarget is a swimlane reduced to what a move needs.
type swimlaneTarget struct {
	id       uuid.UUID
	name     string
	statuses []statusRef
}

type statusRef struct {
	id   uuid.UUID
	name string
}

func (s *Service) swimlaneStatuses(ctx context.Context, swimlaneID uuid.UUID) (*swimlaneTarget, error) {
	target := &swimlaneTarget{id: swimlaneID}

	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := tx.QueryRow(ctx, `SELECT name FROM board_swimlane WHERE id = $1`, swimlaneID).
			Scan(&target.name); errors.Is(err, pgx.ErrNoRows) {
			return ErrSwimlaneNotFound
		} else if err != nil {
			return err
		}

		rows, err := tx.Query(ctx, `
			SELECT st.id, st.name
			FROM board_swimlane_status ls
			JOIN issue_status st ON st.id = ls.status_id
			WHERE ls.swimlane_id = $1
			ORDER BY st.position`, swimlaneID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ref statusRef
			if err := rows.Scan(&ref.id, &ref.name); err != nil {
				return err
			}
			target.statuses = append(target.statuses, ref)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}

	if len(target.statuses) == 0 {
		return nil, fmt.Errorf("%w: %q has no states, so nothing can be moved into it", ErrNoTransition, target.name)
	}
	return target, nil
}

// transitionInto moves an issue into one of a swimlane's statuses by taking a
// transition the workflow offers.
//
// Where a swimlane holds several statuses, the first reachable one wins, in the
// swimlane's own status order. That makes a merged lane such as "In Progress
// and In Review" land on the earlier of the two, which is what dragging a card
// into the start of that lane should mean.
func (s *Service) transitionInto(ctx context.Context, current *issue.Issue, target *swimlaneTarget, actor issue.Actor) (*issue.Issue, string, error) {
	available, err := s.issues.Transitions(ctx, current.Key, actor)
	if err != nil {
		return nil, "", err
	}

	// The transitions are named by their destination step, so resolve each to
	// the status it actually lands on.
	destinations, err := s.transitionDestinations(ctx, current, available)
	if err != nil {
		return nil, "", err
	}

	for _, status := range target.statuses {
		for _, t := range available {
			if destinations[t.ID] != status.id {
				continue
			}
			moved, _, err := s.issues.Transition(ctx, current.Key, issue.TransitionInput{
				TransitionID: t.ID,
			}, actor)
			if err != nil {
				// A validator refusing the move is worth reporting as itself:
				// "this needs an assignee" is actionable, "cannot move" is not.
				return nil, "", err
			}
			return moved, t.Name, nil
		}
	}

	return nil, "", fmt.Errorf("%w: there is no transition from %q to %s",
		ErrNoTransition, current.Status.Name, describeStatuses(target.statuses))
}

// transitionDestinations maps each available transition to the status it leads
// to, by reading the workflow's steps.
func (s *Service) transitionDestinations(ctx context.Context, current *issue.Issue, available []workflowTransition) (map[uuid.UUID]uuid.UUID, error) {
	out := map[uuid.UUID]uuid.UUID{}
	if len(available) == 0 {
		return out, nil
	}

	ids := make([]uuid.UUID, 0, len(available))
	for _, t := range available {
		ids = append(ids, t.ID)
	}

	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT t.id, st.status_id
			FROM workflow_transition t
			JOIN workflow_step st ON st.id = t.to_step_id
			WHERE t.id = ANY($1)`, ids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var transitionID, statusID uuid.UUID
			if err := rows.Scan(&transitionID, &statusID); err != nil {
				return err
			}
			out[transitionID] = statusID
		}
		return rows.Err()
	})
	return out, err
}

// reorder writes the card's new rank, derived from the neighbours it was
// dropped between.
func (s *Service) reorder(ctx context.Context, issueID uuid.UUID, in MoveInput) (string, db.LSN, error) {
	var newRank string

	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		after, err := rankOf(ctx, tx, in.AfterKey)
		if err != nil {
			return err
		}
		before, err := rankOf(ctx, tx, in.BeforeKey)
		if err != nil {
			return err
		}

		// Dropping at the end of a swimlane gives no neighbours at all, so fall
		// back to the last rank in the whole project.
		if after == "" && before == "" {
			if err := tx.QueryRow(ctx, `
				SELECT COALESCE(max(rank), '')
				FROM issue
				WHERE project_id = (SELECT project_id FROM issue WHERE id = $1) AND id <> $1`,
				issueID).Scan(&after); err != nil {
				return err
			}
		}

		generated, err := rank.Between(after, before)
		if err != nil {
			return fmt.Errorf("place the card: %w", err)
		}
		newRank = generated

		_, err = tx.Exec(ctx, `UPDATE issue SET rank = $2 WHERE id = $1`, issueID, newRank)
		return err
	})
	if err != nil {
		return "", 0, err
	}
	return newRank, lsn, nil
}

// rankOf reads one issue's rank by key, or empty when no key was given.
func rankOf(ctx context.Context, tx db.DBTX, key string) (string, error) {
	if key == "" {
		return "", nil
	}
	projectKey, num, err := issue.ParseKey(key)
	if err != nil {
		return "", err
	}
	var value string
	err = tx.QueryRow(ctx, `
		SELECT i.rank FROM issue i
		JOIN project p ON p.id = i.project_id
		WHERE p.key = $1 AND i.key_num = $2`, projectKey, num).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		// A neighbour that has since moved or been deleted is not an error;
		// the card simply lands without that side constrained.
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return value, nil
}

func containsStatus(statuses []statusRef, id uuid.UUID) bool {
	for _, s := range statuses {
		if s.id == id {
			return true
		}
	}
	return false
}

func describeStatuses(statuses []statusRef) string {
	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = s.name
	}
	if len(names) == 1 {
		return names[0]
	}
	return "any of " + joinWithOr(names)
}

func joinWithOr(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		return names[0] + " or " + joinWithOr(names[1:])
	}
}
