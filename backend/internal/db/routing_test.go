package db

import "testing"

// newReplica builds a replica in a given health and replay state.
func newReplica(name string, healthy bool, lsn LSN) *replicaState {
	r := &replicaState{name: name}
	r.healthy.Store(healthy)
	r.replayLSN.Store(uint64(lsn))
	return r
}

func TestPickReplica(t *testing.T) {
	tests := []struct {
		name        string
		replicas    []*replicaState
		start       int
		pin         pin
		wantOutcome routeOutcome
		wantReplica string
	}{
		{
			name:        "no replicas configured falls back to primary",
			replicas:    nil,
			wantOutcome: routeNoHealthy,
		},
		{
			name:        "explicit primary pin skips healthy replicas",
			replicas:    []*replicaState{newReplica("a", true, 100)},
			pin:         pin{forcePrimary: true},
			wantOutcome: routePinned,
		},
		{
			name:        "healthy replica serves an unpinned read",
			replicas:    []*replicaState{newReplica("a", true, 100)},
			wantOutcome: routeReplica,
			wantReplica: "a",
		},
		{
			name:        "unhealthy replicas are skipped",
			replicas:    []*replicaState{newReplica("a", false, 100), newReplica("b", true, 100)},
			wantOutcome: routeReplica,
			wantReplica: "b",
		},
		{
			name:        "all replicas unhealthy falls back to primary",
			replicas:    []*replicaState{newReplica("a", false, 100), newReplica("b", false, 100)},
			wantOutcome: routeNoHealthy,
		},
		{
			name:        "replica caught up to the required lsn is used",
			replicas:    []*replicaState{newReplica("a", true, 150)},
			pin:         pin{requiredLSN: 100},
			wantOutcome: routeReplica,
			wantReplica: "a",
		},
		{
			name:        "replica exactly at the required lsn is used",
			replicas:    []*replicaState{newReplica("a", true, 100)},
			pin:         pin{requiredLSN: 100},
			wantOutcome: routeReplica,
			wantReplica: "a",
		},
		{
			name:        "lagging replica is skipped for the primary",
			replicas:    []*replicaState{newReplica("a", true, 99)},
			pin:         pin{requiredLSN: 100},
			wantOutcome: routeStale,
		},
		{
			name: "a caught-up replica is preferred over a lagging one",
			replicas: []*replicaState{
				newReplica("behind", true, 50),
				newReplica("ahead", true, 200),
			},
			pin:         pin{requiredLSN: 100},
			wantOutcome: routeReplica,
			wantReplica: "ahead",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, outcome := pickReplica(tt.replicas, tt.start, tt.pin)
			if outcome != tt.wantOutcome {
				t.Fatalf("outcome = %v, want %v", outcome, tt.wantOutcome)
			}
			if tt.wantReplica == "" {
				if got != nil {
					t.Fatalf("expected no replica, got %q", got.name)
				}
				return
			}
			if got == nil || got.name != tt.wantReplica {
				t.Fatalf("replica = %v, want %q", got, tt.wantReplica)
			}
		})
	}
}

// TestPickReplicaRotates asserts that consecutive reads spread across replicas
// rather than pinning every read to the same one.
func TestPickReplicaRotates(t *testing.T) {
	replicas := []*replicaState{
		newReplica("a", true, 100),
		newReplica("b", true, 100),
		newReplica("c", true, 100),
	}
	seen := map[string]int{}
	for i := range 30 {
		r, outcome := pickReplica(replicas, i, pin{})
		if outcome != routeReplica {
			t.Fatalf("read %d was not served by a replica: %v", i, outcome)
		}
		seen[r.name]++
	}
	if len(seen) != 3 {
		t.Fatalf("expected all three replicas to be used, got %v", seen)
	}
	for name, n := range seen {
		if n != 10 {
			t.Errorf("replica %s served %d of 30 reads, want an even 10", name, n)
		}
	}
}
