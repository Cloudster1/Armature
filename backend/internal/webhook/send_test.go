package webhook

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// waitLimit is how long a test waits for what should happen at once, and pool
// is a pool wider than one endpoint may use.
const (
	waitLimit = time.Second
	pool      = 8
)

// queueFor makes n deliveries to one endpoint.
func queueFor(endpoint uuid.UUID, n int) []due {
	out := make([]due, n)
	for i := range out {
		out[i] = due{endpointID: endpoint, id: uuid.New()}
	}
	return out
}

// inFlight counts what runs at once, overall and per endpoint, and keeps the
// highest each reached.
type inFlight struct {
	mu          sync.Mutex
	now, most   int
	per, mostOf map[uuid.UUID]int
	tried       map[uuid.UUID]int
}

func newInFlight() *inFlight {
	return &inFlight{per: map[uuid.UUID]int{}, mostOf: map[uuid.UUID]int{}, tried: map[uuid.UUID]int{}}
}

func (f *inFlight) start(d due) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now++
	f.most = max(f.most, f.now)
	f.per[d.endpointID]++
	f.mostOf[d.endpointID] = max(f.mostOf[d.endpointID], f.per[d.endpointID])
	f.tried[d.endpointID]++
}

func (f *inFlight) stop(d due) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now--
	f.per[d.endpointID]--
}

func (f *inFlight) triedOf(endpoint uuid.UUID) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tried[endpoint]
}

// An endpoint that hangs holds no more than its own few places; another
// endpoint's deliveries in the same batch go past it at once.
func TestAHangingEndpointDoesNotHoldUpTheBatch(t *testing.T) {
	hanging, quick := uuid.New(), uuid.New()
	found := append(queueFor(hanging, 10), queueFor(quick, 5)...)
	release := make(chan struct{})
	quickDone := make(chan struct{}, len(found))
	flight := newInFlight()

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		spread(context.Background(), found, pool, func(_ context.Context, d due) bool {
			flight.start(d)
			defer flight.stop(d)
			if d.endpointID == hanging {
				<-release
				return false
			}
			quickDone <- struct{}{}
			return false
		}, func(context.Context, due) { t.Error("nothing timed out, yet an endpoint was paused") })
	}()

	deadline := time.After(waitLimit)
	for range 5 {
		select {
		case <-quickDone:
		case <-deadline:
			close(release)
			t.Fatalf("the quick endpoint got %d of its 5 deliveries while the other one hung", flight.triedOf(quick))
		}
	}
	close(release)
	<-finished

	if flight.mostOf[hanging] > MaxInFlightPerEndpoint {
		t.Errorf("the hanging endpoint had %d requests in flight, want at most %d", flight.mostOf[hanging], MaxInFlightPerEndpoint)
	}
	if flight.tried[hanging] != 10 || flight.tried[quick] != 5 {
		t.Errorf("tried %d and %d, want every delivery once", flight.tried[hanging], flight.tried[quick])
	}
}

// The pool is bounded: however many endpoints a batch reaches, no more than
// the limit are posted at once, and they are posted side by side.
func TestThePoolPostsSideBySideWithinItsLimit(t *testing.T) {
	const limit, endpoints, each = 3, 6, 4
	var found []due
	for range endpoints {
		found = append(found, queueFor(uuid.New(), each)...)
	}
	flight := newInFlight()
	var tried atomic.Int32
	spread(context.Background(), found, limit, func(_ context.Context, d due) bool {
		flight.start(d)
		defer flight.stop(d)
		time.Sleep(10 * time.Millisecond)
		tried.Add(1)
		return false
	}, func(context.Context, due) {})

	if int(tried.Load()) != endpoints*each {
		t.Fatalf("tried %d deliveries, want %d", tried.Load(), endpoints*each)
	}
	if flight.most > limit {
		t.Errorf("%d requests were in flight, the limit is %d", flight.most, limit)
	}
	if flight.most < 2 {
		t.Errorf("never more than %d request in flight: the deliveries were posted one after another", flight.most)
	}
	for endpoint, most := range flight.mostOf {
		if most > MaxInFlightPerEndpoint {
			t.Errorf("endpoint %s had %d requests in flight, want at most %d", endpoint, most, MaxInFlightPerEndpoint)
		}
	}
}

// An endpoint that keeps timing out is paused on its own after a few tries:
// the rest of its batch is left for later and every other endpoint is served.
func TestAnEndpointThatKeepsTimingOutIsPausedAlone(t *testing.T) {
	slow, fine := uuid.New(), uuid.New()
	found := append(queueFor(slow, 20), queueFor(fine, 4)...)
	flight := newInFlight()
	var mu sync.Mutex
	var paused []due
	spread(context.Background(), found, pool, func(_ context.Context, d due) bool {
		flight.start(d)
		defer flight.stop(d)
		return d.endpointID == slow
	}, func(_ context.Context, d due) {
		// A request still out would schedule its retry after the pause began.
		flight.mu.Lock()
		if flight.per[d.endpointID] != 0 {
			t.Errorf("paused with %d requests still in flight", flight.per[d.endpointID])
		}
		flight.mu.Unlock()
		mu.Lock()
		paused = append(paused, d)
		mu.Unlock()
	})

	if len(paused) != 1 || paused[0].endpointID != slow {
		t.Fatalf("paused %+v, want the slow endpoint once", paused)
	}
	if most := TimeoutsBeforePause + MaxInFlightPerEndpoint - 1; flight.tried[slow] > most {
		t.Errorf("the slow endpoint was tried %d times, want at most %d before it was paused", flight.tried[slow], most)
	}
	if flight.tried[fine] != 4 {
		t.Errorf("the other endpoint was tried %d times, want all 4", flight.tried[fine])
	}
}

// Only timeouts in a row pause an endpoint; an answer in between starts the
// count again, so a slow but living endpoint keeps its deliveries.
func TestAnAnswerBetweenTimeoutsStartsTheCountAgain(t *testing.T) {
	endpoint := uuid.New()
	found := queueFor(endpoint, 3*TimeoutsBeforePause)
	answers := map[uuid.UUID]bool{}
	for i, d := range found {
		answers[d.id] = (i+1)%TimeoutsBeforePause == 0
	}
	var tried atomic.Int32
	spread(context.Background(), found, 1, func(_ context.Context, d due) bool {
		tried.Add(1)
		return !answers[d.id]
	}, func(context.Context, due) { t.Error("an endpoint that answers every few tries was paused") })

	if int(tried.Load()) != len(found) {
		t.Errorf("tried %d of %d deliveries", tried.Load(), len(found))
	}
}
