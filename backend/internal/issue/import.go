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

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/rank"
)

// ErrNotAnImport refuses a dated write to anyone but the importer.
var ErrNotAnImport = errors.New("only an import may say when something happened")

// ErrStatusNotInWorkflow names a status the issue's workflow cannot hold.
var ErrStatusNotInWorkflow = errors.New("status not in this workflow")

// ImportInput is an issue as another tracker recorded it: everything Create
// stamps for itself, given instead.
type ImportInput struct {
	CreateInput
	// ExternalKey is the source's own name for the row, which is what makes a
	// second run of the same file a correction rather than a copy.
	ExternalKey string
	// KeyNum is the number the issue had. It is kept when that number is free
	// in the project, so references to it elsewhere still read true.
	KeyNum *int64
	// StatusID is where the issue stands. Create may not say, because starting
	// in Done would skip the process; an import is not starting anything.
	StatusID   uuid.UUID
	ReporterID *uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ResolvedAt *time.Time

	TimeEstimateMinutes  *int
	TimeRemainingMinutes *int

	// Source is where the row came from, for the one changelog entry.
	Source string
}

// ImportResult is what an import wrote, and whether the row was already here.
type ImportResult struct {
	Issue   *Issue
	Updated bool
}

// Import emits no event and tells no observer: a record being reproduced is
// not news, and a file of issues must not become a file of notifications.
func (s *Service) Import(ctx context.Context, in ImportInput, actor Actor) (*ImportResult, db.LSN, error) {
	if !actor.Import {
		return nil, 0, ErrNotAnImport
	}
	summary, description, priority, err := checkIssueFields(in.Summary, in.Description, in.Priority)
	if err != nil {
		return nil, 0, err
	}
	in.Summary, in.Description, in.Priority = summary, description, priority
	if in.CreatedAt.IsZero() {
		in.CreatedAt = time.Now().UTC()
	}
	if in.UpdatedAt.IsZero() {
		in.UpdatedAt = in.CreatedAt
	}
	if strings.TrimSpace(in.Source) == "" {
		in.Source = "a file"
	}

	var out *ImportResult
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		out, err = s.importIssue(ctx, tx, in, actor)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

func (s *Service) importIssue(ctx context.Context, tx db.DBTX, in ImportInput, actor Actor) (*ImportResult, error) {
	var (
		projectID  uuid.UUID
		projectKey string
		archived   *time.Time
	)
	err := tx.QueryRow(ctx, `SELECT id, key, archived_at FROM project WHERE key = $1`,
		project.NormalizeKey(in.ProjectKey)).Scan(&projectID, &projectKey, &archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, project.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if archived != nil {
		return nil, project.ErrArchived
	}

	issueType, err := importType(ctx, tx, in.TypeID)
	if err != nil {
		return nil, err
	}

	parentID, err := s.resolveParent(ctx, tx, in.ParentKey, projectID, issueType)
	if err != nil {
		return nil, err
	}
	statusID, err := s.importStatus(ctx, tx, projectID, issueType.ID, in.StatusID)
	if err != nil {
		return nil, err
	}

	if in.ExternalKey != "" {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM issue WHERE project_id = $1 AND external_key = $2`,
			projectID, in.ExternalKey).Scan(&id)
		if err == nil {
			updated, err := s.updateImported(ctx, tx, id, in, issueType.ID, statusID, parentID)
			if err != nil {
				return nil, err
			}
			return &ImportResult{Issue: updated, Updated: true}, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	made, err := s.insertImported(ctx, tx, projectID, projectKey, in, issueType.ID, statusID, parentID, actor)
	if err != nil {
		return nil, err
	}
	return &ImportResult{Issue: made}, nil
}

// importType is the type the row named, or the first at the ordinary working
// level when it named none, which is what Create does with a file's silence.
func importType(ctx context.Context, tx db.DBTX, id uuid.UUID) (TypeRef, error) {
	const columns = `SELECT id, name, icon, hierarchy_level, is_subtask FROM issue_type `
	var (
		t   TypeRef
		err error
	)
	if id == uuid.Nil {
		err = tx.QueryRow(ctx, columns+`WHERE hierarchy_level = $1 ORDER BY position LIMIT 1`, LevelStandard).
			Scan(&t.ID, &t.Name, &t.Icon, &t.Level, &t.IsSubtask)
	} else {
		err = tx.QueryRow(ctx, columns+`WHERE id = $1`, id).
			Scan(&t.ID, &t.Name, &t.Icon, &t.Level, &t.IsSubtask)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return t, errors.New("that issue type does not exist")
	}
	return t, err
}

// importStatus keeps an imported issue inside its workflow: a status the
// workflow does not hold would be a state the issue could never leave.
func (s *Service) importStatus(ctx context.Context, tx db.DBTX, projectID, typeID, wanted uuid.UUID) (uuid.UUID, error) {
	wf, err := s.workflows.ForIssueType(ctx, tx, projectID, typeID)
	if err != nil {
		return uuid.Nil, err
	}
	if wanted == uuid.Nil {
		initial, err := wf.InitialStep()
		if err != nil {
			return uuid.Nil, err
		}
		return initial.Status.ID, nil
	}
	if _, ok := wf.StepByStatus(wanted); !ok {
		var name string
		_ = tx.QueryRow(ctx, `SELECT name FROM issue_status WHERE id = $1`, wanted).Scan(&name)
		if name == "" {
			name = wanted.String()
		}
		return uuid.Nil, fmt.Errorf("%w: %s is not a step of %s", ErrStatusNotInWorkflow, name, wf.Name)
	}
	return wanted, nil
}

// importKeyNum keeps the number the issue had when it is free. Lifting
// issue_seq past it stops the next issue colliding with an imported one.
func importKeyNum(ctx context.Context, tx db.DBTX, projectID uuid.UUID, wanted *int64) (int64, error) {
	if wanted == nil || *wanted <= 0 {
		var next int64
		err := tx.QueryRow(ctx, `
			UPDATE project SET issue_seq = issue_seq + 1 WHERE id = $1 RETURNING issue_seq`, projectID).Scan(&next)
		if err != nil {
			return 0, fmt.Errorf("allocate issue key: %w", err)
		}
		return next, nil
	}
	// The update takes the row lock first, so a concurrent creator is behind us
	// before we ask whether the number is free.
	var seq int64
	if err := tx.QueryRow(ctx, `
		UPDATE project SET issue_seq = GREATEST(issue_seq, $2) WHERE id = $1 RETURNING issue_seq`,
		projectID, *wanted).Scan(&seq); err != nil {
		return 0, fmt.Errorf("allocate issue key: %w", err)
	}
	var taken bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM issue WHERE project_id = $1 AND key_num = $2)`,
		projectID, *wanted).Scan(&taken); err != nil {
		return 0, err
	}
	if !taken {
		return *wanted, nil
	}
	var next int64
	if err := tx.QueryRow(ctx, `
		UPDATE project SET issue_seq = issue_seq + 1 WHERE id = $1 RETURNING issue_seq`, projectID).Scan(&next); err != nil {
		return 0, fmt.Errorf("allocate issue key: %w", err)
	}
	return next, nil
}

func (s *Service) insertImported(ctx context.Context, tx db.DBTX, projectID uuid.UUID, projectKey string,
	in ImportInput, typeID, statusID uuid.UUID, parentID *uuid.UUID, actor Actor) (*Issue, error) {
	keyNum, err := importKeyNum(ctx, tx, projectID, in.KeyNum)
	if err != nil {
		return nil, err
	}
	assigneeID, err := s.componentAssignee(ctx, tx, in.AssigneeID, in.ComponentIDs, projectID)
	if err != nil {
		return nil, err
	}
	reporterID := in.ReporterID
	if reporterID == nil {
		reporterID = &actor.UserID
	}

	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO issue (org_id, project_id, key_num, issue_type_id, status_id,
		                   summary, description, priority, assignee_id, reporter_id,
		                   parent_id, start_date, due_date, estimate, team_id, sprint_id,
		                   rank, request_type_id, external_key, created_at, updated_at, resolved_at,
		                   time_estimate_minutes, time_remaining_minutes)
		VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
		        $16, $17, $18, $19, $20, $21, $22, $23)
		RETURNING id`,
		projectID, keyNum, typeID, statusID,
		in.Summary, nullableJSON(in.Description), string(in.Priority),
		assigneeID, reporterID, parentID, in.StartDate, in.DueDate, in.Estimate, in.TeamID, in.SprintID,
		rank.Sequential(keyNum), in.RequestTypeID, nullableText(in.ExternalKey), in.CreatedAt, in.UpdatedAt, in.ResolvedAt,
		in.TimeEstimateMinutes, in.TimeRemainingMinutes,
	).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("import issue: %w", err)
	}
	if err := s.putComponents(ctx, tx, id, in.ComponentIDs); err != nil {
		return nil, err
	}
	// One entry, dated with the issue, instead of the chain of changes an
	// ordinary edit leaves: what happened before it arrived happened elsewhere.
	changes, err := json.Marshal([]Change{{Field: "imported", To: in.Source}})
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO issue_history (org_id, issue_id, actor_id, changes, created_at)
		VALUES (current_org_id(), $1, $2, $3, $4)`, id, actor.UserID, changes, in.CreatedAt); err != nil {
		return nil, fmt.Errorf("record import: %w", err)
	}
	return scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, id))
}

