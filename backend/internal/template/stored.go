package template

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/project"
)

// The limits of what an administrator writes about a template.
const (
	MaxNameLength        = 80
	MaxDescriptionLength = 1000
	// copyTries bounds how many numbered names a duplicate tries before it asks for one.
	copyTries = 50
	// StandardTypeConstraint is the database's rule that a template keeps a standard type.
	StandardTypeConstraint = "project_template_keeps_a_standard_type"
)

var (
	// ErrNotFound is a stored template id that names nothing.
	ErrNotFound = errors.New("that project template was not found")
	// ErrArchived is an archived template asked to make a project.
	ErrArchived = errors.New("that project template is archived; restore it or choose another")
	// ErrBuiltin is a change asked of a template that is code.
	ErrBuiltin = errors.New("a built-in template stays as it is; duplicate it and change the copy")
	// ErrNameTaken is a name another template has already, case aside.
	ErrNameTaken = errors.New("there is already a project template with that name; choose another")
	// ErrInvalid prefixes what is wrong with a template as written.
	ErrInvalid = errors.New("invalid project template")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Actor is who changes a template, for the audit log.
type Actor struct {
	UserID uuid.UUID
	IP     string
}

// TemplateScheme names the workflow scheme a template gives its projects. ID is
// absent for a built-in's scheme that no project has brought in yet.
type TemplateScheme struct {
	ID   *uuid.UUID `json:"id,omitempty"`
	Name string     `json:"name"`
}

// TemplateIssueType is an issue type a template gives its projects.
type TemplateIssueType struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Icon  string    `json:"icon"`
	Level int       `json:"level"`
	// Retired types stay listed on the template and are left out of new projects.
	Retired bool `json:"retired"`
}

// ProjectTemplate is a template as the chooser and the Admin page see it, built in or stored.
type ProjectTemplate struct {
	// Key is the built-in's name in code, or the stored template's id.
	Key            string                 `json:"key"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	Kind           project.Kind           `json:"kind"`
	BoardType      board.Type             `json:"boardType"`
	PlanningMethod project.PlanningMethod `json:"planningMethod"`
	WorkflowName   string                 `json:"workflowName,omitempty"`
	Desk           bool                   `json:"desk"`
	Features       []project.Feature      `json:"features"`
	Builtin        bool                   `json:"builtin"`
	// Scheme is null when projects made from it follow everyone's default scheme.
	Scheme     *TemplateScheme     `json:"scheme"`
	IssueTypes []TemplateIssueType `json:"issueTypes"`
	Archived   bool                `json:"archived"`
	ArchivedAt *time.Time          `json:"archivedAt,omitempty"`
	CreatedAt  *time.Time          `json:"createdAt,omitempty"`
	UpdatedAt  *time.Time          `json:"updatedAt,omitempty"`
}

// stored is a template row as Create applies it.
type stored struct {
	ID             uuid.UUID
	Name           string
	Kind           project.Kind
	BoardType      board.Type
	PlanningMethod project.PlanningMethod
	Features       []project.Feature
	SchemeID       *uuid.UUID
	Desk           bool
	Archived       bool
}

// Input is a stored template as an administrator describes it. On an update a
// nil field is left as it is.
type Input struct {
	Name           *string
	Description    *string
	Kind           *project.Kind
	BoardType      *board.Type
	PlanningMethod *project.PlanningMethod
	Features       []project.Feature
	// SchemeID names a scheme; DefaultScheme true follows everyone's default instead.
	SchemeID      *uuid.UUID
	DefaultScheme bool
	Desk          *bool
	IssueTypeIDs  []uuid.UUID
}

// List offers the built-ins in code order, then the stored templates by name;
// archived ones only when asked for.
func (s *Service) List(ctx context.Context, withArchived bool) ([]ProjectTemplate, error) {
	var out []ProjectTemplate
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		for _, tpl := range all {
			o, err := builtinListed(ctx, tx, tpl)
			if err != nil {
				return err
			}
			out = append(out, o)
		}
		rows, err := tx.Query(ctx, `SELECT id FROM project_template WHERE $1 OR archived_at IS NULL ORDER BY lower(name), id`, withArchived)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		for _, id := range ids {
			o, err := storedListed(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, *o)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func builtinListed(ctx context.Context, tx db.DBTX, tpl Template) (ProjectTemplate, error) {
	o := ProjectTemplate{
		Key: tpl.Key, Name: tpl.Name, Description: tpl.Description, Kind: tpl.Kind, BoardType: tpl.BoardType,
		PlanningMethod: tpl.PlanningMethod, WorkflowName: tpl.WorkflowName, Desk: tpl.Desk,
		Features: slices.Clone(tpl.Features), Builtin: true,
	}
	if tpl.workflow != nil {
		o.Scheme = &TemplateScheme{Name: tpl.workflow.schemeName}
		var id uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM workflow_scheme WHERE lower(name) = lower($1)`, tpl.workflow.schemeName).Scan(&id)
		if err == nil {
			o.Scheme.ID = &id
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return o, err
		}
	}
	types, err := builtinTypes(ctx, tx, tpl)
	if err != nil {
		return o, err
	}
	o.IssueTypes = types
	return o, nil
}

