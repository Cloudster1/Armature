package events

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/redis/go-redis/v9"
)

// groupState is what one consumer group still holds on the stream.
type groupState struct {
	lastDelivered string
	pending       int64
	oldestPending string
}

// oldestNeeded is the lowest id a group may still need: its oldest unacknowledged
// delivery, else its last one. False means no group reads the stream at all.
func oldestNeeded(groups []groupState) (string, bool) {
	floor, found := "", false
	for _, g := range groups {
		need := g.lastDelivered
		if g.pending > 0 && g.oldestPending != "" && compareIDs(g.oldestPending, need) < 0 {
			need = g.oldestPending
		}
		if !found || compareIDs(need, floor) < 0 {
			floor, found = need, true
		}
	}
	return floor, found
}

// compareIDs orders two stream ids, a millisecond and a sequence, as numbers.
// An id that does not parse sorts first, so it can only make a trim keep more.
func compareIDs(a, b string) int {
	var ams, aseq, bms, bseq uint64
	_, _ = fmt.Sscanf(a, "%d-%d", &ams, &aseq)
	_, _ = fmt.Sscanf(b, "%d-%d", &bms, &bseq)
	if c := cmp.Compare(ams, bms); c != 0 {
		return c
	}
	return cmp.Compare(aseq, bseq)
}

// room is how many events one pass may add without the slowest group falling
// more than the cap behind.
func room(backlog, capacity int64, batch int) int {
	free := capacity - backlog
	if free <= 0 {
		return 0
	}
	return int(min(free, int64(batch)))
}

// trim drops what every group has acknowledged and returns how many entries
// are left for the slowest group, which is what the relay waits on.
func (r *Relay) trim(ctx context.Context) (int64, error) {
	infos, err := r.redis.XInfoGroups(ctx, r.stream).Result()
	if err != nil {
		if strings.Contains(err.Error(), "no such key") {
			return 0, nil
		}
		return 0, fmt.Errorf("read the stream's groups: %w", err)
	}

	groups := make([]groupState, 0, len(infos))
	for _, info := range infos {
		// A group the worker no longer runs would hold the relay for ever.
		if r.groups != nil && !slices.Contains(r.groups, info.Name) {
			continue
		}
		g := groupState{lastDelivered: info.LastDeliveredID, pending: info.Pending}
		if info.Pending > 0 {
			p, err := r.redis.XPending(ctx, r.stream, info.Name).Result()
			if err != nil {
				return 0, fmt.Errorf("read what %s has not acknowledged: %w", info.Name, err)
			}
			g.pending, g.oldestPending = p.Count, p.Lower
		}
		groups = append(groups, g)
	}

	floor, ok := oldestNeeded(groups)
	if !ok {
		// No group reads the stream, and one made later starts at its end, so
		// the cap alone bounds what is kept.
		return 0, r.redis.XTrimMaxLenApprox(ctx, r.stream, r.maxLen, 0).Err()
	}
	// Exact, so the length left is the slowest group's backlog to the entry.
	if err := r.redis.XTrimMinID(ctx, r.stream, floor).Err(); err != nil {
		return 0, fmt.Errorf("trim the stream: %w", err)
	}
	backlog, err := r.redis.XLen(ctx, r.stream).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return 0, fmt.Errorf("measure the stream: %w", err)
	}
	return backlog, nil
}
