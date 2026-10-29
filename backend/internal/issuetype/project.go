package issuetype

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
)

// ProjectIssueType is a type a project allows, in the project's own order.
type ProjectIssueType struct {
	IssueType
	// Issues counts the project's issues of the type, which keep it allowed.
	Issues int `json:"issues"`
}

var (
	// ErrNoTypes is a project left with no types at all.
	ErrNoTypes = errors.New("a project needs at least one issue type; keep a standard one such as Task")
	// ErrNoStandardType is a set without a type at the working level.
	ErrNoStandardType = errors.New("a project needs a standard issue type that is not retired, such as Task, for issues filed without a type; keep one")
	// ErrRepeated is a type named twice in one set.
	ErrRepeated = errors.New("a type is listed twice; name each type once")
)

// StillUsedError is a type taken away from a project while something there uses it.
type StillUsedError struct {
	Project string
	Type    string
	// Issues and RequestTypes say what still uses it; one of them is set.
	Issues       int
	RequestTypes int
}

func (e *StillUsedError) Error() string {
	if e.Issues == 0 {
		return fmt.Sprintf("A request type of %s files issues as %s, so %s stays. Point the request type at another type first", e.Project, e.Type, e.Type)
	}
	issues := "issues are"
	if e.Issues == 1 {
		issues = "issue is"
	}
	return fmt.Sprintf("%d %s %s in %s, so %s stays. Change their type or move them first", e.Issues, issues, e.Type, e.Project, e.Type)
}

// OnlyStandardError is a type a project depends on as its only standard one.
type OnlyStandardError struct {
	Project string
	Type    string
}

func (e *OnlyStandardError) Error() string {
	return fmt.Sprintf("%s is the only standard type %s uses. Allow another standard type there first", e.Type, e.Project)
}

func projectOf(ctx context.Context, tx db.DBTX, projectKey string) (uuid.UUID, string, bool, error) {
	var (
		id       uuid.UUID
		key      string
		archived bool
	)
	err := tx.QueryRow(ctx, `SELECT id, key, archived_at IS NOT NULL FROM project WHERE key = $1`,
		project.NormalizeKey(projectKey)).Scan(&id, &key, &archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, "", false, project.ErrNotFound
	}
	return id, key, archived, err
}

const selectProjectType = `
SELECT t.id, t.name, t.description, t.icon, t.hierarchy_level, t.is_subtask, t.position, t.builtin, t.retired_at,
       (SELECT count(*) FROM issue i WHERE i.project_id = pit.project_id AND i.issue_type_id = t.id)
FROM project_issue_type pit JOIN issue_type t ON t.id = pit.issue_type_id`

