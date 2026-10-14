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
// project (sprint, team, milestone, versions, components, answers to its own
// fields) is cleared and the history says so. Everything under it comes along.
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

		// Everything under it comes along, so all of it is judged before any
		// row is written: a refusal leaves both projects as they were.
		target := moveTarget{id: targetID, key: targetKey}
		subtree, err := subtreeOf(ctx, tx, before.ID)
		if err != nil {
			return err
		}
		if err := s.checkDescendants(ctx, tx, before, subtree, target); err != nil {
			return err
		}
		changes, err := s.moveRow(ctx, tx, before, target, statusID, statusName, actor)
		if err != nil {
			return err
		}
		for i := range subtree {
			d := &subtree[i]
			if _, err := s.moveRow(ctx, tx, d, target, d.Status.ID, d.Status.Name, actor); err != nil {
				return err
			}
		}
		if err := s.insertComment(ctx, tx, before.ID, nil, TextDocument(fmt.Sprintf("Moved from %s.", before.Key)), false); err != nil {
			return err
		}
		moved, err = scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, before.ID))
		if err != nil {
			return err
		}
		// A desk's clocks belong to the project too, so they follow here.
		if err := s.tellUpdated(ctx, tx, before, moved, actor); err != nil {
			return err
		}
		return events.Emit(ctx, tx, events.TopicIssueUpdated, map[string]any{
			"issueId": moved.ID, "key": moved.Key, "changes": changes, "actorId": actor.UserID, "movedFrom": before.Key,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return moved, lsn, nil
}

// keepOldKey adds the key an issue is leaving to those it had. A key another
// issue left, which takes a project deleted and made again, now names this one.
func keepOldKey(ctx context.Context, tx db.DBTX, issueID uuid.UUID, key string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO issue_old_key (org_id, issue_id, key) VALUES (current_org_id(), $1, $2)
		ON CONFLICT (key) DO UPDATE SET issue_id = EXCLUDED.issue_id, created_at = now()`, issueID, key)
	if err != nil {
		return fmt.Errorf("keep the old key %s: %w", key, err)
	}
	return nil
}

// ByOldKey finds an issue by any key it had before a move, so an old address
// can send its reader on.
func (s *Service) ByOldKey(ctx context.Context, key string) (*Issue, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, err
	}
	var found *Issue
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		found, err = scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = (SELECT k.issue_id FROM issue_old_key k WHERE k.key = $1)`,
			FormatKey(projectKey, num)))
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
