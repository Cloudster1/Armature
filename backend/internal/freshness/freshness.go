// Package freshness remembers how recent a reader's own writes were, so that
// their subsequent reads are never served by a replica that has not replayed
// them yet.
//
// The state is deliberately tiny and disposable: one write ahead log position
// per session, expiring after a few seconds. Losing it is harmless, it only
// means a read that could have gone to a replica goes to the primary instead.
package freshness

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/armature/armature/backend/internal/db"
)

// Tracker records and recalls the last write position observed for a key,
// usually a session id.
type Tracker interface {
	// Note records that the holder of key has just written up to lsn.
	Note(ctx context.Context, key string, lsn db.LSN)
	// Required returns the position a replica must have replayed to serve key,
	// or zero when there is no recent write to respect.
	Required(ctx context.Context, key string) db.LSN
}

// ---------------------------------------------------------------- redis ---

// RedisTracker shares freshness across every api process, which is what makes
// this work behind a load balancer: writing on one process and reading on
// another must still be consistent for the user.
type RedisTracker struct {
	client *redis.Client
	ttl    time.Duration
	prefix string
}

func NewRedisTracker(client *redis.Client, ttl time.Duration) *RedisTracker {
	return &RedisTracker{client: client, ttl: ttl, prefix: "fresh:"}
}

func (t *RedisTracker) Note(ctx context.Context, key string, lsn db.LSN) {
	if key == "" || lsn == 0 {
		return
	}
	// Best effort: a failure here costs a slightly stale read at worst, and
	// must never fail the write that just succeeded.
	_ = t.client.Set(ctx, t.prefix+key, uint64(lsn), t.ttl).Err()
}

func (t *RedisTracker) Required(ctx context.Context, key string) db.LSN {
	if key == "" {
		return 0
	}
	v, err := t.client.Get(ctx, t.prefix+key).Uint64()
	if err != nil {
		return 0
	}
	return db.LSN(v)
}

// --------------------------------------------------------------- memory ---

type entry struct {
	lsn db.LSN
	exp time.Time
}

// MemoryTracker is a single process implementation for tests and for running
// one api instance without Redis.
type MemoryTracker struct {
	mu      sync.Mutex
	entries map[string]entry
	ttl     time.Duration
	now     func() time.Time
}

func NewMemoryTracker(ttl time.Duration) *MemoryTracker {
	return &MemoryTracker{entries: make(map[string]entry), ttl: ttl, now: time.Now}
}

func (t *MemoryTracker) Note(_ context.Context, key string, lsn db.LSN) {
	if key == "" || lsn == 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	// An older position must not overwrite a newer one: requests can complete
	// out of order.
	if cur, ok := t.entries[key]; ok && cur.exp.After(now) && cur.lsn >= lsn {
		return
	}
	t.entries[key] = entry{lsn: lsn, exp: now.Add(t.ttl)}
	t.sweepLocked(now)
}

func (t *MemoryTracker) Required(_ context.Context, key string) db.LSN {
	if key == "" {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[key]
	if !ok || !e.exp.After(t.now()) {
		return 0
	}
	return e.lsn
}

// sweepLocked drops expired entries so an unbounded stream of sessions cannot
// grow the map forever.
func (t *MemoryTracker) sweepLocked(now time.Time) {
	if len(t.entries) < 1024 {
		return
	}
	for k, e := range t.entries {
		if !e.exp.After(now) {
			delete(t.entries, k)
		}
	}
}
