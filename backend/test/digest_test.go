//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/notify"
)

// Both organizations' daily bundles go in the one pass at the digest hour, and
// what went is kept in the database so another worker neither resends nor skips.
func TestEachOrganizationsDigestGoesInOnePassAtTheDigestHour(t *testing.T) {
	h := newHarness(t)
	first := h.newWorkspace(t, "digestfirst")
	second := h.newWorkspace(t, "digestsecond")
	address := h.email(t, "twodigests")
	bob := h.joinExisting(t, first, address, "member")
	h.joinExisting(t, second, address, "member")

	inbox := notify.NewService(h.cluster)
	for _, ws := range []*workspace{first, second} {
		prefs := notify.DefaultPreferences()
		prefs.Digest = notify.DigestDaily
		if _, err := inbox.SavePreferences(ws.ctx, bob, prefs); err != nil {
			t.Fatal(err)
		}
	}

	mailer := &fakeMailer{}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	fan := notify.NewFanOut(h.cluster, nil, mailer, "http://app.test", log)
	queue := func(ws *workspace, summary string) string {
		t.Helper()
		made := ws.newIssue(t, summary)
		bobPtr := &bob
		if _, _, err := ws.issues.Update(ws.ctx, made.Key, issue.UpdateInput{Assignee: &bobPtr}, ws.actor); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"key": made.Key, "actorId": ws.actor.UserID, "changes": []map[string]any{{"field": "assignee"}}})
		if err := fan.Handle(context.Background(), events.Event{ID: uuid.New(), OrgID: ws.orgID, Topic: events.TopicIssueUpdated, Payload: raw}); err != nil {
			t.Fatal(err)
		}
		return made.Key
	}
	bobsMail := func() []string {
		mailer.mu.Lock()
		defer mailer.mu.Unlock()
		var bodies []string
		for _, m := range mailer.sent {
			if m.To == address {
				bodies = append(bodies, m.Body)
			}
		}
		return bodies
	}
	pass := func(at time.Time) {
		t.Helper()
		digest := notify.NewDigest(h.cluster, mailer, "http://app.test", log).WithClock(func() time.Time { return at })
		if _, err := digest.Once(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	day := func(d, hour, minute int) time.Time { return time.Date(2026, 3, d, hour, minute, 0, 0, time.Local) }

	firstKey, secondKey := queue(first, "Fix the door"), queue(second, "Paint the hall")
	pass(day(2, notify.DailyDigestHour, 0))
	got := bobsMail()
	if len(got) != 2 || strings.Contains(got[0], firstKey) == strings.Contains(got[1], firstKey) || !strings.Contains(got[0]+got[1], secondKey) {
		t.Fatalf("mail to bob at the digest hour = %q, want one bundle from each organization", got)
	}

	var kept int
	if err := h.super.QueryRow(context.Background(),
		`SELECT count(*) FROM notification_digest_sent WHERE user_id = $1 AND org_id IN ($2, $3) AND last_sent_at = $4`,
		bob, first.orgID, second.orgID, day(2, notify.DailyDigestHour, 0)).Scan(&kept); err != nil || kept != 2 {
		t.Fatalf("records of the digests sent = %d (%v), want one per organization", kept, err)
	}

	// A worker that restarts the same day has not forgotten the first bundle.
	waiting := queue(first, "Oil the hinge")
	pass(day(2, notify.DailyDigestHour, 30))
	if got := bobsMail(); len(got) != 2 {
		t.Fatalf("mail to bob after a restart the same day = %q, want nothing more", got)
	}

	// The next day's pass that missed the hour still sends it.
	pass(day(3, notify.DailyDigestHour+2, 0))
	if got := bobsMail(); len(got) != 3 || !strings.Contains(got[2], waiting) {
		t.Fatalf("mail after the missed hour = %q, want the waiting bundle", got)
	}

	// Two workers in the same pass mail each bundle once.
	queue(first, "Sweep the floor")
	queue(second, "Fix the light")
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() { pass(day(4, notify.DailyDigestHour, 0)) })
	}
	workers.Wait()
	if got := bobsMail(); len(got) != 5 {
		t.Fatalf("two workers sent %d bundles to bob, want one per organization", len(got)-3)
	}
}

// The record of what was sent belongs to its organization: another one can
// neither read it nor write one for it, whatever the service does.
func TestTheRecordOfADigestStaysInItsOrganization(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	mine := h.newWorkspace(t, "digestmine")
	theirs := h.newWorkspace(t, "digesttheirs")
	bob := h.joinExisting(t, mine, h.email(t, "digestrecord"), "member")
	if _, err := h.super.Exec(context.Background(),
		`INSERT INTO notification_digest_sent (org_id, user_id, last_sent_at) VALUES ($1, $2, now())`, mine.orgID, bob); err != nil {
		t.Fatal(err)
	}

	var seen int
	if err := h.cluster.ReadPrimary(theirs.ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM notification_digest_sent WHERE user_id = $1`, bob).Scan(&seen)
	}); err != nil || seen != 0 {
		t.Errorf("another organization sees %d records of the digest (%v)", seen, err)
	}

	_, err := h.cluster.Write(theirs.ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `INSERT INTO notification_digest_sent (org_id, user_id, last_sent_at) VALUES ($1, $2, now())`, mine.orgID, bob)
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
		t.Errorf("another organization wrote a record of the digest: %v, want a policy violation", err)
	}
}
