//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
)

// TestReadsReachTheReplica establishes the baseline: with replication healthy
// and no freshness requirement, reads are served by the standby. Without this,
// the tests below would pass trivially on a cluster that never used a replica
// at all.
func TestReadsReachTheReplica(t *testing.T) {
	h := newHarness(t)
	if h.replica == nil {
		t.Skip("no replica configured")
	}
	_, ctx := h.makeOrg(t, "routing")

	before := h.cluster.Stats()
	for range 5 {
		if err := h.cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
			var n int
			return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&n)
		}); err != nil {
			t.Fatalf("read: %v", err)
		}
	}
	after := h.cluster.Stats()

	served := after.ReadsToReplica - before.ReadsToReplica
	if served != 5 {
		t.Errorf("%d of 5 reads went to a replica (primary: %d, stale fallbacks: %d)",
			served, after.ReadsToPrimary-before.ReadsToPrimary, after.StaleFallbacks-before.StaleFallbacks)
	}
}

// TestReadYourWritesUnderLag is the reason the routing layer exists.
//
// With replay paused the standby is genuinely behind. A caller who has just
// written must still see their own change; a caller who has not written may be
// served the older snapshot. Pausing replay rather than hoping to win a race is
// what makes this deterministic.
// A position read inside the transaction sits before the commit record, and a
// replica there has not replayed the change: seen as an empty read under load.
func TestWriteReportsThePositionPastItsCommit(t *testing.T) {
	h := newHarness(t)
	orgID, ctx := h.makeOrg(t, "commitlsn")

	var inside db.LSN
	after, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_log (org_id, action, target_type) VALUES ($1, 'commit.lsn', 'org')`, orgID); err != nil {
			return err
		}
		var text string
		if err := tx.QueryRow(ctx, `SELECT pg_current_wal_insert_lsn()::text`).Scan(&text); err != nil {
			return err
		}
		parsed, err := db.ParseLSN(text)
		inside = parsed
		return err
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if after <= inside {
		t.Fatalf("reported position %v is not past the one read inside the transaction %v; the commit record lies beyond it", after, inside)
	}
}

func TestReadYourWritesUnderLag(t *testing.T) {
	h := newHarness(t)
	if h.replica == nil {
		t.Skip("no replica configured")
	}
	orgID, ctx := h.makeOrg(t, "ryw")

	// Let the replica see the organization itself before we freeze it, so the
	// only thing it is missing is the write made below.
	warmup, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO audit_log (org_id, action, target_type) VALUES ($1, 'warmup', 'org')`, orgID)
		return err
	})
	if err != nil {
		t.Fatalf("warmup write: %v", err)
	}
	if !h.waitForReplica(t, warmup, 10*time.Second) {
		t.Fatal("replica never caught up to the warmup write")
	}

	h.pauseReplay(t)

	// This write can only be on the primary now.
	lsn, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO audit_log (org_id, action, target_type) VALUES ($1, 'after.pause', 'org')`, orgID)
		return err
	})
	if err != nil {
		t.Fatalf("write during pause: %v", err)
	}
	if lsn == 0 {
		t.Fatal("Write returned a zero log position, so nothing can be pinned to it")
	}

	count := func(ctx context.Context) int {
		t.Helper()
		var n int
		if err := h.cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action = 'after.pause'`).Scan(&n)
		}); err != nil {
			t.Fatalf("read: %v", err)
		}
		return n
	}

	t.Run("the writer sees their own write", func(t *testing.T) {
		if got := count(db.PinLSN(ctx, lsn)); got != 1 {
			t.Errorf("a reader pinned to their own write saw %d rows, want 1", got)
		}
	})

	t.Run("the pinned read fell back to the primary", func(t *testing.T) {
		before := h.cluster.Stats()
		_ = count(db.PinLSN(ctx, lsn))
		after := h.cluster.Stats()

		if after.StaleFallbacks == before.StaleFallbacks {
			t.Error("the pinned read was not recorded as a stale-replica fallback, so it may have been served by the frozen standby")
		}
		if after.ReadsToPrimary == before.ReadsToPrimary {
			t.Error("the pinned read did not go to the primary")
		}
	})

	t.Run("an unpinned read may still be served the older snapshot", func(t *testing.T) {
		// This is the behaviour we are trading for: a reader with no recent
		// write of their own is allowed a slightly stale answer, which is what
		// makes replica reads worth having.
		if got := count(ctx); got != 0 {
			t.Logf("unpinned read saw %d rows; the replica had already caught up or fell back to the primary", got)
		}
	})

	h.resumeReplay(t)

	if !h.waitForReplica(t, lsn, 10*time.Second) {
		t.Fatal("replica did not catch up after replay resumed")
	}
	if got := count(ctx); got != 1 {
		t.Errorf("after replay resumed an unpinned read saw %d rows, want 1", got)
	}
}

