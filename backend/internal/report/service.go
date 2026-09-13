package report

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/plan"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/sprint"
	"github.com/armature/armature/backend/internal/workflow"
)

// Service keeps dashboards and computes their reports.
type Service struct {
	db    *db.Cluster
	plans *plan.Service
	// sprints is where the burndown's past is read from; nil leaves the
	// burndown empty, which is what a stack without sprints gets.
	sprints *sprint.Service
}

func NewService(cluster *db.Cluster, plans *plan.Service) *Service {
	return &Service{db: cluster, plans: plans}
}

// WithSprints gives the reports the sprint service, for the days it wrote.
func (s *Service) WithSprints(sprints *sprint.Service) *Service {
	s.sprints = sprints
	return s
}

// Provisioner gives every new project a dashboard, in the transaction that
// makes the project, with the widgets that suit its kind.
type Provisioner struct{}

// DefaultName is what a project's first dashboard is called.
const DefaultName = "Overview"

// ProvisionProject creates the project's first dashboard.
func (Provisioner) ProvisionProject(ctx context.Context, tx db.DBTX, p *project.Project, in project.CreateInput, _ []workflow.Status) error {
	return provision(ctx, tx, p.ID, p.Kind)
}

// provision makes the default dashboard for a project of a kind, from the
// same overview template the chooser offers.
func provision(ctx context.Context, tx db.DBTX, projectID uuid.UUID, kind project.Kind) error {
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO dashboard (org_id, project_id, name) VALUES (current_org_id(), $1, $2) RETURNING id`,
		projectID, DefaultName).Scan(&id); err != nil {
		return fmt.Errorf("create dashboard: %w", err)
	}
	overview, _ := findBuiltIn(OverviewKey(kind))
	return applyTemplate(ctx, tx, id, overview, kind, substitution{})
}

// Dashboards lists a project's dashboards with their widgets. A project made
// before dashboards existed gets its default one on first sight.
func (s *Service) Dashboards(ctx context.Context, projectKey string) ([]Dashboard, error) {
	out, err := s.dashboards(ctx, projectKey)
	if err != nil || len(out) > 0 {
		return out, err
	}
	_, err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var (
			id   uuid.UUID
			kind project.Kind
		)
		err := tx.QueryRow(ctx, `SELECT id, kind FROM project WHERE key = $1`, project.NormalizeKey(projectKey)).Scan(&id, &kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return project.ErrNotFound
		}
		if err != nil {
			return err
		}
		var existing int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM dashboard WHERE project_id = $1`, id).Scan(&existing); err != nil {
			return err
		}
		if existing > 0 {
			return nil
		}
		// From the same code a new project gets it from, so no migration has to
		// know what the defaults are.
		return provision(ctx, tx, id, kind)
	})
	if err != nil {
		return nil, err
	}
	return s.dashboards(ctx, projectKey)
}

