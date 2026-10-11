//go:build integration

package test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
)

// coiningWait bounds how long the test waits for a tagging to queue behind a
// coining that has not committed yet.
const coiningWait = 10 * time.Second

// Two people tagging with the same new word at the same moment both get the
// word: whoever comes second waits for the first and then uses that label.
func TestTwoPeopleCoiningTheSameLabelBothSucceed(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "coining")
	labels := label.NewService(h.cluster)
	create := func(summary string) string {
		created, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: ws.project.Key, TypeID: h.issueTypeID(t, ws, "Story"), Summary: summary}, ws.actor)
		if err != nil {
			t.Fatalf("create %q: %v", summary, err)
		}
		return created.Key
	}
	first, second := create("tagged first"), create("tagged second")

	labelOn := func(t *testing.T, refs []issue.LabelRef, name string) uuid.UUID {
		t.Helper()
		for _, ref := range refs {
			if ref.Name == name {
				return ref.ID
			}
		}
		t.Fatalf("%s is not among %v", name, refs)
		return uuid.Nil
	}
	countNamed := func(t *testing.T, name string) int {
		t.Helper()
		var n int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM label WHERE org_id = $1 AND lower(name) = lower($2)`, ws.orgID, name).Scan(&n); err != nil {
			t.Fatalf("count labels: %v", err)
		}
		return n
	}

	t.Run("a tagging that meets an uncommitted coining of the word waits and uses it", func(t *testing.T) {
		// The other coining is held open in plain SQL, so the tagging is sure to
		// meet it after its own lookup found nothing.
		ctx := context.Background()
		held, err := h.super.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = held.Rollback(ctx) }()
		var heldID uuid.UUID
		var heldPID int
		if err := held.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&heldPID); err != nil {
			t.Fatalf("pid: %v", err)
		}
		if err := held.QueryRow(ctx, `INSERT INTO label (org_id, name, color) VALUES ($1, 'held', $2) RETURNING id`, ws.orgID, label.ColorFor("held")).Scan(&heldID); err != nil {
			t.Fatalf("hold a coining open: %v", err)
		}

		type outcome struct {
			refs []issue.LabelRef
			err  error
		}
		done := make(chan outcome, 1)
		go func() {
			refs, _, err := labels.SetIssueLabels(ws.ctx, first, []string{"Held"}, ws.actor)
			done <- outcome{refs, err}
		}()

		deadline := time.Now().Add(coiningWait)
		for {
			var waiting int
			if err := h.super.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))`, heldPID).Scan(&waiting); err != nil {
				t.Fatalf("look for the waiting tagging: %v", err)
			}
			if waiting > 0 {
				break
			}
			select {
			case got := <-done:
				t.Fatalf("the tagging finished without meeting the open coining: %v %v", got.refs, got.err)
			case <-time.After(20 * time.Millisecond):
			}
			if time.Now().After(deadline) {
				t.Fatalf("the tagging never waited for the open coining")
			}
		}
		if err := held.Commit(ctx); err != nil {
			t.Fatalf("commit the held coining: %v", err)
		}

		got := <-done
		if got.err != nil {
			t.Fatalf("the second coining of the same word was refused: %v", got.err)
		}
		if id := labelOn(t, got.refs, "held"); id != heldID {
			t.Errorf("the issue carries label %s, want the one already coined, %s", id, heldID)
		}
		if n := countNamed(t, "held"); n != 1 {
			t.Errorf("the organization has %d labels called held, want 1", n)
		}
	})

	t.Run("two taggings at once with a word nobody has used yet", func(t *testing.T) {
		const rounds = 8
		for round := range rounds {
			word := "race" + uuid.New().String()[:8]
			start := make(chan struct{})
			var wg sync.WaitGroup
			results := make([][]issue.LabelRef, 2)
			errs := make([]error, 2)
			for n, key := range []string{first, second} {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					results[n], _, errs[n] = labels.SetIssueLabels(ws.ctx, key, []string{word}, ws.actor)
				}()
			}
			close(start)
			wg.Wait()
			for n, err := range errs {
				if err != nil {
					t.Fatalf("round %d: tagging %d was refused: %v", round, n, err)
				}
			}
			if a, b := labelOn(t, results[0], word), labelOn(t, results[1], word); a != b {
				t.Errorf("round %d: the two issues carry different labels for %s: %s and %s", round, word, a, b)
			}
			if n := countNamed(t, word); n != 1 {
				t.Errorf("round %d: the organization has %d labels called %s, want 1", round, n, word)
			}
		}
	})
}
