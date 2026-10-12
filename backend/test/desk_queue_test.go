//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/desk"
	"github.com/armature/armature/backend/internal/issue"
)

func TestTheQueuePutsTheMostPressingFirst(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newWorkspace(t, "queueing")
	d, p := ws.aDesk(t, h, "Queue desk")
	customer := ws.customerOf(t, h, "queuer")
	types, _ := d.RequestTypes(ws.ctx, p.Key)
	question := requestTypeNamed(t, types, "Ask a question")

	calm, _, _ := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: question.ID, Summary: "no hurry"}, customer)
	urgent, _, _ := d.Raise(ws.ctx, desk.RaiseInput{RequestTypeID: question.ID, Summary: "getting late"}, customer)
	ageTimer(t, h, ws, urgent.Key, desk.FirstResponse, 7*60)

	queue := func(filter desk.QueueFilter) []desk.QueueRow {
		t.Helper()
		page, err := d.Queue(ws.ctx, p.Key, filter, ws.actor.UserID, issue.Page{})
		if err != nil {
			t.Fatal(err)
		}
		return page.Rows
	}
	rows := queue(desk.QueueOpen)
	if len(rows) != 2 || rows[0].Issue.Key != urgent.Key || rows[1].Issue.Key != calm.Key {
		t.Fatalf("queue order = %v, want the one with least time left first", queueKeys(rows))
	}
	if rows[0].RequestTypeName != "Ask a question" || len(rows[0].Timers) != 2 {
		t.Errorf("queue row = %+v, want its request type and clocks", rows[0])
	}

	ws.move(t, h, urgent.Key, "Start work")
	if mine := queue(desk.QueueMine); len(mine) != 1 || mine[0].Issue.Key != urgent.Key {
		t.Errorf("mine = %v, want the one starting work assigned to me", queueKeys(mine))
	}
	if unassigned := queue(desk.QueueUnassigned); len(unassigned) != 1 || unassigned[0].Issue.Key != calm.Key {
		t.Errorf("unassigned = %v, want the other", queueKeys(unassigned))
	}
}

func queueKeys(rows []desk.QueueRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Issue.Key)
	}
	return out
}

// A desk busier than one page of its queue: the oldest requests are the ones
// overdue, and a queue read from the newest end would never reach them.
const (
	busyDeskRequests   = 250
	busyDeskBreached   = 50
	busyDeskPage       = 50
	mediumFirstAnswer  = 8 * 60
	busyDeskMaxPerPage = 200
)

// The queue is ordered and cut by the database, most overdue first, so a
// request is never lost for being old, and a long queue says how long it is.
func TestTheQueueReadsEveryRequestMostOverdueFirst(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.client(t)
	owner.signup(t, h, "busydesk")
	projectID := uuid.MustParse(idOf(t, want(t, owner.post("/api/v1/projects", map[string]any{"name": "Busy desk", "key": "BSY", "template": "service-desk"}), http.StatusCreated, "create the desk"), "project"))

	keys := make([]string, busyDeskRequests)
	for n := range keys {
		filed := want(t, owner.post("/api/v1/projects/BSY/issues", map[string]any{"summary": fmt.Sprintf("request %d", n+1), "priority": "medium"}), http.StatusCreated, "file a request")
		keys[n] = obj(t, filed, "issue")["key"].(string)
	}

	// The oldest fifty missed their first answer, the very oldest by the most.
	for n, key := range keys[:busyDeskBreached] {
		_, num, err := issue.ParseKey(key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.super.Exec(context.Background(), `
			UPDATE sla_timer t SET running_since = now() - make_interval(mins => $3)
			FROM issue i, sla_policy sp
			WHERE i.id = t.issue_id AND sp.id = t.policy_id
			  AND i.project_id = $1 AND i.key_num = $2 AND sp.metric = 'first_response'`,
			projectID, num, mediumFirstAnswer+busyDeskBreached-n); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("Breached lists every breached request, the most overdue first", func(t *testing.T) {
		got := owner.get(fmt.Sprintf("/api/v1/projects/BSY/queue?filter=breached&limit=%d", busyDeskMaxPerPage))
		want(t, got, http.StatusOK, "the breached queue")
		if total := got.Body["total"]; total != float64(busyDeskBreached) {
			t.Errorf("breached total = %v, want %d", total, busyDeskBreached)
		}
		if rows := queueRowKeys(t, got); !slices.Equal(rows, keys[:busyDeskBreached]) {
			t.Errorf("breached queue = %v, want the oldest %d, oldest first", rows, busyDeskBreached)
		}
	})

	t.Run("the first page of Open starts with them and counts the whole queue", func(t *testing.T) {
		got := owner.get(fmt.Sprintf("/api/v1/projects/BSY/queue?filter=open&limit=%d&offset=0", busyDeskPage))
		want(t, got, http.StatusOK, "the open queue")
		if got.Body["total"] != float64(busyDeskRequests) || got.Body["limit"] != float64(busyDeskPage) || got.Body["offset"] != float64(0) {
			t.Errorf("open page = total %v, limit %v, offset %v; want %d, %d, 0", got.Body["total"], got.Body["limit"], got.Body["offset"], busyDeskRequests, busyDeskPage)
		}
		if rows := queueRowKeys(t, got); !slices.Equal(rows, keys[:busyDeskBreached]) {
			t.Errorf("first page of open = %v, want the %d breached, oldest first", rows, busyDeskBreached)
		}
	})

	t.Run("paging walks every request once", func(t *testing.T) {
		seen := map[string]int{}
		for offset := 0; offset < busyDeskRequests; offset += busyDeskPage {
			got := want(t, owner.get(fmt.Sprintf("/api/v1/projects/BSY/queue?filter=open&limit=%d&offset=%d", busyDeskPage, offset)), http.StatusOK, "a page of the open queue")
			rows := queueRowKeys(t, got)
			if len(rows) != busyDeskPage {
				t.Errorf("page at %d holds %d requests, want %d", offset, len(rows), busyDeskPage)
			}
			for _, key := range rows {
				seen[key]++
			}
		}
		for _, key := range keys {
			if seen[key] != 1 {
				t.Errorf("%s was shown %d times across the pages, want once", key, seen[key])
			}
		}
		past := want(t, owner.get(fmt.Sprintf("/api/v1/projects/BSY/queue?filter=open&limit=%d&offset=%d", busyDeskPage, busyDeskRequests)), http.StatusOK, "past the end")
		if rows := queueRowKeys(t, past); len(rows) != 0 || past.Body["total"] != float64(busyDeskRequests) {
			t.Errorf("past the end = %v of %v, want nothing of %d", rows, past.Body["total"], busyDeskRequests)
		}
	})
}

func queueRowKeys(t *testing.T, r response) []string {
	t.Helper()
	raw, ok := r.Body["rows"].([]any)
	if !ok {
		t.Fatalf("no rows in %s", r.Raw)
	}
	out := []string{}
	for _, row := range raw {
		out = append(out, row.(map[string]any)["issue"].(map[string]any)["key"].(string))
	}
	return out
}
