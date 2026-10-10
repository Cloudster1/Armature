//go:build integration

package test

import (
	"context"
	"encoding/json"
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

// hanging is an endpoint that never answers and counts how many requests it
// holds at once.
type hanging struct {
	mu        sync.Mutex
	now, most int
	got       int
	release   chan struct{}
}

func (h *hanging) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	h.mu.Lock()
	h.got++
	h.now++
	h.most = max(h.most, h.now)
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.now--
		h.mu.Unlock()
	}()
	select {
	case <-h.release:
	case <-req.Context().Done():
	}
	w.WriteHeader(http.StatusOK)
}

// One organization's hanging endpoint does not hold up another's, holds only a
// few requests at once, and is backed off on its own after a few timeouts.
func TestAHangingWebhookDoesNotHoldUpAnotherOrganization(t *testing.T) {
	h := newHarness(t)
	slowOrg := h.newWorkspace(t, "hanghook")
	fastOrg := h.newWorkspace(t, "fasthook")
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	hooks := webhook.NewService(h.cluster, log).WithClient(&http.Client{Timeout: hangTimeout})

	stuck := &hanging{release: make(chan struct{})}
	slowServer := httptest.NewServer(stuck)
	t.Cleanup(slowServer.Close)
	t.Cleanup(func() { close(stuck.release) })
	sink := &receiver{status: http.StatusOK}
	fastServer := httptest.NewServer(sink)
	t.Cleanup(fastServer.Close)

	slow, _, err := hooks.Create(slowOrg.ctx, webhook.Input{Name: "Hangs", URL: slowServer.URL, Topics: []string{events.TopicIssueCreated}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = hooks.Delete(slowOrg.ctx, slow.ID) })
	fast, _, err := hooks.Create(fastOrg.ctx, webhook.Input{Name: "Answers", URL: fastServer.URL, Topics: []string{events.TopicIssueCreated}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = hooks.Delete(fastOrg.ctx, fast.ID) })

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
	got, most := stuck.got, stuck.most
	stuck.mu.Unlock()
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