func projectTypes(ctx context.Context, tx db.DBTX, projectID uuid.UUID, withRetired bool) ([]ProjectIssueType, error) {
	rows, err := tx.Query(ctx, selectProjectType+`
		WHERE pit.project_id = $1 AND ($2 OR t.retired_at IS NULL)
		ORDER BY pit.position, t.position, t.name`, projectID, withRetired)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectIssueType{}
	for rows.Next() {
		var p ProjectIssueType
		t := &p.IssueType
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.Icon, &t.Level, &t.IsSubtask, &t.Position, &t.Builtin, &t.RetiredAt, &p.Issues); err != nil {
			return nil, err
		}
		t.Retired = t.RetiredAt != nil
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProjectTypes lists the types a project allows in its order; retired ones
// only when asked for.
func (s *Service) ProjectTypes(ctx context.Context, projectKey string, withRetired bool) ([]ProjectIssueType, error) {
	var out []ProjectIssueType
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		id, _, _, err := projectOf(ctx, tx, projectKey)
		if err != nil {
			return err
		}
		out, err = projectTypes(ctx, tx, id, withRetired)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SetProjectTypes makes ids, in that order, the types a project allows. A type
// its issues or request types use stays, and so does a standard one.
func (s *Service) SetProjectTypes(ctx context.Context, projectKey string, ids []uuid.UUID) ([]ProjectIssueType, db.LSN, error) {
	if len(ids) == 0 {
		return nil, 0, ErrNoTypes
	}
	position := map[uuid.UUID]int{}
	for i, id := range ids {
		if _, twice := position[id]; twice {
			return nil, 0, ErrRepeated
		}
		position[id] = i + 1
	}
	var out []ProjectIssueType
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		projectID, key, archived, err := projectOf(ctx, tx, projectKey)
		if err != nil {
			return err
		}
		if archived {
			return project.ErrArchived
		}
		// An issue filed meanwhile in a type being taken away is refused by the
		// foreign key when the row goes, which TakenInUseError words.
		current, err := projectTypes(ctx, tx, projectID, true)
		if err != nil {
			return err
		}
		allowed := map[uuid.UUID]bool{}
		for _, c := range current {
			allowed[c.ID] = true
			if _, kept := position[c.ID]; kept {
				continue
			}
			if c.Issues > 0 {
				return &StillUsedError{Project: key, Type: c.Name, Issues: c.Issues}
			}
			var requests int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM request_type WHERE project_id = $1 AND issue_type_id = $2`, projectID, c.ID).Scan(&requests); err != nil {
				return err
			}
			if requests > 0 {
				return &StillUsedError{Project: key, Type: c.Name, RequestTypes: requests}
			}
		}
		standard := false
		for _, id := range ids {
			t, err := scan(tx.QueryRow(ctx, selectType+` WHERE id = $1`, id))
			if err != nil {
				return err
			}
			if t.Retired && !allowed[id] {
				return &issue.TypeRetiredError{Type: t.Name}
			}
			standard = standard || (t.Level == issue.LevelStandard && !t.Retired)
		}
		if !standard {
			return ErrNoStandardType
		}

		if _, err := tx.Exec(ctx, `DELETE FROM project_issue_type WHERE project_id = $1 AND NOT (issue_type_id = ANY($2))`, projectID, ids); err != nil {
			if constraint, ok := issue.IsTypeRule(err); ok && constraint != issue.StandardTypeConstraint {
				return &TakenInUseError{Project: key}
			}
			return err
		}
		for _, id := range ids {
			// An update for a type already allowed, since inserting a retired
			// one the project keeps would be refused.
			if allowed[id] {
				_, err = tx.Exec(ctx, `UPDATE project_issue_type SET position = $3 WHERE project_id = $1 AND issue_type_id = $2`, projectID, id, position[id])
			} else {
				_, err = tx.Exec(ctx, `INSERT INTO project_issue_type (project_id, issue_type_id, position) VALUES ($1, $2, $3)`, projectID, id, position[id])
			}
			if err != nil {
				return err
			}
		}
		out, err = projectTypes(ctx, tx, projectID, true)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// TakenInUseError is the database refusing to take away a type that something
// in the project started using after the check.
type TakenInUseError struct {
	Project string
}

func (e *TakenInUseError) Error() string {
	return fmt.Sprintf("Something in %s started using a type being taken away. Reload its types and try again", e.Project)
}

// onlyStandardIn names a project for which the type is the only standard one
// that is not retired, which retiring, moving or deleting it would leave bare.
func onlyStandardIn(ctx context.Context, tx db.DBTX, t *IssueType) error {
	if t.Level != issue.LevelStandard || t.Retired {
		return nil
	}
	var key string
	err := tx.QueryRow(ctx, `
		SELECT p.key FROM project p
		JOIN project_issue_type pit ON pit.project_id = p.id AND pit.issue_type_id = $1
		WHERE NOT EXISTS (
		    SELECT 1 FROM project_issue_type other JOIN issue_type o ON o.id = other.issue_type_id
		    WHERE other.project_id = p.id AND o.id <> $1 AND o.hierarchy_level = 0 AND o.retired_at IS NULL)
		ORDER BY p.key LIMIT 1`, t.ID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return &OnlyStandardError{Project: key, Type: t.Name}
}
