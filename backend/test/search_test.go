//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/nql"
)

// TestIssuesAreFoundByWhatIsSaidAboutThem runs queries through the compiler and
// the real database, and checks each answer against the same question asked in
// plain SQL, so that the catalog's templates are proven and not just believed.
func TestIssuesAreFoundByWhatIsSaidAboutThem(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "searching")
	other := h.newWorkspace(t, "elsewhere")
	labels := label.NewService(h.cluster)
	fields := field.NewService(h.cluster)

	me := ws.actor.UserID
	create := func(ws *workspace, summary, typeName string, shape func(in *issue.CreateInput)) *issue.Issue {
		in := issue.CreateInput{ProjectKey: ws.project.Key, TypeID: h.issueTypeID(t, ws, typeName), Summary: summary}
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
	a := create(ws, "Login page rejects a valid password", "Bug", func(in *issue.CreateInput) {
		in.Priority = issue.PriorityHigh
		in.AssigneeID = &me
	})
	b := create(ws, "Send the reset email", "Story", func(in *issue.CreateInput) {
		in.Description = issue.TextDocument("Goes through the smtp relay, not the API.")
		in.Estimate = &five
	})
	c := create(ws, "Tidy the docs", "Story", func(in *issue.CreateInput) { in.Priority = issue.PriorityLow })
	otherLogin := create(other, "Login page in another organization", "Bug", nil)

	if _, _, err := labels.SetIssueLabels(ws.ctx, a.Key, []string{"backend", "security"}, ws.actor); err != nil {
		t.Fatalf("label a: %v", err)
	}
	if _, _, err := labels.SetIssueLabels(ws.ctx, b.Key, []string{"backend"}, ws.actor); err != nil {
		t.Fatalf("label b: %v", err)
	}
	closeID := h.transitionID(t, ws, c.Key, "Close")
	if _, _, err := ws.issues.Transition(ws.ctx, c.Key, issue.TransitionInput{TransitionID: closeID}, ws.actor); err != nil {
		t.Fatalf("close c: %v", err)
	}
	customer, _, err := fields.Create(ws.ctx, ws.project.Key, field.Input{Name: "Customer", Kind: field.Text})
	if err != nil {
		t.Fatalf("customer field: %v", err)
	}
	if _, _, err := fields.Set(ws.ctx, b.Key, customer.ID, json.RawMessage(`"Globex"`), ws.actor); err != nil {
		t.Fatalf("set customer: %v", err)
	}
	// The other project has a field of the same name, which a by-name query
	// reaches too: "Customer = Globex" means the customer, wherever the field is.
	otherCustomer, _, err := fields.Create(other.ctx, other.project.Key, field.Input{Name: "Customer", Kind: field.Text})
	if err != nil {
		t.Fatalf("other customer field: %v", err)
	}
	otherIssue := create(other, "Globex asked for this", "Story", nil)
	if _, _, err := fields.Set(other.ctx, otherIssue.Key, otherCustomer.ID, json.RawMessage(`"Globex"`), other.actor); err != nil {
		t.Fatalf("set other customer: %v", err)
	}

	compile := func(t *testing.T, text string) *nql.Compiled {
		q, err := nql.Parse(text)
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		compiled, err := q.Compile(nql.Env{UserID: me, Now: time.Now()})
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		return compiled
	}
	search := func(t *testing.T, ws *workspace, text string) ([]string, int) {
		res, err := ws.issues.List(ws.ctx, issue.Filter{Query: compile(t, text)}, issue.Page{Limit: 50})
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		keys := make([]string, len(res.Issues))
		for i, each := range res.Issues {
			keys[i] = each.Key
		}
		return keys, res.Total
	}
	// bySQL asks the database the same thing in plain SQL, in the tenant's context.
	bySQL := func(t *testing.T, where string, args ...any) []string {
		var keys []string
		err := h.cluster.Read(ws.ctx, func(ctx context.Context, tx db.DBTX) error {
			rows, err := tx.Query(ctx, `
				SELECT p.key || '-' || i.key_num FROM issue i
				JOIN project p ON p.id = i.project_id
				JOIN issue_status s ON s.id = i.status_id
				JOIN issue_type it ON it.id = i.issue_type_id WHERE `+where, args...)
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
	same := func(t *testing.T, what string, got, want []string) {
		t.Helper()
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: got %v, want %v", what, got, want)
		}
	}

	cases := []struct {
		q    string
		want []string
		// sql is the plain question, when the set is worth proving twice.
		sql     string
		sqlArgs []any
	}{
		{q: "assignee = currentUser()", want: sorted(a.Key), sql: "i.assignee_id = $1", sqlArgs: []any{me}},
		{q: "assignee IS EMPTY", want: sorted(b.Key, c.Key), sql: "i.assignee_id IS NULL"},
		{q: "statusCategory != done", want: sorted(a.Key, b.Key), sql: "s.category <> 'done'"},
		{q: "status = Done", want: sorted(c.Key), sql: "s.name = 'Done'"},
		{q: "labels IN (security, nothing)", want: sorted(a.Key),
			sql: "EXISTS (SELECT 1 FROM issue_label il JOIN label l ON l.id = il.label_id WHERE il.issue_id = i.id AND l.name = 'security')"},
		{q: "labels = backend AND priority >= high", want: sorted(a.Key)},
		{q: "labels IS EMPTY", want: sorted(c.Key)},
		{q: `"Customer" = globex`, want: sorted(b.Key),
			sql: "EXISTS (SELECT 1 FROM issue_field_value v WHERE v.issue_id = i.id AND v.value = '\"Globex\"'::jsonb)"},
		{q: `"Customer" IS EMPTY`, want: sorted(a.Key, c.Key)},
		{q: "text ~ smtp", want: sorted(b.Key)},
		{q: "description ~ relay AND summary !~ login", want: sorted(b.Key)},
		{q: "summary ~ LOGIN", want: sorted(a.Key), sql: "i.summary ILIKE '%login%'"},
		{q: "description IS EMPTY", want: sorted(a.Key, c.Key)},
		{q: "resolved >= -1d", want: sorted(c.Key), sql: "i.resolved_at IS NOT NULL"},
		{q: "resolved IS EMPTY", want: sorted(a.Key, b.Key)},
		{q: "estimate > 3", want: sorted(b.Key), sql: "i.estimate > 3"},
		{q: "estimate IS EMPTY AND type = Story", want: sorted(c.Key)},
		{q: "type = Bug OR priority = low", want: sorted(a.Key, c.Key)},
		{q: "NOT (type = Bug OR priority = low)", want: sorted(b.Key)},
		{q: "key IN (" + a.Key + ", " + c.Key + ")", want: sorted(a.Key, c.Key)},
		{q: "project = " + strings.ToLower(ws.project.Key) + " AND priority IN (high, highest)", want: sorted(a.Key)},
		{q: "created >= startOfDay() AND created <= endOfDay() AND updated > -1h", want: sorted(a.Key, b.Key, c.Key)},
		{q: "created < startOfDay(-1d)", want: sorted()},
		{q: "reporter = " + ws.owner.Principal.User.Email, want: sorted(a.Key, b.Key, c.Key)},
		{q: "reporter = \"" + ws.owner.Principal.User.Name + "\"", want: sorted(a.Key, b.Key, c.Key)},
		{q: "parent IS EMPTY AND sprint IS EMPTY AND team IS EMPTY AND milestone IS EMPTY", want: sorted(a.Key, b.Key, c.Key)},
		{q: "sprint IN (openSprints())", want: sorted()},
		{q: "due IS EMPTY AND start IS EMPTY", want: sorted(a.Key, b.Key, c.Key)},
	}
	for _, tc := range cases {
		t.Run(tc.q, func(t *testing.T) {
			got, total := search(t, ws, tc.q)
			same(t, "keys", sorted(got...), tc.want)
			if total != len(tc.want) {
				t.Errorf("total %d, want %d", total, len(tc.want))
			}
			if tc.sql != "" {
				same(t, "against plain SQL", bySQL(t, tc.sql, tc.sqlArgs...), tc.want)
			}
		})
	}

	t.Run("the query's ORDER BY decides the order, unknowns last", func(t *testing.T) {
		got, _ := search(t, ws, "ORDER BY estimate DESC, key")
		same(t, "by estimate", got, []string{b.Key, a.Key, c.Key})
		got, _ = search(t, ws, "ORDER BY priority DESC")
		same(t, "by priority", got, []string{a.Key, b.Key, c.Key})
		got, _ = search(t, ws, "statusCategory != done ORDER BY key DESC")
		same(t, "by key", got, []string{b.Key, a.Key})
	})

	t.Run("paging walks the same order", func(t *testing.T) {
		compiled := compile(t, "ORDER BY key")
		first, err := ws.issues.List(ws.ctx, issue.Filter{Query: compiled}, issue.Page{Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		second, err := ws.issues.List(ws.ctx, issue.Filter{Query: compiled}, issue.Page{Limit: 2, Offset: 2})
		if err != nil {
			t.Fatal(err)
		}
		if len(first.Issues) != 2 || len(second.Issues) != 1 || first.Total != 3 || second.Total != 3 {
			t.Fatalf("pages: %d then %d of %d", len(first.Issues), len(second.Issues), first.Total)
		}
		if first.Issues[0].Key != a.Key || first.Issues[1].Key != b.Key || second.Issues[0].Key != c.Key {
			t.Errorf("order broke across pages")
		}
	})

	t.Run("the query is ANDed with the filter it is given with", func(t *testing.T) {
		res, err := ws.issues.List(ws.ctx, issue.Filter{Query: compile(t, "labels = backend"), Priorities: []issue.Priority{issue.PriorityHigh}}, issue.Page{Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Issues) != 1 || res.Issues[0].Key != a.Key {
			t.Errorf("got %d issues", len(res.Issues))
		}
	})

	t.Run("Keys answers the same set without a page", func(t *testing.T) {
		keys, err := ws.issues.Keys(ws.ctx, issue.Filter{ProjectKey: ws.project.Key, Query: compile(t, "labels = backend")})
		if err != nil {
			t.Fatal(err)
		}
		same(t, "keys", sorted(keys...), sorted(a.Key, b.Key))
	})

	t.Run("a custom field is found by name in whichever project defines it", func(t *testing.T) {
		got, _ := search(t, other, `"customer" = Globex`)
		same(t, "other project", got, []string{otherIssue.Key})
	})

	t.Run("another organization sees none of it", func(t *testing.T) {
		got, _ := search(t, other, "summary ~ login OR labels = backend OR assignee = "+ws.owner.Principal.User.Email)
		same(t, "other org", got, []string{otherLogin.Key})
	})

	t.Run("a query that does not compile never reaches the database", func(t *testing.T) {
		q, err := nql.Parse("assigne = currentUser()")
		if err != nil {
			t.Fatal(err)
		}
		_, err = q.Compile(nql.Env{UserID: me})
		var qe *nql.Error
		if !errors.As(err, &qe) || qe.Pos != 1 {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("the plan marks the keys a query selects and keeps every row", func(t *testing.T) {
		p, err := ws.plans.ForProjectMatching(ws.ctx, ws.project.Key, compile(t, "labels = backend ORDER BY key"))
		if err != nil {
			t.Fatal(err)
		}
		same(t, "matched", sorted(p.Matched...), sorted(a.Key, b.Key))
		if len(p.Items) != 3 {
			t.Errorf("the plan was pruned to %d rows", len(p.Items))
		}
		whole, err := ws.plans.ForProject(ws.ctx, ws.project.Key)
		if err != nil {
			t.Fatal(err)
		}
		if whole.Matched == nil || len(whole.Matched) != 0 {
			t.Errorf("without a query matched is %v, want an empty list", whole.Matched)
		}
	})
}
