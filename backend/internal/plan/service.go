package plan

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/milestone"
	"github.com/armature/armature/backend/internal/nql"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/team"
)

// Sprints is the reader the plan needs. Declared here rather than imported as a
// concrete service so that the plan depends on the behaviour it uses.
type SprintReader interface {
	ForProject(ctx context.Context, projectKey string) ([]sprint.Sprint, error)
}

// TeamReader is the teams the load is read per, declared the same way.
type TeamReader interface {
	List(ctx context.Context, projectKey string) ([]team.Team, error)
}

// Service assembles a project's plan. It owns no tables of its own: a plan is a
// reading of the issues, their hierarchy, their links and their sprints, not a
// second copy of any of them.
type Service struct {
	issues     *issue.Service
	sprints    SprintReader
	milestones MilestoneReader
	teams      TeamReader
	// now is injectable so the window a plan opens on can be tested.
	now func() time.Time
}

func NewService(issues *issue.Service, sprints SprintReader) *Service {
	return &Service{issues: issues, sprints: sprints, now: time.Now}
}

// WithMilestones makes the plan draw a project's milestones as well.
func (s *Service) WithMilestones(reader MilestoneReader) *Service {
	s.milestones = reader
	return s
}

// WithTeams makes the plan read the load per team as well.
func (s *Service) WithTeams(reader TeamReader) *Service {
	s.teams = reader
	return s
}

// ForProject reads the whole plan: the tree, the dependencies, the link types
// the client offers when adding one, and the sprints the work is committed to.
func (s *Service) ForProject(ctx context.Context, projectKey string) (*Plan, error) {
	return s.ForProjectMatching(ctx, projectKey, nil)
}

// ForProjectMatching is the whole plan plus the keys a query selects. The plan
// itself is not pruned: the totals and the warnings stay about the project,
// and the client cuts the rows, keeping what stands above a match.
func (s *Service) ForProjectMatching(ctx context.Context, projectKey string, query *nql.Compiled) (*Plan, error) {
	tree, err := s.issues.Tree(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	matched := []string{}
	if query != nil {
		matched, err = s.issues.Keys(ctx, issue.Filter{ProjectKey: projectKey, Query: query})
		if err != nil {
			return nil, err
		}
	}
	blockers, err := s.issues.Blockers(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	linkTypes, err := s.issues.ListLinkTypes(ctx)
	if err != nil {
		return nil, err
	}

	items := FromTree(tree)
	from, to := Window(items, s.now().UTC())

	sprints := []SprintPlan{}
	if s.sprints != nil {
		found, err := s.sprints.ForProject(ctx, projectKey)
		if err != nil {
			return nil, err
		}
		sprints = Sprints(items, found)
	}

	milestones := []milestone.Milestone{}
	if s.milestones != nil {
		found, err := s.milestones.ForProject(ctx, projectKey)
		if err != nil {
			return nil, err
		}
		milestones = found
	}

	// The calendar shows the sprints and the milestones as well as the work,
	// so it has to reach as far as they do.
	var edges []*time.Time
	for _, s := range sprints {
		edges = append(edges, s.Sprint.StartsOn, s.Sprint.EndsOn)
	}
	for _, m := range milestones {
		edges = append(edges, m.DueOn)
	}
	from, to = Widen(from, to, edges...)
	teams := []team.Team{}
	if s.teams != nil {
		found, err := s.teams.List(ctx, projectKey)
		if err != nil {
			return nil, err
		}
		teams = found
	}
	load := Loads(items, teams, from, to)

	warnings := append(Check(items, blockers), CheckSprints(items, sprints)...)
	warnings = append(warnings, CheckMilestones(items, milestones)...)
	warnings = append(warnings, CheckLoad(load)...)
	if warnings == nil {
		warnings = []Warning{}
	}

	return &Plan{
		ProjectKey:   project.NormalizeKey(projectKey),
		Items:        items,
		Dependencies: blockers,
		Sprints:      sprints,
		Milestones:   milestones,
		Load:         load,
		Warnings:     warnings,
		From:         from,
		To:           to,
		Unscheduled:  Unscheduled(items),
		Unestimated:  Unestimated(items),
		LinkTypes:    linkTypes,
		Matched:      matched,
	}, nil
}

// SprintTotals counts what one sprint holds, so that completing it can record a
// number that will not drift when the issues in it are edited later.
func (s *Service) SprintTotals(ctx context.Context, projectKey string, sprintID uuid.UUID) (sprint.Totals, error) {
	tree, err := s.issues.Tree(ctx, projectKey)
	if err != nil {
		return sprint.Totals{}, err
	}
	counted := Sprints(FromTree(tree), []sprint.Sprint{{ID: sprintID}})[0]
	return sprint.Totals{
		Committed: counted.Committed, Completed: counted.Completed,
		Issues: counted.Issues, IssuesDone: counted.IssuesDone, Unestimated: counted.Unestimated,
	}, nil
}