// TestLaggingReplicaLeavesTheRotation checks the health loop's judgement: a
// standby far enough behind must stop taking reads entirely, not merely be
// skipped by callers who happen to be pinned.
func TestLaggingReplicaLeavesTheRotation(t *testing.T) {
	h := newHarness(t)
	if h.replica == nil {
		t.Skip("no replica configured")
	}
	orgID, ctx := h.makeOrg(t, "lag")

	h.pauseReplay(t)

	// Generate a backlog with timestamps, so that lag is measured as elapsed
	// time and not merely as a log position difference.
	for range 3 {
		if _, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO audit_log (org_id, action, target_type) VALUES ($1, 'backlog', 'org')`, orgID)
			return err
		}); err != nil {
			t.Fatalf("backlog write: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	// The configured threshold is small and the health loop runs every 200ms in
	// tests, so the replica should be dropped well inside this window.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		stats := h.cluster.Stats()
		if len(stats.Replicas) > 0 && !stats.Replicas[0].Healthy {
			t.Logf("replica left the rotation: %s", stats.Replicas[0].LastError)

			before := h.cluster.Stats()
			var n int
			if err := h.cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
				return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&n)
			}); err != nil {
				t.Fatalf("read: %v", err)
			}
			after := h.cluster.Stats()
			if after.ReadsToPrimary == before.ReadsToPrimary {
				t.Error("an unhealthy replica still served a read")
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("replica stayed healthy despite replay being paused for several seconds")
}

// TestIdleReplicaStaysHealthy guards the bug this suite was written after: an
// idle standby has replayed everything it received, so it must report zero lag
// rather than however long ago the last transaction happened to be.
func TestIdleReplicaStaysHealthy(t *testing.T) {
	h := newHarness(t)
	if h.replica == nil {
		t.Skip("no replica configured")
	}
	orgID, ctx := h.makeOrg(t, "idle")

	lsn, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO audit_log (org_id, action, target_type) VALUES ($1, 'idle', 'org')`, orgID)
		return err
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if !h.waitForReplica(t, lsn, 10*time.Second) {
		t.Fatal("replica never caught up")
	}

	// Write nothing for longer than the configured lag threshold. A naive
	// "now() minus last replayed transaction" would cross it here.
	time.Sleep(3 * time.Second)

	stats := h.cluster.Stats()
	if len(stats.Replicas) == 0 {
		t.Skip("no replica in the rotation")
	}
	if !stats.Replicas[0].Healthy {
		t.Errorf("an idle but fully caught-up replica was marked unhealthy: %s", stats.Replicas[0].LastError)
	}
}

// holdTheReplicaHere lets the replica replay everything written so far, waits
// until the router has seen it do so, and stops it: what follows is primary only.
func (h *harness) holdTheReplicaHere(t *testing.T) {
	t.Helper()
	var text string
	if err := h.super.QueryRow(context.Background(), `SELECT pg_current_wal_lsn()::text`).Scan(&text); err != nil {
		t.Fatalf("read the primary's position: %v", err)
	}
	reached, err := db.ParseLSN(text)
	if err != nil {
		t.Fatal(err)
	}
	if !h.waitForReplica(t, reached, 10*time.Second) {
		t.Fatal("the replica never caught up")
	}
	routable := func() bool {
		for _, r := range h.cluster.Stats().Replicas {
			if r.Healthy && r.ReplayLSN >= reached {
				return true
			}
		}
		return false
	}
	for deadline := time.Now().Add(10 * time.Second); !routable(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the cluster never saw the replica catch up")
		}
	}
	h.pauseReplay(t)
}

