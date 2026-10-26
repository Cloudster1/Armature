// Package bulk changes many issues at once, one transaction each, so a refusal
// on one leaves the rest applied and is named in the answer.
package bulk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/project"
)

// MaxSelection bounds one request, so it stays inside the route's timeout.
const MaxSelection = 200

// notFound is the refusal for an issue that is not there and for one the
// caller cannot read, so a refusal never tells which.
const notFound = "That issue was not found."

// Input is what changes on every selected issue. Nil leaves a field alone;
// the inner pointers clear, as in issue.UpdateInput.
type Input struct {
	Priority     *issue.Priority `json:"priority,omitempty"`
	Assignee     *(*uuid.UUID)   `json:"assignee,omitempty"`
	SprintID     *(*uuid.UUID)   `json:"sprintId,omitempty"`
	MilestoneID  *(*uuid.UUID)   `json:"milestoneId,omitempty"`
	TeamID       *(*uuid.UUID)   `json:"teamId,omitempty"`
	AddLabels    []string        `json:"addLabels,omitempty"`
	RemoveLabels []string        `json:"removeLabels,omitempty"`
	FixVersions  *SetEdit        `json:"fixVersions,omitempty"`
	Components   *SetEdit        `json:"components,omitempty"`
	// Fields answers the project's own fields by id; a null clears one.
	Fields map[uuid.UUID]json.RawMessage `json:"fields,omitempty"`
	// Transition is taken by name, so the same word can move issues that
	// follow different workflows; one that does not offer it is refused.
	Transition string `json:"transition,omitempty"`
	// MoveTo and Delete go alone: a move clears what belongs to the old
	// project and gives a new key, and a delete leaves nothing to change.
	MoveTo *MoveTo `json:"moveTo,omitempty"`
	Delete bool    `json:"delete,omitempty"`
}

// SetEdit adds some members of a set and takes others away, leaving the rest.
type SetEdit struct {
	Add    []uuid.UUID `json:"add,omitempty"`
	Remove []uuid.UUID `json:"remove,omitempty"`
}

func (e *SetEdit) empty() bool { return e == nil || len(e.Add)+len(e.Remove) == 0 }

// MoveTo puts every selected issue in another project.
type MoveTo struct {
	ProjectKey string `json:"projectKey"`
	// Statuses maps a status the issues are in to the one they take there;
	// a status left out is kept, and refused where the target lacks it.
	Statuses map[uuid.UUID]uuid.UUID `json:"statuses,omitempty"`
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
	// Moved is every key a move changed, the issues under a moved one included.
	Moved []issue.KeyChange `json:"moved"`
}

// Empty says whether the input asks for anything at all.
func (in Input) Empty() bool {
	return !in.edits() && in.MoveTo == nil && !in.Delete
}

// edits says whether the input changes fields, as opposed to moving or deleting.
func (in Input) edits() bool {
	return in.Priority != nil || in.Assignee != nil || in.SprintID != nil || in.MilestoneID != nil || in.TeamID != nil ||
		len(in.AddLabels) > 0 || len(in.RemoveLabels) > 0 || !in.FixVersions.empty() || !in.Components.empty() ||
		len(in.Fields) > 0 || strings.TrimSpace(in.Transition) != ""
}

// needs is what the caller must hold in an issue's project, the same each
// single-issue route asks for.
func (in Input) needs() []perm.Permission {
	var out []perm.Permission
	if in.Priority != nil || in.Assignee != nil || in.MilestoneID != nil || len(in.AddLabels) > 0 || len(in.RemoveLabels) > 0 ||
		!in.FixVersions.empty() || !in.Components.empty() || len(in.Fields) > 0 || in.MoveTo != nil || in.Delete {
		out = append(out, perm.IssueWrite)
	}
	if in.SprintID != nil {
		out = append(out, perm.SprintManage)
	}
	if in.TeamID != nil {
		out = append(out, perm.TeamManage)
	}
	if strings.TrimSpace(in.Transition) != "" {
		out = append(out, perm.IssueTransition)
	}
	if in.Delete {
		out = append(out, perm.ProjectAdminister)
	}
	return out
}

