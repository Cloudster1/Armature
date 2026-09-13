package workflow

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
)

// Scope is the level of configuration that answered for an issue type.
//
// Workflows are configured twice over: the organization keeps one scheme that
// answers for every project, and a project may name its own scheme to disagree.
// A project's scheme is consulted first and needs only to cover the issue types
// it actually cares about; everything it leaves out falls through to the
// organization, so an override is a difference rather than a replacement.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeTenant  Scope = "tenant"
)

// Origin records where a workflow assignment came from, which is the difference
// between "this project was set up that way" and "the organization says so".
type Origin struct {
	Scope      Scope     `json:"scope"`
	SchemeID   uuid.UUID `json:"schemeId"`
	SchemeName string    `json:"schemeName"`
	// Named is true when the scheme maps this issue type by name rather than
	// catching it with its fallback.
	Named bool `json:"named"`
}

// Assignment is one row of the table an administrator reads: an issue type, the
// workflow its issues move through here, and why.
type Assignment struct {
	IssueTypeID   uuid.UUID `json:"issueTypeId"`
	IssueTypeName string    `json:"issueTypeName"`
	WorkflowID    uuid.UUID `json:"workflowId"`
	WorkflowName  string    `json:"workflowName"`
	Origin        Origin    `json:"origin"`
}

// resolveOne picks the winning mapping for one project and issue type.
//
// The ordering is the whole rule: the project's own scheme beats the
// organization's, and within one scheme a mapping that names the issue type
// beats the fallback. A project's blanket fallback therefore outranks a
// tenant mapping for the specific type, because the narrower scope wins first.
const resolveOne = `
WITH chosen AS (
    SELECT workflow_scheme_id AS scheme_id FROM project WHERE id = $1
)
SELECT i.workflow_id
FROM chosen c
JOIN workflow_scheme s ON s.id = c.scheme_id OR s.is_default
JOIN workflow_scheme_item i ON i.scheme_id = s.id
WHERE i.issue_type_id = $2 OR i.issue_type_id IS NULL
ORDER BY (s.id IS DISTINCT FROM c.scheme_id), (i.issue_type_id IS NULL)
LIMIT 1`

// ForIssueType resolves which workflow a project uses for one issue type.
func (s *Store) ForIssueType(ctx context.Context, tx db.DBTX, projectID, issueTypeID uuid.UUID) (*Workflow, error) {
	var workflowID uuid.UUID
	err := tx.QueryRow(ctx, resolveOne, projectID, issueTypeID).Scan(&workflowID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: no workflow is mapped for that issue type", ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve workflow: %w", err)
	}
	return s.Load(ctx, tx, workflowID)
}

