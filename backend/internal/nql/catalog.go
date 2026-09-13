package nql

import (
	"sort"
	"strings"
)

// fieldKind decides how a field's values are read and what SQL they become.
type fieldKind int

const (
	fkProject fieldKind = iota
	fkKey
	fkSummary
	fkDescription
	fkAnyText
	fkName
	fkEnum
	fkPriority
	fkUser
	fkParent
	fkLookup
	fkLabels
	fkVersion
	fkComponent
	fkNumber
	fkTimestamp
	fkDate
	fkCustom
)

// fieldDef is one row of the catalog. Every piece of SQL a query can produce
// is spelled out here, which is what makes the compiler safe to point at
// text a stranger typed.
type fieldDef struct {
	name string
	kind fieldKind
	// column is the expression the kind compares: a column for direct kinds,
	// an id column for lookups and people.
	column string
	// table is the lookup table for fkLookup, matched by its name column.
	table string
	// enum and enumType are the values and the SQL type of an enum column.
	enum     []string
	enumType string
	// order is the ORDER BY expression; empty means the field cannot be sorted by.
	order string
	// role tells a version field apart: the one that fixes, or the ones affected.
	role string
}

var catalog = []fieldDef{
	{name: "project", kind: fkProject, order: "p.key"},
	{name: "key", kind: fkKey, order: "i.key_num"},
	{name: "summary", kind: fkSummary, column: "i.summary", order: "i.summary"},
	{name: "description", kind: fkDescription},
	{name: "text", kind: fkAnyText},
	{name: "type", kind: fkName, column: "it.name", order: "it.name"},
	{name: "status", kind: fkName, column: "s.name", order: "s.position"},
	{name: "statusCategory", kind: fkEnum, column: "s.category", enum: []string{"todo", "in_progress", "done"}, enumType: "status_category"},
	{name: "priority", kind: fkPriority, column: "i.priority", enum: []string{"lowest", "low", "medium", "high", "highest"}, enumType: "issue_priority", order: "i.priority"},
	{name: "assignee", kind: fkUser, column: "i.assignee_id"},
	{name: "reporter", kind: fkUser, column: "i.reporter_id"},
	{name: "parent", kind: fkParent, column: "i.parent_id"},
	{name: "sprint", kind: fkLookup, column: "i.sprint_id", table: "sprint"},
	{name: "team", kind: fkLookup, column: "i.team_id", table: "team"},
	{name: "milestone", kind: fkLookup, column: "i.milestone_id", table: "milestone"},
	{name: "labels", kind: fkLabels},
	{name: "fixVersion", kind: fkVersion, role: "fix"},
	{name: "affectsVersion", kind: fkVersion, role: "affects"},
	{name: "component", kind: fkComponent},
	{name: "estimate", kind: fkNumber, column: "i.estimate", order: "i.estimate"},
	{name: "created", kind: fkTimestamp, column: "i.created_at", order: "i.created_at"},
	{name: "updated", kind: fkTimestamp, column: "i.updated_at", order: "i.updated_at"},
	{name: "resolved", kind: fkTimestamp, column: "i.resolved_at", order: "i.resolved_at"},
	{name: "due", kind: fkDate, column: "i.due_date", order: "i.due_date"},
	{name: "start", kind: fkDate, column: "i.start_date", order: "i.start_date"},
}

// The operator sets, by kind. "in" and "empty" stand for IN and IS EMPTY.
var (
	opsEquality = []string{"=", "!=", "in"}
	opsOrdered  = []string{"=", "!=", "<", "<=", ">", ">=", "in"}
)

func operatorsFor(kind fieldKind) []string {
	switch kind {
	case fkProject, fkKey, fkName, fkEnum:
		return opsEquality
	case fkSummary:
		return []string{"=", "!=", "~", "!~", "in"}
	case fkDescription:
		return []string{"~", "!~", "empty"}
	case fkAnyText:
		return []string{"~", "!~"}
	case fkPriority:
		return opsOrdered
	case fkUser, fkParent, fkLookup, fkLabels, fkVersion, fkComponent:
		return append(append([]string{}, opsEquality...), "empty")
	case fkNumber, fkTimestamp, fkDate:
		return append(append([]string{}, opsOrdered...), "empty")
	case fkCustom:
		return []string{"=", "!=", "<", "<=", ">", ">=", "~", "!~", "in", "empty"}
	}
	return nil
}

func allows(kind fieldKind, op string) bool {
	for _, each := range operatorsFor(kind) {
		if each == op {
			return true
		}
	}
	return false
}

// describeOperators words an operator list for an error message.
func describeOperators(ops []string) string {
	words := make([]string, 0, len(ops))
	for _, op := range ops {
		switch op {
		case "in":
			words = append(words, "IN")
		case "empty":
			words = append(words, "IS EMPTY")
		default:
			words = append(words, op)
		}
	}
	if len(words) == 1 {
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " or " + words[len(words)-1]
}

// lookup finds a field by name, case-insensitively. A quoted name is always a
// custom field, so that a custom field called "status" is still reachable.
func lookup(f Field) (fieldDef, error) {
	if f.Custom {
		if strings.TrimSpace(f.Name) == "" {
			return fieldDef{}, errAt(f.Pos, "A custom field's name cannot be blank.")
		}
		return fieldDef{name: f.Name, kind: fkCustom}, nil
	}
	for _, def := range catalog {
		if strings.EqualFold(def.name, f.Name) {
			return def, nil
		}
	}
	if near := nearest(f.Name); near != "" {
		return fieldDef{}, errAt(f.Pos, "Unknown field %q. Did you mean %q?", f.Name, near)
	}
	return fieldDef{}, errAt(f.Pos, "Unknown field %q. The fields are %s; quote a custom field's name.", f.Name, strings.Join(Fields(), ", "))
}

// Fields lists the field names, for messages and documentation.
func Fields() []string {
	out := make([]string, len(catalog))
	for i, def := range catalog {
		out[i] = def.name
	}
	sort.Strings(out)
	return out
}

// maxSuggestionDistance is how far a misspelling may be from a field name and
// still get that name suggested.
const maxSuggestionDistance = 2

func nearest(name string) string {
	best, bestDistance := "", maxSuggestionDistance+1
	for _, def := range catalog {
		d := distance(strings.ToLower(name), strings.ToLower(def.name))
		if d < bestDistance {
			best, bestDistance = def.name, d
		}
	}
	return best
}

// distance is the Levenshtein distance between two short strings.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}
