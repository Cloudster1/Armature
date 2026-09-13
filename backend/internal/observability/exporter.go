package observability

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// newExporter sends finished spans to the collector over HTTP. The endpoint
// is the whole URL, /v1/traces included, so a collector on another path is
// one setting away and no gRPC server is needed anywhere.
func newExporter(ctx context.Context, endpoint string) (sdktrace.SpanExporter, error) {
	return otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(endpoint))
}
