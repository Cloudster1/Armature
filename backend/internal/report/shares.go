package report

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/nql"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/tenant"
)

// Share is a link to one dashboard that needs no sign-in. The token is kept
// only as a digest; the row never carries it back.
type Share struct {
	ID          uuid.UUID `json:"id"`
	DashboardID uuid.UUID `json:"dashboardId"`
	Name        string    `json:"name"`
	// Query is the dashboard's filter as it was when the link was made; the
	// link shows that view and no other.
	Query     string     `json:"query,omitempty"`
	CreatedBy *uuid.UUID `json:"createdBy,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// ShareInput is a link as it is asked for.
type ShareInput struct {
	Name      string
	Query     string
	ExpiresAt *time.Time
	// Internal links serve the product's own renderer and are never listed.
	Internal bool
}

// SharedView is what a link opens: the dashboard, whose it is, which of its
// widget kinds the frozen filter reaches, and when it was read.
type SharedView struct {
	Share       Share      `json:"share"`
	Dashboard   Dashboard  `json:"dashboard"`
	ProjectName string     `json:"projectName"`
	Kinds       []KindInfo `json:"kinds"`
	GeneratedAt time.Time  `json:"generatedAt"`
}

var (
	// ErrShareName is returned when a link is made without a name.
	ErrShareName = errors.New("a link needs a name")
	// ErrShareExpiry is returned for an expiry that has already passed.
	ErrShareExpiry = errors.New("choose an expiry that is still ahead")
	// ErrShareGone is returned for a token nobody has, or one revoked or expired.
	ErrShareGone = errors.New("this link is no longer valid")
)

// CreateShare mints a link. The secret leaves once, in the answer, and the
// row keeps its digest and the query the dashboard was filtered by.
func (s *Service) CreateShare(ctx context.Context, dashboardID uuid.UUID, in ShareInput, actor uuid.UUID) (*Share, string, db.LSN, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, "", 0, ErrShareName
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(time.Now()) {
		return nil, "", 0, ErrShareExpiry
	}
	secret, digest, err := auth.GenerateToken()
	if err != nil {
		return nil, "", 0, err
	}
	out := &Share{DashboardID: dashboardID, Name: name, Query: strings.TrimSpace(in.Query), ExpiresAt: in.ExpiresAt}
	lsn, err := s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := dashboardExists(ctx, tx, dashboardID); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO dashboard_share (org_id, dashboard_id, name, query, token_hash, internal, created_by, expires_at)
			VALUES (current_org_id(), $1, $2, $3, $4, $5, $6, $7)
			RETURNING id, created_by, created_at`, dashboardID, out.Name, out.Query, digest, in.Internal, actor, in.ExpiresAt,
		).Scan(&out.ID, &out.CreatedBy, &out.CreatedAt)
	})
	if err != nil {
		return nil, "", 0, err
	}
	return out, secret, lsn, nil
}

// Shares lists a dashboard's live links; the renderer's own are not links anybody holds.
func (s *Service) Shares(ctx context.Context, dashboardID uuid.UUID) ([]Share, error) {
	out := []Share{}
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT id, dashboard_id, name, query, created_by, created_at, expires_at
			FROM dashboard_share
			WHERE dashboard_id = $1 AND revoked_at IS NULL AND NOT internal
			ORDER BY created_at`, dashboardID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var sh Share
			if err := rows.Scan(&sh.ID, &sh.DashboardID, &sh.Name, &sh.Query, &sh.CreatedBy, &sh.CreatedAt, &sh.ExpiresAt); err != nil {
				return err
			}
			out = append(out, sh)
		}
		return rows.Err()
	})
	return out, err
}

// RevokeShare ends a link. The row stays, so the record of who shared what
// survives the link.
func (s *Service) RevokeShare(ctx context.Context, dashboardID, shareID uuid.UUID) (db.LSN, error) {
	return s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, `
			UPDATE dashboard_share SET revoked_at = now()
			WHERE id = $1 AND dashboard_id = $2 AND revoked_at IS NULL`, shareID, dashboardID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// shareRow is what a token resolves to before any tenant is entered.
type shareRow struct {
	orgID       uuid.UUID
	id          uuid.UUID
	dashboardID uuid.UUID
	name        string
	query       string
	createdBy   *uuid.UUID
	createdAt   time.Time
	expiresAt   *time.Time
}

// resolveShare finds the organization a token belongs to. It runs outside
// any tenant, since the token is the whole of what the caller has.
func (s *Service) resolveShare(ctx context.Context, token string) (shareRow, error) {
	digest := auth.HashToken(token)
	var row shareRow
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT org_id, id, dashboard_id, name, query, created_by, created_at, expires_at
			FROM dashboard_share
			WHERE token_hash = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`, digest,
		).Scan(&row.orgID, &row.id, &row.dashboardID, &row.name, &row.query, &row.createdBy, &row.createdAt, &row.expiresAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return shareRow{}, ErrShareGone
	}
	return row, err
}