// Check refuses an input that cannot apply to any issue, before one is touched.
func (in Input) Check() error {
	if in.Empty() {
		return errors.New("say what to change")
	}
	if in.Priority != nil && !in.Priority.Valid() {
		return fmt.Errorf("%q is not a priority", *in.Priority)
	}
	if in.MoveTo != nil && in.Delete {
		return errors.New("move the issues or delete them, not both at once")
	}
	if (in.MoveTo != nil || in.Delete) && in.edits() {
		return errors.New("a move or a delete goes alone; send the other changes in a request of their own")
	}
	if in.MoveTo != nil && strings.TrimSpace(in.MoveTo.ProjectKey) == "" {
		return errors.New("say which project the issues move to")
	}
	for _, removed := range in.RemoveLabels {
		for _, added := range in.AddLabels {
			if strings.EqualFold(strings.TrimSpace(removed), strings.TrimSpace(added)) {
				return fmt.Errorf("%q is both added and removed; keep it in one of the two", strings.TrimSpace(added))
			}
		}
	}
	for _, set := range []*SetEdit{in.FixVersions, in.Components} {
		if set == nil {
			continue
		}
		for _, id := range set.Add {
			if slices.Contains(set.Remove, id) {
				return errors.New("the same version or component is both added and removed; keep it in one of the two")
			}
		}
	}
	return nil
}

type Service struct {
	db     *db.Cluster
	issues *issue.Service
	labels *label.Service
	fields *field.Service
}

func NewService(cluster *db.Cluster, issues *issue.Service, labels *label.Service, fields *field.Service) *Service {
	return &Service{db: cluster, issues: issues, labels: labels, fields: fields}
}

// checkKeys bounds a selection.
func checkKeys(keys []string) error {
	if len(keys) == 0 {
		return errors.New("choose at least one issue")
	}
	if len(keys) > MaxSelection {
		return fmt.Errorf("change at most %d issues at once; this is %d", MaxSelection, len(keys))
	}
	return nil
}

// Edit applies the input to each key in its own transaction; the first step
// that fails is the refusal. The LSN is the latest write, for the next read.
func (s *Service) Edit(ctx context.Context, keys []string, in Input, actor issue.Actor) (*Result, db.LSN, error) {
	if err := checkKeys(keys); err != nil {
		return nil, 0, err
	}
	if err := in.Check(); err != nil {
		return nil, 0, err
	}
	out := &Result{Applied: []string{}, Refused: []Refusal{}, Moved: []issue.KeyChange{}}
	needs := in.needs()
	// carried holds the keys a move or a delete already took along with an
	// issue above them, which then count as applied rather than as gone.
	carried := map[string]bool{}
	type pending struct {
		key string
		err error
	}
	var deferred []pending
	var latest db.LSN
	for _, key := range keys {
		if carried[canonical(key)] {
			out.Applied = append(out.Applied, key)
			continue
		}
		if reason := refusal(key, needs, actor); reason != "" {
			out.Refused = append(out.Refused, Refusal{Key: key, Reason: reason})
			continue
		}
		lsn, moved, gone, err := s.one(ctx, key, in, actor)
		latest = max(latest, lsn)
		if err != nil {
			// A subtask whose parent is also selected moves with it, maybe later in the list.
			if in.MoveTo != nil && (errors.Is(err, issue.ErrSubtaskMovesWithParent) || errors.Is(err, issue.ErrStaysWithParent)) {
				deferred = append(deferred, pending{key, err})
				continue
			}
			out.Refused = append(out.Refused, Refusal{Key: key, Reason: reasonFor(err)})
			continue
		}
		for _, change := range moved {
			carried[change.From] = true
		}
		for _, k := range gone {
			carried[k] = true
		}
		out.Moved = append(out.Moved, moved...)
		out.Applied = append(out.Applied, key)
	}
	for _, d := range deferred {
		if carried[canonical(d.key)] {
			out.Applied = append(out.Applied, d.key)
			continue
		}
		out.Refused = append(out.Refused, Refusal{Key: d.key, Reason: reasonFor(d.err)})
	}
	return out, latest, nil
}

// refusal checks the caller may read the issue and make each change in its
// project, from what they hold, before any row is read.
func refusal(key string, needs []perm.Permission, actor issue.Actor) string {
	projectKey, _, err := issue.ParseKey(key)
	if err != nil {
		return notFound
	}
	projectKey = project.NormalizeKey(projectKey)
	if !actor.Perms.Can(perm.Read, projectKey) {
		return notFound
	}
	for _, p := range needs {
		if actor.Perms.Can(p, projectKey) {
			continue
		}
		if p == perm.IssueWrite {
			return "You cannot change issues in " + projectKey + "."
		}
		return fmt.Sprintf("You need the %s permission in %s to do this.", p, projectKey)
	}
	return ""
}

