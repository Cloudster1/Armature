package issue

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/nql"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/workflow"
)

// Filter narrows an issue listing. A zero filter matches every issue in the
// organization the caller can see.
//
// This is the deliberately simple predecessor to a query language: enough to
// drive a project's issue list and "assigned to me", with an obvious upgrade
// path once the parser exists.
type Filter struct {
	ProjectKey string
	StatusIDs  []uuid.UUID
	// Categories filters by status category, which is how "open issues" is
	// expressed without naming every status.
	Categories []workflow.StatusCategory
	TypeIDs    []uuid.UUID
	AssigneeID *uuid.UUID
	// Unassigned, when true, matches only issues with no assignee. It is
	// separate from AssigneeID because a nil assignee already means "any".
	Unassigned bool
	ReporterID *uuid.UUID
	// InvolvingUser keeps the issues somebody raised or watches, which is what
	// a portal shows as "your requests".
	InvolvingUser *uuid.UUID
	ParentID      *uuid.UUID
	// ProjectKind narrows to projects of one kind, which is how the portal
	// lists a customer's requests across every service desk.
	ProjectKind string
	// LabelIDs keeps issues carrying any of these labels.
	LabelIDs []uuid.UUID
	// MilestoneID keeps the issues counting towards one milestone.
	MilestoneID *uuid.UUID
	// Text matches the summary.
	Text string
	// Priorities filters by priority.
	Priorities []Priority
	// Query is a compiled NQL query, ANDed with the rest. Its ORDER BY, when
	// it has one, takes precedence over the page's.
	Query *nql.Compiled
	// Keys narrows to these issues, which is how a rule asks whether one
	// issue matches a query without listing the project.
	Keys []string
	// Within narrows to the projects somebody may read; Scoped says it applies,
	// so an empty Within means none rather than all.
	Within []string
	Scoped bool
}

// Page describes where in a result set to read.
type Page struct {
	Limit  int
	Offset int
	// OrderBy is one of: created, updated, priority, key, summary, status.
	OrderBy string
	Desc    bool
}

