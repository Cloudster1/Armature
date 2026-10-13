//go:build integration

package test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/events"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/nql"
	"github.com/armature/armature/backend/internal/workflow"
)

// statusWatcher is an observer that remembers every status move it is told
// about, the way the service desk's clocks hear of them.
type statusWatcher struct{ moves []string }

func (w *statusWatcher) IssueCreated(context.Context, db.DBTX, *issue.Issue, issue.Actor) error {
	return nil
}

func (w *statusWatcher) IssueTransitioned(_ context.Context, _ db.DBTX, before, after *issue.Issue, _ issue.Actor) error {
	w.moves = append(w.moves, before.Key+": "+before.Status.Name+"->"+after.Status.Name)
	return nil
}

func (w *statusWatcher) CommentAdded(context.Context, db.DBTX, uuid.UUID, bool, issue.Actor) error {
	return nil
}

func (w *statusWatcher) IssueUpdated(context.Context, db.DBTX, *issue.Issue, *issue.Issue, issue.Actor) error {
	return nil
}

func (w *statusWatcher) IssueDeleted(context.Context, db.DBTX, *issue.Issue, issue.Actor) error {
	return nil
}

// coinType adds an issue type at the ordinary working level, named uniquely
// after name; types have no admin service to coin one with.
func (h *harness) coinType(t *testing.T, ws *workspace, name string) (uuid.UUID, string) {
	t.Helper()
	name = h.name(t, name)
	var id uuid.UUID
	_, err := h.cluster.Write(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `
			INSERT INTO issue_type (org_id, name, description, icon, hierarchy_level, position)
			VALUES (current_org_id(), $1, 'Coined by a test.', 'bug', $2, 99) RETURNING id`,
			name, bootstrap.LevelStandard).Scan(&id)
	})
	if err != nil {
		t.Fatalf("coin issue type %q: %v", name, err)
	}
	return id, name
}

// coinStatus adds a status everyone shares, named uniquely after name.
func (ws *workspace) coinStatus(t *testing.T, h *harness, name string, category workflow.StatusCategory) string {
	t.Helper()
	name = h.name(t, name)
	if _, _, err := ws.admin.CreateStatus(ws.ctx, workflow.StatusInput{Name: name, Category: category}, ws.actor.UserID); err != nil {
		t.Fatalf("coin status %q: %v", name, err)
	}
	return name
}

// statusMoves reads the issue.transitioned events about an issue as from->to.
func (h *harness) statusMoves(t *testing.T, ws *workspace, key string) []string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT payload FROM outbox_event
		WHERE org_id = $1 AND topic = $2 AND payload->>'key' = $3
		ORDER BY id`, ws.orgID, events.TopicIssueTransitioned, key)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var moves []string
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var p struct {
			FromStatus string    `json:"fromStatus"`
			ToStatus   string    `json:"toStatus"`
			ActorID    uuid.UUID `json:"actorId"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatal(err)
		}
		if p.ActorID != ws.actor.UserID {
			t.Errorf("event %s->%s names actor %s, want %s", p.FromStatus, p.ToStatus, p.ActorID, ws.actor.UserID)
		}
		moves = append(moves, p.FromStatus+"->"+p.ToStatus)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return moves
}

