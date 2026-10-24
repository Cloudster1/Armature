package plan

import (
	"context"
	"errors"
	"time"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/milestone"
	"github.com/armature/armature/backend/internal/nql"
	"github.com/armature/armature/backend/internal/project"
)

// Align reads a project's plan and proposes the moves that start every blocked
// issue after its blockers, placing work without dates too when asked. It writes nothing. A query limits which issues may
// move; mayRead says which other projects' blockers the person may see, since a
// blocker they cannot see must not shape what they are shown.
func (s *Service) Align(ctx context.Context, projectKey string, query *nql.Compiled, place bool, mayRead func(projectKey string) bool) (*Alignment, error) {
	tree, err := s.issues.Tree(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	var matched map[string]bool
	if query != nil {
		keys, err := s.issues.Keys(ctx, issue.Filter{ProjectKey: projectKey, Query: query})
		if err != nil {
			return nil, err
		}
		matched = make(map[string]bool, len(keys))
		for _, key := range keys {
			matched[key] = true
		}
	}
	blockers, err := s.issues.Blockers(ctx, projectKey)
	if err != nil {
		return nil, err
	}
	items := FromTree(tree)
	outside, err := s.outsideBlockers(ctx, items, blockers, mayRead)
	if err != nil {
		return nil, err
	}
	today := midnight(s.now().UTC())
	work, err := s.alignWorkdays(ctx, projectKey, items, today)
	if err != nil {
		return nil, err
	}

	proposal := Align(items, AlignInput{Blockers: blockers, Work: work, Today: today, Matched: matched, Outside: outside, Place: place})
	moves, unplaced := proposal.Moves, proposal.Unplaced
	if moves == nil {
		moves = []Move{}
	}
	if unplaced == nil {
		unplaced = []Unplaced{}
	}

	milestones := []milestone.Milestone{}
	if s.milestones != nil {
		if milestones, err = s.milestones.ForProject(ctx, projectKey); err != nil {
			return nil, err
		}
	}
	after := Moved(items, moves)
	warnings := append(Check(after, blockers), CheckMilestones(after, milestones)...)
	if warnings == nil {
		warnings = []Warning{}
	}
	return &Alignment{ProjectKey: project.NormalizeKey(projectKey), Moves: moves, Unplaced: unplaced, Warnings: warnings}, nil
}

// outsideBlockers reads the dates of the blockers that are not rows of the plan
// and sit in a project the person may read.
func (s *Service) outsideBlockers(ctx context.Context, items []Item, blockers []issue.Blocker, mayRead func(string) bool) (map[string]Span, error) {
	rows := map[string]bool{}
	for _, item := range Flatten(items) {
		rows[item.Issue.Key] = true
	}
	out := map[string]Span{}
	for _, b := range blockers {
		key := b.BlockerKey
		if rows[key] || !rows[b.BlockedKey] {
			continue
		}
		if _, read := out[key]; read {
			continue
		}
		projectKey, _, err := issue.ParseKey(key)
		if err != nil || mayRead == nil || !mayRead(projectKey) {
			continue
		}
		found, err := s.issues.ByKey(ctx, key)
		if errors.Is(err, issue.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if found.StartDate != nil && found.DueDate != nil {
			out[key] = Span{Start: *found.StartDate, Due: *found.DueDate}
		}
	}
	return out, nil
}

// alignWorkdays reads the calendars the moves are counted on: the default
// holidays from the earliest work to as far as a chain of moves can reach, and
// each team member's working week.
func (s *Service) alignWorkdays(ctx context.Context, projectKey string, items []Item, today time.Time) (Workdays, error) {
	if s.people == nil {
		return Workdays{}, nil
	}
	from, to := today, today
	if start, due := spanOf(Flatten(items)); start != nil && due != nil {
		from, to = minTime(from, midnight(*start)), maxTime(to, midnight(*due))
	}
	from = maxTime(from, today.AddDate(0, 0, -LoadBackDays))
	to = minTime(to, today.AddDate(0, 0, LoadBackDays)).AddDate(0, 0, AlignReachDays)
	holidays, err := s.people.DefaultHolidays(ctx, from, to)
	if err != nil {
		return Workdays{}, err
	}

	// A week is read only for each person's working week, which every day falls back to.
	week, end := mondayOf(today), mondayOf(today).AddDate(0, 0, daysPerWeek-1)
	people, err := s.people.ProjectPeople(ctx, projectKey, week, end)
	if err != nil {
		return Workdays{}, err
	}
	days, err := s.people.ForPeople(ctx, memberIDs(people.Teams), week, end)
	if err != nil {
		return Workdays{}, err
	}
	return NewWorkdays(holidays, people.Teams, days), nil
}
