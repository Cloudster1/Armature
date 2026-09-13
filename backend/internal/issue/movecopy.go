package issue

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/project"
)

// CloneOptions say what comes along with a copy besides the fields.
type CloneOptions struct {
	Links    bool `json:"links"`
	Subtasks bool `json:"subtasks"`
	// Summary, when given, names the copy; empty keeps the original's.
	Summary string `json:"summary,omitempty"`
}

// Clone makes a new issue from an existing one: its fields, labels, versions,
// components and custom values, and on request its links and its subtasks.
// History starts fresh; the copy's first entry names where it came from.
func (s *Service) Clone(ctx context.Context, key string, opts CloneOptions, actor Actor) (*Issue, db.LSN, error) {
	source, err := s.ByKey(ctx, key)
	if err != nil {
		return nil, 0, err
	}
	summary := strings.TrimSpace(opts.Summary)
	if summary == "" {
		summary = source.Summary
	}
	in := CreateInput{
		ProjectKey: source.ProjectKey, TypeID: source.Type.ID, Summary: summary, Description: source.Description,
		Priority: source.Priority, ParentKey: source.ParentKey, StartDate: source.StartDate, DueDate: source.DueDate,
		Estimate: source.Estimate, TeamID: source.TeamID,
	}
	if source.Assignee != nil {
		id := source.Assignee.ID
		in.AssigneeID = &id
	}
	for _, c := range source.Components {
		in.ComponentIDs = append(in.ComponentIDs, c.ID)
	}
	made, lsn, err := s.Create(ctx, in, actor)
	if err != nil {
		return nil, 0, err
	}
	// What Create does not take: labels, versions, milestone, the note of
	// origin, then links and subtasks when asked. Each is its own write, so a
	// refusal on one leaves a copy that exists and says what it lacks.
	fix, affects := ids(source.FixVersions), ids(source.AffectsVersions)
	if len(fix)+len(affects) > 0 {
		if _, lsn, err = s.SetVersions(ctx, made.Key, fix, affects, actor); err != nil {
			return made, lsn, err
		}
	}
	if len(source.Labels) > 0 {
		lsn, err = s.copyLabels(ctx, source.ID, made.ID)
		if err != nil {
			return made, lsn, err
		}
	}
	if source.MilestoneID != nil {
		if _, lsn, err = s.SetMilestone(ctx, made.Key, source.MilestoneID, actor); err != nil {
			return made, lsn, err
		}
	}
	lsn, err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO issue_field_value (org_id, issue_id, field_id, value)
			SELECT org_id, $2, field_id, value FROM issue_field_value WHERE issue_id = $1`, source.ID, made.ID); err != nil {
			return err
		}
		return s.recordHistory(ctx, tx, made.ID, actor.UserID, []Change{{Field: "clonedFrom", To: source.Key}})
	})
	if err != nil {
		return made, lsn, err
	}
	if opts.Links {
		links, err := s.Links(ctx, source.Key)
		if err != nil {
			return made, lsn, err
		}
		for _, l := range links {
			if l.Direction != "outward" {
				continue
			}
			if _, lsn, err = s.AddLink(ctx, made.Key, LinkInput{TypeID: l.TypeID, TargetKey: l.Issue.Key}, actor); err != nil {
				return made, lsn, err
			}
		}
	}
	if opts.Subtasks {
		children, err := s.Children(ctx, source.Key)
		if err != nil {
			return made, lsn, err
		}
		for _, child := range children {
			if !child.Type.IsSubtask {
				continue
			}
			_, lsn, err = s.Create(ctx, CreateInput{ProjectKey: source.ProjectKey, TypeID: child.Type.ID, Summary: child.Summary, Description: child.Description, Priority: child.Priority, ParentKey: made.Key}, actor)
			if err != nil {
				return made, lsn, err
			}
		}
	}
	updated, err := s.ByKey(ctx, made.Key)
	if err != nil {
		return made, lsn, err
	}
	return updated, lsn, nil
}

func (s *Service) copyLabels(ctx context.Context, from, to uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO issue_label (org_id, issue_id, label_id)
			SELECT org_id, $2, label_id FROM issue_label WHERE issue_id = $1 ON CONFLICT DO NOTHING`, from, to)
		return err
	})
}

func ids(refs []VersionRef) []uuid.UUID {
	var out []uuid.UUID
	for _, r := range refs {
		out = append(out, r.ID)
	}
	return out
}

// MoveInput says where an issue goes and, when the target workflow does not
// know its status, which status it takes there.
type MoveInput struct {
	ProjectKey string
	// StatusID is the status in the target project's workflow; nil keeps the
	// current one when that workflow has it, and is refused otherwise.
	StatusID *uuid.UUID
}

// ErrMoveNeedsStatus is returned when the target workflow does not have the
// issue's status and none was chosen.
var ErrMoveNeedsStatus = errors.New("the target workflow does not have this status; choose one")