// latestChanges is the newest changelog entry of an issue, field by field.
func latestChanges(t *testing.T, ws *workspace, key string) map[string]issue.Change {
	t.Helper()
	history, err := ws.issues.History(ws.ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(history) == 0 {
		t.Fatalf("%s has no history", key)
	}
	out := map[string]issue.Change{}
	for _, c := range history[0].Changes {
		out[c.Field] = c
	}
	return out
}

// matching runs an NQL query and reports whether the issue is in the answer.
func matching(t *testing.T, ws *workspace, query, key string) bool {
	t.Helper()
	q, err := nql.Parse(query + " AND key = " + key)
	if err != nil {
		t.Fatalf("%q: %v", query, err)
	}
	compiled, err := q.Compile(nql.Env{UserID: ws.actor.UserID, Now: time.Now()})
	if err != nil {
		t.Fatalf("%q: %v", query, err)
	}
	res, err := ws.issues.List(ws.ctx, issue.Filter{Query: compiled}, issue.Page{Limit: 5})
	if err != nil {
		t.Fatalf("%q: %v", query, err)
	}
	for _, each := range res.Issues {
		if each.Key == key {
			return true
		}
	}
	return false
}

// A type change that moves the status is a status change like any other:
// recorded, announced, observed, and the resolution follows the new status.
func TestATypeChangeThatMovesTheStatusRecordsTheMove(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "retype")
	watcher := &statusWatcher{}
	ws.issues.Observe(watcher)

	triage := ws.coinStatus(t, h, "Triage", workflow.CategoryTodo)
	incidents := h.straightThrough(t, ws, "Incident response", triage, bootstrap.StatusInProgress)
	archive := h.straightThrough(t, ws, "Archive", bootstrap.StatusDone, bootstrap.StatusInProgress)
	incident, incidentName := h.coinType(t, ws, "Incident")
	archived, _ := h.coinType(t, ws, "Archived")
	h.overrideWith(t, ws, "Incident scheme",
		workflow.SchemeItemInput{IssueTypeID: &incident, WorkflowID: incidents.ID},
		workflow.SchemeItemInput{IssueTypeID: &archived, WorkflowID: archive.ID},
	)

	t.Run("a done bug that becomes an incident is reopened, and says so", func(t *testing.T) {
		bug := ws.typed(t, h, bootstrap.TypeBug, "Checkout times out")
		done := ws.move(t, h, bug.Key, "Close")
		if done.ResolvedAt == nil || done.Status.Name != bootstrap.StatusDone {
			t.Fatalf("closed bug is %q resolved at %v, want Done and resolved", done.Status.Name, done.ResolvedAt)
		}
		if !matching(t, ws, "resolved IS NOT EMPTY", bug.Key) {
			t.Fatal("the closed bug is not found as resolved before the type change")
		}
		watcher.moves = nil

		retyped, _, err := ws.issues.Update(ws.ctx, bug.Key, issue.UpdateInput{TypeID: &incident}, ws.actor)
		if err != nil {
			t.Fatalf("change the type to Incident: %v", err)
		}
		if retyped.Status.Name != triage {
			t.Errorf("status = %q, want the incident workflow's Triage", retyped.Status.Name)
		}
		if retyped.ResolvedAt != nil {
			t.Errorf("resolvedAt = %v, want it cleared now the issue is in Triage", retyped.ResolvedAt)
		}

		changes := latestChanges(t, ws, bug.Key)
		if c := changes["type"]; c.From != bootstrap.TypeBug || c.To != incidentName {
			t.Errorf("type change = %+v, want Bug->Incident", c)
		}
		if c, ok := changes["status"]; !ok || c.From != bootstrap.StatusDone || c.To != triage {
			t.Errorf("status change = %+v (recorded %v), want Done->Triage in the same entry", c, ok)
		}
		if c, ok := changes["resolution"]; !ok || c.To != "Unresolved" {
			t.Errorf("resolution change = %+v (recorded %v), want it marked unresolved", c, ok)
		}

		moves := h.statusMoves(t, ws, bug.Key)
		if len(moves) != 2 || moves[1] != "Done->"+triage {
			t.Errorf("issue.transitioned events = %v, want the close and then Done->Triage", moves)
		}
		if len(watcher.moves) != 1 || watcher.moves[0] != bug.Key+": Done->"+triage {
			t.Errorf("observers heard %v, want the one move to Triage", watcher.moves)
		}

		if matching(t, ws, "resolved IS NOT EMPTY", bug.Key) {
			t.Error("resolved IS NOT EMPTY still finds the incident sitting in Triage")
		}
		if !matching(t, ws, "resolved IS EMPTY", bug.Key) {
			t.Error("resolved IS EMPTY does not find the incident sitting in Triage")
		}
		var resolvedAt *time.Time
		if err := h.super.QueryRow(context.Background(),
			`SELECT resolved_at FROM issue WHERE id = $1`, bug.ID).Scan(&resolvedAt); err != nil {
			t.Fatal(err)
		}
		if resolvedAt != nil {
			t.Errorf("the row still says resolved at %v", resolvedAt)
		}
	})

	t.Run("a type whose workflow starts done resolves the issue", func(t *testing.T) {
		task := ws.typed(t, h, bootstrap.TypeTask, "Old wiki page")
		watcher.moves = nil

		retyped, _, err := ws.issues.Update(ws.ctx, task.Key, issue.UpdateInput{TypeID: &archived}, ws.actor)
		if err != nil {
			t.Fatalf("change the type to Archived: %v", err)
		}
		if retyped.Status.Name != bootstrap.StatusDone || retyped.ResolvedAt == nil {
			t.Errorf("status %q resolved at %v, want Done and resolved", retyped.Status.Name, retyped.ResolvedAt)
		}
		changes := latestChanges(t, ws, task.Key)
		if c, ok := changes["status"]; !ok || c.From != bootstrap.StatusToDo || c.To != bootstrap.StatusDone {
			t.Errorf("status change = %+v (recorded %v), want To Do->Done", c, ok)
		}
		if _, ok := changes["resolution"]; !ok {
			t.Error("no resolution recorded for an issue the type change resolved")
		}
		if moves := h.statusMoves(t, ws, task.Key); len(moves) != 1 || moves[0] != "To Do->Done" {
			t.Errorf("issue.transitioned events = %v, want To Do->Done", moves)
		}
		if len(watcher.moves) != 1 {
			t.Errorf("observers heard %v, want the one move to Done", watcher.moves)
		}
		if !matching(t, ws, "resolved IS NOT EMPTY", task.Key) {
			t.Error("resolved IS NOT EMPTY does not find the issue the type change resolved")
		}
	})

	t.Run("a type whose workflow has the status changes only the type", func(t *testing.T) {
		task := ws.typed(t, h, bootstrap.TypeTask, "Plain enough")
		watcher.moves = nil

		retyped, _, err := ws.issues.Update(ws.ctx, task.Key,
			issue.UpdateInput{TypeID: ptr(h.issueTypeID(t, ws, bootstrap.TypeStory))}, ws.actor)
		if err != nil {
			t.Fatalf("change the type to Story: %v", err)
		}
		if retyped.Status.Name != bootstrap.StatusToDo || retyped.ResolvedAt != nil {
			t.Errorf("status %q resolved at %v, want it left in To Do", retyped.Status.Name, retyped.ResolvedAt)
		}
		changes := latestChanges(t, ws, task.Key)
		if _, ok := changes["status"]; ok {
			t.Errorf("history records a status change where there was none: %+v", changes)
		}
		if moves := h.statusMoves(t, ws, task.Key); len(moves) != 0 {
			t.Errorf("issue.transitioned events = %v, want none", moves)
		}
		if len(watcher.moves) != 0 {
			t.Errorf("observers heard %v, want nothing", watcher.moves)
		}
	})
}
