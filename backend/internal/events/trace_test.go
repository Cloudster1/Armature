package events

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/armature/armature/backend/internal/observability"
)

// The worker's span for an event sits in the trace of the request that
// emitted it, so one trace shows the request and everything it set off.
func TestAConsumerSpanJoinsTheProducersTrace(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tel, err := observability.Setup(t.Context(), observability.Config{Service: "test", SampleRatio: 1}, slog.New(slog.DiscardHandler), observability.WithSpanExporter(exporter))
	if err != nil {
		t.Fatal(err)
	}

	ctx, end := observability.StartSpan(context.Background(), "POST /api/v1/issues")
	e := Event{ID: uuid.New(), Topic: TopicIssueCreated, Trace: observability.Inject(ctx)}
	end(nil)
	if e.Trace == "" {
		t.Fatal("no traceparent was written for the event")
	}

	_, done := Continue(context.Background(), e, "notify")
	done(nil)
	if err := tel.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("spans = %d", len(spans))
	}
	if spans[0].SpanContext.TraceID() != spans[1].SpanContext.TraceID() {
		t.Fatalf("the consumer's span is in another trace")
	}
	consumer := spans[1]
	if consumer.Name != "events.notify "+TopicIssueCreated {
		t.Errorf("consumer span = %q", consumer.Name)
	}
	if consumer.Parent.SpanID() != spans[0].SpanContext.SpanID() {
		t.Errorf("the consumer's span is not the request's child")
	}
}
