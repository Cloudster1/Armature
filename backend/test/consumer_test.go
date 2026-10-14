//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
)

// pendingScan bounds how much of a group's pending list a test reads through.
const pendingScan = 10_000

// quickly is the policy these tests run under: what the worker waits most of
// a minute for, they wait a fraction of a second for.
var quickly = events.Policy{
	MinIdle:       200 * time.Millisecond,
	ClaimEvery:    200 * time.Millisecond,
	MaxDeliveries: 3,
	Grace:         5 * time.Second,
}

// tally counts how often a handler was handed each event and how often it
// finished one, across the consumer's goroutine and the test's.
type tally struct {
	mu   sync.Mutex
	seen map[uuid.UUID]int
	done map[uuid.UUID]int
}

func newTally() *tally {
	return &tally{seen: map[uuid.UUID]int{}, done: map[uuid.UUID]int{}}
}

// handed records an attempt and returns which attempt it is.
func (c *tally) handed(id uuid.UUID) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen[id]++
	return c.seen[id]
}

func (c *tally) handled(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.done[id]++
}

func (c *tally) attempts(id uuid.UUID) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seen[id]
}

func (c *tally) finished(id uuid.UUID) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.done[id]
}

// all says whether every one of the events was finished.
func (c *tally) all(ids []uuid.UUID) bool {
	for _, id := range ids {
		if c.finished(id) == 0 {
			return false
		}
	}
	return true
}

// streamRig runs consumers over the shared stream under groups of the tests'
// own, so they read only what came after and leave the worker's groups alone.
type streamRig struct {
	h   *harness
	rdb *redis.Client
	log *slog.Logger
	// entries maps each event added to its id on the stream.
	entries map[uuid.UUID]string
}

func newStreamRig(t *testing.T) *streamRig {
	t.Helper()
	return &streamRig{
		h:   newHarness(t),
		rdb: redisClient(t),
		log: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),

		entries: map[uuid.UUID]string{},
	}
}

// group makes a consumer group at the stream's end and removes it afterwards.
func (r *streamRig) group(t *testing.T) string {
	t.Helper()
	name := "test-" + uuid.NewString()[:8]
	if err := r.rdb.XGroupCreateMkStream(context.Background(), events.StreamKey, name, "$").Err(); err != nil {
		t.Fatalf("create group: %v", err)
	}
	t.Cleanup(func() { _ = r.rdb.XGroupDestroy(context.Background(), events.StreamKey, name).Err() })
	return name
}

// add appends events for the installation the way the relay does, and returns
// their ids in order.
func (r *streamRig) add(t *testing.T, n int) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, n)
	for i := range n {
		ids[i] = uuid.New()
		id, err := r.rdb.XAdd(context.Background(), &redis.XAddArgs{Stream: events.StreamKey, Values: map[string]any{
			"id": ids[i].String(), "topic": "test.consumed", "payload": `{}`, "trace": "",
		}}).Result()
		if err != nil {
			t.Fatalf("add %d: %v", i, err)
		}
		r.entries[ids[i]] = id
	}
	return ids
}

// run starts a consumer and returns its context and what stops it, which
// waits until it has.
func (r *streamRig) run(t *testing.T, name string, handle events.Handler) (context.Context, func()) {
	t.Helper()
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- events.NewConsumer(r.h.cluster, r.rdb, r.log, name, handle).WithPolicy(quickly).Run(runCtx)
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("the consumer stopped with %v", err)
				}
			case <-time.After(10 * time.Second):
				t.Error("the consumer did not stop within ten seconds of being asked")
			}
		})
	}
	t.Cleanup(stop)
	return runCtx, stop
}

// pending counts which of the events are still unacknowledged in the group;
// whatever else is on the shared stream is not the test's to count.
func (r *streamRig) pending(t *testing.T, name string, ids []uuid.UUID) int64 {
	t.Helper()
	all, err := r.rdb.XPendingExt(context.Background(), &redis.XPendingExtArgs{
		Stream: events.StreamKey, Group: name, Start: "-", End: "+", Count: pendingScan,
	}).Result()
	if err != nil {
		t.Fatal(err)
	}
	ours := map[string]bool{}
	for _, id := range ids {
		ours[r.entries[id]] = true
	}
	var n int64
	for _, p := range all {
		if ours[p.ID] {
			n++
		}
	}
	return n
}

