package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/milestone"
	"github.com/armature/armature/backend/internal/project"
)

// TemplateWidget is one widget a template lays down: the kind, and what it is
// asked with. An empty title or width takes the kind's own.
type TemplateWidget struct {
	Kind   Kind   `json:"kind"`
	Title  string `json:"title,omitempty"`
	Width  int    `json:"width,omitempty"`
	Config Params `json:"config"`
}

// Template is an arrangement a dashboard can start from. Built-ins are code
// keyed by a word and saved ones are rows keyed by id, in one shape.
type Template struct {
	ID          *uuid.UUID       `json:"id,omitempty"`
	Key         string           `json:"key"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Kinds       []project.Kind   `json:"projectKinds,omitempty"`
	Widgets     []TemplateWidget `json:"widgets"`
	BuiltIn     bool             `json:"builtIn"`
}

// OverviewKey is the built-in every project starts from, by kind.
func OverviewKey(kind project.Kind) string {
	switch kind {
	case project.KindService:
		return "overview-service"
	case project.KindBusiness:
		return "overview-business"
	default:
		return "overview-software"
	}
}

func plain(kinds ...Kind) []TemplateWidget {
	out := make([]TemplateWidget, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, TemplateWidget{Kind: k})
	}
	return out
}

// builtIn is every template that ships with the product, in the order the
// chooser offers them.
var builtIn = []Template{
	{Key: "overview-software", Name: "Overview", Description: "Status, the running sprint and its burndown, workload, throughput, epics and velocity.",
		Kinds: []project.Kind{project.KindSoftware}, BuiltIn: true,
		Widgets: plain(StatusBreakdown, Sprint, Burndown, Workload, Throughput, Epics, Velocity)},
	{Key: "overview-business", Name: "Overview", Description: "Status, priority, workload, throughput, epics and cycle time.",
		Kinds: []project.Kind{project.KindBusiness}, BuiltIn: true,
		Widgets: plain(StatusBreakdown, PriorityBreakdown, Workload, Throughput, Epics, CycleTime)},
	{Key: "overview-service", Name: "Overview", Description: "Status, request types, goals met, workload, throughput and cycle time.",
		Kinds: []project.Kind{project.KindService}, BuiltIn: true,
		Widgets: plain(StatusBreakdown, RequestTypes, SLA, Workload, Throughput, CycleTime)},
	{Key: "milestone", Name: "Milestone", Description: "One milestone's progress: what is left by status and by type, and how fast it is arriving.", BuiltIn: true,
		Widgets: []TemplateWidget{
			{Kind: Filter, Config: Params{Filter: &FilterSpec{}}},
			{Kind: Milestones, Title: "Milestone"},
			{Kind: Chart, Title: "Left by status", Config: Params{GroupBy: "status", Shape: "donut"}},
			{Kind: Chart, Title: "Work by type", Config: Params{GroupBy: "type", Shape: "bar"}},
			{Kind: Throughput},
			{Kind: Workload},
		}},
	{Key: "team-health", Name: "Team health", Description: "Who carries what, how long things take, and whether more arrives than gets done.", BuiltIn: true,
		Widgets: plain(TeamWorkload, Workload, CycleTime, Throughput)},
	{Key: "delivery", Name: "Delivery", Description: "The running sprint, its burndown, velocity, the sprints before it and the epics in flight.",
		Kinds: []project.Kind{project.KindSoftware}, BuiltIn: true,
		Widgets: plain(Sprint, Burndown, Velocity, SprintHistory, Epics)},
}

// BuiltIn returns the built-in templates that suit a project of a kind.
func BuiltIn(kind project.Kind) []Template {
	out := []Template{}
	for _, t := range builtIn {
		if t.suits(kind) {
			out = append(out, t)
		}
	}
	return out
}

func (t Template) suits(kind project.Kind) bool { return suitsKind(t.Kinds, kind) }

func findBuiltIn(key string) (Template, bool) {
	for _, t := range builtIn {
		if t.Key == key {
			return t, true
		}
	}
	return Template{}, false
}

// substitution is what a template is told about the dashboard it lands on: a
// milestone, when the dashboard is about one.
type substitution struct {
	milestoneID   *uuid.UUID
	milestoneName string
}

// applyTemplate lays a template's widgets on a dashboard, leaving out kinds a
// project of this kind cannot show rather than refusing the whole template.
func applyTemplate(ctx context.Context, tx db.DBTX, dashboardID uuid.UUID, t Template, kind project.Kind, sub substitution) error {
	position := 0
	for _, w := range t.Widgets {
		info, ok := infoFor(w.Kind)
		if !ok || !info.suits(kind) {
			continue
		}
		title, width := w.Title, w.Width
		if strings.TrimSpace(title) == "" {
			title = info.Title
		}
		width = widthOr(width, info)
		config := w.Config
		if w.Kind == Milestones && sub.milestoneID != nil {
			id := *sub.milestoneID
			config.MilestoneID = &id
		}
		if w.Kind == Filter && sub.milestoneName != "" {
			spec := FilterSpec{}
			if config.Filter != nil {
				spec = *config.Filter
			}
			spec.Milestone = sub.milestoneName
			config.Filter = &spec
		}
		raw, err := json.Marshal(config)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO dashboard_widget (org_id, dashboard_id, kind, title, width, position, config)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6)`, dashboardID, string(w.Kind), title, width, position, raw); err != nil {
			return fmt.Errorf("add widget %s: %w", w.Kind, err)
		}
		position++
	}
	return nil
}

