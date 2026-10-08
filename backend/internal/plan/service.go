package plan

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/availability"
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

// AvailabilityReader is who can work when, declared the same way.
type AvailabilityReader interface {
	ProjectPeople(ctx context.Context, projectKey string, from, to time.Time) (*availability.ProjectPeople, error)
	ForPeople(ctx context.Context, ids []uuid.UUID, from, to time.Time) (map[uuid.UUID]availability.Person, error)
	DefaultHolidays(ctx context.Context, from, to time.Time) ([]availability.Holiday, error)
}

// Service assembles a project's plan. It owns no tables of its own: a plan is a
// reading of the issues, their hierarchy, their links and their sprints, not a
// second copy of any of them.
type Service struct {
	issues     *issue.Service
	sprints    SprintReader
	milestones MilestoneReader
	teams      TeamReader
	people     AvailabilityReader
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

// WithAvailability makes the load and the sprints count holidays and absences.
func (s *Service) WithAvailability(reader AvailabilityReader) *Service {
	s.people = reader
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
	loadFrom, loadTo := LoadSpan(from, to, s.now().UTC())
	work, holidays, err := s.workdays(ctx, projectKey, items, loadFrom, loadTo)
	if err != nil {
		return nil, err
	}
	load := Loads(items, teams, loadFrom, loadTo, work)
	TeamHours(sprints, work)

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
		Holidays:     holidays,
		Warnings:     warnings,
		From:         from,
		To:           to,
		Unscheduled:  Unscheduled(items),
		Unestimated:  Unestimated(items),
		LinkTypes:    linkTypes,
		Matched:      matched,
	}, nil
}

// workdays reads who on the project's teams works when over the window's
// weeks, and the default calendar's holidays over every scheduled range.
func (s *Service) workdays(ctx context.Context, projectKey string, items []Item, from, to time.Time) (Workdays, []availability.Holiday, error) {
	shown := []availability.Holiday{}
	if s.people == nil {
		return Workdays{}, shown, nil
	}
	first, last := mondayOf(from), mondayOf(to).AddDate(0, 0, daysPerWeek-1)
	reach, until := first, last
	if start, due := spanOf(Flatten(items)); start != nil && due != nil {
		reach, until = minTime(reach, midnight(*start)), maxTime(until, midnight(*due))
	}
	reach = maxTime(reach, first.AddDate(0, 0, -LoadBackDays))
	until = minTime(until, last.AddDate(0, 0, LoadBackDays))
	holidays, err := s.people.DefaultHolidays(ctx, reach, until)
	if err != nil {
		return Workdays{}, nil, err
	}
	people, err := s.people.ProjectPeople(ctx, projectKey, first, last)
	if err != nil {
		return Workdays{}, nil, err
	}
	var ids []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, members := range people.Teams {
		for _, id := range members {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	days, err := s.people.ForPeople(ctx, ids, first, last)
	if err != nil {
		return Workdays{}, nil, err
	}
	since, through := midnight(from).Format(availability.DateLayout), midnight(to).Format(availability.DateLayout)
	for _, h := range holidays {
		if h.Day >= since && h.Day <= through {
			shown = append(shown, h)
		}
	}
	return NewWorkdays(holidays, people.Teams, days), shown, nil
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
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
