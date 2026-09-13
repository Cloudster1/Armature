package template

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

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

// Create makes a project the way the template says, in one transaction.
//
// The template fills in what the input leaves open: a kind that was not
// stated, the type of the first board, and a workflow scheme when the template
// brings a workflow and the caller did not name a scheme of their own. A
// workflow the template brings is found by name or made, so the second project
// from the same template shares the first one's rather than getting a copy.
func (s *Service) Create(ctx context.Context, key string, in project.CreateInput, actor uuid.UUID) (*project.Project, db.LSN, error) {
	tpl, ok := Find(key)
	if !ok {
		return nil, 0, fmt.Errorf("%w: %q", ErrUnknown, key)
	}

	if in.Kind == "" {
		in.Kind = tpl.Kind
	}
	in.BoardType = string(tpl.BoardType)
	in.Template = tpl.Key
	in.Features = slices.Clone(tpl.Features)

	var out *project.Project
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if tpl.workflow != nil && in.SchemeID == uuid.Nil {
			schemeID, err := s.ensureScheme(ctx, tx, tpl.workflow)
			if err != nil {
				return err
			}
			in.SchemeID = schemeID
		}
		var err error
		out, err = s.projects.CreateIn(ctx, tx, in, actor)
		if err != nil {
			return err
		}
		if tpl.Desk && s.desk != nil {
			return s.desk.Setup(ctx, tx, out.ID)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
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
			return uuid.Nil, fmt.Errorf("the %s template needs a status called %q, which this organization does not have", spec.name, name)
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
