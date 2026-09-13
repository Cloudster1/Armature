package nql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Env is what a query means by "me" and "now".
type Env struct {
	UserID uuid.UUID
	Now    time.Time
	// Location decides where a day starts; nil is UTC.
	Location *time.Location
}

// Order is one ORDER BY key, its SQL taken from the catalog and nothing else.
type Order struct {
	SQL  string
	Desc bool
}

// Compiled is a query ready to be appended to a WHERE clause. The parameters
// are numbered when it is placed, so it can follow other clauses.
type Compiled struct {
	where string
	args  []any
	Order []Order
}

// placeholder marks a bound argument until SQL numbers it.
var placeholder = regexp.MustCompile(`\?(\d+)`)

// SQL returns the clause with its parameters numbered from first, and the
// arguments in that order. The clause is empty for a query that only sorts.
func (c *Compiled) SQL(first int) (string, []any) {
	if c == nil || c.where == "" {
		return "", nil
	}
	where := placeholder.ReplaceAllStringFunc(c.where, func(m string) string {
		n, _ := strconv.Atoi(m[1:])
		return "$" + strconv.Itoa(first+n)
	})
	return where, append([]any(nil), c.args...)
}

type compiler struct {
	env  Env
	args []any
}

// bind stores a value and returns its placeholder. Nothing typed ever reaches
// the SQL text by any other road.
func (c *compiler) bind(value any) string {
	c.args = append(c.args, value)
	return "?" + strconv.Itoa(len(c.args)-1)
}

// Compile checks every field, operator and value against the catalog and
// produces the SQL. A query that compiles cannot fail in the database for
// anything the person typed.
func (q *Query) Compile(env Env) (*Compiled, error) {
	if env.Location == nil {
		env.Location = time.UTC
	}
	if env.Now.IsZero() {
		env.Now = time.Now()
	}
	c := &compiler{env: env}
	out := &Compiled{}
	if q.Where != nil {
		where, err := c.expr(q.Where)
		if err != nil {
			return nil, err
		}
		out.where = where
	}
	for _, o := range q.Order {
		def, err := lookup(o.Field)
		if err != nil {
			return nil, err
		}
		if def.kind == fkCustom {
			return nil, errAt(o.Field.Pos, "A custom field cannot be sorted by.")
		}
		if def.order == "" {
			return nil, errAt(o.Field.Pos, "%s cannot be sorted by.", def.name)
		}
		out.Order = append(out.Order, Order{SQL: def.order, Desc: o.Desc})
	}
	out.args = c.args
	return out, nil
}

func (c *compiler) expr(e Expr) (string, error) {
	switch e := e.(type) {
	case And:
		l, err := c.expr(e.Left)
		if err != nil {
			return "", err
		}
		r, err := c.expr(e.Right)
		if err != nil {
			return "", err
		}
		return "(" + l + " AND " + r + ")", nil
	case Or:
		l, err := c.expr(e.Left)
		if err != nil {
			return "", err
		}
		r, err := c.expr(e.Right)
		if err != nil {
			return "", err
		}
		return "(" + l + " OR " + r + ")", nil
	case Not:
		inner, err := c.expr(e.Expr)
		if err != nil {
			return "", err
		}
		return "NOT " + inner, nil
	case Compare:
		return c.compare(e)
	case In:
		return c.in(e)
	case Empty:
		return c.empty(e)
	}
	return "", fmt.Errorf("nql: unknown expression %T", e)
}

func (c *compiler) compare(e Compare) (string, error) {
	def, err := lookup(e.Field)
	if err != nil {
		return "", err
	}
	if !allows(def.kind, e.Op) {
		return "", errAt(e.Field.Pos, "%q does not apply to %s. Use %s.", e.Op, def.name, describeOperators(operatorsFor(def.kind)))
	}
	switch e.Op {
	case "=":
		return c.match(def, e.Field, []Value{e.Value})
	case "!=":
		positive, err := c.match(def, e.Field, []Value{e.Value})
		if err != nil {
			return "", err
		}
		return "NOT " + positive, nil
	case "~", "!~":
		clause, err := c.contains(def, e.Field, e.Value)
		if err != nil {
			return "", err
		}
		if e.Op == "!~" {
			return "NOT " + clause, nil
		}
		return clause, nil
	default:
		return c.ordered(def, e.Field, e.Op, e.Value)
	}
}

