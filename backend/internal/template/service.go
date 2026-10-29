package template

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/workflow"
)

// Service makes projects from templates.
type Service struct {
	db       *db.Cluster
	projects *project.Service
	admin    *workflow.Admin
	// desk sets up a service project's request types and goals. Nil means
	// the service desk template is on offer but sets nothing up, which is
	// only right in a test that does not care.
	desk DeskSetup
}

// DeskSetup is what a service desk template needs done for a new project.
type DeskSetup interface {
	Setup(ctx context.Context, tx db.DBTX, projectID uuid.UUID) error
}

func NewService(cluster *db.Cluster, projects *project.Service, admin *workflow.Admin) *Service {
	return &Service{db: cluster, projects: projects, admin: admin}
}

// WithDesk gives the service the desk to set service projects up with.
func (s *Service) WithDesk(d DeskSetup) *Service {
	s.desk = d
	return s
}

// Create makes a project the way the template says, in one transaction. The
// key is a built-in's name or a stored template's id.
//
// The template fills in what the input leaves open: a kind that was not
// stated, the type of the first board, a workflow scheme when the template
// has one and the caller did not name a scheme of their own, and the issue
// types. A workflow a built-in brings is found by name or made, so the second
// project from the same template shares the first one's rather than getting a copy.
func (s *Service) Create(ctx context.Context, key string, in project.CreateInput, actor uuid.UUID) (*project.Project, db.LSN, error) {
	storedID, isStored := uuid.Nil, false
	var tpl Template
	if id, err := uuid.Parse(key); err == nil {
		storedID, isStored = id, true
	} else {
		var ok bool
		if tpl, ok = Find(key); !ok {
			return nil, 0, fmt.Errorf("%w: %q", ErrUnknown, key)
		}
	}

	var out *project.Project
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			types []uuid.UUID
			desk  bool
		)
		if isStored {
			st, err := loadStored(ctx, tx, storedID)
			if err != nil {
				return err
			}
			if st.Archived {
				return ErrArchived
			}
			if in.Kind == "" {
				in.Kind = st.Kind
			}
			in.BoardType = string(st.BoardType)
			in.PlanningMethod = st.PlanningMethod
			in.Template = st.ID.String()
			in.Features = slices.Clone(st.Features)
			if st.SchemeID != nil && in.SchemeID == uuid.Nil {
				in.SchemeID = *st.SchemeID
			}
			// A type retired since the template was written goes into nothing new.
			rows, err := tx.Query(ctx, `
				SELECT ptt.issue_type_id FROM project_template_issue_type ptt JOIN issue_type t ON t.id = ptt.issue_type_id
				WHERE ptt.template_id = $1 AND t.retired_at IS NULL ORDER BY ptt.position, t.name`, st.ID)
			if err != nil {
				return err
			}
			if types, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID]); err != nil {
				return err
			}
			desk = st.Desk
		} else {
			if in.Kind == "" {
				in.Kind = tpl.Kind
			}
			in.BoardType = string(tpl.BoardType)
			in.PlanningMethod = tpl.PlanningMethod
			in.Template = tpl.Key
			in.Features = slices.Clone(tpl.Features)
			if tpl.workflow != nil && in.SchemeID == uuid.Nil {
				schemeID, err := s.ensureScheme(ctx, tx, tpl.workflow)
				if err != nil {
					return err
				}
				in.SchemeID = schemeID
			}
			// Every built-in type is what the project gets on insert already.
			if tpl.IssueTypes != nil {
				named, err := builtinTypes(ctx, tx, tpl)
				if err != nil {
					return err
				}
				for _, t := range named {
					types = append(types, t.ID)
				}
			}
			desk = tpl.Desk
		}

		var err error
		out, err = s.projects.CreateIn(ctx, tx, in, actor)
		if err != nil {
			return err
		}
		if types != nil {
			if err := allowOnly(ctx, tx, out.ID, types); err != nil {
				return err
			}
		}
		if desk && s.desk != nil {
			return s.desk.Setup(ctx, tx, out.ID)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// allowOnly replaces the types a new project was given on insert with the
// template's. The rule that a project keeps a standard type is checked at commit.
func allowOnly(ctx context.Context, tx db.DBTX, projectID uuid.UUID, types []uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM project_issue_type WHERE project_id = $1`, projectID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO project_issue_type (project_id, issue_type_id, position)
		SELECT $1, id, position FROM unnest($2::uuid[]) WITH ORDINALITY AS chosen (id, position)`, projectID, types)
	return err
}

// ensureScheme resolves a template's workflow against the organization's own
// statuses and returns the scheme that maps every issue type onto it.
func (s *Service) ensureScheme(ctx context.Context, tx db.DBTX, spec *workflowSpec) (uuid.UUID, error) {
	statuses, err := statusesByName(ctx, tx)
	if err != nil {
		return uuid.Nil, err
	}
	// The statuses a template brings are made if the organization lacks
	// them, after everything it already has, so the board reads in order.
	for _, st := range spec.statuses {
		if _, ok := statuses[st.name]; ok {
			continue
		}
		id, err := workflow.InsertStatus(ctx, tx, workflow.StatusInput{Name: st.name, Category: st.category, Description: st.description})
		if err != nil {
			return uuid.Nil, err
		}
		statuses[st.name] = id
	}

	// The statuses are looked up by the names the organization started with.
	// An organization that has renamed one away cannot use this template until
	// the name is back, and is told which name that is rather than handed a
	// workflow with a hole in it.
	need := func(name string) (uuid.UUID, error) {
		id, ok := statuses[name]
		if !ok {
			return uuid.Nil, fmt.Errorf("the %s template needs a status called %q, and there is none", spec.name, name)
		}
		return id, nil
	}

	graph := workflow.GraphInput{Name: spec.name, Description: spec.description}
	for _, step := range spec.steps {
		id, err := need(step.status)
		if err != nil {
			return uuid.Nil, err
		}
		graph.Steps = append(graph.Steps, workflow.StepInput{StatusID: id, IsInitial: step.initial})
	}
	for _, t := range spec.transitions {
		to, err := need(t.to)
		if err != nil {
			return uuid.Nil, err
		}
		in := workflow.TransitionInput{Name: t.name, ToStatusID: to, Rules: t.rules}
		if t.from != "" {
			from, err := need(t.from)
			if err != nil {
				return uuid.Nil, err
			}
			in.FromStatusID = &from
		}
		graph.Transitions = append(graph.Transitions, in)
	}

	workflowID, err := s.admin.EnsureWorkflow(ctx, tx, graph)
	if err != nil {
		return uuid.Nil, fmt.Errorf("bring the template's workflow: %w", err)
	}
	schemeID, err := s.admin.EnsureScheme(ctx, tx, workflow.SchemeInput{
		Name:  spec.schemeName,
		Items: []workflow.SchemeItemInput{{WorkflowID: workflowID}},
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("bring the template's scheme: %w", err)
	}
	return schemeID, nil
}

func statusesByName(ctx context.Context, tx db.DBTX) (map[string]uuid.UUID, error) {
	rows, err := tx.Query(ctx, `SELECT id, name FROM issue_status`)
	if err != nil {
		return nil, fmt.Errorf("read the organization's statuses: %w", err)
	}
	defer rows.Close()

	out := map[string]uuid.UUID{}
	for rows.Next() {
		var (
			id   uuid.UUID
			name string
		)
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[name] = id
	}
	return out, rows.Err()
}
