package notify

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/mail"
	"github.com/armature/armature/backend/internal/observability"
	"github.com/armature/armature/backend/internal/tenant"
)

// DailyDigestHour is when a daily bundle goes out, in the server's clock.
const DailyDigestHour = 8

// digestInterval is how often the queue is looked at; well inside an hour so
// an hourly bundle is never skipped for a restart.
const digestInterval = 15 * time.Minute

// Digest bundles queued notifications into one mail per person, on their schedule.
type Digest struct {
	db     *db.Cluster
	mailer mail.Mailer
	log    *slog.Logger
	appURL string
	now    func() time.Time
	// lastHourly remembers which hour was sent so a person gets one bundle
	// per hour however often the loop wakes.
	sent map[uuid.UUID]time.Time
}

func NewDigest(cluster *db.Cluster, mailer mail.Mailer, appURL string, log *slog.Logger) *Digest {
	return &Digest{db: cluster, mailer: mailer, log: log, appURL: strings.TrimRight(appURL, "/"), now: time.Now, sent: map[uuid.UUID]time.Time{}}
}

// Run sends on start and then every interval until the context ends.
func (d *Digest) Run(ctx context.Context) error {
	ticker := time.NewTicker(digestInterval)
	defer ticker.Stop()
	for {
		if n, err := observability.Count(ctx, "digest", d.Once); err != nil {
			d.log.Warn("digest failed", "error", err)
		} else if n > 0 {
			d.log.Info("digests sent", "count", n)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Once mails everyone whose bundle is due and returns how many went. Finding
// them crosses tenants; each bundle is read and cleared inside its own.
func (d *Digest) Once(ctx context.Context) (int, error) {
	type waiting struct{ orgID, userID uuid.UUID }
	var found []waiting
	err := d.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT org_id, user_id FROM notification_digest ORDER BY org_id, user_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var w waiting
			if err := rows.Scan(&w.orgID, &w.userID); err != nil {
				return err
			}
			found = append(found, w)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	now := d.now()
	sent := 0
	for _, w := range found {
		orgCtx := db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: w.orgID}))
		ok, err := d.send(orgCtx, w.userID, now)
		if err != nil {
			d.log.Warn("could not send a digest", "user", w.userID, "error", err)
			continue
		}
		if ok {
			sent++
		}
	}
	return sent, nil
}

// due says whether a person's bundle goes now: hourly ones once an hour,
// daily ones at the digest hour, and anything queued under "off" at once,
// since the person changed their mind after the rows were queued.
func (d *Digest) due(userID uuid.UUID, digest string, now time.Time) bool {
	last := d.sent[userID]
	switch digest {
	case DigestHourly:
		return now.Sub(last) >= time.Hour
	case DigestDaily:
		return now.Hour() == DailyDigestHour && now.Sub(last) >= time.Hour
	default:
		return true
	}
}

func (d *Digest) send(ctx context.Context, userID uuid.UUID, now time.Time) (bool, error) {
	var (
		prefs Preferences
		to    string
		items []Notification
	)
	err := d.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := readPreferences(ctx, tx, userID, &prefs); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT email FROM app_user WHERE id = $1`, userID).Scan(&to); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, selectNotifications+` AND n.id IN (SELECT notification_id FROM notification_digest WHERE user_id = $1) ORDER BY n.created_at`, userID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var n Notification
			if err := rows.Scan(&n.ID, &n.IssueKey, &n.Kind, &n.Title, &n.Body, &n.Link, &n.CreatedAt, &n.ReadAt); err != nil {
				return err
			}
			items = append(items, n)
		}
		return rows.Err()
	})
	if err != nil {
		return false, err
	}
	if len(items) == 0 || !d.due(userID, prefs.Digest, now) {
		return false, nil
	}
	if d.mailer != nil {
		if err := d.mailer.Send(ctx, digestMail(to, items, d.appURL)); err != nil {
			return false, err
		}
	}
	d.sent[userID] = now
	_, err = d.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `DELETE FROM notification_digest WHERE user_id = $1`, userID)
		return err
	})
	return true, err
}

// digestMail words one bundle: a line per thing, and where the inbox is.
func digestMail(to string, items []Notification, appURL string) mail.Mail {
	var b strings.Builder
	fmt.Fprintf(&b, "%d %s since your last mail:\n\n", len(items), plural(len(items), "thing happened", "things happened"))
	for _, n := range items {
		fmt.Fprintf(&b, "- %s\n", n.Title)
		if n.Link != "" {
			fmt.Fprintf(&b, "  %s\n", n.Link)
		}
	}
	fmt.Fprintf(&b, "\nSee everything at %s/inbox\n", appURL)
	return mail.Mail{To: to, Subject: fmt.Sprintf("%d %s on your work", len(items), plural(len(items), "update", "updates")), Body: b.String()}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
