// Package bulk changes many issues at once, one transaction each, so a refusal
// on one leaves the rest applied and is named in the answer.
package bulk

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
)

// MaxSelection bounds one request, so it stays inside the route's timeout.
const MaxSelection = 200

// Input is what changes on every selected issue. Nil leaves a field alone;
// the inner pointers clear, as in issue.UpdateInput.
type Input struct {
	Priority    *issue.Priority `json:"priority,omitempty"`
	Assignee    *(*uuid.UUID)   `json:"assignee,omitempty"`
	SprintID    *(*uuid.UUID)   `json:"sprintId,omitempty"`
	MilestoneID *(*uuid.UUID)   `json:"milestoneId,omitempty"`
	TeamID      *(*uuid.UUID)   `json:"teamId,omitempty"`
	AddLabels   []string        `json:"addLabels,omitempty"`
	// Transition is taken by name, so the same word can move issues that
	// follow different workflows; one that does not offer it is refused.
	Transition string `json:"transition,omitempty"`
}

// Refusal is one issue that was left as it was, and why.
type Refusal struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
}

// Result says which issues changed and which did not.
type Result struct {
	Applied []string  `json:"applied"`
	Refused []Refusal `json:"refused"`
}

// Empty says whether the input asks for anything at all.
func (in Input) Empty() bool {
	return in.Priority == nil && in.Assignee == nil && in.SprintID == nil && in.MilestoneID == nil && in.TeamID == nil && len(in.AddLabels) == 0 && strings.TrimSpace(in.Transition) == ""
}

type Service struct {
	issues *issue.Service
	labels *label.Service
}

func NewService(issues *issue.Service, labels *label.Service) *Service {
	return &Service{issues: issues, labels: labels}
}

// Edit applies the input to each key in turn. Every issue is its own
// transaction; the first thing that fails on an issue is its refusal. The
// LSN is the latest write, so the caller's next read sees them all.
func (s *Service) Edit(ctx context.Context, keys []string, in Input, actor issue.Actor) (*Result, db.LSN, error) {
	if len(keys) == 0 {
		return nil, 0, errors.New("choose at least one issue")
	}
	if len(keys) > MaxSelection {
		return nil, 0, fmt.Errorf("change at most %d issues at once; this is %d", MaxSelection, len(keys))
	}
	if in.Empty() {
		return nil, 0, errors.New("say what to change")
	}
	if in.Priority != nil && !in.Priority.Valid() {
		return nil, 0, fmt.Errorf("%q is not a priority", *in.Priority)
	}
	out := &Result{Applied: []string{}, Refused: []Refusal{}}
	var latest db.LSN
	for _, key := range keys {
		lsn, err := s.one(ctx, key, in, actor)
		latest = max(latest, lsn)
		if err != nil {
			out.Refused = append(out.Refused, Refusal{Key: key, Reason: capitalize(err.Error())})
			continue
		}
		out.Applied = append(out.Applied, key)
	}
	return out, latest, nil
}

func (s *Service) one(ctx context.Context, key string, in Input, actor issue.Actor) (db.LSN, error) {
	var latest db.LSN
	note := func(lsn db.LSN, err error) error {
		latest = max(latest, lsn)
		return err
	}
	if in.Priority != nil || in.Assignee != nil {
		_, lsn, err := s.issues.Update(ctx, key, issue.UpdateInput{Priority: in.Priority, Assignee: in.Assignee}, actor)
		if err := note(lsn, err); err != nil {
			return latest, err
		}
	}
	if in.SprintID != nil {
		_, lsn, err := s.issues.SetSprint(ctx, key, *in.SprintID, actor)
		if err := note(lsn, err); err != nil {
			return latest, err
		}
	}
	if in.MilestoneID != nil {
		_, lsn, err := s.issues.SetMilestone(ctx, key, *in.MilestoneID, actor)
		if err := note(lsn, err); err != nil {
			return latest, err
		}
	}
	if in.TeamID != nil {
		_, lsn, err := s.issues.SetTeam(ctx, key, *in.TeamID, actor)
		if err := note(lsn, err); err != nil {
			return latest, err
		}
	}
	if len(in.AddLabels) > 0 {
		current, err := s.issues.ByKey(ctx, key)
		if err != nil {
			return latest, err
		}
		names := []string{}
		for _, l := range current.Labels {
			names = append(names, l.Name)
		}
		names = append(names, in.AddLabels...)
		_, lsn, err := s.labels.SetIssueLabels(ctx, key, names, actor)
		if err := note(lsn, err); err != nil {
			return latest, err
		}
	}
	if name := strings.TrimSpace(in.Transition); name != "" {
		offered, err := s.issues.Transitions(ctx, key, actor)
		if err != nil {
			return latest, err
		}
		var id uuid.UUID
		for _, t := range offered {
			if strings.EqualFold(t.Name, name) {
				id = t.ID
			}
		}
		if id == uuid.Nil {
			current, err := s.issues.ByKey(ctx, key)
			if err != nil {
				return latest, err
			}
			return latest, fmt.Errorf("%q is not offered from %s", name, current.Status.Name)
		}
		_, lsn, err := s.issues.Transition(ctx, key, issue.TransitionInput{TransitionID: id}, actor)
		if err := note(lsn, err); err != nil {
			return latest, err
		}
	}
	return latest, nil
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
