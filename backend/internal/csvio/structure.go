package csvio

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/component"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/team"
	"github.com/armature/armature/backend/internal/version"
)

// planning are the services that hold what an issue belongs to. Without them
// those columns are reported and skipped rather than guessed at.
type planning struct {
	sprints    *sprint.Service
	versions   *version.Service
	components *component.Service
	teams      *team.Service
}

// WithPlanning lets an import put an issue in the sprint, version, component
// and team the file names, making each by name where it is missing.
func (s *Service) WithPlanning(sprints *sprint.Service, versions *version.Service, components *component.Service, teams *team.Service) *Service {
	s.plan = planning{sprints: sprints, versions: versions, components: components, teams: teams}
	return s
}

// named is what the project already calls things, so a file's name is looked
// up once and made at most once.
type named struct {
	sprints    map[string]uuid.UUID
	versions   map[string]uuid.UUID
	components map[string]uuid.UUID
	teams      map[string]uuid.UUID
}

func (s *Service) namesInProject(ctx context.Context, projectKey string) (*named, error) {
	n := &named{
		sprints: map[string]uuid.UUID{}, versions: map[string]uuid.UUID{},
		components: map[string]uuid.UUID{}, teams: map[string]uuid.UUID{},
	}
	if s.plan.sprints != nil {
		sprints, err := s.plan.sprints.List(ctx, projectKey, true)
		if err != nil {
			return nil, err
		}
		for _, each := range sprints {
			n.sprints[strings.ToLower(each.Name)] = each.ID
		}
	}
	if s.plan.versions != nil {
		versions, err := s.plan.versions.List(ctx, projectKey, true)
		if err != nil {
			return nil, err
		}
		for _, each := range versions {
			n.versions[strings.ToLower(each.Name)] = each.ID
		}
	}
	if s.plan.components != nil {
		components, err := s.plan.components.List(ctx, projectKey)
		if err != nil {
			return nil, err
		}
		for _, each := range components {
			n.components[strings.ToLower(each.Name)] = each.ID
		}
	}
	if s.plan.teams != nil {
		teams, err := s.plan.teams.List(ctx, projectKey)
		if err != nil {
			return nil, err
		}
		for _, each := range teams {
			n.teams[strings.ToLower(each.Name)] = each.ID
		}
	}
	return n, nil
}

// belonging is one row's places: the sprint, versions, components and team it
// names, made where the project does not have them yet.
type belonging struct {
	sprint     *uuid.UUID
	team       *uuid.UUID
	fix        []uuid.UUID
	affects    []uuid.UUID
	components []uuid.UUID
}

func (s *Service) placesFor(ctx context.Context, projectKey string, row Row, req ImportRequest,
	n *named, actor uuid.UUID, report *ImportReport) belonging {
	var out belonging
	if name := row.value(req.Mapping, "sprint"); name != "" && s.plan.sprints != nil {
		if id := s.sprintNamed(ctx, projectKey, name, req.DryRun, n, actor, report); id != uuid.Nil {
			out.sprint = &id
		}
	}
	if name := row.value(req.Mapping, "team"); name != "" && s.plan.teams != nil {
		if id := s.teamNamed(ctx, projectKey, name, req.DryRun, n, actor, report); id != uuid.Nil {
			out.team = &id
		}
	}
	for target, into := range map[string]*[]uuid.UUID{"fixVersions": &out.fix, "affectsVersions": &out.affects} {
		for _, cell := range row.values(req.Mapping, target) {
			for _, name := range splitList(cell) {
				if id := s.versionNamed(ctx, projectKey, name, req.DryRun, n, actor, report); id != uuid.Nil {
					*into = append(*into, id)
				}
			}
		}
	}
	for _, cell := range row.values(req.Mapping, "components") {
		for _, name := range splitList(cell) {
			if id := s.componentNamed(ctx, projectKey, name, req.DryRun, n, report); id != uuid.Nil {
				out.components = append(out.components, id)
			}
		}
	}
	return out
}

// The four are the same shape: look the name up, make it if the project has no
// such thing, and say in the report that it was made.
func (s *Service) sprintNamed(ctx context.Context, projectKey, name string, dry bool, n *named, actor uuid.UUID, report *ImportReport) uuid.UUID {
	if id, ok := n.sprints[strings.ToLower(name)]; ok {
		return id
	}
	if dry {
		note(report, fmt.Sprintf("A sprint called %s would be added to this project.", name))
		return uuid.Nil
	}
	made, _, err := s.plan.sprints.Create(ctx, projectKey, sprint.CreateInput{Name: name}, actor)
	if err != nil {
		note(report, fmt.Sprintf("The sprint %s could not be added: %v", name, err))
		return uuid.Nil
	}
	n.sprints[strings.ToLower(name)] = made.ID
	note(report, fmt.Sprintf("The sprint %s was added to this project.", name))
	return made.ID
}