// deadLetters reads what an administrator reads on the audit page about the
// events the group set aside; other groups' are other tests'.
func (r *streamRig) deadLetters(t *testing.T, group string) []audit.Row {
	t.Helper()
	ctx := context.Background()
	rows, err := audit.NewService(r.h.cluster).List(db.PinPrimary(ctx), audit.Filter{Action: events.ActionDeadLettered})
	if err != nil {
		t.Fatal(err)
	}
	var ours []audit.Row
	for _, row := range rows {
		var data struct{ Group string }
		if json.Unmarshal(row.Data, &data) == nil && data.Group == group {
			ours = append(ours, row)
		}
	}
	return ours
}

func eventually(t *testing.T, within time.Duration, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s did not happen within %s", what, within)
}

// mine keeps a handler to the events its test added; anything else on the
// shared stream is somebody else's and passes.
func mine(ids []uuid.UUID) func(uuid.UUID) bool {
	set := map[uuid.UUID]bool{}
	for _, id := range ids {
		set[id] = true
	}
	return func(id uuid.UUID) bool { return set[id] }
}

// A primary failing over for a moment fails one attempt; the event is not
// acknowledged, so it comes round again and is handled then.
func TestAConsumerRetriesAHandlerThatFailedOnce(t *testing.T) {
	r := newStreamRig(t)
	name := r.group(t)
	ids := r.add(t, 1)
	ours := mine(ids)
	count := newTally()
	r.run(t, name, func(ctx context.Context, e events.Event) error {
		if !ours(e.ID) {
			return nil
		}
		if count.handed(e.ID) == 1 {
			return errors.New("the database is failing over")
		}
		count.handled(e.ID)
		return nil
	})

	eventually(t, 10*time.Second, "the second attempt", func() bool { return count.finished(ids[0]) == 1 })
	eventually(t, 5*time.Second, "the acknowledgement", func() bool { return r.pending(t, name, ids) == 0 })
	if got := count.attempts(ids[0]); got != 2 {
		t.Errorf("the event was handed over %d times, want twice: once failing, once succeeding", got)
	}
	if rows := r.deadLetters(t, name); len(rows) != 0 {
		t.Errorf("an event that succeeded on its second attempt was set aside: %+v", rows)
	}
}

// A shutdown finishes the event in hand with a context that is not cancelled
// and leaves the rest of the batch pending for whoever comes next.
func TestAStoppedConsumerFinishesTheEventInHandAndLeavesTheRest(t *testing.T) {
	r := newStreamRig(t)
	name := r.group(t)
	ids := r.add(t, 5)
	ours := mine(ids)
	count := newTally()

	var (
		first           context.Context
		stopFirst       func()
		mu              sync.Mutex
		cancelledInHand bool
	)
	started := make(chan struct{})
	first, stopFirst = r.run(t, name, func(ctx context.Context, e events.Event) error {
		<-started
		if !ours(e.ID) {
			return nil
		}
		count.handed(e.ID)
		// The shutdown arrives while this event is in hand.
		go stopFirst()
		<-first.Done()
		time.Sleep(100 * time.Millisecond)
		if ctx.Err() != nil {
			mu.Lock()
			cancelledInHand = true
			mu.Unlock()
			return ctx.Err()
		}
		count.handled(e.ID)
		return nil
	})
	close(started)

	eventually(t, 10*time.Second, "the first worker stopping", func() bool { return first.Err() != nil && count.finished(ids[0]) == 1 })
	stopFirst()
	mu.Lock()
	if cancelledInHand {
		t.Fatal("the event in hand was handled with a cancelled context")
	}
	mu.Unlock()
	for _, id := range ids[1:] {
		if count.attempts(id) != 0 {
			t.Fatalf("the stopping worker went on to event %s after the shutdown", id)
		}
	}
	if got := r.pending(t, name, ids); got != int64(len(ids)-1) {
		t.Errorf("%d entries left pending, want the %d the stopped worker never handled", got, len(ids)-1)
	}

	r.run(t, name, func(ctx context.Context, e events.Event) error {
		if ours(e.ID) {
			count.handed(e.ID)
			count.handled(e.ID)
		}
		return nil
	})
	eventually(t, 10*time.Second, "the next worker handling the rest", func() bool { return count.all(ids) })
	eventually(t, 5*time.Second, "the acknowledgements", func() bool { return r.pending(t, name, ids) == 0 })
	for _, id := range ids {
		if got := count.finished(id); got != 1 {
			t.Errorf("event %s was handled %d times, want once", id, got)
		}
	}
}

