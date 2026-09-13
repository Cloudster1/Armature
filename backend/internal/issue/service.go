package issue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/rank"
	"github.com/armature/armature/backend/internal/workflow"
)

// Service implements issue use cases.
type Service struct {
	db        *db.Cluster
	engine    *workflow.Engine
	workflows *workflow.Store
	observers []Observer
}

func NewService(cluster *db.Cluster, engine *workflow.Engine, workflows *workflow.Store) *Service {
	return &Service{db: cluster, engine: engine, workflows: workflows}
}

// Observer is told about an issue's life inside the transaction that changes
// it, so that whatever it keeps (the service desk keeps clocks) can never
// disagree with the issue. An observer that returns an error rolls the change
// back, which is the point of being inside.
type Observer interface {
	IssueCreated(ctx context.Context, tx db.DBTX, created *Issue, actor Actor) error
	IssueTransitioned(ctx context.Context, tx db.DBTX, before, after *Issue, actor Actor) error
	// CommentAdded is told of every comment, internal or not.
	CommentAdded(ctx context.Context, tx db.DBTX, issueID uuid.UUID, internal bool, actor Actor) error
	// IssueDeleted is told before the rows go, while what hangs off the issue
	// can still be read.
	IssueDeleted(ctx context.Context, tx db.DBTX, deleted *Issue, actor Actor) error
}

// Observe adds an observer. It is called at wiring time, before any request.
func (s *Service) Observe(o Observer) { s.observers = append(s.observers, o) }

// Actor is whoever is making the change.
type Actor struct {
	UserID  uuid.UUID
	OrgRole auth.OrgRole
	// Import is set by the importer alone, and is what lets a write say when it
	// happened and who it was. No handler sets it.
	Import bool
}

func (a Actor) toWorkflow() workflow.Actor {
	return workflow.Actor{UserID: a.UserID, OrgRole: string(a.OrgRole)}
}

// selectIssue is the shared projection for reading issues.
const selectIssue = `
SELECT i.id, p.key || '-' || i.key_num, i.project_id, p.key,
       it.id, it.name, it.icon, it.hierarchy_level, it.is_subtask,
       i.summary, i.description,
       s.id, s.name, s.category, s.description, s.position,
       i.priority,
       a.id, a.name, a.email, a.avatar_url,
       r.id, r.name, r.email, r.avatar_url,
       i.parent_id, COALESCE(pp.key || '-' || parent.key_num, ''),
       COALESCE(parent.summary, ''),
       pt.id, COALESCE(pt.name, ''), COALESCE(pt.icon, ''), COALESCE(pt.hierarchy_level, 0),
       i.start_date, i.due_date, i.created_at, i.updated_at, i.resolved_at,
       i.sprint_id, COALESCE(sp.name, ''), COALESCE(sp.state::text, ''), i.estimate,
       i.team_id, COALESCE(tm.name, ''),
       i.milestone_id, COALESCE(ms.name, ''),
       i.request_type_id, COALESCE(rt.name, ''),
       i.time_estimate_minutes, i.time_remaining_minutes,
       COALESCE((SELECT sum(w.minutes) FROM issue_worklog w WHERE w.issue_id = i.id), 0)::int,
       COALESCE((SELECT jsonb_agg(jsonb_build_object('id', l.id, 'name', l.name, 'color', l.color) ORDER BY lower(l.name))
                 FROM issue_label il JOIN label l ON l.id = il.label_id WHERE il.issue_id = i.id), '[]'::jsonb),
       COALESCE((SELECT jsonb_agg(jsonb_build_object('id', ver.id, 'name', ver.name, 'released', ver.released_at IS NOT NULL) ORDER BY ver.position, lower(ver.name))
                 FROM issue_version iv JOIN version ver ON ver.id = iv.version_id WHERE iv.issue_id = i.id AND iv.role = 'fix'), '[]'::jsonb),
       COALESCE((SELECT jsonb_agg(jsonb_build_object('id', ver.id, 'name', ver.name, 'released', ver.released_at IS NOT NULL) ORDER BY ver.position, lower(ver.name))
                 FROM issue_version iv JOIN version ver ON ver.id = iv.version_id WHERE iv.issue_id = i.id AND iv.role = 'affects'), '[]'::jsonb),
       COALESCE((SELECT jsonb_agg(jsonb_build_object('id', co.id, 'name', co.name) ORDER BY lower(co.name))
                 FROM issue_component ic JOIN component co ON co.id = ic.component_id WHERE ic.issue_id = i.id), '[]'::jsonb)
FROM issue i
JOIN project p ON p.id = i.project_id
JOIN issue_type it ON it.id = i.issue_type_id
JOIN issue_status s ON s.id = i.status_id
LEFT JOIN app_user a ON a.id = i.assignee_id
LEFT JOIN app_user r ON r.id = i.reporter_id
LEFT JOIN issue parent ON parent.id = i.parent_id
LEFT JOIN issue_type pt ON pt.id = parent.issue_type_id
LEFT JOIN project pp ON pp.id = parent.project_id
LEFT JOIN request_type rt ON rt.id = i.request_type_id
LEFT JOIN sprint sp ON sp.id = i.sprint_id
LEFT JOIN team tm ON tm.id = i.team_id
LEFT JOIN milestone ms ON ms.id = i.milestone_id`