const selectTypeRef = `SELECT t.id, t.name, t.icon, t.hierarchy_level, t.retired_at IS NOT NULL FROM issue_type t`

func scanTypes(rows pgx.Rows) ([]TemplateIssueType, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (TemplateIssueType, error) {
		var t TemplateIssueType
		err := row.Scan(&t.ID, &t.Name, &t.Icon, &t.Level, &t.Retired)
		return t, err
	})
}

// builtinTypes resolves a built-in's type names in the order a new project
// would get them; nil is every built-in type that is not retired.
func builtinTypes(ctx context.Context, tx db.DBTX, tpl Template) ([]TemplateIssueType, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if tpl.IssueTypes == nil {
		rows, err = tx.Query(ctx, selectTypeRef+`
			WHERE t.builtin AND t.retired_at IS NULL ORDER BY t.hierarchy_level DESC, t.position, t.name`)
	} else {
		rows, err = tx.Query(ctx, selectTypeRef+`
			WHERE t.builtin AND t.retired_at IS NULL AND t.name = ANY($1) ORDER BY array_position($1, t.name)`, tpl.IssueTypes)
	}
	if err != nil {
		return nil, err
	}
	return scanTypes(rows)
}

const selectStored = `
SELECT pt.id, pt.name, pt.description, pt.kind, pt.board_type, pt.planning_method, pt.features, pt.desk,
       pt.archived_at, pt.created_at, pt.updated_at, s.id, s.name
FROM project_template pt LEFT JOIN workflow_scheme s ON s.id = pt.workflow_scheme_id`

func storedListed(ctx context.Context, tx db.DBTX, id uuid.UUID) (*ProjectTemplate, error) {
	var (
		o          ProjectTemplate
		features   []string
		schemeID   *uuid.UUID
		schemeName *string
		archived   *time.Time
		created    time.Time
		updated    time.Time
	)
	err := tx.QueryRow(ctx, selectStored+` WHERE pt.id = $1`, id).Scan(&id, &o.Name, &o.Description, &o.Kind, &o.BoardType,
		&o.PlanningMethod, &features, &o.Desk, &archived, &created, &updated, &schemeID, &schemeName)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	o.Key = id.String()
	for _, f := range features {
		o.Features = append(o.Features, project.Feature(f))
	}
	if o.Features == nil {
		o.Features = []project.Feature{}
	}
	if schemeID != nil {
		o.Scheme = &TemplateScheme{ID: schemeID, Name: *schemeName}
	}
	o.Archived, o.ArchivedAt, o.CreatedAt, o.UpdatedAt = archived != nil, archived, &created, &updated
	rows, err := tx.Query(ctx, selectTypeRef+`
		JOIN project_template_issue_type ptt ON ptt.issue_type_id = t.id
		WHERE ptt.template_id = $1 ORDER BY ptt.position, t.name`, id)
	if err != nil {
		return nil, err
	}
	if o.IssueTypes, err = scanTypes(rows); err != nil {
		return nil, err
	}
	return &o, nil
}

// loadStored reads a template for applying it, locked so an archive waits
// for the project being made from it.
func loadStored(ctx context.Context, tx db.DBTX, id uuid.UUID) (*stored, error) {
	var (
		t        stored
		features []string
	)
	err := tx.QueryRow(ctx, `
		SELECT id, name, kind, board_type, planning_method, features, workflow_scheme_id, desk, archived_at IS NOT NULL
		FROM project_template WHERE id = $1 FOR SHARE`, id).Scan(
		&t.ID, &t.Name, &t.Kind, &t.BoardType, &t.PlanningMethod, &features, &t.SchemeID, &t.Desk, &t.Archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	for _, f := range features {
		t.Features = append(t.Features, project.Feature(f))
	}
	return &t, nil
}
