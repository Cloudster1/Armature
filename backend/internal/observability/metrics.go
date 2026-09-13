package observability

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Metrics is every series the api and the worker publish, named once here so
// the dashboard and the code cannot disagree about a name.
type Metrics struct {
	HTTPRequests  *prometheus.CounterVec
	HTTPDuration  *prometheus.HistogramVec
	HTTPInFlight  prometheus.Gauge
	DBQuery       *prometheus.HistogramVec
	OutboxPending prometheus.Gauge
	OutboxSent    prometheus.Counter
	OutboxFailed  prometheus.Counter
	EventsHandled *prometheus.CounterVec
	EventDuration *prometheus.HistogramVec
	JobRuns       *prometheus.CounterVec
	JobDuration   *prometheus.HistogramVec
	MailSent      *prometheus.CounterVec
}

// The buckets suit a request that is usually milliseconds and a job that is
// usually seconds; both stop where a reader stops caring about the exact value.
var (
	requestBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}
	jobBuckets     = []float64{.01, .05, .1, .5, 1, 5, 10, 30, 60, 300}
)

// NewMetrics registers the series on reg, along with the Go runtime and the
// process collectors, and returns the handles the code records through.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		HTTPRequests:  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "armature_http_requests_total", Help: "Requests answered, by method, route pattern and status."}, []string{"method", "route", "status"}),
		HTTPDuration:  prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "armature_http_request_duration_seconds", Help: "How long a request took, by method and route pattern.", Buckets: requestBuckets}, []string{"method", "route"}),
		HTTPInFlight:  prometheus.NewGauge(prometheus.GaugeOpts{Name: "armature_http_in_flight", Help: "Requests being answered right now."}),
		DBQuery:       prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "armature_db_query_duration_seconds", Help: "How long a statement took, by its first word and the pool it ran on.", Buckets: requestBuckets}, []string{"op", "pool"}),
		OutboxPending: prometheus.NewGauge(prometheus.GaugeOpts{Name: "armature_outbox_pending", Help: "Events written but not yet on the stream, capped at the relay's count limit."}),
		OutboxSent:    prometheus.NewCounter(prometheus.CounterOpts{Name: "armature_outbox_published_total", Help: "Events the relay put on the stream."}),
		OutboxFailed:  prometheus.NewCounter(prometheus.CounterOpts{Name: "armature_outbox_failures_total", Help: "Events the relay could not put on the stream this time."}),
		EventsHandled: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "armature_events_handled_total", Help: "Events a consumer group finished, by group, topic and outcome."}, []string{"group", "topic", "outcome"}),
		EventDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "armature_event_handle_duration_seconds", Help: "How long a consumer group spent on one event.", Buckets: jobBuckets}, []string{"group"}),
		JobRuns:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "armature_jobs_runs_total", Help: "Passes of a worker's periodic job, by job and outcome."}, []string{"job", "outcome"}),
		JobDuration:   prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "armature_job_duration_seconds", Help: "How long one pass of a periodic job took.", Buckets: jobBuckets}, []string{"job"}),
		MailSent:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "armature_mail_sent_total", Help: "Mails handed to SMTP, by outcome."}, []string{"outcome"}),
	}
	reg.MustRegister(
		m.HTTPRequests, m.HTTPDuration, m.HTTPInFlight, m.DBQuery,
		m.OutboxPending, m.OutboxSent, m.OutboxFailed,
		m.EventsHandled, m.EventDuration,
		m.JobRuns, m.JobDuration, m.MailSent,
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return m
}

// outcome is the one word a counter takes for how something ended.
func outcome(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}

// Handled records one event a consumer group is done with.
func (m *Metrics) Handled(group, topic string, err error, took time.Duration) {
	m.EventsHandled.WithLabelValues(group, topic, outcome(err)).Inc()
	m.EventDuration.WithLabelValues(group).Observe(took.Seconds())
}

// Job runs one pass of a periodic job under a span and records how it went,
// so every loop in the worker says the same things in the same words.
func (m *Metrics) Job(ctx context.Context, name string, run func(context.Context) error) error {
	ctx, end := StartSpan(ctx, "job "+name)
	started := time.Now()
	err := run(ctx)
	end(err)
	m.JobRuns.WithLabelValues(name, outcome(err)).Inc()
	m.JobDuration.WithLabelValues(name).Observe(time.Since(started).Seconds())
	return err
}

// Mailed records one mail handed to SMTP.
func (m *Metrics) Mailed(err error) {
	m.MailSent.WithLabelValues(outcome(err)).Inc()
}

// current is the process' metrics, so a loop deep in the worker records
// without every constructor taking one more argument. Until Setup runs it is
// a set on a registry nobody scrapes, which costs nothing and drops nothing.
var current atomic.Pointer[Metrics]

// Current returns the process' metrics.
func Current() *Metrics {
	if m := current.Load(); m != nil {
		return m
	}
	m := NewMetrics(prometheus.NewRegistry())
	if current.CompareAndSwap(nil, m) {
		return m
	}
	return current.Load()
}

// Run is Job on the process' metrics, for a loop that has no handle of its own.
func Run(ctx context.Context, name string, run func(context.Context) error) error {
	return Current().Job(ctx, name, run)
}

// Count is Run for a pass that also says how much it did.
func Count[N int | int64](ctx context.Context, name string, run func(context.Context) (N, error)) (N, error) {
	var n N
	err := Run(ctx, name, func(ctx context.Context) error {
		var err error
		n, err = run(ctx)
		return err
	})
	return n, err
}
