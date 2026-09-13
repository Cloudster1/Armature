package nql

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestParseNormalizesWhatWasTyped(t *testing.T) {
	cases := []struct{ in, want string }{
		{`assignee = currentUser() and not status in ("To Do", Done) order by priority desc, key`,
			`(assignee = currentUser() AND NOT status IN ("To Do", Done)) ORDER BY priority DESC, key`},
		{`a = 1 OR b = 2 AND c = 3`, `(a = 1 OR (b = 2 AND c = 3))`},
		{`(a = 1 OR b = 2) AND c = 3`, `((a = 1 OR b = 2) AND c = 3)`},
		{`NOT NOT a = 1`, `NOT NOT a = 1`},
		{`labels IS NOT EMPTY`, `labels IS NOT EMPTY`},
		{`sprint = EMPTY`, `sprint IS EMPTY`},
		{`sprint != empty`, `sprint IS NOT EMPTY`},
		{`"Customer" ~ 'Glo\'bex'`, `"Customer" ~ "Glo'bex"`},
		{`created >= startOfWeek(-1w)`, `created >= startOfWeek(-1w)`},
		{`resolved >= -7d AND estimate <= 3.5`, `(resolved >= -7d AND estimate <= 3.5)`},
		{`due = 2026-09-04T10:30`, `due = 2026-09-04T10:30`},
		{`assignee = ada@armature.test`, `assignee = ada@armature.test`},
		{`key NOT IN (PR-1, PR-2)`, `key NOT IN (PR-1, PR-2)`},
		{`ORDER BY created`, `ORDER BY created`},
		{``, ``},
		{`   `, ``},
	}
	for _, c := range cases {
		q, err := Parse(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if got := q.String(); got != c.want {
			t.Errorf("%q\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestParseSaysWhereItWentWrong(t *testing.T) {
	cases := []struct {
		in   string
		pos  int
		want string
	}{
		{`status =`, 9, "ends where a value was expected"},
		{`status`, 7, "operator was expected after status"},
		{`status = "open`, 10, "quote opened at character 10 is never closed"},
		{`(status = x`, 1, "bracket opened at character 1 is never closed"},
		{`status = x y = 2`, 12, `"y" was not expected here`},
		{`status ! x`, 8, `"!" on its own means nothing`},
		{`status = 2026-13`, 10, "not a number, a date or a duration"},
		{`ORDER status`, 7, "ORDER is followed by BY"},
		{`status IN x`, 11, "IN is followed by a list"},
		{`status IN (a, b`, 16, "missing its closing bracket"},
		{`status IS x`, 11, "IS is followed by EMPTY"},
		{`= x`, 1, `A field name was expected, but "=" was found`},
		{`status = AND`, 10, `A value was expected before "AND"`},
		{`status # x`, 8, `"#" cannot be used here`},
		{`created > startOfWeek(x)`, 23, "takes at most a duration"},
		{`status NOT x`, 12, "NOT after a field is followed by IN"},
	}
	for _, c := range cases {
		_, err := Parse(c.in)
		var e *Error
		if !errors.As(err, &e) {
			t.Fatalf("%q: expected an Error, got %v", c.in, err)
		}
		if e.Pos != c.pos || !strings.Contains(e.Msg, c.want) {
			t.Errorf("%q: got %d %q, want %d containing %q", c.in, e.Pos, e.Msg, c.pos, c.want)
		}
	}
}

// Friday 4 September 2026, mid-morning, in UTC.
var testNow = time.Date(2026, 9, 4, 10, 0, 0, 0, time.UTC)

var me = uuid.MustParse("00000000-0000-0000-0000-00000000a11a")

func compile(t *testing.T, text string) *Compiled {
	t.Helper()
	q, err := Parse(text)
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	c, err := q.Compile(Env{UserID: me, Now: testNow})
	if err != nil {
		t.Fatalf("%q: %v", text, err)
	}
	return c
}

func TestCompileBindsEveryValue(t *testing.T) {
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }
	cases := []struct {
		in   string
		sql  string
		args []any
	}{
		{`assignee = currentUser()`, `(i.assignee_id = ANY($3::uuid[]))`, []any{[]uuid.UUID{me}}},
		{`reporter IN (currentUser(), "Ada L", ada@armature.test)`,
			`((i.reporter_id = ANY($3::uuid[])) OR (i.reporter_id IN (SELECT id FROM app_user WHERE lower(email::text) = ANY($4) OR lower(name) = ANY($4))))`,
			[]any{[]uuid.UUID{me}, []string{"ada l", "ada@armature.test"}}},
		{`statusCategory != done`, `NOT (s.category = ANY($3::status_category[]))`, []any{[]string{"done"}}},
		{`labels IN (a, B)`, `EXISTS (SELECT 1 FROM issue_label il JOIN label l ON l.id = il.label_id WHERE il.issue_id = i.id AND lower(l.name) = ANY($3))`, []any{[]string{"a", "b"}}},
		{`labels IS EMPTY`, `NOT EXISTS (SELECT 1 FROM issue_label il WHERE il.issue_id = i.id)`, nil},
		{`resolved >= -7d`, `(i.resolved_at >= $3)`, []any{testNow.Add(-7 * 24 * time.Hour)}},
		{`created = 2026-09-01`, `(i.created_at >= $3 AND i.created_at < $4)`, []any{day(2026, 9, 1), day(2026, 9, 2)}},
		{`created <= 2026-09-01`, `(i.created_at < $3)`, []any{day(2026, 9, 2)}},
		{`created > 2026-09-01T08:30`, `(i.created_at > $3)`, []any{time.Date(2026, 9, 1, 8, 30, 0, 0, time.UTC)}},
		{`due <= startOfWeek()`, `(i.due_date <= $3::date)`, []any{"2026-08-31"}},
		{`due < endOfMonth(-1w)`, `(i.due_date < $3::date)`, []any{"2026-08-31"}},
		{`start = now()`, `(i.start_date = $3::date)`, []any{"2026-09-04"}},
		{`key = pr-12`, `(p.key = $3 AND i.key_num = $4)`, []any{"PR", int64(12)}},
		{`parent IN (PR-1, PR-2)`,
			`((i.parent_id IN (SELECT c.id FROM issue c JOIN project cp ON cp.id = c.project_id WHERE cp.key = $3 AND c.key_num = $4)) OR (i.parent_id IN (SELECT c.id FROM issue c JOIN project cp ON cp.id = c.project_id WHERE cp.key = $5 AND c.key_num = $6)))`,
			[]any{"PR", int64(1), "PR", int64(2)}},
		{`project = evr`, `(p.key = ANY($3))`, []any{[]string{"EVR"}}},
		{`type = Bug AND status = "In Progress"`, `((lower(it.name) = ANY($3)) AND (lower(s.name) = ANY($4)))`, []any{[]string{"bug"}, []string{"in progress"}}},
		{`priority >= High`, `(i.priority >= $3::issue_priority)`, []any{"high"}},
		{`priority IN (high, highest)`, `(i.priority = ANY($3::issue_priority[]))`, []any{[]string{"high", "highest"}}},
		{`estimate > 3`, `(i.estimate > $3)`, []any{3.0}},
		{`estimate IN (1, 2.5)`, `(i.estimate = ANY($3::numeric[]))`, []any{[]float64{1, 2.5}}},
		{`text ~ login`, `((i.summary ILIKE '%' || $3 || '%') OR (jsonb_to_tsvector('simple', i.description, '["string"]') @@ plainto_tsquery('simple', $4)))`, []any{"login", "login"}},
		{`summary !~ "reset email"`, `NOT (i.summary ILIKE '%' || $3 || '%')`, []any{"reset email"}},
		{`summary = "Fix it"`, `(lower(i.summary) = ANY($3))`, []any{[]string{"fix it"}}},
		{`sprint in (openSprints(), "Sprint 2")`,
			`((i.sprint_id IN (SELECT id FROM sprint WHERE state = 'active')) OR (i.sprint_id IN (SELECT id FROM sprint WHERE lower(name) = ANY($3))))`,
			[]any{[]string{"sprint 2"}}},
		{`fixVersion IN (unreleasedVersions(), "2.0") AND component = Billing`,
			`((EXISTS (SELECT 1 FROM issue_version iv JOIN version ver ON ver.id = iv.version_id WHERE iv.issue_id = i.id AND iv.role = $3 AND ver.released_at IS NULL AND ver.archived_at IS NULL) OR EXISTS (SELECT 1 FROM issue_version iv JOIN version ver ON ver.id = iv.version_id WHERE iv.issue_id = i.id AND iv.role = $4 AND lower(ver.name) = ANY($5))) AND EXISTS (SELECT 1 FROM issue_component ic JOIN component co ON co.id = ic.component_id WHERE ic.issue_id = i.id AND lower(co.name) = ANY($6)))`,
			[]any{"fix", "fix", []string{"2.0"}, []string{"billing"}}},
		{`affectsVersion IS EMPTY`, `NOT EXISTS (SELECT 1 FROM issue_version iv WHERE iv.issue_id = i.id AND iv.role = $3)`, []any{"affects"}},
		{`team IS EMPTY AND milestone = M1`, `((i.team_id IS NULL) AND (i.milestone_id IN (SELECT id FROM milestone WHERE lower(name) = ANY($3))))`, []any{[]string{"m1"}}},
		{`"Customer" = Globex`,
			`EXISTS (SELECT 1 FROM issue_field_value v JOIN custom_field f ON f.id = v.field_id WHERE v.issue_id = i.id AND lower(f.name) = lower($4) AND lower(v.value #>> '{}') = lower($3))`,
			[]any{"Globex", "Customer"}},
		{`"Cost" > 100`,
			`EXISTS (SELECT 1 FROM issue_field_value v JOIN custom_field f ON f.id = v.field_id WHERE v.issue_id = i.id AND lower(f.name) = lower($4) AND f.kind = 'number' AND (v.value #>> '{}')::numeric > $3)`,
			[]any{100.0, "Cost"}},
		{`"Ship by" < startOfMonth()`,
			`EXISTS (SELECT 1 FROM issue_field_value v JOIN custom_field f ON f.id = v.field_id WHERE v.issue_id = i.id AND lower(f.name) = lower($4) AND f.kind = 'date' AND (v.value #>> '{}')::date < $3::date)`,
			[]any{"2026-09-01", "Ship by"}},
		{`"Customer" IS EMPTY`, `NOT EXISTS (SELECT 1 FROM issue_field_value v JOIN custom_field f ON f.id = v.field_id WHERE v.issue_id = i.id AND lower(f.name) = lower($3))`, []any{"Customer"}},
		{`description IS EMPTY`, `(i.description IS NULL OR jsonb_to_tsvector('simple', i.description, '["string"]') = ''::tsvector)`, nil},
		{`NOT (assignee IS EMPTY OR statusCategory = done)`, `NOT ((i.assignee_id IS NULL) OR (s.category = ANY($3::status_category[])))`, []any{[]string{"done"}}},
		{`ORDER BY priority DESC, key`, ``, nil},
	}
	for _, c := range cases {
		compiled := compile(t, c.in)
		sql, args := compiled.SQL(3)
		if sql != c.sql {
			t.Errorf("%q\n got %s\nwant %s", c.in, sql, c.sql)
		}
		if len(args) != len(c.args) {
			t.Errorf("%q: got %d args %v, want %d %v", c.in, len(args), args, len(c.args), c.args)
			continue
		}
		for i := range args {
			if got, want := stringify(args[i]), stringify(c.args[i]); got != want {
				t.Errorf("%q: arg %d got %s want %s", c.in, i, got, want)
			}
		}
	}
}

func stringify(v any) string {
	if at, ok := v.(time.Time); ok {
		return at.UTC().Format(time.RFC3339)
	}
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(fmtAny(v), "\n", " "), "  ", " "))
}

func fmtAny(v any) string { return strings.TrimSpace(strings.Join(strings.Fields(sprint(v)), " ")) }

func TestCompileOrdersByTheCatalogOnly(t *testing.T) {
	compiled := compile(t, `statusCategory = todo ORDER BY priority DESC, key, status`)
	want := []Order{{"i.priority", true}, {"i.key_num", false}, {"s.position", false}}
	if len(compiled.Order) != len(want) {
		t.Fatalf("got %v", compiled.Order)
	}
	for i := range want {
		if compiled.Order[i] != want[i] {
			t.Errorf("order %d: got %v want %v", i, compiled.Order[i], want[i])
		}
	}
}

func TestCompileRefusesWhatTheCatalogDoesNotKnow(t *testing.T) {
	cases := []struct {
		in   string
		pos  int
		want string
	}{
		{`assigne = x`, 1, `Unknown field "assigne". Did you mean "assignee"?`},
		{`bogus IN (1)`, 1, "The fields are affectsVersion, assignee, component, created"},
		{`priority ~ high`, 1, `"~" does not apply to priority. Use =, !=, <, <=, >, >= or IN.`},
		{`labels < a`, 1, `"<" does not apply to labels`},
		{`estimate = big`, 12, "estimate is a number, and big is not one"},
		{`created >= soon`, 12, "soon is not a date. Write 2026-09-04, -7d or startOfWeek()"},
		{`statusCategory = open`, 18, `"open" is not a statusCategory. Use todo, in_progress or done.`},
		{`priority > urgent`, 12, `"urgent" is not a priority`},
		{`ORDER BY labels`, 10, "labels cannot be sorted by"},
		{`ORDER BY "Customer"`, 10, "A custom field cannot be sorted by"},
		{`assignee = now()`, 12, "now() does not name a person"},
		{`sprint = currentUser()`, 10, "currentUser() does not name a sprint"},
		{`summary IS EMPTY`, 1, "summary is never empty"},
		{`due = tomorrow()`, 7, "There is no function tomorrow(). The date functions are now(), startOfDay()"},
		{`key = 12`, 7, `"12" is not an issue key. Write it like PROJ-12.`},
		{`status = now()`, 10, "now() cannot be compared with status"},
		{`summary ~ ""`, 11, "Give summary some text to look for"},
		{`"" = x`, 1, "A custom field's name cannot be blank"},
		{`text = login`, 1, `"=" does not apply to text. Use ~ or !~.`},
	}
	for _, c := range cases {
		q, err := Parse(c.in)
		if err != nil {
			t.Fatalf("%q: parse: %v", c.in, err)
		}
		_, err = q.Compile(Env{UserID: me, Now: testNow})
		var e *Error
		if !errors.As(err, &e) {
			t.Fatalf("%q: expected an Error, got %v", c.in, err)
		}
		if e.Pos != c.pos || !strings.Contains(e.Msg, c.want) {
			t.Errorf("%q: got %d %q, want %d containing %q", c.in, e.Pos, e.Msg, c.pos, c.want)
		}
	}
}

// Whatever a person types is bound, never spliced: the SQL text must not
// contain it, and the arguments must.
func TestNothingTypedReachesTheSQL(t *testing.T) {
	hostile := []string{
		`summary ~ "'; DROP TABLE issue; --"`,
		`"Cust'omer" = "x) OR 1=1"`,
		`labels IN ("$1", "?0")`,
		`assignee = "robert'); --"`,
		`status = 'pg_sleep(10)'`,
	}
	for _, in := range hostile {
		compiled := compile(t, in)
		sql, args := compiled.SQL(1)
		for _, arg := range args {
			text := sprint(arg)
			for _, piece := range []string{"DROP", "OR 1=1", "pg_sleep", "--"} {
				if strings.Contains(text, piece) && strings.Contains(sql, piece) {
					t.Errorf("%q: %q reached the SQL: %s", in, piece, sql)
				}
			}
		}
		if strings.Contains(sql, "?") {
			t.Errorf("%q: a placeholder was left unnumbered: %s", in, sql)
		}
		if len(args) == 0 {
			t.Errorf("%q: nothing was bound", in)
		}
	}
}

func TestSQLNumbersFromWhereItIsPlaced(t *testing.T) {
	compiled := compile(t, `assignee = currentUser() AND labels = a`)
	first, _ := compiled.SQL(1)
	seventh, _ := compiled.SQL(7)
	if !strings.Contains(first, "$1") || !strings.Contains(first, "$2") {
		t.Errorf("from 1: %s", first)
	}
	if !strings.Contains(seventh, "$7") || !strings.Contains(seventh, "$8") || strings.Contains(seventh, "$1") {
		t.Errorf("from 7: %s", seventh)
	}
	if sql, args := (*Compiled)(nil).SQL(1); sql != "" || args != nil {
		t.Errorf("a nil query is no clause")
	}
}

func TestDistance(t *testing.T) {
	if d := distance("assigne", "assignee"); d != 1 {
		t.Errorf("got %d", d)
	}
	if nearest("zzzzzz") != "" {
		t.Errorf("nothing is near zzzzzz")
	}
	if nearest("Statuss") != "status" {
		t.Errorf("got %q", nearest("Statuss"))
	}
}
