//go:build integration

package test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/armature/armature/backend/internal/attachment"
	"github.com/armature/armature/backend/internal/config"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/observability"
)

// shared is what every test's harness hands out, opened once by TestMain:
// opening five pools and a bucket per test cost more than most tests do.
var shared struct {
	cfg     config.Config
	log     *slog.Logger
	cluster *db.Cluster
	super   *pgxpool.Pool
	replica *pgxpool.Pool
	tel     *observability.Telemetry
	spans   *spanSink
	store   attachment.Store
}

func TestMain(m *testing.M) {
	opened := false
	if os.Getenv(envPrimary) != "" {
		if err := openShared(); err != nil {
			fmt.Fprintf(os.Stderr, "integration harness: %v\n", err)
			closeShared()
			os.Exit(1)
		}
		opened = true
	}
	code := m.Run()
	if !everyOperationWasExercised() && code == 0 {
		code = 1
	}
	if opened {
		closeShared()
	}
	os.Exit(code)
}

func openShared() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	// Health checks run tightly so tests do not wait seconds for a replica to
	// be re-evaluated after replay is paused or resumed.
	cfg.DB.HealthInterval = 200 * time.Millisecond
	shared.cfg = cfg

	ctx := context.Background()
	shared.log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))

	shared.spans = &spanSink{InMemoryExporter: tracetest.NewInMemoryExporter()}
	shared.tel, err = observability.Setup(ctx, observability.Config{Service: "armature-test", Env: "development", SampleRatio: 1}, shared.log, observability.WithSpanExporter(shared.spans))
	if err != nil {
		return fmt.Errorf("telemetry: %w", err)
	}

	shared.cluster, err = db.Open(ctx, cfg.DB, shared.log, db.WithQueryTracer(shared.tel.QueryTracer))
	if err != nil {
		return fmt.Errorf("open cluster: %w", err)
	}
	if err := shared.tel.Register(observability.NewClusterCollector(shared.cluster)); err != nil {
		return fmt.Errorf("cluster collector: %w", err)
	}

	shared.super, err = pgxpool.New(ctx, os.Getenv(envSuperuser))
	if err != nil {
		return fmt.Errorf("connect superuser: %w", err)
	}
	if url := os.Getenv(envReplicaSuper); url != "" {
		shared.replica, err = pgxpool.New(ctx, url)
		if err != nil {
			return fmt.Errorf("connect replica directly: %w", err)
		}
	}

	if cfg.S3.Endpoint == "" {
		return fmt.Errorf("ARMATURE_S3_ENDPOINT is not set; run the integration suite with `make test-integration`")
	}
	store, err := attachment.NewS3(attachment.S3Config{
		Endpoint: cfg.S3.Endpoint, Bucket: cfg.S3.Bucket, AccessKey: cfg.S3.AccessKey,
		SecretKey: cfg.S3.SecretKey, Region: cfg.S3.Region, UseSSL: cfg.S3.UseSSL,
	})
	if err != nil {
		return fmt.Errorf("s3 store: %w", err)
	}
	if err := store.EnsureBucket(ctx); err != nil {
		return fmt.Errorf("attachment bucket: %w", err)
	}
	shared.store = store
	return nil
}

func closeShared() {
	if shared.replica != nil {
		shared.replica.Close()
	}
	if shared.super != nil {
		shared.super.Close()
	}
	if shared.cluster != nil {
		shared.cluster.Close()
	}
	if shared.tel != nil {
		_ = shared.tel.Shutdown(context.Background())
	}
}

// everyOperationWasExercised runs after every test has made its calls: an
// endpoint nobody exercised is an endpoint whose description nobody has checked.
func everyOperationWasExercised() bool {
	if runFiltered() {
		// A filtered run cannot cover the whole surface.
		return true
	}
	neverSucceeded, neverRefused := apiContract.uncovered()
	// A GET with no input and no session cannot be refused; there is nothing
	// to refuse it for.
	unrefusable := map[string]bool{"GET /healthz": true, "GET /readyz": true, "GET /openapi.json": true, "GET /auth/signup": true}
	kept := neverRefused[:0]
	for _, key := range neverRefused {
		if !unrefusable[key] {
			kept = append(kept, key)
		}
	}
	neverRefused = kept
	if len(neverSucceeded) > 0 {
		fmt.Fprintf(os.Stderr, "--- FAIL: every operation exercised both ways\n    %d operations were never answered successfully:\n      %s\n", len(neverSucceeded), strings.Join(neverSucceeded, "\n      "))
	}
	if len(neverRefused) > 0 {
		fmt.Fprintf(os.Stderr, "--- FAIL: every operation exercised both ways\n    %d operations were never refused:\n      %s\n", len(neverRefused), strings.Join(neverRefused, "\n      "))
	}
	return len(neverSucceeded) == 0 && len(neverRefused) == 0
}

// spanSink keeps spans only while a test listens: the tracer lives for the
// whole run and would otherwise hold every statement the suite ever traced.
type spanSink struct {
	mu        sync.Mutex
	listeners int
	*tracetest.InMemoryExporter
}

func (s *spanSink) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	s.mu.Lock()
	listening := s.listeners > 0
	s.mu.Unlock()
	if !listening {
		return nil
	}
	return s.InMemoryExporter.ExportSpans(ctx, spans)
}

// listenToSpans keeps the run's spans from now until the test ends. Other
// tests may be traced alongside, so a test picks its own out by trace id.
func (h *harness) listenToSpans(t *testing.T) {
	t.Helper()
	h.spans.mu.Lock()
	h.spans.listeners++
	h.spans.mu.Unlock()
	t.Cleanup(func() {
		h.spans.mu.Lock()
		defer h.spans.mu.Unlock()
		h.spans.listeners--
		if h.spans.listeners == 0 {
			h.spans.Reset()
		}
	})
}

// ownCluster gives this test a cluster of its own, for a test that counts
// where reads went or watches replica health, which other tests' reads move.
func (h *harness) ownCluster(t *testing.T) {
	t.Helper()
	cluster, err := db.Open(context.Background(), shared.cfg.DB, shared.log, db.WithQueryTracer(h.tel.QueryTracer))
	if err != nil {
		t.Fatalf("open cluster: %v", err)
	}
	t.Cleanup(cluster.Close)
	h.cluster = cluster
}

// ownMetrics gives this test a metrics registry and a cluster of its own, for a
// test that reads the scrape, which every other test's requests add series to.
func (h *harness) ownMetrics(t *testing.T) {
	t.Helper()
	registry := prometheus.NewRegistry()
	h.tel = &observability.Telemetry{Metrics: observability.NewMetrics(registry), Registry: registry}
	h.ownCluster(t)
	if err := h.tel.Register(observability.NewClusterCollector(h.cluster)); err != nil {
		t.Fatalf("cluster collector: %v", err)
	}
}
