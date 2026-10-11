//go:build integration

package test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/webhook"
)

const (
	// fastDelivery is how soon the answering endpoint must have its delivery
	// while the other one hangs.
	fastDelivery = time.Second
	// hangingQueue is how many deliveries wait for the endpoint that hangs:
	// posted one after another they would take far longer than fastDelivery.
	hangingQueue = 12
	// hangTimeout is the client's patience in this test, short so that the
	// endpoint is paused within the run rather than after ten seconds a try.
	hangTimeout = 300 * time.Millisecond
)

// hanging is an endpoint that never answers and counts how often it is tried.
type hanging struct {
	mu      sync.Mutex
	got     int
	release chan struct{}
}

func (h *hanging) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h.mu.Lock()
	h.got++
	h.mu.Unlock()
	// The server only notices the client giving up once the body is read.
	_, _ = io.Copy(io.Discard, req.Body)
	select {
	case <-h.release:
	case <-req.Context().Done():
	}
	w.WriteHeader(http.StatusOK)
}

// inFlight counts the requests sent to one host that have not come back yet,
// counted where they leave so a handler slow to notice a timeout adds none.
type inFlight struct {
	base      http.RoundTripper
	host      string
	mu        sync.Mutex
	now, most int
}

func (c *inFlight) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != c.host {
		return c.base.RoundTrip(req)
	}
	c.mu.Lock()
	c.now++
	c.most = max(c.most, c.now)
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.now--
		c.mu.Unlock()
	}()
	return c.base.RoundTrip(req)
}

// One organization's hanging endpoint does not hold up another's, holds only a
// few requests at once, and is backed off on its own after a few timeouts.
func TestAHangingWebhookDoesNotHoldUpAnotherOrganization(t *testing.T) {
	h := newHarness(t)
	slowOrg := h.newWorkspace(t, "hanghook")
	fastOrg := h.newWorkspace(t, "fasthook")
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	stuck := &hanging{release: make(chan struct{})}
	slowServer := httptest.NewServer(stuck)
	t.Cleanup(slowServer.Close)
	t.Cleanup(func() { close(stuck.release) })
	held := &inFlight{base: http.DefaultTransport, host: slowServer.Listener.Addr().String()}
	hooks := webhook.NewService(h.cluster, log).WithClient(&http.Client{Timeout: hangTimeout, Transport: held})
	sink := &receiver{status: http.StatusOK}
	fastServer := httptest.NewServer(sink)
	t.Cleanup(fastServer.Close)

	slow, _, err := hooks.Create(slowOrg.ctx, webhook.Input{Name: "Hangs", URL: slowServer.URL, Topics: []string{events.TopicIssueCreated}}, slowOrg.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = hooks.Delete(slowOrg.ctx, slow.ID, slowOrg.actor.UserID) })
	fast, _, err := hooks.Create(fastOrg.ctx, webhook.Input{Name: "Answers", URL: fastServer.URL, Topics: []string{events.TopicIssueCreated}}, fastOrg.actor.UserID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = hooks.Delete(fastOrg.ctx, fast.ID, fastOrg.actor.UserID) })

	// The hanging endpoint's deliveries are queued first, so they are first
	// in line as well.
	for range hangingQueue {
		if err := hooks.Handle(context.Background(), events.Event{ID: uuid.New(), OrgID: slowOrg.orgID, Topic: events.TopicIssueCreated, Payload: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
	}
	fastEvent := uuid.New()
	if err := hooks.Handle(context.Background(), events.Event{ID: fastEvent, OrgID: fastOrg.orgID, Topic: events.TopicIssueCreated, Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	sent := make(chan error, 1)
	go func() {
		_, err := hooks.SendDue(ctx)
		sent <- err
	}()

	delivered := func() bool {
		deliveries, err := hooks.Deliveries(fastOrg.ctx, fast.ID, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range deliveries {
			if d.EventID == fastEvent && d.DeliveredAt != nil {
				return true
			}
		}
		return false
	}
	for !delivered() {
		if time.Since(started) > fastDelivery {
			cancel()
			t.Fatalf("the answering endpoint was not delivered within %s while another organization's endpoint hung", fastDelivery)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := <-sent; err != nil {
		t.Fatalf("send due: %v", err)
	}
	stuck.mu.Lock()
	got := stuck.got
	stuck.mu.Unlock()
	held.mu.Lock()
	most := held.most
	held.mu.Unlock()
	if most > webhook.MaxInFlightPerEndpoint {
		t.Errorf("the hanging endpoint held %d requests at once, want at most %d", most, webhook.MaxInFlightPerEndpoint)
	}
	if limit := webhook.TimeoutsBeforePause + webhook.MaxInFlightPerEndpoint - 1; got > limit {
		t.Errorf("the hanging endpoint was tried %d times in one pass, want it backed off after %d timeouts", got, webhook.TimeoutsBeforePause)
	}

	// What was not tried is left for later, every delivery still waiting is
	// due no sooner than the pause, and nothing was given up.
	deliveries, err := hooks.Deliveries(slowOrg.ctx, slow.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	firsts, waiting := 0, 0
	for _, d := range deliveries {
		if d.Attempt == 1 {
			firsts++
		}
		if d.NextAttemptAt == nil {
			continue
		}
		waiting++
		if d.NextAttemptAt.Before(started.Add(webhook.EndpointPause)) {
			t.Errorf("delivery %s attempt %d is due at %s, inside the pause", d.ID, d.Attempt, d.NextAttemptAt)
		}
	}
	if firsts != hangingQueue || waiting != hangingQueue {
		t.Errorf("%d first attempts and %d waiting, want all %d events kept and still to be tried", firsts, waiting, hangingQueue)
	}
}