// Assignments answers for every issue type at once, and says where each answer
// came from. The ordering inside the lateral join is the same rule as
// resolveOne above, applied per issue type.
func (s *Store) Assignments(ctx context.Context, tx db.DBTX, projectID uuid.UUID) ([]Assignment, error) {
	rows, err := tx.Query(ctx, `
		WITH chosen AS (
		    SELECT workflow_scheme_id AS scheme_id FROM project WHERE id = $1
		)
		SELECT t.id, t.name, w.id, w.name, s.id, s.name, r.named,
		       s.id IS NOT DISTINCT FROM c.scheme_id
		FROM issue_type t
		CROSS JOIN chosen c
		CROSS JOIN LATERAL (
		    SELECT i.workflow_id, i.scheme_id, i.issue_type_id IS NOT NULL AS named
		    FROM workflow_scheme sc
		    JOIN workflow_scheme_item i ON i.scheme_id = sc.id
		    WHERE (sc.id = c.scheme_id OR sc.is_default)
		      AND (i.issue_type_id = t.id OR i.issue_type_id IS NULL)
		    ORDER BY (sc.id IS DISTINCT FROM c.scheme_id), (i.issue_type_id IS NULL)
		    LIMIT 1
		) r
		JOIN workflow w ON w.id = r.workflow_id
		JOIN workflow_scheme s ON s.id = r.scheme_id
		ORDER BY t.hierarchy_level DESC, t.position, t.name`, projectID)
	if err != nil {
		return nil, fmt.Errorf("resolve workflows: %w", err)
	}
	defer rows.Close()

	var out []Assignment
	for rows.Next() {
		var (
			a         Assignment
			ownScheme bool
		)
		if err := rows.Scan(
			&a.IssueTypeID, &a.IssueTypeName, &a.WorkflowID, &a.WorkflowName,
			&a.Origin.SchemeID, &a.Origin.SchemeName, &a.Origin.Named, &ownScheme,
		); err != nil {
			return nil, err
		}
		a.Origin.Scope = ScopeTenant
		if ownScheme {
			a.Origin.Scope = ScopeProject
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Scheme maps issue types onto workflows. One scheme per organization is its
// default; the rest exist to be named by a project that wants something else.
type Scheme struct {
	ID        uuid.UUID    `json:"id"`
	Name      string       `json:"name"`
	IsDefault bool         `json:"isDefault"`
	Items     []SchemeItem `json:"items"`
	// Projects are the keys of every project that names this scheme, archived
	// ones included: an archived project can be restored, so its configuration
	// is what keeps the scheme from being deleted. The default scheme is used by
	// far more than these; it is used by all the others.
	Projects []string `json:"projectKeys"`
}

// SchemeItem is one mapping. A nil issue type is the scheme's fallback, which
// catches every type the scheme does not name.
type SchemeItem struct {
	IssueTypeID   *uuid.UUID `json:"issueTypeId,omitempty"`
	IssueTypeName string     `json:"issueTypeName,omitempty"`
	WorkflowID    uuid.UUID  `json:"workflowId"`
	WorkflowName  string     `json:"workflowName"`
}

// IsFallback reports whether the item is the scheme's catch-all.
func (i SchemeItem) IsFallback() bool { return i.IssueTypeID == nil }

// ListSchemes returns every scheme in the organization with its mappings.
func (s *Store) ListSchemes(ctx context.Context, tx db.DBTX) ([]Scheme, error) {
	rows, err := tx.Query(ctx, `
		SELECT sc.id, sc.name, sc.is_default,
		       COALESCE(array_agg(p.key ORDER BY p.key) FILTER (WHERE p.key IS NOT NULL), '{}')
		FROM workflow_scheme sc
		LEFT JOIN project p ON p.workflow_scheme_id = sc.id
		GROUP BY sc.id, sc.name, sc.is_default
		ORDER BY sc.is_default DESC, sc.name`)
	if err != nil {
		return nil, fmt.Errorf("list schemes: %w", err)
	}
	defer rows.Close()

	var (
		out   []Scheme
		byID  = map[uuid.UUID]int{}
		empty = []SchemeItem{}
	)
	for rows.Next() {
		var sc Scheme
		if err := rows.Scan(&sc.ID, &sc.Name, &sc.IsDefault, &sc.Projects); err != nil {
			return nil, err
		}
		sc.Items = empty
		byID[sc.ID] = len(out)
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	items, err := tx.Query(ctx, `
		SELECT i.scheme_id, i.issue_type_id, COALESCE(t.name, ''), i.workflow_id, w.name
		FROM workflow_scheme_item i
		JOIN workflow w ON w.id = i.workflow_id
		LEFT JOIN issue_type t ON t.id = i.issue_type_id
		ORDER BY (i.issue_type_id IS NULL), t.hierarchy_level DESC, t.position, t.name`)
	if err != nil {
		return nil, fmt.Errorf("list scheme items: %w", err)
	}
	defer items.Close()

	for items.Next() {
		var (
			schemeID uuid.UUID
			item     SchemeItem
		)
		if err := items.Scan(&schemeID, &item.IssueTypeID, &item.IssueTypeName,
			&item.WorkflowID, &item.WorkflowName); err != nil {
			return nil, err
		}
		if at, ok := byID[schemeID]; ok {
			out[at].Items = append(out[at].Items, item)
		}
	}
	return out, items.Err()
}

// ProjectStatuses returns every status a project's issues can be in, across all
// the workflows it resolves to, in workflow order. It is what a new board's
// swimlanes are built from.
func (s *Store) ProjectStatuses(ctx context.Context, tx db.DBTX, projectID uuid.UUID) ([]Status, error) {
	rows, err := tx.Query(ctx, `
		WITH chosen AS (
		    SELECT workflow_scheme_id AS scheme_id FROM project WHERE id = $1
		), used AS (
		    SELECT DISTINCT r.workflow_id
		    FROM issue_type t
		    CROSS JOIN chosen c
		    CROSS JOIN LATERAL (
		        SELECT i.workflow_id
		        FROM workflow_scheme sc
		        JOIN workflow_scheme_item i ON i.scheme_id = sc.id
		        WHERE (sc.id = c.scheme_id OR sc.is_default)
		          AND (i.issue_type_id = t.id OR i.issue_type_id IS NULL)
		        ORDER BY (sc.id IS DISTINCT FROM c.scheme_id), (i.issue_type_id IS NULL)
		        LIMIT 1
		    ) r
		)
		SELECT DISTINCT ON (st.id) st.id, st.name, st.category, st.description, st.position,
		       step.position
		FROM used
		JOIN workflow_step step ON step.workflow_id = used.workflow_id
		JOIN issue_status st ON st.id = step.status_id
		ORDER BY st.id, step.position`, projectID)
	if err != nil {
		return nil, fmt.Errorf("read the project's statuses: %w", err)
	}
	defer rows.Close()

	type ordered struct {
		status Status
		at     int
	}
	var found []ordered
	for rows.Next() {
		var o ordered
		if err := rows.Scan(&o.status.ID, &o.status.Name, &o.status.Category,
			&o.status.Description, &o.status.Position, &o.at); err != nil {
			return nil, err
		}
		found = append(found, o)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// DISTINCT ON forces an ordering by status id, so the order a person would
	// recognize has to be restored here.
	slices.SortStableFunc(found, func(a, b ordered) int { return a.at - b.at })
	out := make([]Status, 0, len(found))
	for _, o := range found {
		out = append(out, o.status)
	}
	return out, nil
}
