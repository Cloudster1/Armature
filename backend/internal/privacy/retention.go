package privacy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/observability"
	"github.com/armature/armature/backend/internal/tenant"
)

// sweep is one kind of row and the clause that says it is past its time. $1 is
// the cutoff. Tenant sweeps run inside each organization, as the audit log's
// always has; the rest belong to nobody in particular and run as the admin.
type sweep struct {
	name   string
	table  string
	where  string
	tenant bool
	keep   func(Policy) time.Duration
}

var sweeps = []sweep{
	{"sessions", "user_session", "expires_at < $1", false, func(p Policy) time.Duration { return p.Sessions }},
	{"portal-codes", "portal_code", "expires_at < $1", false, func(p Policy) time.Duration { return p.PortalCodes }},
	{"oidc-logins", "oidc_login", "expires_at < $1", false, func(p Policy) time.Duration { return p.OIDCLogins }},
	{"invites", "org_invite", "expires_at < $1", true, func(p Policy) time.Duration { return p.Invites }},
	{"api-tokens", "api_token", "expires_at < $1", true, func(p Policy) time.Duration { return p.APITokens }},
	{"notifications", "notification", "created_at < $1", true, func(p Policy) time.Duration { return p.Notifications }},
	{"outbox", "outbox_event", "published_at < $1", true, func(p Policy) time.Duration { return p.Outbox }},
	{"webhook-deliveries", "webhook_delivery", "created_at < $1", true, func(p Policy) time.Duration { return p.WebhookDeliveries }},
	{"automation-runs", "automation_run", "started_at < $1", true, func(p Policy) time.Duration { return p.AutomationRuns }},
	{"inbound-mail", "inbound_mail", "received_at < $1", false, func(p Policy) time.Duration { return p.InboundMail }},
	{"import-jobs", "import_job", "created_at < $1", true, func(p Policy) time.Duration { return p.ImportJobs }},
	{"audit", "audit_log", "created_at < $1", true, func(p Policy) time.Duration { return p.Audit }},
}

// Retention prunes what the policy says is past its time, once a day.
type Retention struct {
	db       *db.Cluster
	log      *slog.Logger
	policy   Policy
	interval time.Duration
	now      func() time.Time
}

func NewRetention(cluster *db.Cluster, policy Policy, log *slog.Logger) *Retention {
	return &Retention{db: cluster, log: log, policy: policy, interval: 24 * time.Hour, now: time.Now}
}

// Run prunes on start and then every interval until the context ends.
func (r *Retention) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		counts, err := r.Once(ctx)
		if err != nil {
			r.log.Warn("retention failed", "error", err)
		}
		for name, n := range counts {
			if n > 0 {
				r.log.Info("rows pruned", "kind", name, "count", n)
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Once runs every sweep whose window is set and returns how many rows each
// removed. A sweep that fails does not stop the others.
func (r *Retention) Once(ctx context.Context) (map[string]int64, error) {
	counts := map[string]int64{}
	var firstErr error
	for _, s := range sweeps {
		keep := s.keep(r.policy)
		if keep <= 0 {
			continue
		}
		cutoff := r.now().Add(-keep)
		n, err := observability.Count(ctx, "retention-"+s.name, func(ctx context.Context) (int64, error) {
			return r.prune(ctx, s, cutoff)
		})
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", s.name, err)
		}
		counts[s.name] = n
	}
	return counts, firstErr
}

func (r *Retention) prune(ctx context.Context, s sweep, cutoff time.Time) (int64, error) {
	query := fmt.Sprintf(`DELETE FROM %s WHERE %s`, s.table, s.where)
	if !s.tenant {
		var n int64
		_, err := r.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			tag, err := tx.Exec(ctx, query, cutoff)
			n = tag.RowsAffected()
			return err
		})
		return n, err
	}
	// Finding the organizations is the one step that crosses them; the
	// deletion itself happens inside each tenant, as any write does.
	var orgs []uuid.UUID
	err := r.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, fmt.Sprintf(`SELECT DISTINCT org_id FROM %s WHERE %s`, s.table, s.where), cutoff)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			orgs = append(orgs, id)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	var n int64
	for _, org := range orgs {
		orgCtx := db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: org}))
		_, err := r.db.Write(orgCtx, func(ctx context.Context, tx db.DBTX) error {
			tag, err := tx.Exec(ctx, query, cutoff)
			n += tag.RowsAffected()
			return err
		})
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
