//go:build integration

package test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/freshness"
)

// freshnessTTL is long enough that no key expires while a test reads it back.
const freshnessTTL = time.Minute

// Requests finish out of order; an older position landing last in the shared
// tracker must not let a replica serve the person a stale read.
func TestSharedFreshnessOnlyRises(t *testing.T) {
	rdb := redisClient(t)
	tr := freshness.NewRedisTracker(rdb, freshnessTTL)
	ctx := context.Background()

	session := func(t *testing.T) string {
		t.Helper()
		key := "freshness-test-" + uuid.NewString()
		t.Cleanup(func() { rdb.Del(context.Background(), "fresh:"+key) })
		return key
	}

	t.Run("an older position landing last leaves the newer one", func(t *testing.T) {
		key := session(t)
		tr.Note(ctx, key, 100)
		tr.Note(ctx, key, 90)
		if got := tr.Required(ctx, key); got != 100 {
			t.Fatalf("Required = %v after 100 then 90, want 100", got)
		}
		tr.Note(ctx, key, 110)
		if got := tr.Required(ctx, key); got != 110 {
			t.Errorf("Required = %v after a later 110, want it raised to 110", got)
		}
	})

	t.Run("the same position again changes nothing", func(t *testing.T) {
		key := session(t)
		tr.Note(ctx, key, 100)
		tr.Note(ctx, key, 100)
		if got := tr.Required(ctx, key); got != 100 {
			t.Errorf("Required = %v, want 100", got)
		}
	})

	t.Run("a position with more digits is the greater one", func(t *testing.T) {
		// Compared as text alone, "9" would sort after "10".
		key := session(t)
		tr.Note(ctx, key, 9)
		tr.Note(ctx, key, 10)
		if got := tr.Required(ctx, key); got != 10 {
			t.Errorf("Required = %v after 9 then 10, want 10", got)
		}
	})

	t.Run("positions beyond a double's precision still rise", func(t *testing.T) {
		// Lua numbers are doubles, to which 2^60 and 2^60+1 are the same.
		key := session(t)
		high := db.LSN(1 << 60)
		tr.Note(ctx, key, high)
		tr.Note(ctx, key, high+1)
		if got := tr.Required(ctx, key); got != high+1 {
			t.Errorf("Required = %v, want %v", got, high+1)
		}
		tr.Note(ctx, key, high)
		if got := tr.Required(ctx, key); got != high+1 {
			t.Errorf("Required = %v after an older %v, want %v kept", got, high, high+1)
		}
	})

	t.Run("the position still expires", func(t *testing.T) {
		key := session(t)
		tr.Note(ctx, key, 100)
		tr.Note(ctx, key, 90)
		ttl, err := rdb.PTTL(ctx, "fresh:"+key).Result()
		if err != nil {
			t.Fatal(err)
		}
		if ttl <= 0 || ttl > freshnessTTL {
			t.Errorf("the key lives for %v, want a lifetime of at most %v", ttl, freshnessTTL)
		}
		tr.Note(ctx, key, 110)
		ttl, err = rdb.PTTL(ctx, "fresh:"+key).Result()
		if err != nil {
			t.Fatal(err)
		}
		if ttl <= 0 || ttl > freshnessTTL {
			t.Errorf("after a raise the key lives for %v, want a lifetime of at most %v", ttl, freshnessTTL)
		}
	})

	t.Run("a lapsed position is replaced by any later write", func(t *testing.T) {
		const shortTTL = 50 * time.Millisecond
		short := freshness.NewRedisTracker(rdb, shortTTL)
		key := session(t)
		short.Note(ctx, key, 500)
		deadline := time.Now().Add(5 * time.Second)
		for short.Required(ctx, key) != 0 {
			if time.Now().After(deadline) {
				t.Fatal("the position never expired")
			}
			time.Sleep(10 * time.Millisecond)
		}
		short.Note(ctx, key, 200)
		if got := short.Required(ctx, key); got != 200 {
			t.Errorf("Required = %v after the old pin lapsed, want 200", got)
		}
	})
}