// Move puts an issue in another project. It keeps its id, its history, its
// comments and its files; it gets the project's next key and remembers the
// old one, so the address it had still finds it. What belongs to the old
// project, sprint, team, milestone, versions and components, is cleared and
// the history says so. Subtasks come along with their parent.
func (s *Service) Move(ctx context.Context, key string, in MoveInput, actor Actor) (*Issue, db.LSN, error) {
	var moved *Issue
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		projectKey, num, err := ParseKey(key)
		if err != nil {
			return err
		}
		before, err := scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE p.key = $1 AND i.key_num = $2 FOR UPDATE OF i`, projectKey, num))
		if err != nil {
			return err
		}
		if before.Type.IsSubtask {
			return errors.New("a subtask moves with its parent; move the parent")
		}
		if before.ParentID != nil {
			return errors.New("an issue under a parent stays with it; take it out of the hierarchy first")
		}
		var (
			targetID  uuid.UUID
			targetKey string
			archived  bool
		)
		err = tx.QueryRow(ctx, `SELECT id, key, archived_at IS NOT NULL FROM project WHERE key = $1`, project.NormalizeKey(in.ProjectKey)).Scan(&targetID, &targetKey, &archived)
		if errors.Is(err, pgx.ErrNoRows) {
			return project.ErrNotFound
		}
		if err != nil {
			return err
		}
		if archived {
			return project.ErrArchived
		}
		if targetID == before.ProjectID {
			return errors.New("the issue is already in that project")
		}

		// The status it lands in: its own when the target workflow has it,
		// else the one chosen, which has to be in that workflow.
		wf, err := s.workflows.ForIssueType(ctx, tx, targetID, before.Type.ID)
		if err != nil {
			return err
		}
		statusID := before.Status.ID
		if in.StatusID != nil {
			statusID = *in.StatusID
		}
		has := false
		var statusName string
		for _, step := range wf.Steps {
			if step.Status.ID == statusID {
				has, statusName = true, step.Status.Name
			}
		}
		if !has {
			return ErrMoveNeedsStatus
		}

		var keyNum int64
		if err := tx.QueryRow(ctx, `UPDATE project SET issue_seq = issue_seq + 1 WHERE id = $1 RETURNING issue_seq`, targetID).Scan(&keyNum); err != nil {
			return fmt.Errorf("allocate issue key: %w", err)
		}
		newKey := FormatKey(targetKey, keyNum)

		// The rows that tie the issue to its old project go first, so the
		// same-project guards on the issue row do not fire on the update.
		for _, table := range []string{"issue_version", "issue_component", "issue_field_value"} {
			if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE issue_id = $1`, before.ID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE issue SET project_id = $2, key_num = $3, status_id = $4, sprint_id = NULL, team_id = NULL, milestone_id = NULL, moved_from = $5
			WHERE id = $1`, before.ID, targetID, keyNum, statusID, before.Key); err != nil {
			return fmt.Errorf("move issue: %w", err)
		}
		// Subtasks follow, each with its own new key.
		children, err := tx.Query(ctx, `SELECT c.id, c.key_num FROM issue c WHERE c.parent_id = $1 ORDER BY c.key_num`, before.ID)
		if err != nil {
			return err
		}
		type child struct {
			id  uuid.UUID
			num int64
		}
		var kids []child
		for children.Next() {
			var c child
			if err := children.Scan(&c.id, &c.num); err != nil {
				children.Close()
				return err
			}
			kids = append(kids, c)
		}
		children.Close()
		for _, c := range kids {
			var childNum int64
			if err := tx.QueryRow(ctx, `UPDATE project SET issue_seq = issue_seq + 1 WHERE id = $1 RETURNING issue_seq`, targetID).Scan(&childNum); err != nil {
				return err
			}
			for _, table := range []string{"issue_version", "issue_component", "issue_field_value"} {
				if _, err := tx.Exec(ctx, `DELETE FROM `+table+` WHERE issue_id = $1`, c.id); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx, `UPDATE issue SET project_id = $2, key_num = $3, sprint_id = NULL, team_id = NULL, milestone_id = NULL, moved_from = $4 WHERE id = $1`,
				c.id, targetID, childNum, FormatKey(projectKey, c.num)); err != nil {
				return fmt.Errorf("move subtask: %w", err)
			}
		}

		changes := []Change{{Field: "project", From: projectKey, To: targetKey}, {Field: "key", From: before.Key, To: newKey}}
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
			{"fixVersion", len(before.FixVersions) > 0, versionNames(before.FixVersions)},
			{"affectsVersion", len(before.AffectsVersions) > 0, versionNames(before.AffectsVersions)},
			{"component", len(before.Components) > 0, componentNames(before.Components)},
		} {
			if cleared.had {
				changes = append(changes, Change{Field: cleared.field, From: cleared.from, To: ""})
			}
		}
		if err := s.recordHistory(ctx, tx, before.ID, actor.UserID, changes); err != nil {
			return err
		}
		if err := s.insertComment(ctx, tx, before.ID, nil, TextDocument(fmt.Sprintf("Moved from %s.", before.Key)), false); err != nil {
			return err
		}
		moved, err = scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, before.ID))
		if err != nil {
			return err
		}
		return events.EmitInTenant(ctx, tx, events.TopicIssueUpdated, map[string]any{
			"issueId": moved.ID, "key": moved.Key, "changes": changes, "actorId": actor.UserID, "movedFrom": before.Key,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return moved, lsn, nil
}

// ByOldKey finds an issue by a key it had before a move, so an old address
// can send its reader on.
func (s *Service) ByOldKey(ctx context.Context, key string) (*Issue, error) {
	var found *Issue
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		found, err = scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.moved_from = $1`, strings.ToUpper(strings.TrimSpace(key))))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return found, err
}

func nameOf(s *SprintRef) string {
	if s == nil {
		return ""
	}
	return s.Name
}

func nameOfTeam(t *TeamRef) string {
	if t == nil {
		return ""
	}
	return t.Name
}

func nameOfMilestone(m *MilestoneRef) string {
	if m == nil {
		return ""
	}
	return m.Name
}
