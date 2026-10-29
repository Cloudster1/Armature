package issue

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/workflow"
)

// TypeChangeTransition names the move in the issue.transitioned event when a
// type change, not a workflow transition, put the issue in another status.
const TypeChangeTransition = "Type change"

// applyType changes an issue's type, which moves it in the hierarchy and can
// also change which workflow it follows. moved reports a status change.
func (s *Service) applyType(ctx context.Context, tx db.DBTX, before *Issue, typeID uuid.UUID) (changes []Change, moved bool, err error) {
	if typeID == uuid.Nil {
		return nil, false, ErrNoSuchType
	}
	t, err := ProjectType(ctx, tx, before.ProjectID, before.ProjectKey, typeID, "")
	if err != nil {
		return nil, false, err
	}
	name, level := t.Name, t.Level
	if err := s.checkTypeFitsTheTree(ctx, tx, before, name, level); err != nil {
		return nil, false, err
	}

	if _, err := tx.Exec(ctx, `UPDATE issue SET issue_type_id = $2 WHERE id = $1`, before.ID, typeID); err != nil {
		return nil, false, err
	}
	changes = []Change{{Field: "type", From: before.Type.Name, To: name}}

	// A workflow without the issue's status cannot describe where it is, so it
	// starts over in that workflow's first status.
	wf, err := s.workflows.ForIssueType(ctx, tx, before.ProjectID, typeID)
	if err != nil {
		return nil, false, err
	}
	if _, inWorkflow := wf.StepByStatus(before.Status.ID); inWorkflow {
		return changes, false, nil
	}
	initial, err := wf.InitialStep()
	if err != nil {
		return nil, false, err
	}
	toDone := initial.Status.Category == workflow.CategoryDone
	// Reports and NQL read resolved_at, so it follows the status it lands in;
	// an issue that was already resolved keeps the moment it was.
	if _, err := tx.Exec(ctx, `
		UPDATE issue SET status_id = $2,
		       resolved_at = CASE WHEN $3 THEN coalesce(resolved_at, now()) END
		WHERE id = $1`, before.ID, initial.Status.ID, toDone); err != nil {
		return nil, false, err
	}
	changes = append(changes, Change{Field: "status", From: before.Status.Name, To: initial.Status.Name})
	wasResolved := before.ResolvedAt != nil
	switch {
	case toDone && !wasResolved:
		changes = append(changes, Change{Field: "resolution", To: "Done"})
	case !toDone && wasResolved:
		changes = append(changes, Change{Field: "resolution", From: "Done", To: "Unresolved"})
	}
	return changes, true, nil
}

// checkTypeFitsTheTree refuses a type change that would strand the issue's
// parent or its children, and says which so the user knows what to fix first.
func (s *Service) checkTypeFitsTheTree(ctx context.Context, tx db.DBTX, before *Issue, name string, level int) error {
	if before.Parent == nil {
		if NeedsParent(level) {
			return levelError{ErrNeedsParent, article(name) + " needs a parent issue"}
		}
	} else if !CanParent(before.Parent.Type.Level, level) {
		return levelError{ErrParentLevel, fmt.Sprintf("%s is %s, which cannot be the parent of %s",
			before.Parent.Key, article(before.Parent.Type.Name), article(name))}
	}

	var stranded int
	err := tx.QueryRow(ctx, `
		SELECT count(*) FROM issue c
		JOIN issue_type ct ON ct.id = c.issue_type_id
		WHERE c.parent_id = $1 AND ct.hierarchy_level <> $2`, before.ID, level-1).Scan(&stranded)
	if err != nil {
		return err
	}
	if stranded > 0 {
		return levelError{ErrChildrenInTheWay,
			fmt.Sprintf("%d of this issue's children cannot sit under %s", stranded, article(name))}
	}
	return nil
}