func (c *compiler) in(e In) (string, error) {
	def, err := lookup(e.Field)
	if err != nil {
		return "", err
	}
	if !allows(def.kind, "in") {
		return "", errAt(e.Field.Pos, "IN does not apply to %s. Use %s.", def.name, describeOperators(operatorsFor(def.kind)))
	}
	positive, err := c.match(def, e.Field, e.Values)
	if err != nil {
		return "", err
	}
	if e.Not {
		return "NOT " + positive, nil
	}
	return positive, nil
}

func (c *compiler) empty(e Empty) (string, error) {
	def, err := lookup(e.Field)
	if err != nil {
		return "", err
	}
	if !allows(def.kind, "empty") {
		return "", errAt(e.Field.Pos, "%s is never empty, so IS EMPTY does not apply to it.", def.name)
	}
	var clause string
	switch def.kind {
	case fkLabels:
		clause = "NOT EXISTS (SELECT 1 FROM issue_label il WHERE il.issue_id = i.id)"
	case fkVersion:
		clause = "NOT EXISTS (SELECT 1 FROM issue_version iv WHERE iv.issue_id = i.id AND iv.role = " + c.bind(def.role) + ")"
	case fkComponent:
		clause = "NOT EXISTS (SELECT 1 FROM issue_component ic WHERE ic.issue_id = i.id)"
	case fkDescription:
		clause = "(i.description IS NULL OR jsonb_to_tsvector('simple', i.description, '[\"string\"]') = ''::tsvector)"
	case fkCustom:
		clause = "NOT EXISTS (SELECT 1 FROM issue_field_value v JOIN custom_field f ON f.id = v.field_id WHERE v.issue_id = i.id AND lower(f.name) = lower(" + c.bind(def.name) + "))"
	default:
		clause = "(" + def.column + " IS NULL)"
	}
	if e.Not {
		return "NOT " + clause, nil
	}
	return clause, nil
}

// match is the positive form of = and IN: the issue's value is one of these.
func (c *compiler) match(def fieldDef, f Field, values []Value) (string, error) {
	switch def.kind {
	case fkProject:
		keys, err := c.texts(def, values, strings.ToUpper)
		if err != nil {
			return "", err
		}
		return "(p.key = ANY(" + c.bind(keys) + "))", nil
	case fkKey:
		return c.keys(values, "(p.key = %s AND i.key_num = %s)")
	case fkParent:
		return c.keys(values, "(i.parent_id IN (SELECT c.id FROM issue c JOIN project cp ON cp.id = c.project_id WHERE cp.key = %s AND c.key_num = %s))")
	case fkSummary, fkName:
		names, err := c.texts(def, values, strings.ToLower)
		if err != nil {
			return "", err
		}
		return "(lower(" + def.column + ") = ANY(" + c.bind(names) + "))", nil
	case fkEnum, fkPriority:
		members, err := c.texts(def, values, strings.ToLower)
		if err != nil {
			return "", err
		}
		for i, m := range members {
			if !contains(def.enum, m) {
				return "", errAt(values[i].Pos, "%q is not a %s. Use %s.", values[i].Text, def.name, describeOperators(def.enum))
			}
		}
		return "(" + def.column + " = ANY(" + c.bind(members) + "::" + def.enumType + "[]))", nil
	case fkUser:
		return c.users(def, values)
	case fkLookup:
		var parts []string
		var names []string
		for _, v := range values {
			if v.Kind == VFunction {
				if def.table != "sprint" || !strings.EqualFold(v.Text, "openSprints") {
					return "", errAt(v.Pos, "%s() does not name a %s.", v.Text, def.name)
				}
				parts = append(parts, "("+def.column+" IN (SELECT id FROM sprint WHERE state = 'active'))")
				continue
			}
			text, err := plainText(def, v)
			if err != nil {
				return "", err
			}
			names = append(names, strings.ToLower(text))
		}
		if len(names) > 0 {
			parts = append(parts, "("+def.column+" IN (SELECT id FROM "+def.table+" WHERE lower(name) = ANY("+c.bind(names)+")))")
		}
		return anyOf(parts), nil
	case fkLabels:
		names, err := c.texts(def, values, strings.ToLower)
		if err != nil {
			return "", err
		}
		return "EXISTS (SELECT 1 FROM issue_label il JOIN label l ON l.id = il.label_id WHERE il.issue_id = i.id AND lower(l.name) = ANY(" + c.bind(names) + "))", nil
	case fkVersion:
		// releasedVersions() and unreleasedVersions() name the versions by
		// state; a word names one version.
		var parts []string
		var names []string
		for _, v := range values {
			if v.Kind == VFunction {
				var state string
				switch {
				case strings.EqualFold(v.Text, "releasedVersions"):
					state = "ver.released_at IS NOT NULL"
				case strings.EqualFold(v.Text, "unreleasedVersions"):
					state = "ver.released_at IS NULL AND ver.archived_at IS NULL"
				default:
					return "", errAt(v.Pos, "%s() does not name a version. Use releasedVersions() or unreleasedVersions().", v.Text)
				}
				parts = append(parts, "EXISTS (SELECT 1 FROM issue_version iv JOIN version ver ON ver.id = iv.version_id WHERE iv.issue_id = i.id AND iv.role = "+c.bind(def.role)+" AND "+state+")")
				continue
			}
			text, err := plainText(def, v)
			if err != nil {
				return "", err
			}
			names = append(names, strings.ToLower(text))
		}
		if len(names) > 0 {
			parts = append(parts, "EXISTS (SELECT 1 FROM issue_version iv JOIN version ver ON ver.id = iv.version_id WHERE iv.issue_id = i.id AND iv.role = "+c.bind(def.role)+" AND lower(ver.name) = ANY("+c.bind(names)+"))")
		}
		return anyOf(parts), nil
	case fkComponent:
		names, err := c.texts(def, values, strings.ToLower)
		if err != nil {
			return "", err
		}
		return "EXISTS (SELECT 1 FROM issue_component ic JOIN component co ON co.id = ic.component_id WHERE ic.issue_id = i.id AND lower(co.name) = ANY(" + c.bind(names) + "))", nil
	case fkNumber:
		var numbers []float64
		for _, v := range values {
			n, err := number(def, v)
			if err != nil {
				return "", err
			}
			numbers = append(numbers, n)
		}
		return "(" + def.column + " = ANY(" + c.bind(numbers) + "::numeric[]))", nil
	case fkTimestamp, fkDate:
		var parts []string
		for _, v := range values {
			clause, err := c.ordered(def, f, "=", v)
			if err != nil {
				return "", err
			}
			parts = append(parts, clause)
		}
		return anyOf(parts), nil
	case fkCustom:
		var parts []string
		for _, v := range values {
			clause, err := c.custom(def, "=", v)
			if err != nil {
				return "", err
			}
			parts = append(parts, clause)
		}
		return anyOf(parts), nil
	default:
		return "", errAt(f.Pos, "%s cannot be compared with =.", def.name)
	}
}

