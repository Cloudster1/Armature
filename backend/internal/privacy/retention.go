package privacy

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/observability"
)

// sweep is one kind of row and the clause that says it is past its time. $1 is
// the cutoff.
type sweep struct {
	name  string
	table string
	where string
	keep  func(Policy) time.Duration
}

var sweeps = []sweep{
	{"sessions", "user_session", "expires_at < $1", func(p Policy) time.Duration { return p.Sessions }},
	{"portal-codes", "portal_code", "expires_at < $1", func(p Policy) time.Duration { return p.PortalCodes }},
	{"oidc-logins", "oidc_login", "expires_at < $1", func(p Policy) time.Duration { return p.OIDCLogins }},
	// The code step of a password sign-in is the same kind of half-done
	// handshake, so it keeps the same window.
	{"login-challenges", "login_challenge", "expires_at < $1", func(p Policy) time.Duration { return p.OIDCLogins }},
	{"invites", "invite", "expires_at < $1", func(p Policy) time.Duration { return p.Invites }},
	{"api-tokens", "api_token", "expires_at < $1", func(p Policy) time.Duration { return p.APITokens }},
	{"notifications", "notification", "created_at < $1", func(p Policy) time.Duration { return p.Notifications }},
	{"outbox", "outbox_event", "published_at < $1", func(p Policy) time.Duration { return p.Outbox }},
	{"webhook-deliveries", "webhook_delivery", "created_at < $1", func(p Policy) time.Duration { return p.WebhookDeliveries }},
	{"automation-runs", "automation_run", "started_at < $1", func(p Policy) time.Duration { return p.AutomationRuns }},
	{"inbound-mail", "inbound_mail", "received_at < $1", func(p Policy) time.Duration { return p.InboundMail }},
	{"import-jobs", "import_job", "created_at < $1", func(p Policy) time.Duration { return p.ImportJobs }},
	{"audit", "audit_log", "created_at < $1", func(p Policy) time.Duration { return p.Audit }},
	{"absences", "absence", "ends_on < $1::date", func(p Policy) time.Duration { return p.Absences }},
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

// prune runs one sweep as one statement, as the admin role, which may delete
// from every table a sweep names.
func (r *Retention) prune(ctx context.Context, s sweep, cutoff time.Time) (int64, error) {
	var n int64
	_, err := r.db.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		tag, err := tx.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s`, s.table, s.where), cutoff)
		n = tag.RowsAffected()
		return err
	})
	return n, err
}