func (s *Service) updateImported(ctx context.Context, tx db.DBTX, id uuid.UUID,
	in ImportInput, typeID, statusID uuid.UUID, parentID *uuid.UUID) (*Issue, error) {
	_, err := tx.Exec(ctx, `
		UPDATE issue SET issue_type_id = $2, status_id = $3, summary = $4, description = $5,
		                 priority = $6, assignee_id = $7, reporter_id = COALESCE($8, reporter_id),
		                 parent_id = $9, start_date = $10, due_date = $11, estimate = $12,
		                 team_id = $13, sprint_id = $14, updated_at = $15, resolved_at = $16,
		                 time_estimate_minutes = $17, time_remaining_minutes = $18
		WHERE id = $1`,
		id, typeID, statusID, in.Summary, nullableJSON(in.Description),
		string(in.Priority), in.AssigneeID, in.ReporterID, parentID, in.StartDate, in.DueDate, in.Estimate,
		in.TeamID, in.SprintID, in.UpdatedAt, in.ResolvedAt, in.TimeEstimateMinutes, in.TimeRemainingMinutes)
	if err != nil {
		return nil, fmt.Errorf("import issue: %w", err)
	}
	return scanIssue(tx.QueryRow(ctx, selectIssue+` WHERE i.id = $1`, id))
}

