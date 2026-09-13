package observability

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// Two processes' worth of metrics on two registries must not collide, and
// every name the dashboard expects must appear on a scrape.
func TestMetricsRegisterOnceAndRender(t *testing.T) {
	first := prometheus.NewRegistry()
	m := NewMetrics(first)
	NewMetrics(prometheus.NewRegistry())

	m.HTTPRequests.WithLabelValues("GET", "/x", "200").Inc()
	m.DBQuery.WithLabelValues("select", "primary").Observe(0.01)
	m.HTTPDuration.WithLabelValues("GET", "/x").Observe(0.01)
	m.OutboxPending.Set(3)
	m.Handled("notify", "issue.created", nil, 0)
	m.Handled("notify", "issue.created", errors.New("no"), 0)
	m.Mailed(nil)
	if err := m.Job(context.Background(), "sweep", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}

	families, err := first.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range families {
		names = append(names, f.GetName())
	}
	joined := strings.Join(names, " ")
	for _, want := range []string{
		"armature_http_requests_total", "armature_http_request_duration_seconds", "armature_http_in_flight",
		"armature_db_query_duration_seconds", "armature_outbox_pending", "armature_events_handled_total",
		"armature_event_handle_duration_seconds", "armature_jobs_runs_total", "armature_job_duration_seconds",
		"armature_mail_sent_total", "go_goroutines",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("no %s among %s", want, joined)
		}
	}
	if got := testutil.ToFloat64(m.EventsHandled.WithLabelValues("notify", "issue.created", "error")); got != 1 {
		t.Errorf("errors handled = %v", got)
	}
	if got := testutil.ToFloat64(m.JobRuns.WithLabelValues("sweep", "ok")); got != 1 {
		t.Errorf("job runs = %v", got)
	}
}

// A loop that records before Setup ran must not lose the process' handle later.
func TestCurrentIsOneSetForTheProcess(t *testing.T) {
	if Current() != Current() {
		t.Fatal("two calls gave two sets")
	}
}