func scanIssue(row pgx.Row) (*Issue, error) {
	var (
		i                            Issue
		assigneeID, reporterID       *uuid.UUID
		assigneeName, assigneeEmail  *string
		reporterName, reporterEmail  *string
		assigneeAvatar, reporterAv   *string
		parentSummary                string
		parentTypeID                 *uuid.UUID
		parentTypeName, parentTypeIc string
		parentTypeLevel              int
		sprintName, sprintState      string
		teamName, milestoneName      string
		labels                       []byte
		fixVersions, affects, comps  []byte
	)
	err := row.Scan(
		&i.ID, &i.Key, &i.ProjectID, &i.ProjectKey,
		&i.Type.ID, &i.Type.Name, &i.Type.Icon, &i.Type.Level, &i.Type.IsSubtask,
		&i.Summary, &i.Description,
		&i.Status.ID, &i.Status.Name, &i.Status.Category, &i.Status.Description, &i.Status.Position,
		&i.Priority,
		&assigneeID, &assigneeName, &assigneeEmail, &assigneeAvatar,
		&reporterID, &reporterName, &reporterEmail, &reporterAv,
		&i.ParentID, &i.ParentKey,
		&parentSummary, &parentTypeID, &parentTypeName, &parentTypeIc, &parentTypeLevel,
		&i.StartDate, &i.DueDate, &i.CreatedAt, &i.UpdatedAt, &i.ResolvedAt,
		&i.SprintID, &sprintName, &sprintState, &i.Estimate,
		&i.TeamID, &teamName,
		&i.MilestoneID, &milestoneName,
		&i.RequestTypeID, &i.RequestTypeName,
		&i.TimeEstimateMinutes, &i.TimeRemainingMinutes, &i.TimeSpentMinutes, &labels, &fixVersions, &affects, &comps,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	i.Labels = []LabelRef{}
	if err := json.Unmarshal(labels, &i.Labels); err != nil {
		return nil, fmt.Errorf("decode labels: %w", err)
	}
	i.FixVersions, i.AffectsVersions, i.Components = []VersionRef{}, []VersionRef{}, []ComponentRef{}
	if err := json.Unmarshal(fixVersions, &i.FixVersions); err != nil {
		return nil, fmt.Errorf("decode versions: %w", err)
	}
	if err := json.Unmarshal(affects, &i.AffectsVersions); err != nil {
		return nil, fmt.Errorf("decode versions: %w", err)
	}
	if err := json.Unmarshal(comps, &i.Components); err != nil {
		return nil, fmt.Errorf("decode components: %w", err)
	}
	if assigneeID != nil {
		i.Assignee = userRef(*assigneeID, assigneeName, assigneeEmail, assigneeAvatar)
	}
	if reporterID != nil {
		i.Reporter = userRef(*reporterID, reporterName, reporterEmail, reporterAv)
	}
	if i.SprintID != nil {
		i.Sprint = &SprintRef{ID: *i.SprintID, Name: sprintName, State: sprintState}
	}
	if i.TeamID != nil {
		i.Team = &TeamRef{ID: *i.TeamID, Name: teamName}
	}
	if i.MilestoneID != nil {
		i.Milestone = &MilestoneRef{ID: *i.MilestoneID, Name: milestoneName}
	}
	if i.ParentID != nil && parentTypeID != nil {
		i.Parent = &ParentRef{
			ID:      *i.ParentID,
			Key:     i.ParentKey,
			Summary: parentSummary,
			Type: TypeRef{
				ID:        *parentTypeID,
				Name:      parentTypeName,
				Icon:      parentTypeIc,
				Level:     parentTypeLevel,
				IsSubtask: parentTypeLevel < LevelStandard,
			},
		}
	}
	return &i, nil
}

// MaxSummary keeps a summary to the one line it is meant to be.
const MaxSummary = 255

// checkIssueFields is what every writer of an issue agrees on, whether the
// issue is being made here or reproduced from somewhere else.
func checkIssueFields(rawSummary string, description json.RawMessage, priority Priority) (string, json.RawMessage, Priority, error) {
	summary := strings.TrimSpace(rawSummary)
	if summary == "" {
		return "", nil, "", errors.New("an issue needs a summary")
	}
	if len([]rune(summary)) > MaxSummary {
		return "", nil, "", fmt.Errorf("the summary must be %d characters or fewer", MaxSummary)
	}
	// An empty document is no description; a full one must be showable.
	if IsEmptyDocument(description) {
		description = nil
	} else if err := ValidateDocument(description, "description"); err != nil {
		return "", nil, "", err
	}
	if priority == "" {
		priority = PriorityMedium
	}
	if !priority.Valid() {
		return "", nil, "", fmt.Errorf("%q is not a priority", priority)
	}
	return summary, description, priority, nil
}

// CreateInput describes a new issue.
type CreateInput struct {
	ProjectKey  string
	TypeID      uuid.UUID
	Summary     string
	Description json.RawMessage
	Priority    Priority
	AssigneeID  *uuid.UUID
	ParentKey   string
	StartDate   *time.Time
	DueDate     *time.Time
	// Estimate sizes the issue as it is created. Nil is unestimated.
	Estimate *float64
	// TeamID hands the issue to a team as it is created. Nil leaves it with the
	// project at large.
	TeamID *uuid.UUID
	// SprintID commits the issue to a sprint as it is created, which is one
	// request instead of a create and a move, and no half-committed issue if
	// the sprint refuses.
	SprintID *uuid.UUID
	// RequestTypeID says the issue was raised through the portal, and as what.
	RequestTypeID *uuid.UUID
	// ComponentIDs puts the issue in the project's parts as it is created; the
	// first component with a default assignee takes it when nobody was named.
	ComponentIDs []uuid.UUID
}

// Create makes an issue.
//
// The status is not the caller's to choose: it is whatever the workflow mapped
// to this project and issue type says new issues start in. That is what stops a
// client from creating an issue directly in Done and skipping the process.
func (s *Service) Create(ctx context.Context, in CreateInput, actor Actor) (*Issue, db.LSN, error) {
	summary, description, priority, err := checkIssueFields(in.Summary, in.Description, in.Priority)
	if err != nil {
		return nil, 0, err
	}
	in.Description = description

	var created *Issue
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			projectID  uuid.UUID
			projectKey string
			archived   *time.Time
		)
		err := tx.QueryRow(ctx, `SELECT id, key, archived_at FROM project WHERE key = $1`,
			project.NormalizeKey(in.ProjectKey)).Scan(&projectID, &projectKey, &archived)
		if errors.Is(err, pgx.ErrNoRows) {
			return project.ErrNotFound
		}
		if err != nil {
			return err
		}
		if archived != nil {
			return project.ErrArchived
		}

		var issueType TypeRef
		if in.TypeID == uuid.Nil {
			// No type given: the first type at the ordinary working level.
			err = tx.QueryRow(ctx, `
				SELECT id, name, icon, hierarchy_level, is_subtask FROM issue_type
				WHERE hierarchy_level = $1 ORDER BY position LIMIT 1`, LevelStandard).
				Scan(&issueType.ID, &issueType.Name, &issueType.Icon, &issueType.Level, &issueType.IsSubtask)
		} else {
			err = tx.QueryRow(ctx, `
				SELECT id, name, icon, hierarchy_level, is_subtask FROM issue_type WHERE id = $1`, in.TypeID).
				Scan(&issueType.ID, &issueType.Name, &issueType.Icon, &issueType.Level, &issueType.IsSubtask)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("that issue type does not exist")
		}
		if err != nil {
			return err
		}
		typeID := issueType.ID

		parentID, err := s.resolveParent(ctx, tx, in.ParentKey, projectID, issueType)
		if err != nil {
			return err
		}

		wf, err := s.workflows.ForIssueType(ctx, tx, projectID, typeID)
		if err != nil {
			return err
		}
		initial, err := wf.InitialStep()
		if err != nil {
			return err
		}

		// Allocating the number inside this transaction is what makes two
		// concurrent creators impossible to give the same key: the row lock on
		// the project serialises them, and a rollback gives the number back.
		var keyNum int64
		if err := tx.QueryRow(ctx, `
			UPDATE project SET issue_seq = issue_seq + 1
			WHERE id = $1 RETURNING issue_seq`, projectID).Scan(&keyNum); err != nil {
			return fmt.Errorf("allocate issue key: %w", err)
		}

		// New work joins the end of the list, and its rank is derived from the
		// issue number rather than from the current lowest rank. Repeatedly
		// squeezing new cards past one end would make every rank a little
		// longer than the last; a fixed width value never grows, and dragging
		// still works because the rank generator happily produces values
		// between two of these.
		cardRank := rank.Sequential(keyNum)

		// The sprint has to be this project's and still open; a closed one has
		// its report written, and the same sentences SetSprint uses apply.
		if in.SprintID != nil {
			var name, state string
			err := tx.QueryRow(ctx, `SELECT name, state::text FROM sprint WHERE id = $1 AND project_id = $2`,
				*in.SprintID, projectID).Scan(&name, &state)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrSprintNotFound
			}
			if err != nil {
				return err
			}
			if state == "closed" {
				return fmt.Errorf("%w: %s", ErrSprintClosed, name)
			}
		}

		assigneeID, err := s.componentAssignee(ctx, tx, in.AssigneeID, in.ComponentIDs, projectID)
		if err != nil {
			return err
		}

		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO issue (org_id, project_id, key_num, issue_type_id, status_id,
			                   summary, description, priority, assignee_id, reporter_id,
			                   parent_id, start_date, due_date, estimate, team_id, sprint_id, rank, request_type_id)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
			RETURNING id`,
			projectID, keyNum, typeID, initial.Status.ID,
			summary, nullableJSON(in.Description), string(priority),
			assigneeID, actor.UserID, parentID, in.StartDate, in.DueDate, in.Estimate, in.TeamID, in.SprintID, cardRank, in.RequestTypeID,
		).Scan(&id)
		if err != nil {
			return fmt.Errorf("create issue: %w", err)
		}

		if err := s.putComponents(ctx, tx, id, in.ComponentIDs); err != nil {
			return err
		}

		// The creation itself is the first changelog entry, so the history is
		// complete from the very beginning rather than starting at the first
		// edit. A component's default assignee is written down too, since
		// nobody chose them.
		changes := []Change{{Field: "created", To: FormatKey(projectKey, keyNum)}}
		if assigneeID != nil && in.AssigneeID == nil {
			var name string
			_ = tx.QueryRow(ctx, `SELECT name FROM app_user WHERE id = $1`, *assigneeID).Scan(&name)
			changes = append(changes, Change{Field: "assignee", From: "Unassigned", To: name})
		}
		if err := s.recordHistory(ctx, tx, id, actor.UserID, changes); err != nil {
			return err
		}

		created, err = scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, id))
		if err != nil {
			return err
		}
		// Naming somebody in the description brings them onto the issue, as
		// naming them in a comment does.
		if in.Description != nil {
			if _, err := s.mentionWatchers(ctx, tx, id, created.Key, in.Description, actor); err != nil {
				return err
			}
		}
		for _, o := range s.observers {
			if err := o.IssueCreated(ctx, tx, created, actor); err != nil {
				return err
			}
		}

		return events.EmitInTenant(ctx, tx, events.TopicIssueCreated, map[string]any{
			"issueId": created.ID,
			"key":     created.Key,
			"summary": created.Summary,
			"actorId": actor.UserID,
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return created, lsn, nil
}

// ByKey reads one issue.
func (s *Service) ByKey(ctx context.Context, key string) (*Issue, error) {
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return nil, err
	}

	var found *Issue
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		found, err = scanIssue(tx.QueryRow(ctx,
			selectIssue+` WHERE p.key = $1 AND i.key_num = $2`, projectKey, num))
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM issue c WHERE c.parent_id = $1),
			       (SELECT count(*) FROM issue_comment c WHERE c.issue_id = $1)`,
			found.ID).Scan(&found.ChildCount, &found.CommentCount)
	})
	// A key an issue had before it was moved still finds it.
	if errors.Is(err, ErrNotFound) {
		if moved, oldErr := s.ByOldKey(ctx, key); oldErr == nil {
			return moved, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return found, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// nullableJSON turns an empty document into a SQL null, so "no description" is
// null rather than the JSON literal null or an empty string.
func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	return []byte(raw)
}