// AddImportedComment posts a comment as somebody else, on the day they wrote
// it. Nobody is told: the conversation already happened.
func (s *Service) AddImportedComment(ctx context.Context, key string, body json.RawMessage,
	authorID *uuid.UUID, at time.Time, externalKey string, actor Actor) (db.LSN, error) {
	if !actor.Import {
		return 0, ErrNotAnImport
	}
	if err := validateDoc(body); err != nil {
		return 0, err
	}
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return 0, err
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var issueID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT i.id FROM issue i JOIN project p ON p.id = i.project_id
			WHERE p.key = $1 AND i.key_num = $2`, projectKey, num).Scan(&issueID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO issue_comment (org_id, issue_id, author_id, body, is_internal, external_key, created_at, updated_at)
			VALUES (current_org_id(), $1, $2, $3, false, $4, $5, $5)
			ON CONFLICT (org_id, external_key) DO NOTHING`,
			issueID, authorID, []byte(body), nullableText(externalKey), at)
		if err != nil {
			return fmt.Errorf("import comment: %w", err)
		}
		return nil
	})
}

// LogImportedWork leaves the remaining estimate alone, unlike LogWork: the
// file already says what remained, and subtracting it twice would contradict it.
func (s *Service) LogImportedWork(ctx context.Context, key string, in WorklogInput,
	authorID *uuid.UUID, externalKey string, actor Actor) (db.LSN, error) {
	if !actor.Import {
		return 0, ErrNotAnImport
	}
	in, err := checkWorklog(in)
	if err != nil {
		return 0, err
	}
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return 0, err
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var issueID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT i.id FROM issue i JOIN project p ON p.id = i.project_id
			WHERE p.key = $1 AND i.key_num = $2`, projectKey, num).Scan(&issueID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO issue_worklog (org_id, issue_id, author_id, minutes, started_on, note, external_key)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6)
			ON CONFLICT (org_id, external_key) DO NOTHING`,
			issueID, authorID, in.Minutes, *in.StartedOn, in.Note, nullableText(externalKey))
		if err != nil {
			return fmt.Errorf("import worklog: %w", err)
		}
		return nil
	})
}

// nullableText keeps an absent external key out of the unique index, where an
// empty string would collide with the next row that also has none.
func nullableText(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
