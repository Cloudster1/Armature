//go:build integration

package test

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/nql"
)

// People search as they type, so a description matches by the start of its
// words (#293), and nothing they type is read as query syntax.
func TestADescriptionIsFoundByTheStartOfItsWords(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	ws := h.newProject(t, "prefixes")
	create := func(summary string, description json.RawMessage) string {
		in := issue.CreateInput{ProjectKey: ws.project.Key, TypeID: h.issueTypeID(t, ws, "Story"), Summary: summary, Description: description}
		created, _, err := ws.issues.Create(ws.ctx, in, ws.actor)
		if err != nil {
			t.Fatalf("create %q: %v", summary, err)
		}
		return created.Key
	}
	metadata := create("One", issue.TextDocument("Some metadata about data"))
	release := create("Two", issue.TextDocument("Ship v1.2.3 by e-mail to Müller"))
	hello := create("Three", json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[`+
		`{"type":"text","text":"Hello","marks":[{"type":"link","attrs":{"href":"https://wiki.test/runbook"}}]}]}]}`))
	empty := create("Four", json.RawMessage(`{"type":"doc","content":[{"type":"paragraph"}]}`))

	cases := []struct {
		q    string
		want []string
	}{
		{`text ~ meta`, []string{metadata}},
		{`text ~ "data meta"`, []string{metadata}},
		{`description ~ "ab da"`, []string{metadata}},
		{`description ~ "v1.2 e-ma mül"`, []string{release}},
		// & | ! : * ( ) quotes and backslashes are characters, never operators.
		{`description ~ "(meta) & !dat"`, []string{metadata}},
		{`description ~ "'meta' \\ dat:*"`, []string{metadata}},
		{`description ~ "meta:* | hel"`, nil},
		{`description ~ "hel)"`, []string{hello}},
		// Node names and link addresses are not words anybody wrote.
		{`description ~ par`, nil},
		{`description ~ doc`, nil},
		{`description ~ runbook`, nil},
		{`text ~ "***"`, nil},
		{`description !~ meta`, []string{release, hello, empty}},
		{`description IS EMPTY`, []string{empty}},
	}
	for _, c := range cases {
		t.Run(c.q, func(t *testing.T) {
			q, err := nql.Parse(c.q)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			compiled, err := q.Compile(nql.Env{UserID: ws.actor.UserID, Now: time.Now()})
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			res, err := ws.issues.List(ws.ctx, issue.Filter{Query: compiled, Within: []string{ws.project.Key}, Scoped: true}, issue.Page{Limit: 50})
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			var got []string
			for _, each := range res.Issues {
				got = append(got, each.Key)
			}
			sort.Strings(got)
			want := append([]string(nil), c.want...)
			sort.Strings(want)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

// The words a query looks in must be the expression the index holds, or every
// search reads every description.
func TestADescriptionSearchCanUseItsIndex(t *testing.T) {
	t.Parallel()
	h := newHarness(t)
	q, err := nql.Parse(`description ~ meta`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	compiled, err := q.Compile(nql.Env{Now: time.Now()})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	where, args := compiled.SQL(1)
	ctx := context.Background()
	tx, err := h.cluster.Primary().Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	// The table is small in a test, so the planner is told to prefer the index.
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatalf("set: %v", err)
	}
	rows, err := tx.Query(ctx, "EXPLAIN SELECT i.id FROM issue i WHERE "+where, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	var plan []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan: %v", err)
		}
		plan = append(plan, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if text := strings.Join(plan, "\n"); !strings.Contains(text, "issue_description_words_idx") {
		t.Errorf("the search does not use the description index:\n%s", text)
	}
}
