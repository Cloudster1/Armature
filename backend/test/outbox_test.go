//go:build integration

package test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// redisClient connects to the stack's Redis, or skips the test when it is not
// reachable.
func redisClient(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv("ARMATURE_REDIS_URL")
	if url == "" {
		t.Skip("ARMATURE_REDIS_URL is not set")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	client := redis.NewClient(opts)
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("redis is not reachable: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// countUnpublished reports how many events are still waiting for the relay.
func (h *harness) countUnpublished(t *testing.T, orgID uuid.UUID) int {
	t.Helper()
	var n int
	err := h.super.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox_event WHERE org_id = $1 AND published_at IS NULL`, orgID).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestOutbox(t *testing.T) {
	h := newHarness(t)

	t.Run("an event is committed with the change that caused it", func(t *testing.T) {
		orgID, orgCtx := h.makeOrg(t, "outbox")

		if _, err := h.cluster.Write(orgCtx, func(ctx context.Context, tx db.DBTX) error {
			if _, err := tx.Exec(ctx, `
				INSERT INTO audit_log (org_id, action, target_type) VALUES ($1, 'thing.happened', 'thing')`, orgID); err != nil {
				return err
			}
			return events.EmitInTenant(ctx, tx, "test.thing.happened", map[string]any{"n": 1})
		}); err != nil {
			t.Fatalf("write: %v", err)
		}

		if got := h.countUnpublished(t, orgID); got != 1 {
			t.Errorf("%d unpublished events, want 1", got)
		}
	})

	// The whole point of the outbox: an event must not survive a transaction
	// that did not.
	t.Run("a rolled back change leaves no event behind", func(t *testing.T) {
		orgID, orgCtx := h.makeOrg(t, "rollback")

		sentinel := errors.New("deliberate failure after emitting")
		_, err := h.cluster.Write(orgCtx, func(ctx context.Context, tx db.DBTX) error {
			if err := events.EmitInTenant(ctx, tx, "test.should.not.exist", map[string]any{}); err != nil {
				return err
			}
			return sentinel
		})
		if !errors.Is(err, sentinel) {
			t.Fatalf("error = %v, want the sentinel", err)
		}

		if got := h.countUnpublished(t, orgID); got != 0 {
			t.Errorf("%d events survived a rolled back transaction, want 0", got)
		}
	})

	t.Run("an event cannot be emitted into another tenant", func(t *testing.T) {
		orgA, ctxA := h.makeOrg(t, "emitter")
		orgB, _ := h.makeOrg(t, "victim")

		_, err := h.cluster.Write(ctxA, func(ctx context.Context, tx db.DBTX) error {
			return events.Emit(ctx, tx, orgB, "test.cross.tenant", map[string]any{})
		})
		if err == nil {
			t.Error("an event was emitted into another organization")
		}
		if got := h.countUnpublished(t, orgB); got != 0 {
			t.Errorf("organization B has %d events from A, want 0", got)
		}
		_ = orgA
	})
}

func TestOutboxRelay(t *testing.T) {
	h := newHarness(t)
	rdb := redisClient(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	// emit writes n events for a fresh organization and returns its id.
	emit := func(t *testing.T, name string, n int) uuid.UUID {
		t.Helper()
		orgID, orgCtx := h.makeOrg(t, name)
		for i := range n {
			if _, err := h.cluster.Write(orgCtx, func(ctx context.Context, tx db.DBTX) error {
				return events.EmitInTenant(ctx, tx, "test.relayed", map[string]any{"i": i})
			}); err != nil {
				t.Fatalf("emit %d: %v", i, err)
			}
		}
		return orgID
	}

	// drain runs the relay until the organization has nothing unpublished, or
	// the deadline passes.
	drain := func(t *testing.T, orgID uuid.UUID, within time.Duration) {
		t.Helper()
		runCtx, cancel := context.WithTimeout(ctx, within)
		defer cancel()

		done := make(chan struct{})
		go func() {
			_ = events.NewRelay(h.cluster, rdb, log).Run(runCtx)
			close(done)
		}()

		deadline := time.Now().Add(within)
		for time.Now().Before(deadline) {
			if h.countUnpublished(t, orgID) == 0 {
				cancel()
				<-done
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		cancel()
		<-done
		t.Fatalf("the relay left %d events unpublished after %s", h.countUnpublished(t, orgID), within)
	}

	// The stream is capped, so its length stops growing once it is full; what
	// arrived is counted from the last id instead.
	mark := func(t *testing.T) string {
		t.Helper()
		info, err := rdb.XInfoStream(ctx, events.StreamKey).Result()
		if err != nil {
			if strings.Contains(err.Error(), "no such key") {
				return "0-0"
			}
			t.Fatal(err)
		}
		return info.LastGeneratedID
	}
	since := func(t *testing.T, id string) int64 {
		t.Helper()
		entries, err := rdb.XRange(ctx, events.StreamKey, "("+id, "+").Result()
		if err != nil {
			t.Fatal(err)
		}
		return int64(len(entries))
	}

	t.Run("committed events reach the stream and are marked published", func(t *testing.T) {
		before := mark(t)

		orgID := emit(t, "relayed", 3)
		drain(t, orgID, 10*time.Second)

		if grew := since(t, before); grew < 3 {
			t.Errorf("the stream grew by %d, want at least the 3 events emitted", grew)
		}
	})

	t.Run("a second pass does not republish what it already sent", func(t *testing.T) {
		orgID := emit(t, "once", 2)
		drain(t, orgID, 10*time.Second)

		before := mark(t)

		// Run the relay again over the same, now-published, rows.
		runCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		_ = events.NewRelay(h.cluster, rdb, log).Run(runCtx)
		cancel()

		if grew := since(t, before); grew != 0 {
			t.Errorf("the stream grew by %d on a second pass over published events, want 0", grew)
		}
	})

	// Several worker replicas relay at once in production. FOR UPDATE SKIP
	// LOCKED is what stops them blocking on, or duplicating, each other's rows.
	t.Run("concurrent relays neither block nor duplicate", func(t *testing.T) {
		const eventCount = 40
		orgID := emit(t, "concurrent", eventCount)

		before := mark(t)

		runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = events.NewRelay(h.cluster, rdb, log).Run(runCtx)
			}()
		}

		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) && h.countUnpublished(t, orgID) > 0 {
			time.Sleep(50 * time.Millisecond)
		}
		cancel()
		wg.Wait()

		if remaining := h.countUnpublished(t, orgID); remaining != 0 {
			t.Fatalf("%d events left unpublished", remaining)
		}

		// At-least-once delivery is the contract, so a duplicate is tolerable
		// but a large excess would mean the locking is not working at all.
		grew := since(t, before)
		if grew < eventCount {
			t.Errorf("the stream grew by %d, want at least %d", grew, eventCount)
		}
		if grew > eventCount*2 {
			t.Errorf("the stream grew by %d for %d events, which suggests relays are duplicating each other's work", grew, eventCount)
		}
	})

	t.Run("a publish failure is recorded and the event is retried", func(t *testing.T) {
		orgID := emit(t, "failing", 1)

		// A client pointed at nothing, so publishing cannot succeed.
		broken := redis.NewClient(&redis.Options{
			Addr:        "127.0.0.1:1",
			DialTimeout: 200 * time.Millisecond,
			MaxRetries:  -1,
		})
		defer broken.Close()

		runCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_ = events.NewRelay(h.cluster, broken, log).Run(runCtx)
		cancel()

		if got := h.countUnpublished(t, orgID); got != 1 {
			t.Errorf("%d unpublished events after a failed publish, want the event to still be waiting", got)
		}

		var attempts int
		var lastError *string
		if err := h.super.QueryRow(ctx,
			`SELECT attempts, last_error FROM outbox_event WHERE org_id = $1`, orgID).Scan(&attempts, &lastError); err != nil {
			t.Fatal(err)
		}
		if attempts == 0 {
			t.Error("the failed attempt was not counted")
		}
		if lastError == nil || *lastError == "" {
			t.Error("the failure reason was not recorded")
		}

		// And a working relay still delivers it afterwards.
		drain(t, orgID, 10*time.Second)
	})
}