// reasonFor turns a refusal into a sentence for the person. A database or a
// timeout failing says so in our words, never in the driver's.
func reasonFor(err error) string {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, issue.ErrNotFound) || errors.Is(err, pgx.ErrNoRows):
		return notFound
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		return "The request ran out of time before this issue was changed. Select it again and apply."
	case errors.As(err, &pgErr):
		return "This issue could not be saved. Try again, and tell an administrator if it keeps failing."
	}
	text := capitalize(err.Error())
	if !strings.HasSuffix(text, ".") {
		text += "."
	}
	return text
}

// one makes every change to one issue in one transaction, so a refusal at any
// step leaves the issue, its history and its events as they were.
func (s *Service) one(ctx context.Context, key string, in Input, actor issue.Actor) (db.LSN, []issue.KeyChange, []string, error) {
	var (
		moved []issue.KeyChange
		gone  []string
	)
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var err error
		switch {
		case in.MoveTo != nil:
			// A map, even an empty one, so an unmapped status is named in the refusal.
			statuses := in.MoveTo.Statuses
			if statuses == nil {
				statuses = map[uuid.UUID]uuid.UUID{}
			}
			_, moved, err = s.issues.MoveIn(ctx, tx, key, issue.MoveInput{ProjectKey: in.MoveTo.ProjectKey, Statuses: statuses}, actor)
			return err
		case in.Delete:
			gone, err = s.issues.DeleteIn(ctx, tx, key, actor)
			return err
		}
		return s.edit(ctx, tx, key, in, actor)
	})
	if err != nil {
		return lsn, nil, nil, err
	}
	return lsn, moved, gone, nil
}

func (s *Service) edit(ctx context.Context, tx db.DBTX, key string, in Input, actor issue.Actor) error {
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
	if !in.FixVersions.empty() {
		if err := note(s.issues.ChangeFixVersionsIn(ctx, tx, key, in.FixVersions.Add, in.FixVersions.Remove)); err != nil {
			return err
		}
	}
	if !in.Components.empty() {
		if err := note(s.issues.ChangeComponentsIn(ctx, tx, key, in.Components.Add, in.Components.Remove)); err != nil {
			return err
		}
	}
	// In id order, so the history reads the same however the map was sent.
	ids := make([]uuid.UUID, 0, len(in.Fields))
	for id := range in.Fields {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for _, id := range ids {
		changedID, _, more, err := s.fields.SetIn(ctx, tx, key, id, in.Fields[id])
		if err != nil {
			return err
		}
		issueID = changedID
		changes = append(changes, more...)
	}
	if len(in.AddLabels) > 0 || len(in.RemoveLabels) > 0 {
		id, _, more, err := s.labels.ChangeIssueLabelsIn(ctx, tx, key, in.AddLabels, in.RemoveLabels)
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
}

// Transitions names the transitions every selected issue the caller can read
// offers, in the first one's order, so one name can be picked for them all.
func (s *Service) Transitions(ctx context.Context, keys []string, actor issue.Actor) ([]string, error) {
	if err := checkKeys(keys); err != nil {
		return nil, err
	}
	var shared []string
	started := false
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		for _, key := range keys {
			if refusal(key, nil, actor) != "" {
				continue
			}
			_, offered, err := s.issues.TransitionsIn(ctx, tx, key, actor)
			if errors.Is(err, issue.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			var here []string
			for _, t := range offered {
				if !containsFold(here, t.Name) {
					here = append(here, t.Name)
				}
			}
			if !started {
				shared, started = here, true
				continue
			}
			shared = slices.DeleteFunc(shared, func(name string) bool { return !containsFold(here, name) })
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if shared == nil {
		shared = []string{}
	}
	return shared, nil
}

func containsFold(names []string, name string) bool {
	return slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, name) })
}

// canonical spells a key the way events and moves carry it.
func canonical(key string) string {
	projectKey, num, err := issue.ParseKey(key)
	if err != nil {
		return key
	}
	return issue.FormatKey(projectKey, num)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
