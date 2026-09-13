package observability

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// A statement is a span under the request that ran it, named by its verb and
// carrying the statement cut to size; on its own it is only a histogram sample.
func TestQueryTracerDrawsAStatementUnderItsCallerOnly(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tel, err := Setup(t.Context(), Config{Service: "test", SampleRatio: 1}, slog.New(slog.DiscardHandler), WithSpanExporter(exporter))
	if err != nil {
		t.Fatal(err)
	}
	tracer := tel.QueryTracer("primary")
	long := "SELECT " + strings.Repeat("x", 2*statementLimit)

	// Under a caller's span: a child, named and trimmed.
	ctx, end := StartSpan(context.Background(), "request")
	qctx := tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: long})
	tracer.TraceQueryEnd(qctx, nil, pgx.TraceQueryEndData{Err: pgx.ErrNoRows})
	end(nil)

	// On its own: nothing drawn.
	qctx = tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "UPDATE issue SET x = 1"})
	tracer.TraceQueryEnd(qctx, nil, pgx.TraceQueryEndData{})
	if err := tel.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}

	spans := exporter.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("spans = %d, want the request and its statement", len(spans))
	}
	var query tracetest.SpanStub
	for _, s := range spans {
		if s.Name == "db.select" {
			query = s
		}
	}
	if query.Name == "" {
		t.Fatalf("no db.select span among %v", spans)
	}
	if query.Parent.SpanID() != spans[1].SpanContext.SpanID() && query.Parent.SpanID() != spans[0].SpanContext.SpanID() {
		t.Errorf("the statement is not under the request")
	}
	if query.Status.Code.String() != "Unset" {
		t.Errorf("no rows was marked an error: %v", query.Status)
	}
	for _, attr := range query.Attributes {
		if attr.Key == "db.query.text" && len(attr.Value.AsString()) != statementLimit {
			t.Errorf("statement kept at %d characters, want %d", len(attr.Value.AsString()), statementLimit)
		}
	}
	if firstWord("  with x as (select 1) select 1") != "with" || firstWord("") != "other" || firstWord("VACUUM") != "other" {
		t.Error("firstWord")
	}
}
