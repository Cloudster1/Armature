package db

import (
	"context"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"

	"github.com/armature/armature/backend/internal/config"
)

// downReplica drops every connection like a Postgres still starting, and counts
// them so a test can see whether anybody is still trying.
func downReplica(t *testing.T) (url string, attempts *atomic.Int64) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	attempts = new(atomic.Int64)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			attempts.Add(1)
			_ = conn.Close()
		}
	}()
	return "postgres://app:secret@" + ln.Addr().String() + "/armature?sslmode=disable&connect_timeout=2", attempts
}

// A replica that is down when the process starts must still be part of the
// cluster, so the health loop keeps trying it and /readyz shows it missing.
func TestReplicaDownAtStartIsRegisteredAndRetried(t *testing.T) {
	url, attempts := downReplica(t)
	c := &Cluster{log: slog.New(slog.DiscardHandler)}
	cfg := config.DB{MaxConns: 1}

	c.addReplica(cfg, "replica-0", url, nil)
	c.checkAll(t.Context())

	stats := c.Stats()
	if len(stats.Replicas) != 1 {
		t.Fatalf("the cluster lists %d replicas, want the one that is down", len(stats.Replicas))
	}
	got := stats.Replicas[0]
	if got.Name != "replica-0" {
		t.Errorf("replica is named %q, want replica-0", got.Name)
	}
	if got.Healthy {
		t.Error("a replica that cannot be reached is in rotation")
	}
	if got.LastError == "" {
		t.Error("a replica that cannot be reached reports no error")
	}

	if r, outcome := pickReplica(c.replicas, 0, pin{}); r != nil || outcome != routeNoHealthy {
		t.Errorf("a read was routed to %v (%v), want the primary because no replica is healthy", r, outcome)
	}

	tried := attempts.Load()
	if tried == 0 {
		t.Fatal("nothing tried to connect to the replica")
	}
	c.checkAll(context.Background())
	if attempts.Load() <= tried {
		t.Error("the health check did not try to connect to the replica again")
	}
}
