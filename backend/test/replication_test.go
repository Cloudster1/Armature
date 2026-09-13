//go:build integration

package test

import (
	"context"
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

var _ = uuid.Nil