// contains is ~: the words appear somewhere in the text.
func (c *compiler) contains(def fieldDef, f Field, v Value) (string, error) {
	text, err := plainText(def, v)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", errAt(v.Pos, "Give %s some text to look for.", def.name)
	}
	summary := func() string { return "(i.summary ILIKE '%' || " + c.bind(text) + " || '%')" }
	description := func() string {
		return "(jsonb_to_tsvector('simple', i.description, '[\"string\"]') @@ plainto_tsquery('simple', " + c.bind(text) + "))"
	}
	switch def.kind {
	case fkSummary:
		return summary(), nil
	case fkDescription:
		return description(), nil
	case fkAnyText:
		return "(" + summary() + " OR " + description() + ")", nil
	case fkCustom:
		return c.custom(def, "~", v)
	}
	return "", errAt(f.Pos, "%q does not apply to %s.", "~", def.name)
}

// ordered is < <= > >= and, for dates, = on a whole day.
func (c *compiler) ordered(def fieldDef, f Field, op string, v Value) (string, error) {
	switch def.kind {
	case fkPriority:
		member, err := plainText(def, v)
		if err != nil {
			return "", err
		}
		member = strings.ToLower(member)
		if !contains(def.enum, member) {
			return "", errAt(v.Pos, "%q is not a priority. Use %s.", v.Text, describeOperators(def.enum))
		}
		return "(" + def.column + " " + op + " " + c.bind(member) + "::" + def.enumType + ")", nil
	case fkNumber:
		n, err := number(def, v)
		if err != nil {
			return "", err
		}
		return "(" + def.column + " " + op + " " + c.bind(n) + ")", nil
	case fkTimestamp:
		at, wholeDay, err := c.instant(def, v)
		if err != nil {
			return "", err
		}
		if !wholeDay {
			return "(" + def.column + " " + op + " " + c.bind(at) + ")", nil
		}
		// A day named without a time is the whole day: from its midnight up to
		// the next, so "= today" and "<= today" both mean what they say.
		next := at.AddDate(0, 0, 1)
		switch op {
		case ">=":
			return "(" + def.column + " >= " + c.bind(at) + ")", nil
		case ">":
			return "(" + def.column + " >= " + c.bind(next) + ")", nil
		case "<":
			return "(" + def.column + " < " + c.bind(at) + ")", nil
		case "<=":
			return "(" + def.column + " < " + c.bind(next) + ")", nil
		default:
			return "(" + def.column + " >= " + c.bind(at) + " AND " + def.column + " < " + c.bind(next) + ")", nil
		}
	case fkDate:
		at, _, err := c.instant(def, v)
		if err != nil {
			return "", err
		}
		return "(" + def.column + " " + op + " " + c.bind(at.In(c.env.Location).Format("2006-01-02")) + "::date)", nil
	case fkCustom:
		return c.custom(def, op, v)
	}
	return "", errAt(f.Pos, "%q does not apply to %s. Use %s.", op, def.name, describeOperators(operatorsFor(def.kind)))
}

