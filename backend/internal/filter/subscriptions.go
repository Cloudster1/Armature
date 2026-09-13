package filter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/mail"
	"github.com/armature/armature/backend/internal/nql"
	"github.com/armature/armature/backend/internal/observability"
	"github.com/armature/armature/backend/internal/perm"
	"github.com/armature/armature/backend/internal/tenant"
)

const (
	// subscriptionInterval is how often due subscriptions are looked for;
	// well inside an hour, so a daily hour is never missed for a restart.
	subscriptionInterval = 10 * time.Minute
	// MailRows caps how many issues one subscription mail lists.
	MailRows = 50
)

// Subscriptions mails each subscribed filter's result on its schedule.
type Subscriptions struct {
	db     *db.Cluster
	issues *issue.Service
	mailer mail.Mailer
	log    *slog.Logger
	appURL string
	now    func() time.Time
	// perms narrows each mail to what its reader may read.
	perms *perm.Store
}

func NewSubscriptions(cluster *db.Cluster, issues *issue.Service, mailer mail.Mailer, appURL string, log *slog.Logger) *Subscriptions {
	return &Subscriptions{db: cluster, issues: issues, mailer: mailer, log: log, appURL: strings.TrimRight(appURL, "/"), now: time.Now, perms: perm.NewStore(cluster)}
}

// Run sends on start and then every interval until the context ends.
func (s *Subscriptions) Run(ctx context.Context) error {
	ticker := time.NewTicker(subscriptionInterval)
	defer ticker.Stop()
	for {
		if n, err := observability.Count(ctx, "filter-subscriptions", s.Once); err != nil {
			s.log.Warn("filter subscriptions failed", "error", err)
		} else if n > 0 {
			s.log.Info("filter subscriptions sent", "count", n)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// due says whether a subscription's moment has come since it was last sent:
// today's (or this week's) hour has passed and nothing went out since.
func due(schedule string, hour int, weekday *int, lastSent *time.Time, now time.Time) bool {
	moment := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
	if moment.After(now) {
		moment = moment.AddDate(0, 0, -1)
	}
	if schedule == Weekly && weekday != nil {
		for moment.Weekday() != time.Weekday(*weekday) {
			moment = moment.AddDate(0, 0, -1)
		}
	}
	return lastSent == nil || lastSent.Before(moment)
}

// Once mails every due subscription across organizations and returns how many.
func (s *Subscriptions) Once(ctx context.Context) (int, error) {
	type row struct {
		orgID, id, filterID, userID uuid.UUID
		schedule                    string
		hour                        int
		weekday                     *int
		lastSent                    *time.Time
	}
	var found []row
	err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT org_id, id, filter_id, user_id, schedule, hour, weekday, last_sent_at FROM saved_filter_subscription`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.orgID, &r.id, &r.filterID, &r.userID, &r.schedule, &r.hour, &r.weekday, &r.lastSent); err != nil {
				return err
			}
			found = append(found, r)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	now := s.now()
	sent := 0
	for _, r := range found {
		if !due(r.schedule, r.hour, r.weekday, r.lastSent, now) {
			continue
		}
		orgCtx := db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: r.orgID}))
		if err := s.send(orgCtx, r.id, r.filterID, r.userID, r.orgID, now); err != nil {
			s.log.Warn("could not mail a subscription", "subscription", r.id, "error", err)
			continue
		}
		sent++
	}
	return sent, nil
}

func (s *Subscriptions) send(ctx context.Context, subID, filterID, userID, orgID uuid.UUID, now time.Time) error {
	var (
		name, query, to string
		role            *string
		visible         bool
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT f.name, f.query, u.email, m.org_role, (f.owner_id = $2 OR f.shared)
			FROM saved_filter f JOIN app_user u ON u.id = $2
			LEFT JOIN org_member m ON m.user_id = u.id AND m.org_id = f.org_id
			WHERE f.id = $1`, filterID, userID).Scan(&name, &query, &to, &role, &visible)
	})
	if err != nil {
		return err
	}
	// Somebody who has left, or whom the filter is no longer shared with, is
	// not owed a mail about its issues.
	if role == nil || !visible {
		return nil
	}
	parsed, err := nql.Parse(query)
	if err != nil {
		return err
	}
	compiled, err := parsed.Compile(nql.Env{UserID: userID, Now: now})
	if err != nil {
		return err
	}
	// The mail lists what the reader may read, not what the query finds.
	f := issue.Filter{Query: compiled}
	if s.perms != nil {
		set, err := s.perms.ResolveFor(ctx, orgID, userID, *role == "owner")
		if err != nil {
			return err
		}
		if all, keys := set.Readable(); !all {
			f.Scoped, f.Within = true, keys
		}
	}
	result, err := s.issues.List(ctx, f, issue.Page{Limit: MailRows})
	if err != nil {
		return err
	}
	if s.mailer != nil {
		if err := s.mailer.Send(ctx, s.mail(to, name, filterID, result)); err != nil {
			return err
		}
	}
	_, err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE saved_filter_subscription SET last_sent_at = $2 WHERE id = $1`, subID, now)
		return err
	})
	return err
}

// mail words the result as a list somebody can read in a mail client.
func (s *Subscriptions) mail(to, name string, filterID uuid.UUID, result *issue.Result) mail.Mail {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d issue%s\n\n", name, result.Total, plural(result.Total))
	for _, i := range result.Issues {
		fmt.Fprintf(&b, "- %s  %s  [%s]\n", i.Key, i.Summary, i.Status.Name)
	}
	if result.Total > len(result.Issues) {
		fmt.Fprintf(&b, "\n%d more, not listed here.\n", result.Total-len(result.Issues))
	}
	fmt.Fprintf(&b, "\nOpen the search at %s/filters/%s\n", s.appURL, filterID)
	return mail.Mail{To: to, Subject: fmt.Sprintf("%s: %d issue%s", name, result.Total, plural(result.Total)), Body: b.String()}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
