package webhook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/netguard"
	"github.com/armature/armature/backend/internal/tenant"
)

const (
	// MaxInFlightPerEndpoint is how many requests one endpoint is sent at once,
	// so a hanging one holds no more of the pool than this.
	MaxInFlightPerEndpoint = 2
	// TimeoutsBeforePause is how many timeouts in a row pause an endpoint for
	// the rest of a pass; an answer in between starts the count again.
	TimeoutsBeforePause = 3
	// EndpointPause is how long a paused endpoint's waiting deliveries are put
	// off, so the passes in between are not spent waiting on it.
	EndpointPause = 5 * time.Minute
	// sendBatch is how many due deliveries one pass reads; passes repeat until
	// the queue is empty, so a backlog is drained rather than nibbled.
	sendBatch = 200
	// notInTime is what a delivery row says when the endpoint timed out.
	notInTime = "the endpoint did not answer in time"
)

// due is one delivery a pass found, with what decides where it may run.
type due struct{ orgID, endpointID, id uuid.UUID }

// SendDue posts every delivery whose time has come, across organizations, and
// returns how many were tried.
func (s *Service) SendDue(ctx context.Context) (int, error) {
	tried := 0
	for {
		var found []due
		err := s.db.ReadAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			rows, err := tx.Query(ctx, `
				SELECT org_id, endpoint_id, id FROM webhook_delivery
				WHERE delivered_at IS NULL AND next_attempt_at IS NOT NULL AND next_attempt_at <= now()
				ORDER BY next_attempt_at LIMIT $1`, sendBatch)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var d due
				if err := rows.Scan(&d.orgID, &d.endpointID, &d.id); err != nil {
					return err
				}
				found = append(found, d)
			}
			return rows.Err()
		})
		if err != nil {
			return tried, err
		}
		var mu sync.Mutex
		spread(ctx, found, s.concurrency, func(ctx context.Context, d due) bool {
			_, timedOut, err := s.send(inOrg(ctx, d), d.id)
			if err != nil {
				s.log.Warn("webhook attempt failed", "delivery", d.id, "error", err)
			}
			mu.Lock()
			tried++
			mu.Unlock()
			return timedOut
		}, s.pause)
		if len(found) < sendBatch || ctx.Err() != nil {
			return tried, nil
		}
	}
}

func inOrg(ctx context.Context, d due) context.Context {
	return db.PinPrimary(tenant.WithOrg(ctx, tenant.Org{ID: d.orgID}))
}