// custom compares a custom field by name. The field is found by name in any
// project on purpose: "Customer = Globex" across projects is what a person
// means, even though each project defines its own field. The cast follows
// the literal and is guarded by the field's kind, so a text field of the same
// name in another project cannot make a numeric cast fail.
func (c *compiler) custom(def fieldDef, op string, v Value) (string, error) {
	var test string
	switch {
	case v.Kind == VNumber:
		n, _ := strconv.ParseFloat(v.Text, 64)
		test = "f.kind = 'number' AND (v.value #>> '{}')::numeric " + op + " " + c.bind(n)
	case v.Kind == VDate || v.Kind == VDuration || v.Kind == VFunction:
		at, _, err := c.instant(def, v)
		if err != nil {
			return "", err
		}
		test = "f.kind = 'date' AND (v.value #>> '{}')::date " + op + " " + c.bind(at.In(c.env.Location).Format("2006-01-02")) + "::date"
	case op == "~":
		test = "v.value #>> '{}' ILIKE '%' || " + c.bind(v.Text) + " || '%'"
	case op == "=":
		test = "lower(v.value #>> '{}') = lower(" + c.bind(v.Text) + ")"
	default:
		test = "v.value #>> '{}' " + op + " " + c.bind(v.Text)
	}
	return "EXISTS (SELECT 1 FROM issue_field_value v JOIN custom_field f ON f.id = v.field_id WHERE v.issue_id = i.id AND lower(f.name) = lower(" + c.bind(def.name) + ") AND " + test + ")", nil
}

func (c *compiler) users(def fieldDef, values []Value) (string, error) {
	var ids []uuid.UUID
	var names []string
	for _, v := range values {
		if v.Kind == VFunction {
			if !strings.EqualFold(v.Text, "currentUser") {
				return "", errAt(v.Pos, "%s() does not name a person. Use currentUser(), an email address or a name.", v.Text)
			}
			ids = append(ids, c.env.UserID)
			continue
		}
		text, err := plainText(def, v)
		if err != nil {
			return "", err
		}
		names = append(names, strings.ToLower(text))
	}
	var parts []string
	if len(ids) > 0 {
		parts = append(parts, "("+def.column+" = ANY("+c.bind(ids)+"::uuid[]))")
	}
	if len(names) > 0 {
		list := c.bind(names)
		parts = append(parts, "("+def.column+" IN (SELECT id FROM app_user WHERE lower(email::text) = ANY("+list+") OR lower(name) = ANY("+list+")))")
	}
	return anyOf(parts), nil
}

// keyPattern is the issue package's, repeated here because the issue package
// is what imports this one.
var keyPattern = regexp.MustCompile(`^([A-Z][A-Z0-9]{1,9})-([1-9][0-9]{0,17})$`)

// keys binds issue keys into a template with two slots, project key and number.
func (c *compiler) keys(values []Value, template string) (string, error) {
	var parts []string
	for _, v := range values {
		if v.Kind != VWord && v.Kind != VString {
			return "", errAt(v.Pos, "%q is not an issue key. Write it like PROJ-12.", v.String())
		}
		m := keyPattern.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(v.Text)))
		if m == nil {
			return "", errAt(v.Pos, "%q is not an issue key. Write it like PROJ-12.", v.Text)
		}
		num, _ := strconv.ParseInt(m[2], 10, 64)
		parts = append(parts, fmt.Sprintf(template, c.bind(m[1]), c.bind(num)))
	}
	return anyOf(parts), nil
}

