package plan

import (
	"context"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/team"
)

// Resources reads a project's resource view over whole weeks, set against the
// teams or the people as the project plans.
func (s *Service) Resources(ctx context.Context, p *project.Project, fromText, toText string) (*ResourcePlan, error) {
	from, to, err := ResourceWindow(fromText, toText, s.now().UTC())
	if err != nil {
		return nil, err
	}
	tree, err := s.issues.Tree(ctx, p.Key)
	if err != nil {
		return nil, err
	}
	open := openIssues(tree)
	teams := []team.Team{}
	if s.teams != nil {
		if teams, err = s.teams.List(ctx, p.Key); err != nil {
			return nil, err
		}
	}
	in := ResourceInput{Grouping: p.ResourceGrouping, From: from, To: to, Issues: open, Teams: teams}
	if s.people != nil {
		if err := s.resourcePeople(ctx, p.Key, &in); err != nil {
			return nil, err
		}
	}
	out := ReadResources(in)
	out.ProjectKey, out.Method = p.Key, p.PlanningMethod
	return &out, nil
}

// resourcePeople reads who works when: everybody on the project's teams or
// assigned its work, over the window and the days its work reaches into.
func (s *Service) resourcePeople(ctx context.Context, projectKey string, in *ResourceInput) error {
	reach, until := in.From, in.To
	for _, i := range in.Issues {
		if i.StartDate != nil && i.DueDate != nil {
			reach, until = minTime(reach, midnight(*i.StartDate)), maxTime(until, midnight(*i.DueDate))
		}
	}
	reach = maxTime(reach, in.From.AddDate(0, 0, -reachDays))
	until = minTime(until, in.To.AddDate(0, 0, reachDays))

	holidays, err := s.people.DefaultHolidays(ctx, reach, until)
	if err != nil {
		return err
	}
	people, err := s.people.ProjectPeople(ctx, projectKey, in.From, in.To)
	if err != nil {
		return err
	}
	ids := people.IDs()
	for _, i := range in.Issues {
		if i.Assignee != nil {
			ids = append(ids, i.Assignee.ID)
		}
	}
	// An assignee who has left keeps their issues but has no week here.
	members, err := s.people.Members(ctx, uniqueIDs(ids))
	if err != nil {
		return err
	}
	ids = ids[:0]
	for id := range members {
		ids = append(ids, id)
	}
	days, err := s.people.ForPeople(ctx, ids, reach, until)
	if err != nil {
		return err
	}
	shares, err := s.people.Shares(ctx, projectKey)
	if err != nil {
		return err
	}
	in.People, in.Work, in.Shares = members, NewWorkdays(holidays, people.Teams, days), shares
	return nil
}

// openIssues is every issue of the tree that is not done.
func openIssues(nodes []issue.Node) []issue.Issue {
	var out []issue.Issue
	for _, n := range nodes {
		if !n.Issue.IsDone() {
			out = append(out, n.Issue)
		}
		out = append(out, openIssues(n.Children)...)
	}
	return out
}

func uniqueIDs(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
