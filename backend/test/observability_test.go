//go:build integration

package test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A request is one span named by its route, in the caller's trace when one
// came along, with its statements underneath; and it is one count on a series
// named the same way, scraped off a handler that is not part of the API.
func TestARequestIsCountedAndTraced(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "watched")
	h.spans.Reset()

	const traceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, owner.base+"/api/v1/auth/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("traceparent", "00-"+traceID+"-00f067aa0ba902b7-01")
	resp, err := owner.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me: %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Trace-Id"); got != traceID {
		t.Errorf("X-Trace-Id = %q, want the caller's trace", got)
	}
	if err := h.tel.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}

	var request, statements int
	for _, span := range h.spans.GetSpans() {
		if span.SpanContext.TraceID().String() != traceID {
			continue
		}
		switch {
		case span.Name == "GET /api/v1/auth/me":
			request++
		case strings.HasPrefix(span.Name, "db."):
			statements++
		}
	}
	if request != 1 {
		t.Errorf("request spans in the caller's trace = %d, want 1", request)
	}
	if statements == 0 {
		t.Errorf("no statement span joined the request's trace")
	}

	scrape := httptest.NewServer(h.tel.Handler())
	defer scrape.Close()
	body := scrapeBody(t, scrape.URL+"/metrics")
	for _, want := range []string{
		`armature_http_requests_total{method="GET",route="/api/v1/auth/me",status="200"}`,
		`armature_db_query_duration_seconds_count{op="select",pool="`,
		`armature_db_pool_connections{pool="primary",state="idle"}`,
		"armature_db_reads_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the scrape lacks %s", want)
		}
	}
	if strings.Contains(body, `route="/api/v1/auth/me/`) {
		t.Errorf("a route label carries a path, not a pattern")
	}
}

// scrapeBody reads a metrics page as text.
func scrapeBody(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 64<<10)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}
