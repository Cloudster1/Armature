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
	db     *db.Cluster
	issues *issue.Service
	labels *label.Service
}

func NewService(cluster *db.Cluster, issues *issue.Service, labels *label.Service) *Service {
	return &Service{db: cluster, issues: issues, labels: labels}
}

// Edit applies the input to each key in its own transaction; the first step
// that fails is the refusal. The LSN is the latest write, for the next read.
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

// one makes every change to one issue in one transaction, so a refusal at any
// step leaves the issue, its history and its events as they were.
func (s *Service) one(ctx context.Context, key string, in Input, actor issue.Actor) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			issueID uuid.UUID
			changes []issue.Change
		)
		note := func(changed *issue.Issue, more []issue.Change, err error) error {
			if err != nil {
				return err
			}
			issueID = changed.ID
			changes = append(changes, more...)
			return nil
		}
		if in.Priority != nil || in.Assignee != nil {
			if err := note(s.issues.UpdateIn(ctx, tx, key, issue.UpdateInput{Priority: in.Priority, Assignee: in.Assignee}, actor)); err != nil {
				return err
			}
		}
		// The team goes before the sprint: a sprint takes only its own team's work,
		// and a new team takes the issue out of the old team's sprint.
		if in.TeamID != nil {
			if err := note(s.issues.SetTeamIn(ctx, tx, key, *in.TeamID)); err != nil {
				return err
			}
		}
		if in.SprintID != nil {
			if err := note(s.issues.SetSprintIn(ctx, tx, key, *in.SprintID)); err != nil {
				return err
			}
		}
		if in.MilestoneID != nil {
			if err := note(s.issues.SetMilestoneIn(ctx, tx, key, *in.MilestoneID)); err != nil {
				return err
			}
		}
		if len(in.AddLabels) > 0 {
			id, _, more, err := s.labels.AddIssueLabelsIn(ctx, tx, key, in.AddLabels)
			if err != nil {
				return err
			}
			issueID = id
			changes = append(changes, more...)
		}
		if len(changes) > 0 {
			// One entry and one event for all the fields, as one edit makes.
			if err := issue.AnnounceUpdate(ctx, tx, issueID, canonical(key), actor, changes); err != nil {
				return err
			}
		}

		name := strings.TrimSpace(in.Transition)
		if name == "" {
			return nil
		}
		current, offered, err := s.issues.TransitionsIn(ctx, tx, key, actor)
		if err != nil {
			return err
		}
		var id uuid.UUID
		for _, t := range offered {
			if strings.EqualFold(t.Name, name) {
				id = t.ID
			}
		}
		if id == uuid.Nil {
			return fmt.Errorf("%q is not offered from %s", name, current.Status.Name)
		}
		_, err = s.issues.TransitionIn(ctx, tx, key, issue.TransitionInput{TransitionID: id}, actor)
		return err
	})
}

// canonical spells a key the way events carry it; Edit has parsed it already.
func canonical(key string) string {
	projectKey, num, _ := issue.ParseKey(key)
	return issue.FormatKey(projectKey, num)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
