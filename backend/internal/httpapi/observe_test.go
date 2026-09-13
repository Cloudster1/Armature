package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/armature/armature/backend/internal/observability"
)

// A counter labelled by path would grow a series per issue; the route pattern
// keeps /issues/{issueKey} one series, and every miss shares another.
func TestObserveRecordsTheRoutePatternNotThePath(t *testing.T) {
	metrics := observability.NewMetrics(prometheus.NewRegistry())
	r := chi.NewRouter()
	r.Use(observe(metrics))
	r.Get("/api/v1/issues/{issueKey}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Delete("/api/v1/issues/{issueKey}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })

	for _, path := range []string{"/api/v1/issues/ABC-1", "/api/v1/issues/ABC-2", "/api/v1/issues/XYZ-9"} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	}
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodDelete, "/api/v1/issues/ABC-1", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nowhere/at/all", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nowhere/else", nil))

	if got := testutil.ToFloat64(metrics.HTTPRequests.WithLabelValues("GET", "/api/v1/issues/{issueKey}", "200")); got != 3 {
		t.Fatalf("three reads of three issues = %v series hits, want 3 on one series", got)
	}
	if got := testutil.ToFloat64(metrics.HTTPRequests.WithLabelValues("DELETE", "/api/v1/issues/{issueKey}", "403")); got != 1 {
		t.Fatalf("the refused delete = %v", got)
	}
	if got := testutil.ToFloat64(metrics.HTTPRequests.WithLabelValues("GET", unmatched, "404")); got != 2 {
		t.Fatalf("two misses = %v on the unmatched series", got)
	}
	if got := testutil.CollectAndCount(metrics.HTTPRequests); got != 3 {
		t.Fatalf("series = %d, want 3: one per route and status, never per path", got)
	}
	if got := testutil.ToFloat64(metrics.HTTPInFlight); got != 0 {
		t.Fatalf("in flight after everything answered = %v", got)
	}
}

// The listener is the process' own: not in the API, not in its document.
func TestMetricsAreServedOffTheirOwnHandler(t *testing.T) {
	tel, err := observability.Setup(t.Context(), observability.Config{Service: "test"}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	tel.Metrics.HTTPRequests.WithLabelValues("GET", "/x", "200").Inc()
	rec := httptest.NewRecorder()
	tel.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{`armature_http_requests_total{method="GET",route="/x",status="200"} 1`, "go_goroutines", "process_open_fds"} {
		if !strings.Contains(body, want) {
			t.Errorf("the scrape lacks %q", want)
		}
	}
}
