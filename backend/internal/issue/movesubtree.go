package issue

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/workflow"
)

// moveTarget is the project a move lands in.
type moveTarget struct {
	id  uuid.UUID
	key string
}

// subtreeOf reads and locks everything under an issue at any depth, parents
// before children, so each row's parent has already moved when it is written.
func subtreeOf(ctx context.Context, tx db.DBTX, rootID uuid.UUID) ([]Issue, error) {
	return readIssues(ctx, tx, `
		WITH RECURSIVE subtree (id, depth) AS (
		    SELECT id, 1 FROM issue WHERE parent_id = $1
		    UNION ALL
		    SELECT c.id, t.depth + 1 FROM issue c JOIN subtree t ON c.parent_id = t.id
		)`+selectIssue+`
		JOIN subtree st ON st.id = i.id
		ORDER BY st.depth, i.key_num
		FOR UPDATE OF i`, rootID)
}

// checkDescendants refuses a move when any issue under the one being moved has
// no workflow in the target, or a status its workflow there does not have.
func (s *Service) checkDescendants(ctx context.Context, tx db.DBTX, root *Issue, subtree []Issue, target moveTarget, statuses map[uuid.UUID]uuid.UUID) (map[uuid.UUID]workflow.Status, error) {
	flows := map[uuid.UUID]*workflow.Workflow{}
	landing := map[uuid.UUID]workflow.Status{}
	for _, d := range subtree {
		wf, ok := flows[d.Type.ID]
		if !ok {
			var err error
			wf, err = s.workflows.ForIssueType(ctx, tx, target.id, d.Type.ID)
			if errors.Is(err, workflow.ErrNotFound) {
				return nil, fmt.Errorf("%s cannot move with %s: %s has no workflow for %s. Map one in its workflow scheme, then move %s again.",
					d.Key, root.Key, target.key, article(d.Type.Name), root.Key)
			}
			if err != nil {
				return nil, err
			}
			flows[d.Type.ID] = wf
		}
		want := d.Status.ID
		if to, mapped := statuses[want]; mapped {
			want = to
		}
		found := false
		for _, step := range wf.Steps {
			if step.Status.ID == want {
				landing[d.ID], found = step.Status, true
			}
		}
		if !found && statuses != nil {
			return nil, fmt.Errorf("%s cannot move with %s: %s has no %s status for %s; map %s to one of its statuses",
				d.Key, root.Key, target.key, d.Status.Name, article(d.Type.Name), d.Status.Name)
		}
		if !found {
			return nil, fmt.Errorf("%s cannot move with %s: %s has no %s status for %s. Move %s to a status it has there, then move %s again.",
				d.Key, root.Key, target.key, d.Status.Name, article(d.Type.Name), d.Key, root.Key)
		}
	}
	return landing, nil
}

// moveRow puts one issue in the target under its next key and writes the
// history entry; what belonged to the old project is cleared and named.
func (s *Service) moveRow(ctx context.Context, tx db.DBTX, before *Issue, target moveTarget, statusID uuid.UUID, statusName string, actor Actor) ([]Change, string, error) {
	var keyNum int64
	if err := tx.QueryRow(ctx, `UPDATE project SET issue_seq = issue_seq + 1 WHERE id = $1 RETURNING issue_seq`, target.id).Scan(&keyNum); err != nil {
		return nil, "", fmt.Errorf("allocate issue key: %w", err)
	}

	// The rows that tie the issue to its old project go first, so the
	// same-project guards on the issue row do not fire on the update.
	for _, table := range []string{"issue_version", "issue_component"} {
		if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE issue_id = $1`, before.ID); err != nil {
			return nil, "", err
		}
	}
	droppedValues, err := dropProjectFieldValues(ctx, tx, before.ID)
	if err != nil {
		return nil, "", err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE issue SET project_id = $2, key_num = $3, status_id = $4, sprint_id = NULL, team_id = NULL, milestone_id = NULL,
		       company_id = NULL, shared_with_company = false
		WHERE id = $1`, before.ID, target.id, keyNum, statusID); err != nil {
		return nil, "", fmt.Errorf("move %s: %w", before.Key, err)
	}
	if err := keepOldKey(ctx, tx, before.ID, before.Key); err != nil {
		return nil, "", err
	}

	changes := []Change{{Field: "project", From: before.ProjectKey, To: target.key}, {Field: "key", From: before.Key, To: FormatKey(target.key, keyNum)}}
	if statusID != before.Status.ID {
		changes = append(changes, Change{Field: "status", From: before.Status.Name, To: statusName})
	}
	for _, cleared := range []struct {
		field string
		had   bool
		from  string
	}{
		{"sprint", before.Sprint != nil, nameOf(before.Sprint)},
		{"team", before.Team != nil, nameOfTeam(before.Team)},
		{"milestone", before.Milestone != nil, nameOfMilestone(before.Milestone)},
		{"company", before.Company != nil, nameOfCompany(before.Company)},
		{"fixVersion", len(before.FixVersions) > 0, versionNames(before.FixVersions)},
		{"affectsVersion", len(before.AffectsVersions) > 0, versionNames(before.AffectsVersions)},
		{"component", len(before.Components) > 0, componentNames(before.Components)},
	} {
		if cleared.had {
			changes = append(changes, Change{Field: cleared.field, From: cleared.from, To: ""})
		}
	}
	changes = append(changes, droppedValues...)
	if err := s.recordHistory(ctx, tx, before.ID, actor.UserID, changes); err != nil {
		return nil, "", err
	}
	return changes, FormatKey(target.key, keyNum), nil
}