// Result is one page of issues plus the total that matched.
type Result struct {
	Issues []Issue `json:"issues"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}

// orderColumns maps the sort names the API accepts to SQL. Whitelisting them
// is what lets the ordering be interpolated without opening an injection.
var orderColumns = map[string]string{
	"created": "i.created_at",
	"updated": "i.updated_at",
	"key":     "i.key_num",
	"summary": "i.summary",
	"status":  "s.position",
	// Priority is an enum, and Postgres orders enums by their declaration
	// order, which runs lowest to highest.
	"priority": "i.priority",
}

// listCap is the most issues one unpaged read returns, matching the tree.
const listCap = 2000

// List returns the issues matching a filter.
func (s *Service) List(ctx context.Context, filter Filter, page Page) (*Result, error) {
	if page.Limit <= 0 || page.Limit > 200 {
		page.Limit = 50
	}
	if page.Offset < 0 {
		page.Offset = 0
	}
	ordering := orderClause(filter, page)

	where, args := filter.build()
	result := &Result{Limit: page.Limit, Offset: page.Offset}

	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		countQuery := `
			SELECT count(*)
			FROM issue i
			JOIN project p ON p.id = i.project_id
			JOIN issue_type it ON it.id = i.issue_type_id
			JOIN issue_status s ON s.id = i.status_id` + where
		if err := tx.QueryRow(ctx, countQuery, args...).Scan(&result.Total); err != nil {
			return err
		}
		if result.Total == 0 {
			return nil
		}

		// The key is appended to the sort so that ties are broken the same way
		// every time; without it, paging can show the same issue twice.
		query := fmt.Sprintf("%s%s ORDER BY %s, i.key_num DESC LIMIT $%d OFFSET $%d",
			selectIssue, where, ordering, len(args)+1, len(args)+2)
		args = append(args, page.Limit, page.Offset)

		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			issue, err := scanIssue(rows)
			if err != nil {
				return err
			}
			result.Issues = append(result.Issues, *issue)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	return result, nil
}

// orderClause is the ORDER BY text: the query's own sort when it has one, else
// the page's whitelisted column. Both come from tables in code, never from text.
func orderClause(filter Filter, page Page) string {
	if filter.Query != nil && len(filter.Query.Order) > 0 {
		parts := make([]string, len(filter.Query.Order))
		for i, o := range filter.Query.Order {
			// Unknowns go last either way: an unestimated issue is not the
			// biggest when sorting by estimate downwards.
			direction := "ASC NULLS LAST"
			if o.Desc {
				direction = "DESC NULLS LAST"
			}
			parts[i] = o.SQL + " " + direction
		}
		return strings.Join(parts, ", ")
	}
	orderBy, ok := orderColumns[page.OrderBy]
	if !ok {
		orderBy = "i.created_at"
		page.Desc = true
	}
	if page.Desc {
		return orderBy + " DESC"
	}
	return orderBy + " ASC"
}

// Keys returns the keys of every issue a filter matches, in key order and
// unpaged, which is what the plan needs to mark the rows a query selects.
func (s *Service) Keys(ctx context.Context, filter Filter) ([]string, error) {
	where, args := filter.build()
	keys := []string{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, fmt.Sprintf(`
			SELECT p.key || '-' || i.key_num
			FROM issue i
			JOIN project p ON p.id = i.project_id
			JOIN issue_type it ON it.id = i.issue_type_id
			JOIN issue_status s ON s.id = i.status_id%s ORDER BY i.key_num LIMIT %d`, where, listCap), args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				return err
			}
			keys = append(keys, key)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("match issues: %w", err)
	}
	return keys, nil
}

// build turns a filter into a WHERE clause and its arguments. Every value is a
// bind parameter; nothing the caller supplies reaches the SQL text.
func (f Filter) build() (string, []any) {
	var (
		clauses []string
		args    []any
	)
	add := func(clause string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(clause, len(args)))
	}

	if f.ProjectKey != "" {
		add("p.key = $%d", project.NormalizeKey(f.ProjectKey))
	}
	if f.Scoped {
		add("p.key = ANY($%d)", append([]string{}, f.Within...))
	}
	if len(f.Keys) > 0 {
		add("p.key || '-' || i.key_num = ANY($%d)", f.Keys)
	}
	if len(f.StatusIDs) > 0 {
		add("i.status_id = ANY($%d)", f.StatusIDs)
	}
	if len(f.Categories) > 0 {
		categories := make([]string, len(f.Categories))
		for i, c := range f.Categories {
			categories[i] = string(c)
		}
		add("s.category = ANY($%d::status_category[])", categories)
	}
	if len(f.TypeIDs) > 0 {
		add("i.issue_type_id = ANY($%d)", f.TypeIDs)
	}
	if f.Unassigned {
		clauses = append(clauses, "i.assignee_id IS NULL")
	} else if f.AssigneeID != nil {
		add("i.assignee_id = $%d", *f.AssigneeID)
	}
	if f.ReporterID != nil {
		add("i.reporter_id = $%d", *f.ReporterID)
	}
	if f.InvolvingUser != nil {
		args = append(args, *f.InvolvingUser)
		n := len(args)
		clauses = append(clauses, fmt.Sprintf("(i.reporter_id = $%d OR EXISTS (SELECT 1 FROM issue_watcher w WHERE w.issue_id = i.id AND w.user_id = $%d))", n, n))
	}
	if f.ProjectKind != "" {
		add("p.kind = $%d::project_kind", f.ProjectKind)
	}
	if f.ParentID != nil {
		add("i.parent_id = $%d", *f.ParentID)
	}
	if f.MilestoneID != nil {
		add("i.milestone_id = $%d", *f.MilestoneID)
	}
	if len(f.LabelIDs) > 0 {
		add("EXISTS (SELECT 1 FROM issue_label il WHERE il.issue_id = i.id AND il.label_id = ANY($%d))", f.LabelIDs)
	}
	if len(f.Priorities) > 0 {
		priorities := make([]string, len(f.Priorities))
		for i, p := range f.Priorities {
			priorities[i] = string(p)
		}
		add("i.priority = ANY($%d::issue_priority[])", priorities)
	}
	if text := strings.TrimSpace(f.Text); text != "" {
		// ILIKE with a wrapped pattern rather than full text search: this is a
		// type-ahead over one column, where a prefix match on a partial word is
		// what people expect and a stemmed match is not.
		add("i.summary ILIKE '%%' || $%d || '%%'", text)
	}
	if f.Query != nil {
		// The query numbers its own parameters from wherever it is placed.
		if clause, queryArgs := f.Query.SQL(len(args) + 1); clause != "" {
			clauses = append(clauses, clause)
			args = append(args, queryArgs...)
		}
	}

	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// Delete removes an issue and everything hanging off it. Comments, history,
// links and subtasks all cascade, which is why this is an administrator action
// rather than something anybody can do by accident.
func (s *Service) Delete(ctx context.Context, key string, actor Actor) (db.LSN, error) {
	if !actor.OrgRole.CanAdminister() {
		return 0, errors.New("only organization admins can delete issues")
	}
	projectKey, num, err := ParseKey(key)
	if err != nil {
		return 0, err
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		doomed, err := scanIssue(tx.QueryRow(ctx,
			selectIssue+` WHERE p.key = $1 AND i.key_num = $2 FOR UPDATE OF i`, projectKey, num))
		if err != nil {
			return err
		}
		// Observers go first: the attachments an issue carries can only be
		// read while their rows are still there.
		for _, o := range s.observers {
			if err := o.IssueDeleted(ctx, tx, doomed, actor); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `DELETE FROM issue WHERE id = $1`, doomed.ID)
		return err
	})
}
