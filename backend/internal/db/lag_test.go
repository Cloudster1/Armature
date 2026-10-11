package db

import (
	"log/slog"
	"testing"
	"time"
)

const testMaxLag = 2 * time.Second

func judgingCluster() *Cluster {
	return &Cluster{maxLag: testMaxLag, log: slog.New(slog.DiscardHandler)}
}

// One slow health check is noise; ARMATURE_DB_REPLICA_LAG_SAMPLES says how many
// readings decide, and their median is what is held against the threshold.
func TestOneLateLagSampleKeepsAReplicaInTheRotation(t *testing.T) {
	c := judgingCluster()
	r := newReplicaState("a", nil, 3)
	late := 10 * time.Second

	for i, step := range []struct {
		lag         time.Duration
		wantHealthy bool
		why         string
	}{
		{0, true, "a caught-up replica is used"},
		{0, true, "a caught-up replica is used"},
		{late, true, "one late reading out of three must not take it out"},
		{late, false, "two late readings out of three are a replica that is behind"},
		{0, false, "one prompt reading must not bring it back while two of three are late"},
		{0, true, "two prompt readings out of three bring it back"},
	} {
		c.judge(r, true, step.lag, "streaming")
		if got := r.healthy.Load(); got != step.wantHealthy {
			t.Fatalf("after reading %d (%s): healthy = %v; %s", i+1, step.lag, got, step.why)
		}
	}
}

func TestAWindowOfOneActsOnEveryReading(t *testing.T) {
	for _, size := range []int{1, 0, -4} {
		c := judgingCluster()
		r := newReplicaState("a", nil, size)
		c.judge(r, true, 0, "streaming")
		c.judge(r, true, 10*time.Second, "streaming")
		if r.healthy.Load() {
			t.Errorf("window %d: a late reading should take the replica out at once", size)
		}
	}
}

// A dropped WAL receiver reports zero lag forever, so it is not a reading the
// window may outvote.
func TestADroppedReceiverTakesAReplicaOutAtOnce(t *testing.T) {
	c := judgingCluster()
	r := newReplicaState("a", nil, 3)
	c.judge(r, true, 0, "streaming")
	c.judge(r, true, 0, "stopping")
	if r.healthy.Load() {
		t.Fatal("a replica whose receiver is not streaming should leave the rotation")
	}
}

func TestLagWindowMedian(t *testing.T) {
	s := time.Second
	for _, tc := range []struct {
		name    string
		size    int
		samples []time.Duration
		want    time.Duration
	}{
		{"a single reading is its own median", 3, []time.Duration{7 * s}, 7 * s},
		{"odd window takes the middle", 3, []time.Duration{9 * s, 1 * s, 4 * s}, 4 * s},
		{"the oldest reading is forgotten", 3, []time.Duration{9 * s, 9 * s, 1 * s, 1 * s}, 1 * s},
		{"even window takes the lower middle", 4, []time.Duration{0, 10 * s, 0, 10 * s}, 0},
		{"a window of one is the last reading", 1, []time.Duration{9 * s, 1 * s}, 1 * s},
	} {
		w := newLagWindow(tc.size)
		var got time.Duration
		for _, lag := range tc.samples {
			got = w.add(lag)
		}
		if got != tc.want {
			t.Errorf("%s: median = %s, want %s", tc.name, got, tc.want)
		}
	}
}
