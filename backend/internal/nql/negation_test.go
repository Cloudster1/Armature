package nql

import (
	"fmt"
	"testing"
)

// A missing value matches no positive test, so every negation must count it on
// the negated side. NOT over a SQL NULL is NULL, which would drop the issue.
func TestNegationCountsAMissingValueAsNotMatching(t *testing.T) {
	descriptionTest := "(" + descriptionWords + " @@ " + startsOfWords("$%d") + ")"
	cases := []struct{ in, sql string }{
		{`text !~ payment`,
			`NOT COALESCE(((i.summary ILIKE '%' || $3 || '%') OR ` + fmt.Sprintf(descriptionTest, 4) + `), false)`},
		{`description !~ payment`, `NOT COALESCE(` + fmt.Sprintf(descriptionTest, 3) + `, false)`},
		{`NOT text ~ payment`,
			`NOT COALESCE(((i.summary ILIKE '%' || $3 || '%') OR ` + fmt.Sprintf(descriptionTest, 4) + `), false)`},
		{`assignee != currentUser()`, `NOT COALESCE((i.assignee_id = ANY($3::uuid[])), false)`},
		{`reporter NOT IN (currentUser())`, `NOT COALESCE((i.reporter_id = ANY($3::uuid[])), false)`},
		{`sprint NOT IN ("Sprint 2")`, `NOT COALESCE((i.sprint_id IN (SELECT id FROM sprint WHERE lower(name) = ANY($3))), false)`},
		{`team != Alpha`, `NOT COALESCE((i.team_id IN (SELECT id FROM team WHERE lower(name) = ANY($3))), false)`},
		{`parent != PR-1`,
			`NOT COALESCE((i.parent_id IN (SELECT c.id FROM issue c JOIN project cp ON cp.id = c.project_id WHERE cp.key = $3 AND c.key_num = $4)), false)`},
		{`estimate != 5`, `NOT COALESCE((i.estimate = ANY($3::numeric[])), false)`},
		{`NOT estimate > 3`, `NOT COALESCE((i.estimate > $3), false)`},
		{`due != 2026-09-04`, `NOT COALESCE((i.due_date = $3::date), false)`},
		{`resolved != 2026-09-01`, `NOT COALESCE((i.resolved_at >= $3 AND i.resolved_at < $4), false)`},
		// IS EMPTY is never NULL itself, so its negation needs no help.
		{`description IS NOT EMPTY`, "NOT (i.description IS NULL OR " + descriptionWords + " = ''::tsvector)"},
		{`assignee IS NOT EMPTY`, `NOT (i.assignee_id IS NULL)`},
	}
	for _, c := range cases {
		sql, _ := compile(t, c.in).SQL(3)
		if sql != c.sql {
			t.Errorf("%q\n got %s\nwant %s", c.in, sql, c.sql)
		}
	}
}
