package db

import (
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// lagWindow holds a replica's last few lag readings. Only the health loop
// touches it, one check at a time, so it needs no lock.
type lagWindow struct {
	samples []time.Duration
	next    int
	size    int
}

// newLagWindow keeps size readings; fewer than one keeps one, which judges on
// every check alone.
func newLagWindow(size int) *lagWindow {
	size = max(size, 1)
	return &lagWindow{samples: make([]time.Duration, 0, size), size: size}
}

// add records a reading and returns the lower median of those kept, so an even
// window needs more than half its readings late before the median is.
func (w *lagWindow) add(lag time.Duration) time.Duration {
	if len(w.samples) < w.size {
		w.samples = append(w.samples, lag)
	} else {
		w.samples[w.next] = lag
	}
	w.next = (w.next + 1) % w.size

	sorted := slices.Clone(w.samples)
	slices.Sort(sorted)
	return sorted[(len(sorted)-1)/2]
}

func newReplicaState(name string, pool *pgxpool.Pool, lagSamples int) *replicaState {
	r := &replicaState{name: name, lags: newLagWindow(lagSamples)}
	if pool != nil {
		r.pool.Store(pool)
	}
	return r
}

// judge decides from one health reading whether a replica takes reads. Lag is
// judged on the window's median so a single slow check does not flap it.
func (c *Cluster) judge(r *replicaState, inRecovery bool, lag time.Duration, receiverStatus string) {
	r.lagMillis.Store(lag.Milliseconds())

	if inRecovery {
		// A dropped receiver reports zero lag forever; an empty status means
		// this role cannot read the view, and the lag figure is trusted.
		if receiverStatus != "" && receiverStatus != "streaming" {
			c.markUnhealthy(r, fmt.Sprintf("wal receiver is %q, not streaming", receiverStatus))
			return
		}
		typical := r.lags.add(lag)
		if c.maxLag > 0 && typical > c.maxLag {
			c.markUnhealthy(r, fmt.Sprintf("replication lag %s (median of the last %d readings) exceeds %s", typical, len(r.lags.samples), c.maxLag))
			return
		}
	}

	if was := r.healthy.Swap(true); !was {
		r.lastErr.Store(nil)
		c.log.Info("replica healthy", "replica", r.name, "lag", lag)
	}
}
