package observability

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Config is what a process needs to say how it is doing.
type Config struct {
	// Service names the binary in a trace and on a dashboard: armature-api or armature-worker.
	Service string
	// Env is the deployment, kept on every span.
	Env string
	// MetricsAddr serves /metrics on its own listener; blank serves none.
	MetricsAddr string
	// OTLPEndpoint is where traces go over HTTP; blank keeps them.
	OTLPEndpoint string
	// SampleRatio is the share of new traces kept.
	SampleRatio float64
}

// Telemetry is the process' metrics and its tracer, made once at startup.
type Telemetry struct {
	Metrics  *Metrics
	Registry *prometheus.Registry
	addr     string
	log      *slog.Logger
	tracing  *tracing
}

// Option adjusts Setup, for tests that want to see what would have been sent.
type Option func(*setupOptions)

type setupOptions struct {
	exporter spanExporter
}

// Setup builds the metrics registry and, when asked, the tracer. It is called
// once per process; the metrics it makes become what Current returns.
func Setup(ctx context.Context, cfg Config, log *slog.Logger, opts ...Option) (*Telemetry, error) {
	var o setupOptions
	for _, opt := range opts {
		opt(&o)
	}
	registry := prometheus.NewRegistry()
	t := &Telemetry{Metrics: NewMetrics(registry), Registry: registry, addr: cfg.MetricsAddr, log: log}
	current.Store(t.Metrics)
	tr, err := setupTracing(ctx, cfg, log, o.exporter)
	if err != nil {
		return nil, err
	}
	t.tracing = tr
	return t, nil
}

// Handler serves the registry in the exposition format Prometheus scrapes.
func (t *Telemetry) Handler() http.Handler {
	return promhttp.HandlerFor(t.Registry, promhttp.HandlerOpts{})
}

// Register adds a collector that reads its values on each scrape.
func (t *Telemetry) Register(c prometheus.Collector) error {
	return t.Registry.Register(c)
}

// Serve listens on the metrics address until ctx ends. It returns at once when
// there is no address, so a caller can always run it.
func (t *Telemetry) Serve(ctx context.Context) error {
	if t.addr == "" {
		return nil
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", t.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	srv := &http.Server{Addr: t.addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	done := make(chan error, 1)
	go func() {
		t.log.Info("metrics listening", "addr", t.addr)
		err := srv.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return srv.Shutdown(stopCtx)
	}
}

// Flush sends what the tracer holds without ending it; a test reads its
// exporter after this, since ending the tracer would empty the exporter too.
func (t *Telemetry) Flush(ctx context.Context) error {
	if t.tracing == nil {
		return nil
	}
	return t.tracing.provider.ForceFlush(ctx)
}

// Shutdown flushes what the tracer still holds.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t.tracing == nil {
		return nil
	}
	return t.tracing.shutdown(ctx)
}
