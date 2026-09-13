package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/armature/armature/backend/internal/observability"
)

// The brake remembers an address only while it is knocking.
func TestThrottleForgetsAnAddressOnceItsWindowHasPassed(t *testing.T) {
	brake := newThrottle(2, time.Minute)
	start := time.Now()
	brake.allow("10.0.0.1", start)
	brake.allow("10.0.0.2", start)
	if len(brake.seen) != 2 {
		t.Fatalf("two knocks = %d addresses", len(brake.seen))
	}
	brake.allow("10.0.0.3", start.Add(2*time.Minute))
	if _, kept := brake.seen["10.0.0.1"]; kept {
		t.Error("an address whose window passed is still remembered")
	}
	if len(brake.seen) != 1 {
		t.Fatalf("after the window = %d addresses, want the one still knocking", len(brake.seen))
	}
}

// A shared dashboard's token names its organization; the trace must not.
func TestTheSpanCarriesTheRedactedPath(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tel, err := observability.Setup(t.Context(), observability.Config{Service: "test", SampleRatio: 1}, slog.New(slog.DiscardHandler), observability.WithSpanExporter(exporter))
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(requestID)
	r.Use(observe(tel.Metrics))
	r.Get(sharedPrefix+"{token}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, sharedPrefix+"secret-token-value", nil))
	if err := tel.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d", len(spans))
	}
	for _, attr := range spans[0].Attributes {
		if strings.Contains(attr.Value.AsString(), "secret-token-value") {
			t.Fatalf("attribute %s carries the token: %s", attr.Key, attr.Value.AsString())
		}
	}
}
