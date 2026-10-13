//go:build integration

package test

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/nql"
)

// An issue with no description does not mention payment, and an unassigned one
// is not yours, so every negation over an empty column must keep the issue.
func TestNegationKeepsIssuesWithNothingThere(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "negating")

	me := ws.actor.UserID
	create := func(summary string, shape func(in *issue.CreateInput)) *issue.Issue {
		in := issue.CreateInput{ProjectKey: ws.project.Key, TypeID: h.issueTypeID(t, ws, "Story"), Summary: summary}
		if shape != nil {
			shape(&in)
		}
		created, _, err := ws.issues.Create(ws.ctx, in, ws.actor)
		if err != nil {
			t.Fatalf("create %q: %v", summary, err)
		}
		return created
	}
	five := 5.0
	// login never had a description; wording had one and lost it, which stores NULL.
	login := create("Login page", func(in *issue.CreateInput) { in.AssigneeID = &me })
	checkout := create("Checkout", func(in *issue.CreateInput) {
		in.Description = issue.TextDocument("The payment goes through the card provider.")
		in.Estimate = &five
	})
	wording := create("Payment page wording", func(in *issue.CreateInput) {
		in.Description = issue.TextDocument("Say what the fee is.")
	})
	emptied := json.RawMessage(`null`)
	if _, _, err := ws.issues.Update(ws.ctx, wording.Key, issue.UpdateInput{Description: &emptied}, ws.actor); err != nil {
		t.Fatalf("clear the description: %v", err)
	}

	search := func(t *testing.T, text string) []string {
		t.Helper()
		q, err := nql.Parse(text)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		compiled, err := q.Compile(nql.Env{UserID: me, Now: time.Now()})
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		// Within the test's project: the owner reads every project in the installation.
		res, err := ws.issues.List(ws.ctx, issue.Filter{Query: compiled, Within: []string{ws.project.Key}, Scoped: true}, issue.Page{Limit: 50})
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		keys := make([]string, len(res.Issues))
		for i, each := range res.Issues {
			keys[i] = each.Key
		}
		if res.Total != len(keys) {
			t.Errorf("%q: total %d for %d issues", text, res.Total, len(keys))
		}
		sort.Strings(keys)
		return keys
	}
	// bySQL asks the same question in plain SQL, with the empty case spelled out.
	bySQL := func(t *testing.T, where string, args ...any) []string {
		t.Helper()
		var keys []string
		err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			rows, err := tx.Query(ctx, `SELECT p.key || '-' || i.key_num FROM issue i
				JOIN project p ON p.id = i.project_id WHERE p.id = $1 AND (`+where+`)`,
				append([]any{ws.project.ID}, args...)...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var key string
				if err := rows.Scan(&key); err != nil {
					return err
				}
				keys = append(keys, key)
			}
			return rows.Err()
		})
		if err != nil {
			t.Fatalf("sql %q: %v", where, err)
		}
		sort.Strings(keys)
		return keys
	}
	sorted := func(keys ...string) []string {
		out := append([]string(nil), keys...)
		sort.Strings(out)
		return out
	}

	var stored int
	if err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM issue WHERE project_id = $1 AND description IS NULL`, ws.project.ID).Scan(&stored)
	}); err != nil || stored != 2 {
		t.Fatalf("issues stored without a description: %d (%v), want 2", stored, err)
	}

	cases := []struct {
		q    string
		want []string
		// sql is the plain question, when the set is worth proving twice.
		sql     string
		sqlArgs []any
	}{
		{q: `text !~ payment`, want: sorted(login.Key),
			sql: "i.summary NOT ILIKE '%payment%' AND (i.description IS NULL OR NOT i.description::text ILIKE '%payment%')"},
		{q: `NOT text ~ payment`, want: sorted(login.Key)},
		{q: `description !~ payment`, want: sorted(login.Key, wording.Key),
			sql: "i.description IS NULL OR NOT i.description::text ILIKE '%payment%'"},
		{q: `description !~ payment AND description IS NOT EMPTY`, want: sorted()},
		{q: `summary !~ payment`, want: sorted(login.Key, checkout.Key)},
		{q: `assignee != currentUser()`, want: sorted(checkout.Key, wording.Key),
			sql: "i.assignee_id IS DISTINCT FROM $2", sqlArgs: []any{me}},
		{q: `assignee NOT IN (currentUser())`, want: sorted(checkout.Key, wording.Key)},
		{q: `NOT assignee = currentUser()`, want: sorted(checkout.Key, wording.Key)},
		{q: `estimate != 5`, want: sorted(login.Key, wording.Key),
			sql: "i.estimate IS DISTINCT FROM 5"},
		{q: `NOT estimate > 3`, want: sorted(login.Key, wording.Key)},
		{q: `sprint NOT IN ("Sprint 1")`, want: sorted(login.Key, checkout.Key, wording.Key)},
		{q: `team != Alpha AND milestone != M1`, want: sorted(login.Key, checkout.Key, wording.Key)},
		{q: `due != 2026-09-04`, want: sorted(login.Key, checkout.Key, wording.Key)},
		{q: `resolved != 2026-09-01`, want: sorted(login.Key, checkout.Key, wording.Key)},
		{q: `parent != ` + login.Key, want: sorted(login.Key, checkout.Key, wording.Key)},
		// The positive tests still leave the empty issue out.
		{q: `text ~ payment`, want: sorted(checkout.Key, wording.Key)},
		{q: `assignee = currentUser()`, want: sorted(login.Key)},
	}
	for _, tc := range cases {
		t.Run(tc.q, func(t *testing.T) {
			if got := search(t, tc.q); strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("got %v, want %v", got, tc.want)
			}
			if tc.sql != "" {
				if got := bySQL(t, tc.sql, tc.sqlArgs...); strings.Join(got, ",") != strings.Join(tc.want, ",") {
					t.Errorf("against plain SQL: got %v, want %v", got, tc.want)
				}
			}
		})
	}
}