// pause puts off everything the endpoint has waiting, retries included, so
// the next passes go by it; a redelivery or a test by hand still goes at once.
func (s *Service) pause(ctx context.Context, d due) {
	until := s.now().Add(EndpointPause)
	_, err := s.db.Write(inOrg(ctx, d), func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			UPDATE webhook_delivery SET next_attempt_at = $2
			WHERE endpoint_id = $1 AND delivered_at IS NULL AND next_attempt_at IS NOT NULL AND next_attempt_at < $2`,
			d.endpointID, until)
		return err
	})
	if err != nil {
		s.log.Warn("webhook endpoint could not be paused", "endpoint", d.endpointID, "error", err)
		return
	}
	s.log.Info("webhook endpoint paused after timing out", "endpoint", d.endpointID, "until", until)
}

// spread runs a batch through a pool of limit places, each endpoint in at most
// MaxInFlightPerEndpoint of them, so one slow endpoint cannot hold the rest.
func spread(ctx context.Context, found []due, limit int, try func(context.Context, due) bool, pause func(context.Context, due)) {
	places := make(chan struct{}, max(limit, 1))
	lanes := map[uuid.UUID]*lane{}
	var order []*lane
	for _, d := range found {
		l, ok := lanes[d.endpointID]
		if !ok {
			l = &lane{}
			lanes[d.endpointID] = l
			order = append(order, l)
		}
		l.queue = append(l.queue, d)
	}
	var all sync.WaitGroup
	for _, l := range order {
		all.Go(func() {
			first := l.queue[0]
			var running sync.WaitGroup
			for range min(MaxInFlightPerEndpoint, len(l.queue)) {
				running.Go(func() { l.run(ctx, places, try) })
			}
			// Paused only once nothing is in flight: a request still out would
			// schedule its retry after the pause began.
			running.Wait()
			if l.paused {
				pause(ctx, first)
			}
		})
	}
	all.Wait()
}

// lane is one endpoint's share of a batch, taken by its runners in turn.
type lane struct {
	mu       sync.Mutex
	queue    []due
	timeouts int
	paused   bool
}

func (l *lane) run(ctx context.Context, places chan struct{}, try func(context.Context, due) bool) {
	for ctx.Err() == nil {
		places <- struct{}{}
		d, ok := l.next()
		if !ok {
			<-places
			return
		}
		timedOut := try(ctx, d)
		l.record(timedOut)
		<-places
	}
}

func (l *lane) next() (due, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.paused || len(l.queue) == 0 {
		return due{}, false
	}
	d := l.queue[0]
	l.queue = l.queue[1:]
	return d, true
}

func (l *lane) record(timedOut bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !timedOut {
		l.timeouts = 0
		return
	}
	l.timeouts++
	if l.timeouts >= TimeoutsBeforePause {
		l.paused = true
	}
}

// why says what went wrong in a word. The error itself names hosts and ports
// this server can reach, which the log may say and a delivery row may not.
func why(err error) string {
	switch {
	case errors.Is(err, netguard.ErrBlocked):
		return "that address is not one this server may reach"
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(err.Error(), "timeout"):
		return notInTime
	default:
		return "the endpoint could not be reached"
	}
}

// attempt posts one logged delivery and records the outcome: delivered, or
// failed with the next attempt scheduled, or given up.
func (s *Service) attempt(ctx context.Context, deliveryID uuid.UUID) (*Delivery, error) {
	out, _, err := s.send(ctx, deliveryID)
	return out, err
}

// send is attempt that also says whether the endpoint timed out, which is
// what pauses it.
func (s *Service) send(ctx context.Context, deliveryID uuid.UUID) (*Delivery, bool, error) {
	var (
		url, secret, topic string
		body               []byte
		attempt            int
		endpointID         uuid.UUID
		eventID            uuid.UUID
	)
	err := s.db.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			SELECT e.url, e.secret, d.topic, d.request_body, d.attempt, d.endpoint_id, d.event_id
			FROM webhook_delivery d JOIN webhook_endpoint e ON e.id = d.endpoint_id
			WHERE d.id = $1`, deliveryID).Scan(&url, &secret, &topic, &body, &attempt, &endpointID, &eventID)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, ErrNotFound
	}
	if err != nil {
		return nil, false, err
	}

	status, sendErr := s.post(ctx, url, secret, topic, deliveryID, body)
	delivered := sendErr == nil && status >= 200 && status < 300
	var next *time.Time
	if !delivered && attempt < MaxAttempts {
		at := s.now().Add(Backoff[min(attempt-1, len(Backoff)-1)])
		next = &at
	}
	errText := ""
	if sendErr != nil {
		errText = why(sendErr)
	} else if !delivered {
		errText = fmt.Sprintf("the endpoint answered %d", status)
	}
	var statusPtr *int
	if sendErr == nil {
		statusPtr = &status
	}

	var out *Delivery
	_, err = s.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		if delivered {
			_, err := tx.Exec(ctx, `UPDATE webhook_delivery SET status = $2, error = '', next_attempt_at = NULL, delivered_at = now() WHERE id = $1`, deliveryID, statusPtr)
			if err != nil {
				return err
			}
		} else {
			if _, err := tx.Exec(ctx, `UPDATE webhook_delivery SET status = $2, error = $3, next_attempt_at = NULL WHERE id = $1`, deliveryID, statusPtr, errText); err != nil {
				return err
			}
			if next != nil {
				if _, err := tx.Exec(ctx, `
					INSERT INTO webhook_delivery (org_id, endpoint_id, event_id, topic, attempt, request_body, next_attempt_at)
					VALUES (current_org_id(), $1, $2, $3, $4, $5, $6)
					ON CONFLICT (endpoint_id, event_id, attempt) DO NOTHING`, endpointID, eventID, topic, attempt+1, body, *next); err != nil {
					return err
				}
			}
		}
		var err error
		out, err = scanDelivery(tx.QueryRow(ctx, selectDeliveries+` WHERE id = $1`, deliveryID))
		return err
	})
	return out, errText == notInTime, err
}

// post sends one request and returns the status, or the error that stopped it.
func (s *Service) post(ctx context.Context, url, secret, topic string, deliveryID uuid.UUID, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Armature-Webhook")
	req.Header.Set("X-Armature-Event", topic)
	req.Header.Set("X-Armature-Delivery", deliveryID.String())
	// Signed as sent, so a retry or a redelivery carries its own time.
	sent := s.now()
	req.Header.Set(HeaderSignature, Sign(body, secret))
	req.Header.Set(HeaderTimestamp, strconv.FormatInt(sent.Unix(), 10))
	req.Header.Set(HeaderTimestampedSignature, SignTimestamped(body, secret, sent))
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
	return resp.StatusCode, nil
}
