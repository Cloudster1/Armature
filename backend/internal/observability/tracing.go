package observability

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is what every span from this module is attributed to.
const tracerName = "github.com/armature/armature/backend"

// spanExporter is where finished spans go; a test hands in one that keeps them.
type spanExporter = sdktrace.SpanExporter

// WithSpanExporter sends spans somewhere other than the collector, for a test
// that wants to read what would have been sent.
func WithSpanExporter(exporter sdktrace.SpanExporter) Option {
	return func(o *setupOptions) { o.exporter = exporter }
}

type tracing struct {
	provider *sdktrace.TracerProvider
}

// setupTracing installs the W3C propagator always, so a traceparent is read
// and passed on whatever else is on, and a provider only when spans have
// somewhere to go; without one the global no-op tracer makes every span free.
func setupTracing(ctx context.Context, cfg Config, log *slog.Logger, exporter spanExporter) (*tracing, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		// A collector that is down is not this process' problem to shout about.
		log.Debug("telemetry", "error", err)
	}))
	if exporter == nil && cfg.OTLPEndpoint != "" {
		var err error
		exporter, err = newExporter(ctx, cfg.OTLPEndpoint)
		if err != nil {
			return nil, err
		}
	}
	if exporter == nil {
		return nil, nil
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.Service),
		semconv.DeploymentEnvironmentNameKey.String(cfg.Env),
	))
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(provider)
	return &tracing{provider: provider}, nil
}

func (t *tracing) shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}
	return t.provider.Shutdown(ctx)
}

// Tracer is the module's tracer, whichever provider is installed.
func Tracer() trace.Tracer { return otel.Tracer(tracerName) }

// StartSpan begins a span named name under the one on ctx; end closes it and
// marks err on it. Without a provider both are no-ops.
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, func(err error)) {
	ctx, span := Tracer().Start(ctx, name, trace.WithAttributes(attrs...))
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}

// Inject writes the span on ctx as a traceparent carrier, for an event to
// carry through the outbox to the worker.
func Inject(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

// Extract reads a traceparent back onto a context, so the worker's span is a
// child of the request that emitted the event.
func Extract(ctx context.Context, traceparent string) context.Context {
	if traceparent == "" {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": traceparent})
}

// TraceIDFrom is the trace id on ctx as a string, or "" when there is none.
func TraceIDFrom(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