// listsInvite says whether the caller's list of invitations holds one.
func listsInvite(t *testing.T, c *client, inviteID string) bool {
	t.Helper()
	for _, item := range list(t, want(t, c.get("/api/v1/invites"), http.StatusOK, "invitations"), "invites") {
		if item.(map[string]any)["id"] == inviteID {
			return true
		}
	}
	return false
}

// An administrator who sends or withdraws an invitation sees the list as they
// left it, however far a replica lags (issue #100).
func TestTheInvitationsListShowsWhatWasJustSentOrWithdrawn(t *testing.T) {
	h := newHarness(t)
	if h.replica == nil {
		t.Skip("no replica configured")
	}
	api := newAPIServer(t, h)

	// Two sessions, so that the one write each makes is the only one pinning it.
	sender := api.client(t)
	sender.signup(t, h, "invsend")
	withdrawer := api.client(t)
	withdrawer.signup(t, h, "invwithdraw")
	withdrawn := idOf(t, want(t, withdrawer.post("/api/v1/invites", map[string]string{"email": h.email(t, "invgone"), "role": "member"}), http.StatusCreated, "an invitation"), "invite")

	h.holdTheReplicaHere(t)

	t.Run("an invitation just sent is listed", func(t *testing.T) {
		sent := idOf(t, want(t, sender.post("/api/v1/invites", map[string]string{"email": h.email(t, "invnew"), "role": "member"}), http.StatusCreated, "an invitation"), "invite")
		if !listsInvite(t, sender, sent) {
			t.Error("the invitation just sent is missing from the list read straight after")
		}
	})

	t.Run("an invitation just withdrawn is gone", func(t *testing.T) {
		want(t, withdrawer.delete("/api/v1/invites/"+withdrawn), http.StatusNoContent, "withdrawing the invitation")
		if listsInvite(t, withdrawer, withdrawn) {
			t.Error("the invitation just withdrawn is still in the list read straight after")
		}
	})
}

// A project with no dashboard is given its default one on first sight; that
// same answer must show it, not a replica that has not seen it made.
func TestADashboardMadeOnFirstSightIsShownAtOnce(t *testing.T) {
	h := newHarness(t)
	if h.replica == nil {
		t.Skip("no replica configured")
	}
	api := newAPIServer(t, h)
	c := api.client(t)
	orgID := principalField(t, c.signup(t, h, "dashsight"), "principal", "org", "id").(string)
	key := "DSH" + strings.ToUpper(uuid.New().String()[:3])
	want(t, c.post("/api/v1/projects", map[string]any{"name": "Sighted", "key": key, "template": "kanban"}), http.StatusCreated, "a project")

	// As a project made before dashboards existed: it has none at all.
	if _, err := h.super.Exec(context.Background(), `
		DELETE FROM dashboard WHERE project_id = (SELECT id FROM project WHERE org_id = $1 AND key = $2)`, orgID, key); err != nil {
		t.Fatal(err)
	}
	h.holdTheReplicaHere(t)

	boards := list(t, want(t, c.get("/api/v1/projects/"+key+"/dashboards"), http.StatusOK, "dashboards"), "dashboards")
	if len(boards) == 0 {
		t.Error("the dashboard made on first sight is missing from the answer that made it")
	}
}

// Saving preferences answers with what was saved, however far a replica lags:
// the answer is read after the write, in the same request.
func TestSavedPreferencesAreTheAnswerWhileTheReplicaLags(t *testing.T) {
	h := newHarness(t)
	if h.replica == nil {
		t.Skip("no replica configured")
	}
	api := newAPIServer(t, h)
	c := api.client(t)
	c.signup(t, h, "prefsback")

	h.holdTheReplicaHere(t)

	saved := want(t, c.put("/api/v1/notification-preferences", map[string]any{"digest": "daily", "watchOwn": false, "mail": map[string]bool{}, "inapp": map[string]bool{}}), http.StatusOK, "saving")
	p := saved.Body["preferences"].(map[string]any)
	if p["digest"] != "daily" || p["watchOwn"] != false {
		t.Errorf("the answer to saving is %v, not what was saved", p)
	}
}
