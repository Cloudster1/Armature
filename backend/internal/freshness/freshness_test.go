package freshness

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/armature/armature/backend/internal/db"
)

func TestMemoryTrackerRoundTrip(t *testing.T) {
	tr := NewMemoryTracker(time.Minute)
	ctx := context.Background()

	if got := tr.Required(ctx, "session-1"); got != 0 {
		t.Errorf("a key that never wrote requires %v, want 0", got)
	}

	tr.Note(ctx, "session-1", 500)
	if got := tr.Required(ctx, "session-1"); got != 500 {
		t.Errorf("Required = %v, want 500", got)
	}
	if got := tr.Required(ctx, "session-2"); got != 0 {
		t.Errorf("one session's write leaked into another: got %v", got)
	}
}

func TestMemoryTrackerKeepsTheNewerPosition(t *testing.T) {
	tr := NewMemoryTracker(time.Minute)
	ctx := context.Background()

	tr.Note(ctx, "s", 900)
	// A slower request finishing later must not walk the requirement backwards.
	tr.Note(ctx, "s", 100)
	if got := tr.Required(ctx, "s"); got != 900 {
		t.Errorf("Required = %v, want the newer position 900", got)
	}
}

func TestMemoryTrackerExpires(t *testing.T) {
	tr := NewMemoryTracker(30 * time.Second)
	ctx := context.Background()
	clock := time.Now()
	tr.now = func() time.Time { return clock }

	tr.Note(ctx, "s", 42)
	if got := tr.Required(ctx, "s"); got != 42 {
		t.Fatalf("Required = %v, want 42", got)
	}

	// Once a replica has certainly caught up, the pin should lapse so reads go
	// back to the replicas.
	clock = clock.Add(31 * time.Second)
	if got := tr.Required(ctx, "s"); got != 0 {
		t.Errorf("Required = %v after expiry, want 0", got)
	}
}

func TestMemoryTrackerIgnoresEmptyInput(t *testing.T) {
	tr := NewMemoryTracker(time.Minute)
	ctx := context.Background()
	tr.Note(ctx, "", 100)
	tr.Note(ctx, "s", 0)
	if got := tr.Required(ctx, ""); got != 0 {
		t.Errorf("Required(\"\") = %v, want 0", got)
	}
	if got := tr.Required(ctx, "s"); got != 0 {
		t.Errorf("a zero position should not be recorded, got %v", got)
	}
}

func TestRedisTrackerFailureIsBestEffort(t *testing.T) {
	// Pointed at nothing: noting must not fail or hang, and a read requires
	// nothing, so it goes to the primary as before.
	client := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 200 * time.Millisecond,
		MaxRetries:  -1,
	})
	defer client.Close()
	tr := NewRedisTracker(client, time.Minute)
	ctx := context.Background()

	tr.Note(ctx, "s", 100)
	if got := tr.Required(ctx, "s"); got != 0 {
		t.Errorf("Required = %v with Redis unreachable, want 0", got)
	}
}

var _ Tracker = (*MemoryTracker)(nil)
var _ Tracker = (*RedisTracker)(nil)

// db.LSN is compared numerically throughout; guard the assumption.
var _ = db.LSN(0)
