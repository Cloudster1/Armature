package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/armature/armature/backend/internal/observability"
)

// unmatched is the one route label every request that hit no route shares, so
// a scan of made-up paths cannot mint a series each.
const unmatched = "unmatched"

// observe counts, times and traces every request by its route pattern, not its
// path, so /issues/{issueKey} is one series however many issues there are.
// The pattern is only known after the handler ran, which is why it wraps the
// whole chain and reads chi's context at the end. A traceparent header makes
// the span a child of the caller's; the trace id goes back as X-Trace-Id.
func observe(metrics *observability.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			metrics.HTTPInFlight.Inc()
			defer metrics.HTTPInFlight.Dec()

			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			ctx, span := observability.Tracer().Start(ctx, r.Method, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(r.Method),
				semconv.URLPath(redactPath(r.URL.Path)),
				attribute.String("armature.request_id", RequestIDFrom(r.Context())),
			))
			defer span.End()
			if id := observability.TraceIDFrom(ctx); id != "" {
				w.Header().Set("X-Trace-Id", id)
			}

			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r.WithContext(ctx))

			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			route := unmatched
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				if pattern := rctx.RoutePattern(); pattern != "" {
					route = pattern
				}
			}
			metrics.HTTPRequests.WithLabelValues(r.Method, route, strconv.Itoa(rec.status)).Inc()
			metrics.HTTPDuration.WithLabelValues(r.Method, route).Observe(time.Since(started).Seconds())

			span.SetName(r.Method + " " + route)
			span.SetAttributes(semconv.HTTPRoute(route), semconv.HTTPResponseStatusCode(rec.status))
			if rec.status >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(rec.status))
			}
		})
	}
}
