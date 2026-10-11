//go:build integration

package test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/config"
	"github.com/armature/armature/backend/internal/db"
)

// replicaGate stands between the cluster and the real standby. While shut it
// drops every connection, the way a standby that is still starting does.
type replicaGate struct {
	url string
	up  atomic.Bool
}

func newReplicaGate(t *testing.T, target string) *replicaGate {
	t.Helper()
	u, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse the replica address: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	backend := u.Host
	u.Host = ln.Addr().String()
	g := &replicaGate{url: u.String()}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go g.serve(conn, backend)
		}
	}()
	return g
}

func (g *replicaGate) serve(client net.Conn, backend string) {
	if !g.up.Load() {
		_ = client.Close()
		return
	}
	server, err := net.Dial("tcp", backend)
	if err != nil {
		_ = client.Close()
		return
	}
	go func() {
		_, _ = io.Copy(server, client)
		_ = server.Close()
	}()
	_, _ = io.Copy(client, server)
	_ = client.Close()
}

// A standby still starting while the api boots must join the rotation when it
// answers, not at the api's next restart.
func TestReplicaDownAtStartJoinsWhenItReturns(t *testing.T) {
	h := newHarness(t)
	if h.replica == nil {
		t.Skip("no replica configured")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(cfg.DB.ReplicaURLs) == 0 {
		t.Skip("no replica configured")
	}
	cfg.DB.HealthInterval = 200 * time.Millisecond
	gate := newReplicaGate(t, cfg.DB.ReplicaURLs[0])
	cfg.DB.ReplicaURLs = []string{gate.url}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	cluster, err := db.Open(context.Background(), cfg.DB, log)
	if err != nil {
		t.Fatalf("a replica that is down stopped the cluster from opening: %v", err)
	}
	t.Cleanup(cluster.Close)

	_, ctx := h.makeOrg(t, "latejoin")
	read := func(t *testing.T) {
		t.Helper()
		if err := cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
			var n int
			return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&n)
		}); err != nil {
			t.Fatalf("read: %v", err)
		}
	}

	t.Run("while it is down the replica is listed out of rotation", func(t *testing.T) {
		stats := cluster.Stats()
		if len(stats.Replicas) != 1 {
			t.Fatalf("the cluster lists %d replicas, want the one that is down", len(stats.Replicas))
		}
		if stats.Replicas[0].Healthy {
			t.Error("a replica nobody can reach is in rotation")
		}
		if stats.Replicas[0].LastError == "" {
			t.Error("a replica nobody can reach reports no error")
		}

		before := cluster.Stats()
		for range 3 {
			read(t)
		}
		after := cluster.Stats()
		if after.ReadsToReplica != before.ReadsToReplica {
			t.Errorf("%d reads went to a replica nobody can reach", after.ReadsToReplica-before.ReadsToReplica)
		}
		if after.ReadsToPrimary-before.ReadsToPrimary != 3 {
			t.Errorf("%d of 3 reads went to the primary", after.ReadsToPrimary-before.ReadsToPrimary)
		}
	})

	gate.up.Store(true)

	joined := false
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if s := cluster.Stats(); len(s.Replicas) == 1 && s.Replicas[0].Healthy {
			joined = true
			break
		}
	}
	if !joined {
		var reason string
		if s := cluster.Stats(); len(s.Replicas) == 1 {
			reason = s.Replicas[0].LastError
		}
		t.Fatalf("the replica came back but never joined the rotation (last error: %q)", reason)
	}

	t.Run("reads reach the replica once it answers", func(t *testing.T) {
		before := cluster.Stats()
		for range 5 {
			read(t)
		}
		after := cluster.Stats()
		if served := after.ReadsToReplica - before.ReadsToReplica; served != 5 {
			t.Errorf("%d of 5 reads went to the replica that came back (primary: %d)",
				served, after.ReadsToPrimary-before.ReadsToPrimary)
		}
	})

	t.Run("readyz lists the replica in rotation", func(t *testing.T) {
		late := *h
		late.cluster = cluster
		ready := want(t, newAPIServer(t, &late).client(t).get("/readyz"), http.StatusOK, "readiness")
		routing, _ := ready.Body["routing"].(map[string]any)
		replicas, _ := routing["replicas"].([]any)
		if len(replicas) != 1 {
			t.Fatalf("readyz lists %d replicas, want 1: %v", len(replicas), routing)
		}
		replica, _ := replicas[0].(map[string]any)
		if replica["name"] != "replica-0" || replica["healthy"] != true {
			t.Errorf("readyz shows %v, want replica-0 healthy", replica)
		}
	})
}