func (s *Service) versionNamed(ctx context.Context, projectKey, name string, dry bool, n *named, actor uuid.UUID, report *ImportReport) uuid.UUID {
	if id, ok := n.versions[strings.ToLower(name)]; ok {
		return id
	}
	if dry || s.plan.versions == nil {
		note(report, fmt.Sprintf("A version called %s would be added to this project.", name))
		return uuid.Nil
	}
	made, _, err := s.plan.versions.Create(ctx, projectKey, version.Input{Name: &name}, actor)
	if err != nil {
		note(report, fmt.Sprintf("The version %s could not be added: %v", name, err))
		return uuid.Nil
	}
	n.versions[strings.ToLower(name)] = made.ID
	note(report, fmt.Sprintf("The version %s was added to this project.", name))
	return made.ID
}

func (s *Service) componentNamed(ctx context.Context, projectKey, name string, dry bool, n *named, report *ImportReport) uuid.UUID {
	if id, ok := n.components[strings.ToLower(name)]; ok {
		return id
	}
	if dry || s.plan.components == nil {
		note(report, fmt.Sprintf("A component called %s would be added to this project.", name))
		return uuid.Nil
	}
	made, _, err := s.plan.components.Create(ctx, projectKey, component.Input{Name: &name})
	if err != nil {
		note(report, fmt.Sprintf("The component %s could not be added: %v", name, err))
		return uuid.Nil
	}
	n.components[strings.ToLower(name)] = made.ID
	note(report, fmt.Sprintf("The component %s was added to this project.", name))
	return made.ID
}

func (s *Service) teamNamed(ctx context.Context, projectKey, name string, dry bool, n *named, actor uuid.UUID, report *ImportReport) uuid.UUID {
	if id, ok := n.teams[strings.ToLower(name)]; ok {
		return id
	}
	if dry {
		note(report, fmt.Sprintf("A team called %s would be added to this project.", name))
		return uuid.Nil
	}
	made, _, err := s.plan.teams.Create(ctx, projectKey, team.CreateInput{Name: name}, actor)
	if err != nil {
		note(report, fmt.Sprintf("The team %s could not be added: %v", name, err))
		return uuid.Nil
	}
	n.teams[strings.ToLower(name)] = made.ID
	note(report, fmt.Sprintf("The team %s was added to this project.", name))
	return made.ID
}

// said is one comment as Jira packs it into a cell: date;author;body.
type said struct {
	at     time.Time
	author string
	body   string
}

func parseComment(cell string) (said, bool) {
	parts := strings.SplitN(cell, ";", 3)
	if len(parts) < 3 {
		return said{}, false
	}
	at, err := parseMoment(parts[0])
	if err != nil {
		return said{}, false
	}
	body := strings.TrimRight(strings.TrimSpace(parts[2]), ";")
	if body == "" {
		return said{}, false
	}
	return said{at: at, author: strings.TrimSpace(parts[1]), body: body}, true
}

// spent is one worklog as Jira packs it: comment;date;author;seconds.
type spent struct {
	note    string
	at      time.Time
	author  string
	minutes int
}

func parseWorklog(cell string) (spent, bool) {
	parts := strings.SplitN(cell, ";", 4)
	if len(parts) < 4 {
		return spent{}, false
	}
	at, err := parseMoment(parts[1])
	if err != nil {
		return spent{}, false
	}
	minutes, err := parseMinutes(parts[3])
	if err != nil || minutes <= 0 {
		return spent{}, false
	}
	return spent{note: strings.TrimSpace(parts[0]), at: at, author: strings.TrimSpace(parts[2]), minutes: minutes}, true
}

// linkTargets are the columns that name another issue, with the type and the
// direction each column means.
var linkTargets = map[string]struct {
	kind    string
	outward bool
}{
	"links:blocks":       {"Blocks", true},
	"links:blockedBy":    {"Blocks", false},
	"links:relates":      {"Relates", true},
	"links:duplicates":   {"Duplicates", true},
	"links:duplicatedBy": {"Duplicates", false},
}
