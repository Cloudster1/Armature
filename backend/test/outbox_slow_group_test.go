//go:build integration

package test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/observability"
)

// An import emits events far faster than a group writing once per event takes
// them; the relay must wait for that group, not trim what it has not read.
func TestOutboxRelayWaitsForTheSlowestGroup(t *testing.T) {
	h := newHarness(t)
	rdb := redisClient(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	const (
		streamCap = 50
		// Past three of the 100 entry nodes a stream is kept in, so a trim by
		// length has whole nodes it could drop.
		eventCount = 300
		group      = "slow"
		// What the group spends on each of the import's events, so the relay
		// outruns it.
		handleCost = 2 * time.Millisecond
	)

	// A stream of its own, so the worker's groups, stopped for the suite, do
	// not hold this relay back too.
	stream := "armature:events:test:" + uuid.NewString()
	t.Cleanup(func() { rdb.Del(context.Background(), stream) })
	if err := rdb.XGroupCreateMkStream(ctx, stream, group, "0").Err(); err != nil {
		t.Fatal(err)
	}

	orgID, orgCtx := h.makeOrg(t, "slow-group")
	if _, err := h.cluster.Write(orgCtx, func(ctx context.Context, tx db.DBTX) error {
		for i := range eventCount {
			if err := events.EmitInTenant(ctx, tx, events.TopicIssueCreated, map[string]any{"i": i}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("emit: %v", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	relayDone := make(chan struct{})
	go func() {
		_ = events.NewRelay(h.cluster, rdb, log).Onto(stream, streamCap).Run(runCtx)
		close(relayDone)
	}()
	defer func() {
		cancel()
		<-relayDone
	}()

	var longest int64
	measure := func(t *testing.T) int64 {
		t.Helper()
		n, err := rdb.XLen(ctx, stream).Result()
		if err != nil {
			t.Fatal(err)
		}
		longest = max(longest, n)
		return n
	}

	// Before the group reads anything the relay fills the stream to the cap,
	// and then waits there instead of pushing the rest past it.
	deadline := time.Now().Add(30 * time.Second)
	for measure(t) < streamCap && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	settle := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(settle) {
		measure(t)
		time.Sleep(20 * time.Millisecond)
	}
	if longest > streamCap {
		t.Errorf("the stream grew to %d entries the slow group had not read, past the cap of %d", longest, streamCap)
	}
	if h.countUnpublished(t, orgID) == 0 {
		t.Error("the relay published the whole import while the group had read none of it")
	}

	// The group's lag is what an operator alerts on, so the collector must
	// report it while the relay waits.
	reg := prometheus.NewRegistry()
	reg.MustRegister(observability.NewStreamCollector(rdb, stream, []string{group}))
	if lag, ok := groupGauge(t, reg, "armature_stream_lag", group); !ok || lag < 1 {
		t.Errorf("armature_stream_lag{group=%q} = %v (reported: %v), want the entries it has not read", group, lag, ok)
	}

	// Now the group reads, slowly, until the relay has published everything
	// and the stream has nothing more to give.
	seen := map[uuid.UUID]bool{}
	deadline = time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		res, err := rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: group, Consumer: "test", Streams: []string{stream, ">"}, Count: 10, Block: 500 * time.Millisecond,
		}).Result()
		if errors.Is(err, redis.Nil) {
			if h.countUnpublished(t, orgID) == 0 {
				break
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range res {
			for _, msg := range s.Messages {
				if e := events.FromMessage(msg); e.OrgID == orgID {
					seen[e.ID] = true
					time.Sleep(handleCost)
				}
				if err := rdb.XAck(ctx, stream, group, msg.ID).Err(); err != nil {
					t.Fatal(err)
				}
			}
		}
		measure(t)
	}

	if len(seen) != eventCount {
		t.Errorf("the slow group handled %d of %d events; the rest were trimmed before it read them", len(seen), eventCount)
	}
	if longest > streamCap {
		t.Errorf("the stream grew to %d entries, past the cap of %d", longest, streamCap)
	}
	if left := h.countUnpublished(t, orgID); left != 0 {
		t.Errorf("%d events were never relayed", left)
	}
}

// groupGauge reads one group's value of a gauge off a registry.
func groupGauge(t *testing.T, reg *prometheus.Registry, name, group string) (float64, bool) {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "group" && l.GetValue() == group {
					return m.GetGauge().GetValue(), true
				}
			}
		}
	}
	return 0, false
}