func (s *Service) dashboards(ctx context.Context, projectKey string) ([]Dashboard, error) {
	out := []Dashboard{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT d.id, d.project_id, p.key, d.name, d.position, d.created_at
			FROM dashboard d JOIN project p ON p.id = d.project_id
			WHERE p.key = $1 ORDER BY d.position, d.created_at`, project.NormalizeKey(projectKey))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d Dashboard
			if err := rows.Scan(&d.ID, &d.ProjectID, &d.ProjectKey, &d.Name, &d.Position, &d.CreatedAt); err != nil {
				return err
			}
			d.Widgets = []Widget{}
			out = append(out, d)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		for i := range out {
			widgets, err := s.widgetsOf(ctx, tx, out[i].ID)
			if err != nil {
				return err
			}
			out[i].Widgets = widgets
		}
		return nil
	})
	return out, err
}

// dashboardExists refuses a dashboard the caller cannot see, which under row
// level security is the same as one that is not there.
func dashboardExists(ctx context.Context, tx db.DBTX, id uuid.UUID) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM dashboard WHERE id = $1)`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

func (s *Service) widgetsOf(ctx context.Context, tx db.DBTX, dashboardID uuid.UUID) ([]Widget, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, dashboard_id, kind, title, width, position, config
		FROM dashboard_widget WHERE dashboard_id = $1 ORDER BY position, created_at`, dashboardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Widget{}
	for rows.Next() {
		var w Widget
		if err := rows.Scan(&w.ID, &w.DashboardID, &w.Kind, &w.Title, &w.Width, &w.Position, &w.Config); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// CreateInput is a dashboard as it is asked for: a name, the template it
// starts from if any, and the milestone it is about if any.
type CreateInput struct {
	Name string
	// Template is a built-in key or a saved template's id; empty is blank.
	Template string
	// MilestoneID pins the template's filter and milestones widget to one milestone.
	MilestoneID *uuid.UUID
}

// CreateDashboardFrom adds a dashboard to a project, laid out from a template
// when one is named; what the template said is copied, never followed.
func (s *Service) CreateDashboardFrom(ctx context.Context, projectKey string, in CreateInput, actor uuid.UUID) (*Dashboard, db.LSN, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, 0, errors.New("a dashboard needs a name")
	}
	var out Dashboard
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var kind project.Kind
		err := tx.QueryRow(ctx, `
			INSERT INTO dashboard (org_id, project_id, name, position)
			SELECT current_org_id(), p.id, $2, (SELECT COALESCE(max(position) + 1, 0) FROM dashboard WHERE project_id = p.id)
			FROM project p WHERE p.key = $1 AND p.archived_at IS NULL
			RETURNING id, project_id, $1::text, name, position, created_at,
			          (SELECT kind FROM project WHERE id = project_id)`,
			project.NormalizeKey(projectKey), name).Scan(&out.ID, &out.ProjectID, &out.ProjectKey, &out.Name, &out.Position, &out.CreatedAt, &kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return project.ErrNotFound
		}
		if isUniqueViolation(err) {
			return ErrNameTaken
		}
		if err != nil {
			return fmt.Errorf("create dashboard: %w", err)
		}
		out.Widgets = []Widget{}
		if in.Template == "" && in.MilestoneID == nil {
			return nil
		}
		template := Template{}
		if in.Template != "" {
			template, err = resolveTemplate(ctx, tx, in.Template)
			if err != nil {
				return err
			}
		}
		sub := substitution{milestoneID: in.MilestoneID}
		if in.MilestoneID != nil {
			sub.milestoneName, err = milestoneNamed(ctx, tx, out.ProjectID, *in.MilestoneID)
			if err != nil {
				return err
			}
		}
		if err := applyTemplate(ctx, tx, out.ID, template, kind, sub); err != nil {
			return err
		}
		out.Widgets, err = s.widgetsOf(ctx, tx, out.ID)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return &out, lsn, nil
}

// RenameDashboard changes a dashboard's name.
func (s *Service) RenameDashboard(ctx context.Context, id uuid.UUID, name string) (db.LSN, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, errors.New("a dashboard needs a name")
	}
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `UPDATE dashboard SET name = $2 WHERE id = $1`, id, name)
		if isUniqueViolation(err) {
			return ErrNameTaken
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// DeleteDashboard removes a dashboard and its widgets. The last one stays.
func (s *Service) DeleteDashboard(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var remaining int
		err := tx.QueryRow(ctx, `
			SELECT count(*) FROM dashboard o WHERE o.project_id = (SELECT project_id FROM dashboard WHERE id = $1)`, id).Scan(&remaining)
		if err != nil {
			return err
		}
		if remaining == 0 {
			return ErrNotFound
		}
		if remaining == 1 {
			return ErrLastDashboard
		}
		_, err = tx.Exec(ctx, `DELETE FROM dashboard WHERE id = $1`, id)
		return err
	})
}

// WidgetInput describes a widget to add.
type WidgetInput struct {
	Kind   Kind
	Title  string
	Width  int
	Config Params
}

// AddWidget puts a report on a dashboard.
func (s *Service) AddWidget(ctx context.Context, dashboardID uuid.UUID, in WidgetInput) (*Widget, db.LSN, error) {
	info, ok := infoFor(in.Kind)
	if !ok {
		return nil, 0, fmt.Errorf("%w: %q", ErrBadKind, in.Kind)
	}
	if in.Title = strings.TrimSpace(in.Title); in.Title == "" {
		in.Title = info.Title
	}
	in.Width = widthOr(in.Width, info)
	config, _ := json.Marshal(in.Config)

	var out Widget
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if in.Kind == Filter {
			var already bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (SELECT 1 FROM dashboard_widget WHERE dashboard_id = $1 AND kind = $2)`,
				dashboardID, string(Filter)).Scan(&already); err != nil {
				return err
			}
			if already {
				return ErrOneFilter
			}
		}
		// A filter goes first, above what it narrows; anything else joins the end.
		place := `(SELECT COALESCE(max(position) + 1, 0) FROM dashboard_widget WHERE dashboard_id = d.id)`
		if in.Kind == Filter {
			place = `(SELECT COALESCE(min(position) - 1, 0) FROM dashboard_widget WHERE dashboard_id = d.id)`
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO dashboard_widget (org_id, dashboard_id, kind, title, width, position, config)
			SELECT current_org_id(), d.id, $2, $3, $4, `+place+`, $5
			FROM dashboard d WHERE d.id = $1
			RETURNING id, dashboard_id, kind, title, width, position, config`,
			dashboardID, string(in.Kind), in.Title, in.Width, config,
		).Scan(&out.ID, &out.DashboardID, &out.Kind, &out.Title, &out.Width, &out.Position, &out.Config)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("add widget: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return &out, lsn, nil
}

// UpdateWidgetInput carries what an edit may change. Nil leaves a field alone.
type UpdateWidgetInput struct {
	Title  *string
	Width  *int
	Config *Params
}

// UpdateWidget changes a widget's title, width or parameters.
func (s *Service) UpdateWidget(ctx context.Context, id uuid.UUID, in UpdateWidgetInput) (*Widget, db.LSN, error) {
	var out Widget
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, `SELECT id, dashboard_id, kind, title, width, position, config FROM dashboard_widget WHERE id = $1`, id).
			Scan(&out.ID, &out.DashboardID, &out.Kind, &out.Title, &out.Width, &out.Position, &out.Config)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if in.Title != nil {
			if out.Title = strings.TrimSpace(*in.Title); out.Title == "" {
				info, _ := infoFor(out.Kind)
				out.Title = info.Title
			}
		}
		if in.Width != nil {
			if !validWidth(*in.Width) {
				return errors.New("a widget is one or two columns wide")
			}
			out.Width = *in.Width
		}
		if in.Config != nil {
			out.Config, _ = json.Marshal(*in.Config)
		}
		_, err = tx.Exec(ctx, `UPDATE dashboard_widget SET title = $2, width = $3, config = $4 WHERE id = $1`,
			id, out.Title, out.Width, out.Config)
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return &out, lsn, nil
}

// RemoveWidget takes a report off a dashboard.
func (s *Service) RemoveWidget(ctx context.Context, id uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `DELETE FROM dashboard_widget WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ReorderWidgets sets a dashboard's whole order at once, so two people moving
// widgets cannot interleave into an arrangement neither asked for.
func (s *Service) ReorderWidgets(ctx context.Context, dashboardID uuid.UUID, order []uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM dashboard_widget WHERE dashboard_id = $1`, dashboardID).Scan(&count); err != nil {
			return err
		}
		if count != len(order) {
			return fmt.Errorf("the dashboard has %d widgets but the new order lists %d", count, len(order))
		}
		for position, id := range order {
			tag, err := tx.Exec(ctx, `UPDATE dashboard_widget SET position = $3 WHERE id = $1 AND dashboard_id = $2`, id, dashboardID, position)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return fmt.Errorf("%w: widget %s", ErrNotFound, id)
			}
		}
		return nil
	})
}

// ParamsFrom reads a widget's stored configuration.
func ParamsFrom(config json.RawMessage) Params {
	var p Params
	_ = json.Unmarshal(config, &p)
	return p
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
