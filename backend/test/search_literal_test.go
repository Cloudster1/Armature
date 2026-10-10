//go:build integration

package test

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/armature/armature/backend/internal/field"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/nql"
)

// A search for "50%" or "snake_case" finds those characters, not whatever the
// database would read into them as a pattern.
func TestATextSearchFindsPercentAndUnderscoreAsTyped(t *testing.T) {
	h := newHarness(t)
	ws := h.newWorkspace(t, "literal")
	fields := field.NewService(h.cluster)
	code, _, err := fields.Create(ws.ctx, ws.project.Key, field.Input{Name: "Code", Kind: field.Text})
	if err != nil {
		t.Fatalf("code field: %v", err)
	}
	create := func(summary, codeValue string) string {
		created, _, err := ws.issues.Create(ws.ctx, issue.CreateInput{ProjectKey: ws.project.Key, TypeID: h.issueTypeID(t, ws, "Story"), Summary: summary}, ws.actor)
		if err != nil {
			t.Fatalf("create %q: %v", summary, err)
		}
		raw, _ := json.Marshal(codeValue)
		if _, _, err := fields.Set(ws.ctx, created.Key, code.ID, raw, ws.actor); err != nil {
			t.Fatalf("set code on %q: %v", summary, err)
		}
		return created.Key
	}
	percent := create("Half the rollout, 50% done", "a_b")
	fifty := create("Sold 500 units", "axb")
	snake := create(`Rename snake_case and C:\temp`, `x\y`)
	camel := create("Rename snakeXcase", "xy")

	cases := []struct {
		q    string
		want []string
	}{
		{`summary ~ "50%"`, []string{percent}},
		{`summary ~ "50"`, []string{fifty, percent}},
		{`summary ~ snake_case`, []string{snake}},
		{`text ~ "C:\\temp"`, []string{snake}},
		{`summary ~ "%"`, []string{percent}},
		{`summary !~ "_"`, []string{percent, fifty, camel}},
		{`"Code" ~ "a_b"`, []string{percent}},
		{`"Code" ~ "x\\y"`, []string{snake}},
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
			res, err := ws.issues.List(ws.ctx, issue.Filter{Query: compiled}, issue.Page{Limit: 50})
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