// Shared opens a link: the dashboard as its arrangement stands, read in the
// organization the token names. Nothing is written by opening it.
func (s *Service) Shared(ctx context.Context, token string) (*SharedView, error) {
	row, err := s.resolveShare(ctx, token)
	if err != nil {
		return nil, err
	}
	ctx = tenant.WithOrg(ctx, tenant.Org{ID: row.orgID})
	out := &SharedView{
		Share:       Share{ID: row.id, DashboardID: row.dashboardID, Name: row.name, Query: row.query, CreatedAt: row.createdAt, ExpiresAt: row.expiresAt},
		GeneratedAt: time.Now(),
	}
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		d := &out.Dashboard
		var kind project.Kind
		err := tx.QueryRow(ctx, `
			SELECT d.id, d.project_id, p.key, p.name, p.kind, d.name, d.position, d.created_at
			FROM dashboard d JOIN project p ON p.id = d.project_id WHERE d.id = $1`, row.dashboardID,
		).Scan(&d.ID, &d.ProjectID, &d.ProjectKey, &out.ProjectName, &kind, &d.Name, &d.Position, &d.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrShareGone
		}
		if err != nil {
			return err
		}
		out.Kinds = Kinds(kind)
		d.Widgets, err = s.widgetsOf(ctx, tx, d.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SharedWidget answers one of the shared dashboard's widgets with its own
// stored settings and the link's frozen query, so a visitor asks nothing more.
func (s *Service) SharedWidget(ctx context.Context, token string, widgetID uuid.UUID) (any, error) {
	row, err := s.resolveShare(ctx, token)
	if err != nil {
		return nil, err
	}
	ctx = tenant.WithOrg(ctx, tenant.Org{ID: row.orgID})
	var (
		projectKey string
		kind       Kind
		params     Params
	)
	err = s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		var raw []byte
		err := tx.QueryRow(ctx, `
			SELECT p.key, w.kind, w.config
			FROM dashboard_widget w JOIN dashboard d ON d.id = w.dashboard_id JOIN project p ON p.id = d.project_id
			WHERE w.id = $1 AND w.dashboard_id = $2`, widgetID, row.dashboardID).Scan(&projectKey, &kind, &raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		params = ParamsFrom(raw)
		return nil
	})
	if err != nil {
		return nil, err
	}
	info, ok := infoFor(kind)
	if ok && info.Narrows && row.query != "" {
		// The query is compiled as the sharer, so currentUser() means them.
		var sharer uuid.UUID
		if row.createdBy != nil {
			sharer = *row.createdBy
		}
		parsed, err := nql.Parse(row.query)
		if err != nil {
			return nil, fmt.Errorf("the link's query: %w", err)
		}
		params.Narrow, err = parsed.Compile(nql.Env{UserID: sharer, Now: time.Now()})
		if err != nil {
			return nil, fmt.Errorf("the link's query: %w", err)
		}
	}
	return s.Report(ctx, projectKey, kind, params)
}
