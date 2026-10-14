package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/mail"
	"github.com/armature/armature/backend/internal/observability"
)

// DailyDigestHour is when a daily bundle goes out, in the server's clock.
const DailyDigestHour = 8

// digestInterval is how often the queue is looked at; well inside an hour so
// an hourly bundle is never skipped for a restart.
const digestInterval = 15 * time.Minute

// Digest bundles queued notifications into one mail per person and
// organization, on the schedule they chose there.
type Digest struct {
	db     *db.Cluster
	mailer mail.Mailer
	log    *slog.Logger
	appURL string
	now    func() time.Time
}

func NewDigest(cluster *db.Cluster, mailer mail.Mailer, appURL string, log *slog.Logger) *Digest {
	return &Digest{db: cluster, mailer: mailer, log: log, appURL: strings.TrimRight(appURL, "/"), now: time.Now}
}

// WithClock sets where the digest reads the time, which a test moves by hand.
func (d *Digest) WithClock(now func() time.Time) *Digest {
	d.now = now
	return d
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

// Once mails everyone whose bundle is due and returns how many went.
func (d *Digest) Once(ctx context.Context) (int, error) {
	var found []uuid.UUID
	err := d.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT user_id FROM notification_digest ORDER BY user_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var userID uuid.UUID
			if err := rows.Scan(&userID); err != nil {
				return err
			}
			found = append(found, userID)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, err
	}
	now := d.now()
	sent := 0
	for _, userID := range found {
		ok, err := d.send(db.PinPrimary(ctx), userID, now)
		if err != nil {
			d.log.Warn("could not send a digest", "user", userID, "error", err)
			continue
		}
		if ok {
			sent++
		}
	}
	return sent, nil
}

// due is given when the bundle last went. A daily one goes
// at any pass from the digest hour on, so a missed hour does not skip a day.
func due(digest string, last, now time.Time) bool {
	switch digest {
	case DigestHourly:
		return now.Sub(last) >= time.Hour
	case DigestDaily:
		y, m, day := now.Date()
		today := time.Date(y, m, day, DailyDigestHour, 0, 0, 0, now.Location())
		return !now.Before(today) && last.Before(today)
	default:
		// Queued before the person turned the digest off: they want it now.
		return true
	}
}

// claimDigest moves the record of the last bundle on only if it still says
// what this pass read, so of two workers in one pass only one mails.
const claimDigest = `
	INSERT INTO notification_digest_sent (user_id, last_sent_at) VALUES ($1, $2)
	ON CONFLICT (user_id) DO UPDATE SET last_sent_at = EXCLUDED.last_sent_at
	 WHERE notification_digest_sent.last_sent_at IS NOT DISTINCT FROM $3
	RETURNING true`

func (d *Digest) send(ctx context.Context, userID uuid.UUID, now time.Time) (bool, error) {
	var (
		prefs Preferences
		to    string
		items []Notification
		last  *time.Time
	)
	err := d.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := readPreferences(ctx, tx, userID, &prefs); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT email FROM app_user WHERE id = $1`, userID).Scan(&to); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `SELECT last_sent_at FROM notification_digest_sent WHERE user_id = $1`, userID).Scan(&last)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
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
	var since time.Time
	if last != nil {
		since = *last
	}
	if len(items) == 0 || !due(prefs.Digest, since, now) {
		return false, nil
	}
	ids := make([]uuid.UUID, len(items))
	for i, n := range items {
		ids[i] = n.ID
	}
	// The mail goes inside the transaction: if it fails, the claim and the
	// queue roll back and the next pass tries again.
	claimed := false
	_, err = d.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		err := tx.QueryRow(ctx, claimDigest, userID, now, last).Scan(&claimed)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM notification_digest WHERE user_id = $1 AND notification_id = ANY($2)`, userID, ids); err != nil {
			return err
		}
		if d.mailer == nil {
			return nil
		}
		return d.mailer.Send(ctx, digestMail(to, items, d.appURL))
	})
	if err != nil {
		return false, err
	}
	return claimed, nil
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