// texts reads plain values, normalized, refusing functions and numbers.
func (c *compiler) texts(def fieldDef, values []Value, normalize func(string) string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, v := range values {
		text, err := plainText(def, v)
		if err != nil {
			return nil, err
		}
		out = append(out, normalize(text))
	}
	return out, nil
}

func plainText(def fieldDef, v Value) (string, error) {
	switch v.Kind {
	case VWord, VString:
		return v.Text, nil
	case VNumber:
		return v.Text, nil
	case VFunction:
		return "", errAt(v.Pos, "%s() cannot be compared with %s. Write the value itself.", v.Text, def.name)
	default:
		return "", errAt(v.Pos, "%q is not a value %s takes.", v.Text, def.name)
	}
}

func number(def fieldDef, v Value) (float64, error) {
	if v.Kind != VNumber {
		return 0, errAt(v.Pos, "%s is a number, and %s is not one.", def.name, v)
	}
	n, err := strconv.ParseFloat(v.Text, 64)
	if err != nil {
		return 0, errAt(v.Pos, "%q is not a number.", v.Text)
	}
	return n, nil
}

// dateFunctions are the moments a query can name relative to now.
var dateFunctions = []string{"now", "startOfDay", "endOfDay", "startOfWeek", "endOfWeek", "startOfMonth", "endOfMonth"}

// instant resolves a date value. wholeDay says the value named a day with no
// time, which the timestamp comparison turns into a range.
func (c *compiler) instant(def fieldDef, v Value) (time.Time, bool, error) {
	loc := c.env.Location
	switch v.Kind {
	case VDate:
		for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
			if at, err := time.ParseInLocation(layout, v.Text, loc); err == nil {
				return at, false, nil
			}
		}
		at, err := time.ParseInLocation("2006-01-02", v.Text, loc)
		if err != nil {
			return time.Time{}, false, errAt(v.Pos, "%q is not a calendar date. Write it like 2026-09-04.", v.Text)
		}
		return at, true, nil
	case VDuration:
		d, err := duration(v)
		if err != nil {
			return time.Time{}, false, err
		}
		return c.env.Now.In(loc).Add(d), false, nil
	case VFunction:
		ref := c.env.Now.In(loc)
		if v.Arg != nil {
			d, err := duration(*v.Arg)
			if err != nil {
				return time.Time{}, false, err
			}
			ref = ref.Add(d)
		}
		y, m, d := ref.Date()
		day := time.Date(y, m, d, 0, 0, 0, 0, loc)
		// Weeks start on Monday, as the plan's weeks do.
		weekday := (int(day.Weekday()) + 6) % 7
		week := day.AddDate(0, 0, -weekday)
		month := time.Date(y, m, 1, 0, 0, 0, 0, loc)
		switch strings.ToLower(v.Text) {
		case "now":
			return ref, false, nil
		case "startofday":
			return day, false, nil
		case "endofday":
			return day.AddDate(0, 0, 1).Add(-time.Second), false, nil
		case "startofweek":
			return week, false, nil
		case "endofweek":
			return week.AddDate(0, 0, 7).Add(-time.Second), false, nil
		case "startofmonth":
			return month, false, nil
		case "endofmonth":
			return month.AddDate(0, 1, 0).Add(-time.Second), false, nil
		}
		return time.Time{}, false, errAt(v.Pos, "There is no function %s(). The date functions are %s.", v.Text, describeOperators(withParens(dateFunctions)))
	}
	return time.Time{}, false, errAt(v.Pos, "%s is not a date. Write 2026-09-04, -7d or startOfWeek().", v)
}

// duration reads -7d, 2w or 36h. A day is 24 hours here; a query does not
// care about the hour a clock changed.
func duration(v Value) (time.Duration, error) {
	text := v.Text
	unit := text[len(text)-1]
	n, err := strconv.Atoi(text[:len(text)-1])
	if err != nil {
		return 0, errAt(v.Pos, "%q is not a duration. Write it like -7d, 2w or 36h.", text)
	}
	switch unit {
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	default:
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	}
}

func withParens(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = n + "()"
	}
	return out
}

func anyOf(parts []string) string {
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

func contains(list []string, s string) bool {
	for _, each := range list {
		if each == s {
			return true
		}
	}
	return false
}
