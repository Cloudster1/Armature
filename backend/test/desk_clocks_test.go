//go:build integration

package test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/report"
)

// passTime moves every running clock of the request back by the given
// minutes, as if they had passed; a stopped clock has nothing to move.
func passTime(t *testing.T, h *harness, ws *workspace, key string, minutes int) {
	t.Helper()
	projectKey, num, _ := issue.ParseKey(key)
	_, err := h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			UPDATE sla_timer t SET running_since = t.running_since - make_interval(mins => $3)
			FROM issue i, project p
			WHERE i.id = t.issue_id AND p.id = i.project_id AND p.key = $1 AND i.key_num = $2`,
			projectKey, num, minutes)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func breachEvents(t *testing.T, h *harness, ws *workspace) int {
	t.Helper()
	var n int
	if err := h.super.QueryRow(context.Background(),
		`SELECT count(*) FROM outbox_event WHERE org_id = $1 AND topic = $2`, ws.orgID, events.TopicSLABreached).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func slaMetric(t *testing.T, r *report.Service, ws *workspace, projectKey, metric string) report.SLAMetric {
	t.Helper()
	out, err := r.Report(ws.ctx, projectKey, report.SLA, report.Params{})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range out.(*report.SLAReport).Metrics {
		if m.Metric == metric {
			return m
		}
	}
	t.Fatalf("no %s in the SLA report %+v", metric, out)
	return report.SLAMetric{}
}

// A request resolved without a public reply, a duplicate or one the customer
// sorted out, has nobody left to answer: its first response clock is done.
func TestResolvingWithoutAReplyStopsTheFirstResponseClock(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "unanswered")
	d, p := ws.aDesk(t, h, "Quiet desk")
	customer := ws.customerOf(t, h, "duplicate")
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: requestTypeNamed(t, types, "Ask a question").ID, Summary: "Same as yesterday"}, customer)
	if err != nil {
		t.Fatal(err)
	}

	ws.move(t, h, raised.Key, "Resolve")
	first := timerFor(t, mustTimers(t, d, ws, raised.Key), desk.FirstResponse)
	if first.CompletedAt == nil || first.RunningSince != nil || first.Breached || first.BreachedAt != nil {
		t.Errorf("first response clock after resolving = %+v, want completed within its goal", first)
	}

	// Medium priority: first response within 8 hours. Nine pass after resolving.
	passTime(t, h, ws, raised.Key, 9*60)
	watch := desk.NewWatch(h.cluster, d, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if _, err := watch.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	first = timerFor(t, mustTimers(t, d, ws, raised.Key), desk.FirstResponse)
	if first.Breached || first.BreachedAt != nil {
		t.Errorf("first response clock of a resolved request = %+v, want no breach", first)
	}
	if n := breachEvents(t, h, ws); n != 0 {
		t.Errorf("%d breach events for a resolved request, want none", n)
	}
	if m := slaMetric(t, ws.reports(h), ws, p.Key, "first_response"); m.Met != 1 || m.Breached != 0 || m.Running != 0 {
		t.Errorf("first response in the SLA report = %+v, want one met", m)
	}

	t.Run("reopening restarts resolution only", func(t *testing.T) {
		ws.move(t, h, raised.Key, "Reopen")
		timers := mustTimers(t, d, ws, raised.Key)
		if first := timerFor(t, timers, desk.FirstResponse); first.CompletedAt == nil || first.RunningSince != nil {
			t.Errorf("first response clock after reopening = %+v, want still completed", first)
		}
		if resolution := timerFor(t, timers, desk.Resolution); resolution.CompletedAt != nil || resolution.RunningSince == nil {
			t.Errorf("resolution clock after reopening = %+v, want started over", resolution)
		}
	})
}

// Resolving ends a clock that already missed its goal, and the miss stays
// on the record: closing a request is not a way to clean up a breach.
func TestResolvingAfterABreachKeepsIt(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "toolate")
	d, p := ws.aDesk(t, h, "Late desk")
	customer := ws.customerOf(t, h, "forgotten")
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	question := requestTypeNamed(t, types, "Ask a question").ID
	raised, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: question, Summary: "Anyone there?"}, customer)
	if err != nil {
		t.Fatal(err)
	}

	ageTimer(t, h, ws, raised.Key, desk.FirstResponse, 9*60)
	watch := desk.NewWatch(h.cluster, d, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if _, err := watch.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	breachedAt := timerFor(t, mustTimers(t, d, ws, raised.Key), desk.FirstResponse).BreachedAt
	if breachedAt == nil {
		t.Fatal("the watch did not record the missed first response")
	}

	ws.move(t, h, raised.Key, "Resolve")
	first := timerFor(t, mustTimers(t, d, ws, raised.Key), desk.FirstResponse)
	if first.CompletedAt == nil || first.RunningSince != nil {
		t.Errorf("first response clock after resolving = %+v, want completed", first)
	}
	if !first.Breached || first.BreachedAt == nil || !first.BreachedAt.Equal(*breachedAt) {
		t.Errorf("first response clock after resolving = %+v, want the breach at %v kept", first, *breachedAt)
	}
	if n := breachEvents(t, h, ws); n != 1 {
		t.Errorf("%d breach events, want the one", n)
	}
	if m := slaMetric(t, ws.reports(h), ws, p.Key, "first_response"); m.Breached != 1 || m.Met != 0 || m.Running != 0 {
		t.Errorf("first response in the SLA report = %+v, want one breached and none running", m)
	}

	t.Run("a miss the watch had not seen yet is recorded on resolving", func(t *testing.T) {
		unseen, _, err := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: question, Summary: "Hello?"}, customer)
		if err != nil {
			t.Fatal(err)
		}
		ageTimer(t, h, ws, unseen.Key, desk.FirstResponse, 9*60)
		ws.move(t, h, unseen.Key, "Resolve")
		first := timerFor(t, mustTimers(t, d, ws, unseen.Key), desk.FirstResponse)
		if first.CompletedAt == nil || first.BreachedAt == nil || !first.Breached {
			t.Errorf("first response clock resolved past its goal = %+v, want completed and breached", first)
		}
	})
}