// Templates lists what a project of a kind may start from: the built-ins that
// suit it, then the organization's own, by name.
func (s *Service) Templates(ctx context.Context, kind project.Kind) ([]Template, error) {
	out := BuiltIn(kind)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT id, name, description, widgets FROM dashboard_template ORDER BY lower(name)`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t Template
			var id uuid.UUID
			var raw []byte
			if err := rows.Scan(&id, &t.Name, &t.Description, &raw); err != nil {
				return err
			}
			if err := json.Unmarshal(raw, &t.Widgets); err != nil {
				return fmt.Errorf("read template %s: %w", id, err)
			}
			t.ID, t.Key = &id, id.String()
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// resolveTemplate finds a template by its key, a built-in word or a saved
// template's id.
func resolveTemplate(ctx context.Context, tx db.DBTX, key string) (Template, error) {
	if t, ok := findBuiltIn(key); ok {
		return t, nil
	}
	id, err := uuid.Parse(key)
	if err != nil {
		return Template{}, ErrNoTemplate
	}
	var t Template
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT id, name, description, widgets FROM dashboard_template WHERE id = $1`, id).Scan(&id, &t.Name, &t.Description, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Template{}, ErrNoTemplate
	}
	if err != nil {
		return Template{}, err
	}
	if err := json.Unmarshal(raw, &t.Widgets); err != nil {
		return Template{}, fmt.Errorf("read template %s: %w", id, err)
	}
	t.ID, t.Key = &id, id.String()
	return t, nil
}

// SaveTemplate keeps a dashboard's arrangement for the organization: the ids
// that belong to one project are dropped, the names in the filter travel.
func (s *Service) SaveTemplate(ctx context.Context, dashboardID uuid.UUID, name, description string, actor uuid.UUID) (*Template, db.LSN, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, 0, ErrTemplateName
	}
	out := &Template{Name: name, Description: strings.TrimSpace(description), Widgets: []TemplateWidget{}}
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := dashboardExists(ctx, tx, dashboardID); err != nil {
			return err
		}
		widgets, err := s.widgetsOf(ctx, tx, dashboardID)
		if err != nil {
			return err
		}
		for _, w := range widgets {
			config := ParamsFrom(w.Config)
			// A team, a sprint or a milestone means nothing in the next project.
			config.TeamID, config.SprintID, config.MilestoneID = nil, nil, nil
			out.Widgets = append(out.Widgets, TemplateWidget{Kind: w.Kind, Title: w.Title, Width: w.Width, Config: config})
		}
		raw, err := json.Marshal(out.Widgets)
		if err != nil {
			return err
		}
		var id uuid.UUID
		err = tx.QueryRow(ctx, `
			INSERT INTO dashboard_template (org_id, name, description, widgets, created_by)
			VALUES (current_org_id(), $1, $2, $3, $4) RETURNING id`, out.Name, out.Description, raw, actor).Scan(&id)
		if isUniqueViolation(err) {
			return ErrTemplateNameTaken
		}
		if err != nil {
			return fmt.Errorf("save template: %w", err)
		}
		out.ID, out.Key = &id, id.String()
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// DeleteTemplate removes one of the organization's own templates. What was
// made from it stays: a dashboard is a copy, never a reference.
func (s *Service) DeleteTemplate(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM dashboard_template WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// milestoneNamed reads the milestone a dashboard is about, refusing one from
// another project, which under row level security is simply absent.
func milestoneNamed(ctx context.Context, tx db.DBTX, projectID, milestoneID uuid.UUID) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `SELECT name FROM milestone WHERE id = $1 AND project_id = $2`, milestoneID, projectID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", milestone.ErrNotFound
	}
	return name, err
}
