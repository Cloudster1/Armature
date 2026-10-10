//go:build integration

package test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bulk"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
)

// A refusal is the whole issue's: what bulk edit did to it before the refused
// step is undone, so the dialog's "left as it was" is true of the database.
func TestBulkEditRefusalLeavesTheIssueAsItWas(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "allornot")
	edits := bulk.NewService(ws.issues, label.NewService(h.cluster))

	fresh := ws.newIssue(t, "Still to do, so Approve is not offered")
	ready := ws.newIssue(t, "In review, so Approve is offered")
	ws.move(t, h, ready.Key, "Start progress")
	ws.move(t, h, ready.Key, "Ready for review")

	// Counted straight from the tables, so nothing the service says about
	// itself stands in for what it wrote.
	historyOf := func(id uuid.UUID) int {
		t.Helper()
		var n int
		if err := h.super.QueryRow(context.Background(),
			`SELECT count(*) FROM issue_history WHERE issue_id = $1`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	announced := func(key, topic string) []json.RawMessage {
		t.Helper()
		rows, err := h.super.Query(context.Background(), `
			SELECT payload FROM outbox_event
			WHERE org_id = $1 AND payload->>'key' = $2 AND ($3 = '' OR topic = $3)
			ORDER BY created_at, id`, ws.orgID, key, topic)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []json.RawMessage
		for rows.Next() {
			var p json.RawMessage
			if err := rows.Scan(&p); err != nil {
				t.Fatal(err)
			}
			out = append(out, p)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	freshHistory, freshEvents := historyOf(fresh.ID), len(announced(fresh.Key, ""))
	readyHistory := historyOf(ready.ID)
	readyUpdates := len(announced(ready.Key, events.TopicIssueUpdated))
	readyMoves := len(announced(ready.Key, events.TopicIssueTransitioned))

	high := issue.PriorityHigh
	result, _, err := edits.Edit(ws.ctx, []string{fresh.Key, ready.Key},
		bulk.Input{Priority: &high, AddLabels: []string{"triaged"}, Transition: "Approve"}, ws.actor)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Refused) != 1 || result.Refused[0].Key != fresh.Key || !strings.Contains(result.Refused[0].Reason, "not offered") {
		t.Fatalf("refused = %+v, want %s refused for its transition", result.Refused, fresh.Key)
	}
	if len(result.Applied) != 1 || result.Applied[0] != ready.Key {
		t.Fatalf("applied = %v, want %s", result.Applied, ready.Key)
	}

	t.Run("the refused issue keeps its priority, labels, history and silence", func(t *testing.T) {
		after, err := ws.issues.ByKey(ws.ctx, fresh.Key)
		if err != nil {
			t.Fatal(err)
		}
		if after.Priority != fresh.Priority {
			t.Errorf("priority is %s, want %s as it was", after.Priority, fresh.Priority)
		}
		if len(after.Labels) != 0 {
			t.Errorf("labels are %+v, want none as before", after.Labels)
		}
		if got := historyOf(fresh.ID); got != freshHistory {
			t.Errorf("the refused issue has %d history rows, want %d as before", got, freshHistory)
		}
		if got := len(announced(fresh.Key, "")); got != freshEvents {
			t.Errorf("the refused issue announced %d events, want none", got-freshEvents)
		}
	})

	t.Run("the applied issue announces its fields once and its move once", func(t *testing.T) {
		after, err := ws.issues.ByKey(ws.ctx, ready.Key)
		if err != nil {
			t.Fatal(err)
		}
		if after.Priority != high || len(after.Labels) != 1 || after.Status.Name != "Done" {
			t.Fatalf("applied issue = priority %s, labels %+v, status %s; want high, triaged, Done",
				after.Priority, after.Labels, after.Status.Name)
		}
		// One row for the fields and one for the move, as a single edit and a
		// single transition would leave.
		if got := historyOf(ready.ID); got != readyHistory+2 {
			t.Errorf("the applied issue gained %d history rows, want 2", got-readyHistory)
		}
		if got := len(announced(ready.Key, events.TopicIssueTransitioned)); got != readyMoves+1 {
			t.Errorf("the applied issue announced %d moves, want one", got-readyMoves)
		}
		updates := announced(ready.Key, events.TopicIssueUpdated)
		if len(updates) != readyUpdates+1 {
			t.Fatalf("the applied issue announced %d field updates, want one", len(updates)-readyUpdates)
		}
		var payload struct {
			Changes []issue.Change `json:"changes"`
		}
		if err := json.Unmarshal(updates[len(updates)-1], &payload); err != nil {
			t.Fatal(err)
		}
		fields := []string{}
		for _, c := range payload.Changes {
			fields = append(fields, c.Field)
		}
		if strings.Join(fields, ",") != "priority,labels" {
			t.Errorf("the update names %v, want priority and labels", fields)
		}
	})
}
