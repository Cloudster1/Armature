package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/armature/armature/backend/internal/observability"
)

// A span per request, named by its route pattern, continuing the caller's
// trace when a traceparent came with it and saying which trace it was in.
func TestTracingContinuesTheCallersTraceAndNamesTheRoute(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tel, err := observability.Setup(t.Context(), observability.Config{Service: "test", SampleRatio: 1}, slog.New(slog.DiscardHandler), observability.WithSpanExporter(exporter))
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(requestID)
	r.Use(observe(tel.Metrics))
	r.Get("/api/v1/issues/{issueKey}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/issues/ABC-1", nil)
	req.Header.Set("traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if err := tel.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want the one request", len(spans))
	}
	span := spans[0]
	if span.Name != "GET /api/v1/issues/{issueKey}" {
		t.Errorf("span name = %q", span.Name)
	}
	if got := span.SpanContext.TraceID().String(); got != traceID {
		t.Errorf("trace id = %s, want the caller's", got)
	}
	if got := span.Parent.SpanID().String(); got != "00f067aa0ba902b7" {
		t.Errorf("parent = %s, want the caller's span", got)
	}
	if got := rec.Header().Get("X-Trace-Id"); got != traceID {
		t.Errorf("X-Trace-Id = %q", got)
	}
}
