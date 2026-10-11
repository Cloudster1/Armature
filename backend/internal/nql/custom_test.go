package nql

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

// valueCast finds every cast of a stored value, with what comes before it, so
// the test can see whether a CASE on the field's kind guards it.
var valueCast = regexp.MustCompile(`(CASE WHEN f\.kind = '(\w+)' THEN )?\(v\.value #>> '\{\}'\)::(\w+)`)

// A cast left as a plain AND can be evaluated on another field's value first,
// and ~ has no numeric or date form: either one is a 500 for a typed query.
func TestCustomFieldCastsOnlyItsOwnKind(t *testing.T) {
	literals := []string{`4711`, `2.5`, `2026-09-04`, `-7d`, `startOfWeek()`, `high`, `"in review"`}
	var queries []string
	for _, op := range []string{"=", "!=", "<", "<=", ">", ">=", "~", "!~"} {
		for _, lit := range literals {
			queries = append(queries, `"Score" `+op+` `+lit)
		}
	}
	queries = append(queries,
		`"Score" IN (4711, 2026-09-04, startOfWeek(), high)`,
		`"Score" NOT IN (4711, 2026-09-04, startOfWeek(), high)`,
		`"Score" IS EMPTY`, `"Score" IS NOT EMPTY`,
	)
	for _, text := range queries {
		q, err := Parse(text)
		if err != nil {
			t.Fatalf("%q: parse: %v", text, err)
		}
		compiled, err := q.Compile(Env{UserID: me, Now: testNow})
		contains := strings.Contains(text, " ~ ") || strings.Contains(text, " !~ ")
		if err != nil {
			// Only a function after ~ is refused: it names a moment, not text.
			var e *Error
			if !errors.As(err, &e) || !contains || !strings.HasSuffix(text, "()") {
				t.Errorf("%q: got %v, want it to compile", text, err)
			}
			continue
		}
		sql, _ := compiled.SQL(1)
		for _, m := range valueCast.FindAllStringSubmatch(sql, -1) {
			if contains {
				t.Errorf("%q: ~ casts the value to %s instead of comparing text: %s", text, m[3], sql)
				continue
			}
			if m[1] == "" || m[2] != map[string]string{"numeric": "number", "date": "date"}[m[3]] {
				t.Errorf("%q: the ::%s cast is not guarded by the field's kind: %s", text, m[3], sql)
			}
		}
	}
}

func TestContainsOnACustomFieldComparesTheTextTyped(t *testing.T) {
	for _, c := range []struct{ in, text string }{
		{`"Order ID" ~ 4711`, "4711"},
		{`"Order ID" ~ 47.10`, "47.10"},
		{`"Ship by" ~ 2026-09-04`, "2026-09-04"},
		{`"Ship by" !~ -7d`, "-7d"},
	} {
		sql, args := compile(t, c.in).SQL(1)
		if !strings.Contains(sql, "v.value #>> '{}' ILIKE '%' || $1 || '%'") {
			t.Errorf("%q: not a text search: %s", c.in, sql)
		}
		if len(args) == 0 || args[0] != c.text {
			t.Errorf("%q: looks for %v, want %q", c.in, args, c.text)
		}
	}
}