// A worker killed outright leaves its batch read and unacknowledged; the next
// one claims it on start.
func TestAKilledConsumersBatchIsHandledByTheNext(t *testing.T) {
	r := newStreamRig(t)
	name := r.group(t)
	ids := r.add(t, 3)
	ours := mine(ids)

	read, err := r.rdb.XReadGroup(context.Background(), &redis.XReadGroupArgs{
		Group: name, Consumer: "worker", Streams: []string{events.StreamKey, ">"}, Count: 50, Block: time.Second,
	}).Result()
	if err != nil || len(read) == 0 || len(read[0].Messages) < len(ids) {
		t.Fatalf("the killed worker's read: %v %v", read, err)
	}

	count := newTally()
	r.run(t, name, func(ctx context.Context, e events.Event) error {
		if ours(e.ID) {
			count.handed(e.ID)
			count.handled(e.ID)
		}
		return nil
	})
	eventually(t, 10*time.Second, "the new worker handling the killed worker's batch", func() bool { return count.all(ids) })
	eventually(t, 5*time.Second, "the acknowledgements", func() bool { return r.pending(t, name, ids) == 0 })
}

// An event no attempt can handle is tried a bounded number of times, then
// acknowledged and recorded where the organization's administrator reads.
func TestAnEventThatAlwaysFailsEndsInTheDeadLetterRecord(t *testing.T) {
	r := newStreamRig(t)
	name := r.group(t)
	ids := r.add(t, 1)
	ours := mine(ids)
	count := newTally()
	r.run(t, name, func(ctx context.Context, e events.Event) error {
		if !ours(e.ID) {
			return nil
		}
		count.handed(e.ID)
		return errors.New("the mail relay refuses everything")
	})

	eventually(t, 15*time.Second, "the dead letter", func() bool { return len(r.deadLetters(t, name)) == 1 })
	eventually(t, 5*time.Second, "the acknowledgement", func() bool { return r.pending(t, name, ids) == 0 })
	if got := count.attempts(ids[0]); got != int(quickly.MaxDeliveries) {
		t.Errorf("the event was handed over %d times, want %d before it was set aside", got, quickly.MaxDeliveries)
	}

	row := r.deadLetters(t, name)[0]
	if row.TargetID == nil || *row.TargetID != ids[0] || row.TargetType != "event" {
		t.Errorf("dead letter targets %s %v, want the event %s", row.TargetType, row.TargetID, ids[0])
	}
	var data map[string]any
	if err := json.Unmarshal(row.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data["group"] != name || data["topic"] != "test.consumed" || data["attempts"] != float64(quickly.MaxDeliveries) {
		t.Errorf("dead letter says %v, want the group, the topic and how often it was tried", data)
	}

	// It stays set aside: nothing comes back for it once acknowledged.
	time.Sleep(3 * quickly.ClaimEvery)
	if got := count.attempts(ids[0]); got != int(quickly.MaxDeliveries) {
		t.Errorf("the event was handed over again after it was set aside: %d attempts", got)
	}
}

// A handler that knows a retry cannot help, or would repeat what already went
// out, says so; the event is set aside on its first attempt.
func TestAPermanentFailureIsSetAsideAtOnce(t *testing.T) {
	r := newStreamRig(t)
	name := r.group(t)
	ids := r.add(t, 1)
	ours := mine(ids)
	count := newTally()
	r.run(t, name, func(ctx context.Context, e events.Event) error {
		if !ours(e.ID) {
			return nil
		}
		count.handed(e.ID)
		return events.Permanent(errors.New("one of two mails went out; a retry would send it again"))
	})

	eventually(t, 10*time.Second, "the dead letter", func() bool { return len(r.deadLetters(t, name)) == 1 })
	eventually(t, 5*time.Second, "the acknowledgement", func() bool { return r.pending(t, name, ids) == 0 })
	time.Sleep(3 * quickly.ClaimEvery)
	if got := count.attempts(ids[0]); got != 1 {
		t.Errorf("a permanent failure was handed over %d times, want once", got)
	}
}
